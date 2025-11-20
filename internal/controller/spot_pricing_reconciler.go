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
	"sync"
	"time"

	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/nextdoor/lumina/internal/cache"
	"github.com/nextdoor/lumina/pkg/aws"
	"github.com/nextdoor/lumina/pkg/config"
	"github.com/nextdoor/lumina/pkg/metrics"
)

// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create

// SpotPricingReconciler reconciles AWS spot pricing data by lazy-loading
// prices for instance types that are actually running in the environment.
//
// This reconciler follows the same pattern as SPRatesReconciler:
// - Waits for EC2Cache to populate (via EC2ReadyChan)
// - Extracts unique instance types and availability zones from EC2Cache
// - Checks cache for missing spot prices
// - Only queries AWS for missing combinations
// - Uses short intervals (15s default) since steady-state = 0 API calls
//
// Why lazy-loading?
// - EC2 supports ~600 instance types × 3-6 AZs per region = 1,800-3,600 prices per region
// - Most environments use <100 unique instance types
// - Spot prices change hourly, so we don't want to query all prices constantly
// - Lazy-loading reduces API calls from 1,800+ per cycle to <100 per cycle
//
// Spot pricing characteristics:
//   - Changes frequently (hourly price updates from AWS)
//   - Per availability zone (not just per region)
//   - Same across all accounts in the same AZ (region-specific, not account-specific)
//   - Requires EC2 API credentials to query, but we only query once per region
//   - API call: DescribeSpotPriceHistory (EC2 API, not Pricing API)
type SpotPricingReconciler struct {
	// AWS client for making API calls
	AWSClient aws.Client

	// Configuration with AWS account details
	Config *config.Config

	// EC2Cache provides running instance inventory to determine which instance types
	// and availability zones to fetch spot prices for. This enables lazy-loading.
	EC2Cache *cache.EC2Cache

	// Cache for storing pricing data (includes spot prices)
	Cache *cache.PricingCache

	// Metrics for observability
	Metrics *metrics.Metrics

	// Logger
	Log logr.Logger

	// ProductDescriptions to query (operating systems)
	// Default: ["Linux/UNIX"]
	// Options: ["Linux/UNIX", "Windows", "SUSE Linux", "Red Hat Enterprise Linux"]
	ProductDescriptions []string

	// EC2ReadyChan is used to wait for EC2 cache to be populated before
	// starting reconciliation. This ensures we have instance data to determine
	// which spot prices to fetch.
	EC2ReadyChan chan struct{}

	// ReadyChan is an optional channel that will be closed after the initial
	// reconciliation completes successfully. This allows downstream reconcilers
	// (like CostReconciler) to wait for spot pricing data to be populated before
	// they start their work.
	ReadyChan chan struct{}

	// readyOnce ensures ReadyChan is closed only once (after first reconciliation)
	readyOnce sync.Once
}

// Reconcile performs a single reconciliation cycle using lazy-loading.
// This is called by controller-runtime on a timer at the configured interval.
//
// Lazy-loading algorithm:
//  1. Wait for EC2Cache to be populated (via EC2ReadyChan)
//  2. Extract unique instance types and availability zones from EC2Cache
//  3. Check cache for missing spot prices for those combinations
//  4. Only query AWS for missing prices
//  5. Update cache with new prices
//
// In steady state (no new instance types), this does 0 API calls per cycle.
// This is why we use 15-second intervals - most cycles are instant.
func (r *SpotPricingReconciler) Reconcile(ctx context.Context, _ ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("reconciler", "spot-pricing")

	// Wait for EC2Cache to be populated before starting
	// This ensures we have instance data to determine which spot prices to fetch
	select {
	case <-r.EC2ReadyChan:
		// EC2 cache is ready, proceed
	case <-ctx.Done():
		return ctrl.Result{}, ctx.Err()
	}

	log.V(1).Info("starting spot pricing reconciliation cycle (lazy-loading)")
	startTime := time.Now()

	// Get unique instance types and availability zones from EC2Cache
	// These determine which spot prices we need to fetch
	instanceTypes, availabilityZones := r.getUniqueInstanceTypesAndAvailabilityZones()

	// If no instances are running, we don't need any spot prices
	if len(instanceTypes) == 0 {
		log.V(1).Info("no instances found in EC2Cache, skipping spot pricing query")

		// Record metrics even when no instances - reconciliation succeeded, no data to fetch
		r.Metrics.DataLastSuccess.WithLabelValues("", "", "spot-pricing").Set(1)
		r.Metrics.DataFreshness.WithLabelValues("", "", "spot-pricing").Set(float64(time.Now().Unix()))

		// Signal ready on first cycle even with no instances
		r.readyOnce.Do(func() {
			if r.ReadyChan != nil {
				close(r.ReadyChan)
				log.V(1).Info("signaled that spot pricing cache is ready (no instances)")
			}
		})
		return r.scheduleNextReconciliation(log), nil
	}

	log.V(1).Info("discovered running instances",
		"unique_instance_types", len(instanceTypes),
		"unique_availability_zones", len(availabilityZones))

	// Find missing spot prices (lazy-loading: check cache first)
	missingCombinations := r.findMissingSpotPrices(instanceTypes, availabilityZones)

	// If all prices are cached, we're done (0 API calls!)
	if len(missingCombinations) == 0 {
		log.V(1).Info("all spot prices are cached, no queries needed (0 API calls)")

		// Record metrics even when all cached - reconciliation succeeded, cache is fresh
		r.Metrics.DataLastSuccess.WithLabelValues("", "", "spot-pricing").Set(1)
		r.Metrics.DataFreshness.WithLabelValues("", "", "spot-pricing").Set(float64(time.Now().Unix()))

		r.readyOnce.Do(func() {
			if r.ReadyChan != nil {
				close(r.ReadyChan)
				log.V(1).Info("signaled that spot pricing cache is ready (all cached)")
			}
		})
		return r.scheduleNextReconciliation(log), nil
	}

	log.Info("fetching missing spot prices",
		"missing_combinations", len(missingCombinations),
		"duration_seconds", time.Since(startTime).Seconds())

	// Fetch missing spot prices from AWS
	// Group by account+region for efficient querying
	newPrices, fetchErrors := r.fetchMissingSpotPrices(ctx, missingCombinations)

	// Insert/update cache with new prices (merges with existing prices)
	if len(newPrices) > 0 {
		newCount := r.Cache.InsertSpotPrices(newPrices)
		log.Info("updated spot pricing cache",
			"total_prices_fetched", len(newPrices),
			"newly_added", newCount,
			"refreshed", len(newPrices)-newCount,
			"duration_seconds", time.Since(startTime).Seconds())
	}

	// Record metrics
	if len(fetchErrors) == 0 {
		r.Metrics.DataLastSuccess.WithLabelValues("", "", "spot-pricing").Set(1)
	} else {
		r.Metrics.DataLastSuccess.WithLabelValues("", "", "spot-pricing").Set(0)
		log.Info("reconciliation cycle completed with errors",
			"error_count", len(fetchErrors),
			"new_prices", len(newPrices))
	}
	r.Metrics.DataFreshness.WithLabelValues("", "", "spot-pricing").Set(float64(time.Now().Unix()))

	// Signal that initial reconciliation is complete (thread-safe, only once)
	r.readyOnce.Do(func() {
		if r.ReadyChan != nil {
			close(r.ReadyChan)
			log.V(1).Info("signaled that spot pricing cache is ready")
		}
	})

	return r.scheduleNextReconciliation(log), nil
}

// getUniqueInstanceTypesAndAvailabilityZones extracts unique instance types and
// availability zones from all running instances in EC2Cache.
//
// This determines which spot prices we need to fetch. By querying only for running
// instance types, we avoid fetching prices for hundreds of unused instance types.
//
// Returns:
//   - instanceTypes: unique instance types (e.g., ["m5.large", "c5.xlarge"])
//   - availabilityZones: unique AZs (e.g., ["us-west-2a", "us-west-2b"])
func (r *SpotPricingReconciler) getUniqueInstanceTypesAndAvailabilityZones() ([]string, []string) {
	// Get all instances from EC2Cache
	instances := r.EC2Cache.GetAllInstances()

	// Use maps to deduplicate
	instanceTypeSet := make(map[string]bool)
	azSet := make(map[string]bool)

	for _, inst := range instances {
		instanceTypeSet[inst.InstanceType] = true
		azSet[inst.AvailabilityZone] = true
	}

	// Convert sets to slices
	instanceTypes := make([]string, 0, len(instanceTypeSet))
	for it := range instanceTypeSet {
		instanceTypes = append(instanceTypes, it)
	}

	availabilityZones := make([]string, 0, len(azSet))
	for az := range azSet {
		availabilityZones = append(availabilityZones, az)
	}

	return instanceTypes, availabilityZones
}

// SpotPriceCombination represents a missing spot price that needs to be fetched.
// We track the account and region to efficiently group API queries.
type SpotPriceCombination struct {
	InstanceType     string
	AvailabilityZone string
	AccountID        string
	Region           string
}

// findMissingSpotPrices checks the cache for missing or stale spot prices.
// Returns combinations that need to be fetched from AWS.
//
// This method implements the core lazy-loading + automatic refresh logic for spot prices:
//
//  1. LAZY-LOADING: Only checks prices for instance types that are actually running
//     (determined by instanceTypes/availabilityZones from EC2Cache)
//
//  2. AUTOMATIC REFRESH: Checks FetchedAt timestamps to determine if cached prices are stale
//     Staleness threshold comes from Config.Pricing.SpotPriceCacheExpiration (default: 1h)
//
// WHY STALENESS MATTERS:
// AWS spot prices change hourly. If we used stale prices (e.g., from yesterday),
// cost calculations would be inaccurate. By checking FetchedAt and refreshing stale
// prices, we ensure cost metrics always reflect current spot market conditions.
//
// PERFORMANCE OPTIMIZATION:
// In steady-state (no new instance types, prices not stale), this returns an empty list,
// meaning 0 AWS API calls. This is why we can run reconciliation every 15 seconds -
// most cycles complete instantly by checking local cache timestamps.
//
// Returns:
//   - []SpotPriceCombination: List of instance type + AZ combinations that need fetching
//   - Empty slice if all prices are cached and fresh
func (r *SpotPricingReconciler) findMissingSpotPrices(
	instanceTypes []string,
	availabilityZones []string,
) []SpotPriceCombination {
	var missing []SpotPriceCombination

	// Get all spot prices with full SpotPrice structs (includes FetchedAt timestamps)
	// We need timestamps to determine staleness, not just the price values
	allPricesWithTimestamps := r.Cache.GetAllSpotPricesWithTimestamps()

	// Parse staleness threshold from config, with 1 hour default
	// This is configurable because different users may have different accuracy requirements:
	// - Tight budget → shorter expiration (more accurate, more API calls)
	// - Loose budget → longer expiration (less accurate, fewer API calls)
	staleThreshold := 1 * time.Hour // Default: matches AWS's hourly spot price updates
	if r.Config.Pricing.SpotPriceCacheExpiration != "" {
		if duration, err := time.ParseDuration(r.Config.Pricing.SpotPriceCacheExpiration); err == nil {
			staleThreshold = duration
		} else {
			// Invalid duration in config (e.g., "foo" or "1zz") - log and use default
			// We continue with default rather than failing to ensure reconciliation proceeds
			r.Log.V(1).Info("invalid spot price cache expiration in config, using default",
				"configured", r.Config.Pricing.SpotPriceCacheExpiration,
				"default", "1h",
				"error", err.Error())
		}
	}

	// Check each combination of instance type + AZ against cache
	// This is a cartesian product: if 10 instance types × 3 AZs = 30 combinations to check
	for _, instanceType := range instanceTypes {
		for _, az := range availabilityZones {
			// Build cache key (case-insensitive)
			// Cache keys are lowercase: "m5.xlarge:us-west-2a"
			key := strings.ToLower(fmt.Sprintf("%s:%s", instanceType, az))

			// Check if price exists in cache and if it's stale
			spotPrice, exists := allPricesWithTimestamps[key]
			needsRefresh := false

			if !exists {
				// Price doesn't exist in cache - need to fetch from AWS
				// This happens when:
				// - First reconciliation (empty cache)
				// - New instance type launched (lazy-loading)
				needsRefresh = true
			} else {
				// Price exists - check if it's stale based on FetchedAt timestamp
				// FetchedAt is when WE fetched it (not when AWS recorded it)
				age := time.Since(spotPrice.FetchedAt)
				if age > staleThreshold {
					needsRefresh = true
					// Log at V(2) because this is expected behavior (prices expire hourly)
					r.Log.V(2).Info("spot price is stale, will refresh",
						"instance_type", instanceType,
						"az", az,
						"age_hours", age.Hours(),
						"threshold_hours", staleThreshold.Hours())
				}
			}

			if needsRefresh {
				// Determine which AWS account+region this AZ belongs to
				// We need this to create the correct EC2 client for the API call
				accountID, region := r.getAccountAndRegionForAZ(az)
				missing = append(missing, SpotPriceCombination{
					InstanceType:     instanceType,
					AvailabilityZone: az,
					AccountID:        accountID,
					Region:           region,
				})
			}
		}
	}

	return missing
}

// getAccountAndRegionForAZ determines which AWS account and region an availability zone belongs to.
// Returns the first account that has instances in this AZ.
//
// Note: This assumes AZs are uniquely named within an organization (which is true in practice).
func (r *SpotPricingReconciler) getAccountAndRegionForAZ(az string) (string, string) {
	instances := r.EC2Cache.GetAllInstances()
	for _, inst := range instances {
		if inst.AvailabilityZone == az {
			return inst.AccountID, inst.Region
		}
	}
	// Fallback (should never happen if EC2Cache is populated correctly)
	return r.Config.AWSAccounts[0].AccountID, r.Config.DefaultRegion
}

// fetchMissingSpotPrices queries AWS for missing spot prices.
// Groups queries by region only (spot prices are the same across all accounts).
//
// Returns:
//   - prices: map of newly fetched SpotPrice structs keyed by "instanceType:availabilityZone"
//   - errors: slice of errors encountered during fetching (doesn't stop on first error)
func (r *SpotPricingReconciler) fetchMissingSpotPrices(
	ctx context.Context,
	missing []SpotPriceCombination,
) (map[string]aws.SpotPrice, []error) {
	// Group missing combinations by region only (spot prices are region-specific, not account-specific).
	// We pick the first account we see for each region to make the API call.
	type regionQuery struct {
		accountID    string
		region       string
		combinations []SpotPriceCombination
	}
	queries := make(map[string]*regionQuery) // key: region

	for _, combo := range missing {
		if _, exists := queries[combo.Region]; !exists {
			// First time seeing this region - use this account
			queries[combo.Region] = &regionQuery{
				accountID:    combo.AccountID,
				region:       combo.Region,
				combinations: []SpotPriceCombination{combo},
			}
		} else {
			// Already have an account for this region, just add the combination
			queries[combo.Region].combinations = append(queries[combo.Region].combinations, combo)
		}
	}

	// Fetch spot prices for each region (one query per region, not per account)
	prices := make(map[string]aws.SpotPrice)
	var errors []error
	var mu sync.Mutex

	var wg sync.WaitGroup
	for _, query := range queries {
		wg.Add(1)
		go func(accountID, region string, combos []SpotPriceCombination) {
			defer wg.Done()

			// Extract unique instance types and product descriptions for this query
			instanceTypeSet := make(map[string]bool)
			for _, combo := range combos {
				instanceTypeSet[combo.InstanceType] = true
			}
			instanceTypes := make([]string, 0, len(instanceTypeSet))
			for it := range instanceTypeSet {
				instanceTypes = append(instanceTypes, it)
			}

			// Determine product descriptions
			productDescriptions := r.ProductDescriptions
			if len(productDescriptions) == 0 {
				productDescriptions = []string{"Linux/UNIX"}
			}

			// Find account config
			var account config.AWSAccount
			for _, acc := range r.Config.AWSAccounts {
				if acc.AccountID == accountID {
					account = acc
					break
				}
			}

			// Get EC2 client
			ec2Client, err := r.AWSClient.EC2(ctx, aws.AccountConfig{
				AccountID:     account.AccountID,
				Name:          account.Name,
				AssumeRoleARN: account.AssumeRoleARN,
				Region:        region,
			})
			if err != nil {
				mu.Lock()
				errors = append(errors, fmt.Errorf("failed to create EC2 client for %s/%s: %w",
					accountID, region, err))
				mu.Unlock()
				return
			}

			// Query spot prices
			spotPrices, err := ec2Client.DescribeSpotPriceHistory(
				ctx,
				[]string{region},
				instanceTypes,
				productDescriptions,
			)
			if err != nil {
				mu.Lock()
				errors = append(errors, fmt.Errorf("failed to describe spot prices for %s/%s: %w",
					accountID, region, err))
				mu.Unlock()
				return
			}

			// Add to prices map (store full SpotPrice struct with timestamp)
			mu.Lock()
			for _, sp := range spotPrices {
				key := fmt.Sprintf("%s:%s", sp.InstanceType, sp.AvailabilityZone)
				prices[key] = sp
			}
			mu.Unlock()

		}(query.accountID, query.region, query.combinations)
	}

	wg.Wait()
	return prices, errors
}

// scheduleNextReconciliation determines when to run the next reconciliation cycle.
func (r *SpotPricingReconciler) scheduleNextReconciliation(log logr.Logger) ctrl.Result {
	// Parse reconciliation interval from config, with default fallback to 15 seconds.
	//
	// Why 15 seconds (not 15 minutes)?
	// - Lazy-loading means steady-state = 0 API calls per cycle
	// - Most reconciliation cycles just check cache and return immediately
	// - Short intervals provide fast response to new instance types
	// - Only makes API calls when new instance types are discovered
	requeueAfter := 15 * time.Second // Default
	if r.Config.Reconciliation.SpotPricing != "" {
		if duration, err := time.ParseDuration(r.Config.Reconciliation.SpotPricing); err == nil {
			requeueAfter = duration
		} else {
			log.Error(err, "invalid spot pricing reconciliation interval, using default",
				"configured_interval", r.Config.Reconciliation.SpotPricing,
				"default", "15s")
		}
	}

	log.V(1).Info("reconciliation interval configured", "next_run_in", requeueAfter.String())
	return ctrl.Result{RequeueAfter: requeueAfter}
}

// Run runs the reconciler as a goroutine with timer-based reconciliation.
//
// Performs an initial BLOCKING reconciliation on startup to ensure spot pricing cache
// is populated before serving cost metrics. Then sets up periodic reconciliation at the
// configured interval (default: 15 seconds for lazy-loading).
//
// coverage:ignore - This is a top-level runner called by main.go, not unit tested
func (r *SpotPricingReconciler) Run(ctx context.Context) error {
	log := r.Log
	log.Info("starting spot pricing reconciler (lazy-loading mode)")

	// Run immediately on startup (BLOCKING)
	log.Info("⏳ running initial spot pricing reconciliation (BLOCKING)")
	if _, err := r.Reconcile(ctx, ctrl.Request{}); err != nil {
		// Initial spot pricing load failure is not fatal - we can still serve costs
		// using on-demand pricing. Log the error but continue.
		log.Error(err, "⚠️  initial spot pricing reconciliation failed - will use on-demand pricing")
	} else {
		log.Info("✅ initial spot pricing reconciliation completed successfully")
	}

	// Parse reconciliation interval from config
	interval := 15 * time.Second // Default (fast because lazy-loading = 0 API calls in steady state)
	if r.Config.Reconciliation.SpotPricing != "" {
		if duration, err := time.ParseDuration(r.Config.Reconciliation.SpotPricing); err == nil {
			interval = duration
		} else {
			log.Error(err, "invalid spot pricing reconciliation interval, using default",
				"configured_interval", r.Config.Reconciliation.SpotPricing,
				"default", "15s")
		}
	}

	// Setup ticker for periodic spot pricing updates
	log.Info("configured reconciliation interval", "interval", interval.String())
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info("shutting down spot pricing reconciler")
			return ctx.Err()
		case <-ticker.C:
			log.V(1).Info("running scheduled reconciliation")
			if _, err := r.Reconcile(ctx, ctrl.Request{}); err != nil {
				log.Error(err, "scheduled reconciliation failed")
				// Don't exit - continue with next cycle
			}
		}
	}
}
