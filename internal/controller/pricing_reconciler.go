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

// PricingReconciler reconciles AWS pricing data by bulk-loading all on-demand
// pricing for EC2 instances across configured regions and operating systems.
//
// AWS pricing data changes infrequently (typically monthly), so the default
// 24-hour refresh cycle is appropriate. The reconciler preloads ALL pricing data
// at startup to ensure the cache is populated before cost calculations begin.
//
// The reconciliation interval can be configured via config.Reconciliation.Pricing.
//
// Pricing data characteristics:
//   - Changes infrequently (monthly price updates from AWS)
//   - ~84,000 entries for 600 instance types × 35 regions × 4 OS types
//   - ~10-15 MB memory footprint
//   - ~42 seconds initial load time (840 API calls at 20 req/sec limit)
//   - AWS Pricing API is public (no account-specific credentials needed)
type PricingReconciler struct {
	// AWS client for making API calls
	AWSClient aws.Client

	// Configuration with AWS account details
	Config *config.Config

	// Cache for storing pricing data
	Cache *cache.PricingCache

	// Metrics for observability
	Metrics *metrics.Metrics

	// Logger
	Log logr.Logger

	// Regions to load pricing for (discovered or configured)
	// If empty, defaults to common regions
	Regions []string

	// OperatingSystems to load pricing for
	// Default: ["Linux", "Windows"]
	OperatingSystems []string
}

// Reconcile performs a single reconciliation cycle.
// This is called by controller-runtime on a timer at the configured interval.
//
// Unlike EC2 and RISP reconcilers which query per-account, pricing reconciliation
// is global - pricing data is not account-specific and comes from the public
// AWS Pricing API.
func (r *PricingReconciler) Reconcile(ctx context.Context, _ ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("reconciler", "pricing")
	log.Info("starting pricing reconciliation cycle")

	// Track cycle timing
	startTime := time.Now()

	// Determine regions to load pricing for.
	// Uses a fallback chain to ensure we always have regions to query:
	//  1. Config.Regions (from config file 'regions' field)
	//  2. r.Regions (from reconciler initialization)
	//  3. config.DefaultRegions (common US regions) as final fallback
	regions := r.Config.Regions
	if len(regions) == 0 {
		// Config didn't specify regions, try reconciler default
		regions = r.Regions
	}
	if len(regions) == 0 {
		// No regions configured anywhere, use the default fallback regions
		regions = config.DefaultRegions
	}

	// Determine operating systems to load pricing for.
	// Uses a fallback chain similar to regions:
	//  1. Config.Pricing.OperatingSystems (from config file 'pricing.operatingSystems' field)
	//  2. r.OperatingSystems (from reconciler initialization)
	//  3. ["Linux", "Windows"] as final fallback
	operatingSystems := r.Config.Pricing.OperatingSystems
	if len(operatingSystems) == 0 {
		// Config didn't specify OSes, try reconciler default
		operatingSystems = r.OperatingSystems
	}
	if len(operatingSystems) == 0 {
		// No OSes configured anywhere, use default fallback
		operatingSystems = []string{"Linux", "Windows"}
	}

	// Calculate expected pricing entries
	// Note: Actual count may be lower if some instance types aren't available in all regions/OS
	expectedEntries := 600 * len(regions) * len(operatingSystems) // ~600 instance types per region
	estimatedAPIcalls := expectedEntries / 100                    // 100 entries per page
	estimatedDuration := float64(estimatedAPIcalls) * 0.05        // ~0.05s per API call
	// Check if we have test data configured (for E2E tests)
	var prices map[string]float64
	var err error
	var duration time.Duration

	if r.Config.TestData != nil && r.Config.TestData.Pricing() != nil {
		// Use test data instead of calling AWS Pricing API
		// This allows E2E tests to run hermetically without external API dependencies
		prices = r.Config.TestData.Pricing()
		log.Info("using test data for pricing", "count", len(prices))
		duration = time.Since(startTime)
	} else {
		// Normal production path: query AWS Pricing API
		log.Info("loading pricing data from AWS Pricing API",
			"regions", regions,
			"operating_systems", operatingSystems,
			"expected_entries", expectedEntries,
			"estimated_duration_seconds", estimatedDuration)

		// Get the pricing client
		// Pricing API is not account-specific, so no account config needed
		pricingClient := r.AWSClient.Pricing(ctx)

		// Bulk-load all pricing data
		// This queries the AWS Pricing API with pagination to fetch all EC2 instance
		// pricing for the specified regions and operating systems.
		//
		// Expected performance:
		//   - ~84,000 entries (600 types × 35 regions × 4 OS)
		//   - ~840 API calls with 100 results per page
		//   - ~42 seconds at 20 req/sec rate limit
		//   - ~10-15 MB memory footprint
		prices, err = pricingClient.LoadAllPricing(ctx, regions, operatingSystems)
		duration = time.Since(startTime)
	}

	if err != nil {
		// Record failure in metrics
		// Pricing is global, so we use empty strings for account/region labels
		r.Metrics.DataLastSuccess.WithLabelValues(
			"", // Not account-specific
			"", // Not region-specific
			"pricing",
		).Set(0)

		log.Error(err, "failed to load pricing data",
			"duration_seconds", duration.Seconds())

		// Return error but continue reconciliation loop
		// The next cycle will retry
		return r.scheduleNextReconciliation(log), fmt.Errorf("failed to load pricing data: %w", err)
	}

	// Update cache with new pricing data
	// This atomically replaces all pricing data in the cache
	r.Cache.SetOnDemandPrices(prices)

	// Record success in metrics
	r.Metrics.DataLastSuccess.WithLabelValues(
		"", // Not account-specific
		"", // Not region-specific
		"pricing",
	).Set(1)

	// Record the Unix timestamp of this successful collection
	r.Metrics.DataFreshness.WithLabelValues(
		"", // Not account-specific
		"", // Not region-specific
		"pricing",
	).Set(float64(time.Now().Unix()))

	// Log cache statistics with detailed information
	stats := r.Cache.GetStats()
	log.Info("✅ pricing data loaded successfully",
		"price_count", stats.OnDemandPriceCount,
		"regions", len(regions),
		"operating_systems", len(operatingSystems),
		"cache_age_hours", stats.AgeHours,
		"duration_seconds", duration.Seconds(),
		"prices_per_second", float64(stats.OnDemandPriceCount)/duration.Seconds())

	return r.scheduleNextReconciliation(log), nil
}

// scheduleNextReconciliation determines when to run the next reconciliation cycle.
// Returns a ctrl.Result with the appropriate RequeueAfter duration.
func (r *PricingReconciler) scheduleNextReconciliation(log logr.Logger) ctrl.Result {
	// Parse reconciliation interval from config, with default fallback to 24 hours
	// The interval determines how often we refresh pricing data
	// AWS pricing changes monthly, so 24 hours is a safe and reasonable default
	requeueAfter := 24 * time.Hour // Default
	if r.Config.Reconciliation.Pricing != "" {
		if duration, err := time.ParseDuration(r.Config.Reconciliation.Pricing); err == nil {
			requeueAfter = duration
		} else {
			log.Error(err, "invalid pricing reconciliation interval, using default",
				"configured_interval", r.Config.Reconciliation.Pricing,
				"default", "24h")
		}
	}

	// Log the configured interval (helpful for verifying configuration)
	log.V(1).Info("reconciliation interval configured", "next_run_in", requeueAfter.String())

	// Requeue after configured interval
	// This creates the periodic reconciliation loop without requiring external triggers
	return ctrl.Result{RequeueAfter: requeueAfter}
}

// RunStandalone runs the reconciler in standalone mode without Kubernetes.
//
// This method is designed for local development and testing, allowing the reconciler
// to run without a Kubernetes cluster. It executes the same reconciliation logic as
// Reconcile() but uses a simple time.Ticker instead of controller-runtime's requeue mechanism.
//
// Behavior:
//   - Runs initial reconciliation immediately on startup (BLOCKING)
//   - This ensures pricing cache is populated before serving metrics
//   - Sets up ticker for periodic reconciliation at configured interval (default: 24 hours)
//   - Continues running even if periodic reconciliation cycles fail
//   - Stops gracefully when context is cancelled (SIGTERM/SIGINT)
//
// This is used when the controller is run with the --no-kubernetes flag via:
//
//	go run ./cmd/main.go --no-kubernetes --config=config.yaml
//
// or via the convenience Make target:
//
//	make run-local
//
// coverage:ignore - standalone mode, tested manually
func (r *PricingReconciler) RunStandalone(ctx context.Context) error {
	log := r.Log.WithValues("mode", "standalone")
	log.Info("starting pricing reconciler in standalone mode")

	// Run immediately on startup (BLOCKING)
	// This is critical - we must have pricing data before serving cost metrics
	log.Info("⏳ running initial pricing reconciliation (BLOCKING - this may take 30-60 seconds)")
	if _, err := r.Reconcile(ctx, ctrl.Request{}); err != nil {
		// Initial pricing load failure is FATAL in standalone mode
		// Without pricing data, cost calculations will fail
		log.Error(err, "❌ initial pricing reconciliation failed - exiting")
		return fmt.Errorf("fatal: initial pricing load failed: %w", err)
	}
	log.Info("✅ initial pricing reconciliation completed successfully - cache is populated and ready")

	// Parse reconciliation interval from config, with default fallback to 24 hours
	interval := 24 * time.Hour // Default
	if r.Config.Reconciliation.Pricing != "" {
		if duration, err := time.ParseDuration(r.Config.Reconciliation.Pricing); err == nil {
			interval = duration
		} else {
			log.Error(err, "invalid pricing reconciliation interval, using default",
				"configured_interval", r.Config.Reconciliation.Pricing,
				"default", "24h")
		}
	}

	// Setup ticker for periodic pricing updates
	log.Info("configured reconciliation interval", "interval", interval.String())
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info("shutting down pricing reconciler")
			return ctx.Err()
		case <-ticker.C:
			log.Info("running scheduled reconciliation")
			if _, err := r.Reconcile(ctx, ctrl.Request{}); err != nil {
				log.Error(err, "scheduled reconciliation failed")
				// Don't exit - continue with next cycle
				// Periodic failures are not fatal (cache still has data from previous load)
			}
		}
	}
}
