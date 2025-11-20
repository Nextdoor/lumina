// Copyright 2025 Lumina Contributors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/nextdoor/lumina/internal/cache"
	"github.com/nextdoor/lumina/pkg/aws"
	"github.com/nextdoor/lumina/pkg/config"
	"github.com/nextdoor/lumina/pkg/metrics"
)

// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create

// SPRatesReconciler implements lazy-loading reconciliation for Savings Plan rates.
//
// This reconciler queries the actual purchase-time rates for each active Savings Plan
// using the DescribeSavingsPlanRates API. These rates are cached per-SP ARN + instance type + region + tenancy.
//
// Algorithm (runs every 1-2 minutes):
//  1. Get all active Savings Plans from RISP cache
//  2. For each SP: Check if we've already cached rates for this SP
//  3. If not cached: Query DescribeSavingsPlanRates(spId) for this SP's actual rates
//  4. Convert rates to cache format: "spArn:instanceType:region:tenancy" -> rate
//  5. Add new rates to PricingCache
//
// This provides natural lazy-loading - rates are only fetched once per SP, then cached.
//
// API call efficiency:
//   - Steady state: 0 API calls (all SP rates cached)
//   - New Savings Plan purchased: 1 API call per new SP
//   - First run: N API calls (one per active SP)
//
// Note: Each DescribeSavingsPlanRates call returns 100-1000+ rates (one per instance type/region/tenancy
// combination that the SP covers), so the total number of API calls is much lower than the
// number of rates being cached.
type SPRatesReconciler struct {
	// AWS client for making API calls
	AWSClient aws.Client

	// Configuration with AWS account details
	Config *config.Config

	// Cache for EC2 instances - used to determine which instance types to fetch rates for
	EC2Cache *cache.EC2Cache

	// Cache for Reserved Instances and Savings Plans
	RISPCache *cache.RISPCache

	// Cache for storing SP rates
	PricingCache *cache.PricingCache

	// Metrics for observability
	Metrics *metrics.Metrics

	// Logger
	Log logr.Logger

	// OperatingSystems is the list of operating systems to fetch SP rates for.
	// This should match the configured operating systems in PricingReconciler.
	// Normalized to lowercase for cache keys (e.g., ["linux", "windows"]).
	OperatingSystems []string

	// RISPReadyChan is an optional channel that this reconciler will wait on
	// before running its initial reconciliation. This ensures the RISP cache is
	// populated with Savings Plans before we try to fetch rates.
	RISPReadyChan chan struct{}

	// EC2ReadyChan is an optional channel that this reconciler will wait on
	// before running its initial reconciliation. This ensures the EC2 cache is
	// populated with instance types before we try to fetch rates.
	EC2ReadyChan chan struct{}

	// ReadyChan is an optional channel that will be closed after the initial
	// reconciliation completes successfully. This allows downstream reconcilers
	// (like CostReconciler) to wait for SP rates to be populated before they
	// start their cost calculations.
	ReadyChan chan struct{}
}

// Reconcile performs a single reconciliation cycle.
// Implements lazy-loading: only fetches rates for Savings Plans not yet in cache.
func (r *SPRatesReconciler) Reconcile(ctx context.Context, _ ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("reconciler", "sp_rates")
	log.V(1).Info("starting SP rates reconciliation cycle")

	// Track cycle timing
	startTime := time.Now()

	// Step 1: Get all active Savings Plans from RISP cache
	savingsPlans := r.RISPCache.GetAllSavingsPlans()
	if len(savingsPlans) == 0 {
		log.V(1).Info("no active Savings Plans found, skipping SP rates reconciliation")
		return ctrl.Result{}, nil
	}

	log.V(1).Info("discovered active Savings Plans",
		"sp_count", len(savingsPlans))

	// Step 2: Find Savings Plans that don't have rates cached yet
	spsNeedingRates := r.findMissingSPRates(savingsPlans)
	if len(spsNeedingRates) == 0 {
		log.V(1).Info("all Savings Plans have cached rates, skipping API calls")
		return ctrl.Result{}, nil
	}

	log.Info("discovered Savings Plans needing rate fetches",
		"sp_count", len(spsNeedingRates))

	// Step 3: Fetch rates for each SP from AWS using the specific filters
	totalNewRates := 0
	totalSentinelValues := 0
	for _, spWithRates := range spsNeedingRates {
		sp := spWithRates.SavingsPlan

		// Log whether this is a full fetch or incremental fetch
		fetchType := "incremental"
		if spWithRates.IsFullFetch {
			fetchType = "full"
		}

		rates, err := r.fetchRatesForSPWithFilters(
			ctx,
			sp,
			spWithRates.InstanceTypes,
			spWithRates.Regions,
			spWithRates.Tenancies,
			spWithRates.OperatingSystems,
		)
		if err != nil {
			log.Error(err, "failed to fetch SP rates",
				"sp_arn", sp.SavingsPlanARN,
				"sp_type", sp.SavingsPlanType,
				"fetch_type", fetchType)
			// Continue with other SPs even if one fails
			continue
		}

		// Step 4: Identify which rate combinations were queried but AWS returned nothing
		// Store sentinel values for these combinations to prevent future API calls
		sentinelRates := r.buildSentinelRatesForMissingCombinations(
			sp,
			spWithRates.InstanceTypes,
			spWithRates.Regions,
			spWithRates.Tenancies,
			spWithRates.OperatingSystems,
			rates,
		)

		// Step 5: Add both actual rates and sentinel values to cache
		addedCount := r.PricingCache.AddSPRates(rates)
		totalNewRates += addedCount

		sentinelCount := r.PricingCache.AddSPRates(sentinelRates)
		totalSentinelValues += sentinelCount

		log.V(1).Info("fetched rates for Savings Plan",
			"sp_arn", sp.SavingsPlanARN,
			"sp_type", sp.SavingsPlanType,
			"fetch_type", fetchType,
			"rates_added", addedCount,
			"sentinel_values_added", sentinelCount)
	}

	duration := time.Since(startTime)
	log.Info("SP rates reconciliation completed",
		"duration", duration.String(),
		"sps_processed", len(spsNeedingRates),
		"new_rates_added", totalNewRates,
		"sentinel_values_added", totalSentinelValues,
		"total_rates_cached", len(r.PricingCache.GetAllSPRates()))

	// TODO: Update metrics for SP rates cache

	// Requeue after 15 seconds to check for new Savings Plans or instance types
	// This is fast because in steady state there are 0 API calls (all rates cached)
	return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
}

// SPWithMissingRates represents a Savings Plan that needs rates fetched, along with
// the specific filters to use for fetching only the missing rates.
type SPWithMissingRates struct {
	SavingsPlan      aws.SavingsPlan
	InstanceTypes    []string
	Regions          []string
	Tenancies        []string
	OperatingSystems []string
	IsFullFetch      bool // true if this is the first fetch for this SP, false if incremental
}

// findMissingSPRates returns Savings Plans that need rate fetching, along with the specific
// filters to use for each SP. This supports both initial fetching (when no rates exist) and
// incremental fetching (when new instance types are discovered).
//
// The function checks:
// 1. If no rates exist for an SP, it needs a full fetch
// 2. If rates exist, it checks for missing combinations of instance type + region + tenancy + OS
// 3. Returns only SPs that have missing data
func (r *SPRatesReconciler) findMissingSPRates(savingsPlans []aws.SavingsPlan) []SPWithMissingRates {
	var spsNeedingRates []SPWithMissingRates

	// Get currently running instance types, regions, and tenancies from EC2 cache
	instanceTypes, regions, tenancies := r.getUniqueInstanceTypesRegionsAndTenancies()

	// Get operating systems from config and normalize to cache format ("linux", "windows")
	// If not configured, defaults to ["Linux", "Windows"] via reconciler initialization
	operatingSystems := r.getOperatingSystemsNormalized()

	for _, sp := range savingsPlans {
		// Check if we have ANY rate cached for this SP ARN
		if !r.PricingCache.HasAnySPRate(sp.SavingsPlanARN) {
			// No rates exist at all - need full fetch
			spsNeedingRates = append(spsNeedingRates, SPWithMissingRates{
				SavingsPlan:      sp,
				InstanceTypes:    instanceTypes,
				Regions:          regions,
				Tenancies:        tenancies,
				OperatingSystems: operatingSystems,
				IsFullFetch:      true,
			})
			r.Log.Info("SP needs full rate fetch (no existing rates)",
				"sp_arn", sp.SavingsPlanARN,
				"instance_types", len(instanceTypes),
				"regions", len(regions),
				"tenancies", len(tenancies),
			)
			continue
		}

		// Some rates exist - check for missing combinations
		missingInstanceTypes, missingRegions, missingTenancies, missingOS := r.PricingCache.GetMissingSPRatesForInstances(
			sp.SavingsPlanARN,
			instanceTypes,
			regions,
			tenancies,
			operatingSystems,
		)

		// If any combinations are missing, add this SP to the list for incremental fetch
		if len(missingInstanceTypes) > 0 {
			spsNeedingRates = append(spsNeedingRates, SPWithMissingRates{
				SavingsPlan:      sp,
				InstanceTypes:    missingInstanceTypes,
				Regions:          missingRegions,
				Tenancies:        missingTenancies,
				OperatingSystems: missingOS,
				IsFullFetch:      false,
			})
			r.Log.Info("SP needs incremental rate fetch (new instance types discovered)",
				"sp_arn", sp.SavingsPlanARN,
				"missing_instance_types", missingInstanceTypes,
				"missing_regions", missingRegions,
				"missing_tenancies", missingTenancies,
				"missing_os", missingOS,
			)
		}
	}

	return spsNeedingRates
}

// getUniqueInstanceTypesRegionsAndTenancies returns unique instance types, regions, and tenancies from the EC2 cache.
// This is used to filter SP rates queries to only fetch rates for what we're actually running.
// Returns: instanceTypes, regions, tenancies
func (r *SPRatesReconciler) getUniqueInstanceTypesRegionsAndTenancies() ([]string, []string, []string) {
	instances := r.EC2Cache.GetAllInstances()

	// Use maps to deduplicate
	instanceTypeSet := make(map[string]bool)
	regionSet := make(map[string]bool)
	tenancySet := make(map[string]bool)

	for _, inst := range instances {
		instanceTypeSet[inst.InstanceType] = true
		regionSet[inst.Region] = true
		tenancySet[inst.Tenancy] = true
	}

	// Convert to slices
	instanceTypes := make([]string, 0, len(instanceTypeSet))
	for instanceType := range instanceTypeSet {
		instanceTypes = append(instanceTypes, instanceType)
	}

	regions := make([]string, 0, len(regionSet))
	for region := range regionSet {
		regions = append(regions, region)
	}

	tenancies := make([]string, 0, len(tenancySet))
	for tenancy := range tenancySet {
		tenancies = append(tenancies, tenancy)
	}

	return instanceTypes, regions, tenancies
}

// getOperatingSystemsNormalized returns the configured operating systems normalized
// to lowercase cache format. Returns ["linux", "windows"] if not configured.
//
// Config uses title case (e.g., "Linux", "Windows") but the cache uses lowercase
// (e.g., "linux", "windows") for consistent lookups.
func (r *SPRatesReconciler) getOperatingSystemsNormalized() []string {
	// Use configured operating systems, defaulting to [Linux, Windows]
	osList := r.OperatingSystems
	if len(osList) == 0 {
		// Default to both Linux and Windows platforms
		return []string{aws.PlatformLinux, aws.PlatformWindows}
	}

	// Normalize to lowercase for cache keys
	normalized := make([]string, len(osList))
	for i, os := range osList {
		normalized[i] = strings.ToLower(os)
	}

	return normalized
}

// fetchRatesForSPWithFilters fetches rates for a specific Savings Plan from AWS
// using the provided filters. This supports both full fetches (when no rates exist)
// and incremental fetches (when new instance types are discovered).
//
// Returns a map keyed by "spArn,instanceType,region,tenancy,os" -> rate.
func (r *SPRatesReconciler) fetchRatesForSPWithFilters(
	ctx context.Context,
	sp aws.SavingsPlan,
	instanceTypes []string,
	regions []string,
	tenancies []string,
	operatingSystems []string,
) (map[string]float64, error) {
	// Use the SP's account to fetch rates
	accountConfig := aws.AccountConfig{
		AccountID:     sp.AccountID,
		Region:        r.Config.DefaultRegion,
		AssumeRoleARN: r.Config.GetDefaultAccount().AssumeRoleARN,
	}

	spClient, err := r.AWSClient.SavingsPlans(ctx, accountConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create Savings Plans client: %w", err)
	}

	// Convert OS from cache format ("linux", "windows") to AWS API format ("Linux/UNIX", "Windows")
	awsOperatingSystems := make([]string, len(operatingSystems))
	for i, os := range operatingSystems {
		switch os {
		case aws.PlatformLinux:
			awsOperatingSystems[i] = "Linux/UNIX"
		case aws.PlatformWindows:
			awsOperatingSystems[i] = "Windows"
		default:
			awsOperatingSystems[i] = os
		}
	}

	// Log what filters we're using for debugging
	r.Log.Info("fetching SP rates with filters",
		"sp_id", sp.SavingsPlanID,
		"sp_arn", sp.SavingsPlanARN,
		"instance_types_count", len(instanceTypes),
		"instance_types", instanceTypes,
		"regions_count", len(regions),
		"regions", regions,
		"operating_systems", awsOperatingSystems,
		"tenancies_count", len(tenancies),
		"tenancies", tenancies)

	// Query AWS for this SP's actual purchase-time rates, filtered by instance types, regions, OS, and tenancy
	rates, err := spClient.DescribeSavingsPlanRates(ctx, sp.SavingsPlanID, instanceTypes, regions, awsOperatingSystems, tenancies)
	if err != nil {
		return nil, fmt.Errorf("failed to query Savings Plan rates for %s: %w", sp.SavingsPlanID, err)
	}

	// Convert to map format for cache
	// Use cache.buildSPRateKey() for consistent key format
	// The ProductDescription field contains the normalized OS (either "linux" or "windows")
	ratesMap := make(map[string]float64)
	for _, rate := range rates {
		key := cache.BuildSPRateKey(sp.SavingsPlanARN, rate.InstanceType, rate.Region, rate.Tenancy, rate.ProductDescription)
		ratesMap[key] = rate.Rate
	}

	return ratesMap, nil
}

// buildSentinelRatesForMissingCombinations identifies rate combinations that were queried
// from AWS but returned no results, and returns a map of sentinel values (-1.0) for those
// combinations. This prevents repeated API calls for rate combinations that don't exist
// (e.g., Windows rates for a Linux-only Savings Plan).
//
// The function compares the requested combinations (instanceTypes × regions × tenancies × OS)
// against the actual rates returned from AWS. Any combination that was requested but not
// returned gets a sentinel value in the cache.
func (r *SPRatesReconciler) buildSentinelRatesForMissingCombinations(
	sp aws.SavingsPlan,
	requestedInstanceTypes []string,
	requestedRegions []string,
	requestedTenancies []string,
	requestedOperatingSystems []string,
	actualRates map[string]float64,
) map[string]float64 {
	sentinelRates := make(map[string]float64)

	// Build a set of all combinations that AWS returned
	// Key format: "instanceType,region,tenancy,os" (without SP ARN)
	returnedCombinations := make(map[string]bool)
	for key := range actualRates {
		// Parse the key: "spArn,instanceType,region,tenancy,os"
		parts := strings.Split(key, ",")
		if len(parts) == 5 {
			// Store without SP ARN for comparison
			combinationKey := fmt.Sprintf("%s,%s,%s,%s", parts[1], parts[2], parts[3], parts[4])
			returnedCombinations[strings.ToLower(combinationKey)] = true
		}
	}

	// Check each requested combination against what AWS returned
	for _, instanceType := range requestedInstanceTypes {
		for _, region := range requestedRegions {
			for _, tenancy := range requestedTenancies {
				for _, os := range requestedOperatingSystems {
					// Build the combination key (without SP ARN)
					combinationKey := strings.ToLower(fmt.Sprintf("%s,%s,%s,%s",
						instanceType, region, tenancy, os))

					// If this combination was NOT returned by AWS, store a sentinel value
					if !returnedCombinations[combinationKey] {
						// Build the full cache key with SP ARN using the standard builder
						cacheKey := cache.BuildSPRateKey(sp.SavingsPlanARN, instanceType, region, tenancy, os)
						sentinelRates[cacheKey] = cache.SPRateNotAvailable
					}
				}
			}
		}
	}

	return sentinelRates
}

// Run runs the SP rates reconciler as a goroutine with timer-based reconciliation.
//
// Waits for RISP and EC2 caches to be populated before first run, then re-runs every 15 seconds
// to lazy-load rates for new Savings Plans or newly discovered instance types.
func (r *SPRatesReconciler) Run(ctx context.Context) error {
	r.Log.Info("starting SP rates reconciler", "interval", "15s")

	// Wait for RISP reconciler to complete its initial run and populate the cache
	// This avoids a race condition where we try to fetch rates before any SPs are discovered
	if r.RISPReadyChan != nil {
		r.Log.Info("waiting for RISP cache to be populated before initial run")
		select {
		case <-ctx.Done():
			r.Log.Info("SP rates reconciler shutting down before RISP cache was ready")
			return nil
		case <-r.RISPReadyChan:
			r.Log.Info("RISP cache ready")
		}
	}

	// Wait for EC2 reconciler to complete its initial run and populate the cache
	// This ensures we have actual instance types to query rates for, avoiding
	// fetching rates for an incomplete set of instance types at startup
	if r.EC2ReadyChan != nil {
		r.Log.Info("waiting for EC2 cache to be populated before initial run")
		select {
		case <-ctx.Done():
			r.Log.Info("SP rates reconciler shutting down before EC2 cache was ready")
			return nil
		case <-r.EC2ReadyChan:
			r.Log.Info("EC2 cache ready, proceeding with initial SP rates reconciliation")
		}
	}

	// Run initial reconciliation now that both RISP and EC2 caches are populated
	r.Log.Info("running initial SP rates reconciliation")
	if _, err := r.Reconcile(ctx, ctrl.Request{}); err != nil {
		r.Log.Error(err, "initial SP rates reconciliation failed")
		// Don't return error - continue with periodic reconciliation
	}

	// Signal that initial reconciliation is complete (if channel was provided)
	// This allows downstream reconcilers (e.g., CostReconciler) to wait for
	// SP rates to be populated before starting their cost calculations
	if r.ReadyChan != nil {
		close(r.ReadyChan)
		r.Log.V(1).Info("signaled that SP rates cache is ready for dependent reconcilers")
	}

	// Set up 15-second ticker for periodic reconciliation
	// This is fast because in steady state there are 0 API calls (all rates cached)
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	r.Log.Info("SP rates reconciler ready, periodic reconciliation every 15 seconds")

	for {
		select {
		case <-ctx.Done():
			r.Log.Info("SP rates reconciler shutting down")
			return nil
		case <-ticker.C:
			r.Log.V(1).Info("running periodic SP rates reconciliation")
			if _, err := r.Reconcile(ctx, ctrl.Request{}); err != nil {
				r.Log.Error(err, "periodic SP rates reconciliation failed")
				// Don't return error - continue reconciliation loop
			}
		}
	}
}
