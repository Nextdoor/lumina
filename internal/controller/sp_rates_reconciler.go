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
// using the DescribeSavingsPlanRates API. These rates are cached per-SP ARN + instance type + region.
//
// Algorithm (runs every 1-2 minutes):
//  1. Get all active Savings Plans from RISP cache
//  2. For each SP: Check if we've already cached rates for this SP
//  3. If not cached: Query DescribeSavingsPlanRates(spId) for this SP's actual rates
//  4. Convert rates to cache format: "spArn:instanceType:region" -> rate
//  5. Add new rates to PricingCache
//
// This provides natural lazy-loading - rates are only fetched once per SP, then cached.
//
// API call efficiency:
//   - Steady state: 0 API calls (all SP rates cached)
//   - New Savings Plan purchased: 1 API call per new SP
//   - First run: N API calls (one per active SP)
//
// Note: Each DescribeSavingsPlanRates call returns 100-1000+ rates (one per instance type/region
// combination that the SP covers), so the total number of API calls is much lower than the
// number of rates being cached.
type SPRatesReconciler struct {
	// AWS client for making API calls
	AWSClient aws.Client

	// Configuration with AWS account details
	Config *config.Config

	// Cache for Reserved Instances and Savings Plans
	RISPCache *cache.RISPCache

	// Cache for storing SP rates
	PricingCache *cache.PricingCache

	// Metrics for observability
	Metrics *metrics.Metrics

	// Logger
	Log logr.Logger

	// RISPReadyChan is an optional channel that this reconciler will wait on
	// before running its initial reconciliation. This ensures the RISP cache is
	// populated with Savings Plans before we try to fetch rates.
	RISPReadyChan chan struct{}
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
	missingSPs := r.findMissingSPRates(savingsPlans)
	if len(missingSPs) == 0 {
		log.V(1).Info("all Savings Plans have cached rates, skipping API calls")
		return ctrl.Result{}, nil
	}

	log.Info("discovered Savings Plans needing rate fetches",
		"missing_sp_count", len(missingSPs))

	// Step 3: Fetch rates for each missing SP from AWS
	totalNewRates := 0
	for _, sp := range missingSPs {
		rates, err := r.fetchRatesForSP(ctx, sp)
		if err != nil {
			log.Error(err, "failed to fetch SP rates",
				"sp_arn", sp.SavingsPlanARN,
				"sp_type", sp.SavingsPlanType)
			// Continue with other SPs even if one fails
			continue
		}

		// Step 4: Add rates to cache
		addedCount := r.PricingCache.AddSPRates(rates)
		totalNewRates += addedCount

		log.V(1).Info("fetched rates for Savings Plan",
			"sp_arn", sp.SavingsPlanARN,
			"sp_type", sp.SavingsPlanType,
			"rates_added", addedCount)
	}

	duration := time.Since(startTime)
	log.Info("SP rates reconciliation completed",
		"duration", duration.String(),
		"sps_processed", len(missingSPs),
		"new_rates_added", totalNewRates,
		"total_rates_cached", len(r.PricingCache.GetAllSPRates()))

	// TODO: Update metrics for SP rates cache

	// Requeue after 2 minutes to check for new Savings Plans
	// This provides a good balance between responsiveness and API efficiency
	return ctrl.Result{RequeueAfter: 2 * time.Minute}, nil
}

// findMissingSPRates returns Savings Plans that don't have rates cached yet.
// We check if ANY rate exists for the SP ARN - if yes, we assume all rates are cached.
// If no rates exist, we need to fetch them.
func (r *SPRatesReconciler) findMissingSPRates(savingsPlans []aws.SavingsPlan) []aws.SavingsPlan {
	var missingSPs []aws.SavingsPlan

	// Get all currently cached SP rates to check which SPs are missing
	cachedRates := r.PricingCache.GetAllSPRates()

	for _, sp := range savingsPlans {
		// Check if we have ANY rate cached for this SP ARN
		// If we have at least one rate, we assume all rates for this SP are cached
		// (because DescribeSavingsPlanRates returns all rates in one call)
		hasAnyRate := false
		for key := range cachedRates {
			// Key format: "spArn:instanceType:region"
			// Check if this key starts with the SP ARN
			if len(key) > len(sp.SavingsPlanARN) && key[:len(sp.SavingsPlanARN)] == sp.SavingsPlanARN {
				hasAnyRate = true
				break
			}
		}

		if !hasAnyRate {
			missingSPs = append(missingSPs, sp)
		}
	}

	return missingSPs
}

// fetchRatesForSP fetches rates for a specific Savings Plan from AWS.
// Returns a map keyed by "spArn:instanceType:region" -> rate.
func (r *SPRatesReconciler) fetchRatesForSP(
	ctx context.Context,
	sp aws.SavingsPlan,
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

	// Query AWS for this SP's actual purchase-time rates
	rates, err := spClient.DescribeSavingsPlanRates(ctx, sp.SavingsPlanID)
	if err != nil {
		return nil, fmt.Errorf("failed to query Savings Plan rates for %s: %w", sp.SavingsPlanID, err)
	}

	// Convert to map format for cache
	// Key format: "spArn:instanceType:region"
	ratesMap := make(map[string]float64)
	for _, rate := range rates {
		key := fmt.Sprintf("%s:%s:%s", sp.SavingsPlanARN, rate.InstanceType, rate.Region)
		ratesMap[key] = rate.Rate
	}

	return ratesMap, nil
}

// Run runs the SP rates reconciler as a goroutine with timer-based reconciliation.
//
// Waits for RISP cache to be populated before first run, then re-runs every 2 minutes
// to lazy-load rates for new Savings Plans.
func (r *SPRatesReconciler) Run(ctx context.Context) error {
	r.Log.Info("starting SP rates reconciler", "interval", "2m")

	// Wait for RISP reconciler to complete its initial run and populate the cache
	// This avoids a race condition where we try to fetch rates before any SPs are discovered
	if r.RISPReadyChan != nil {
		r.Log.Info("waiting for RISP cache to be populated before initial run")
		select {
		case <-ctx.Done():
			r.Log.Info("SP rates reconciler shutting down before RISP cache was ready")
			return nil
		case <-r.RISPReadyChan:
			r.Log.Info("RISP cache ready, proceeding with initial SP rates reconciliation")
		}
	}

	// Run initial reconciliation now that RISP cache is populated
	r.Log.Info("running initial SP rates reconciliation")
	if _, err := r.Reconcile(ctx, ctrl.Request{}); err != nil {
		r.Log.Error(err, "initial SP rates reconciliation failed")
		// Don't return error - continue with periodic reconciliation
	}

	// Set up 2-minute ticker for periodic reconciliation
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()

	r.Log.Info("SP rates reconciler ready, periodic reconciliation every 2 minutes")

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
