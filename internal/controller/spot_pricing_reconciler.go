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
			"new_prices", newCount,
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
// We track the account, region, and platform to efficiently group API queries.
type SpotPriceCombination struct {
	InstanceType     string
	AvailabilityZone string
	AccountID        string
	Region           string
	Platform         string // Operating system platform (e.g., "linux", "windows")
}

// findMissingSpotPrices checks the cache for missing or stale spot prices.
// Returns combinations that need to be fetched from AWS.
//
// This method implements the core lazy-loading + automatic refresh logic for spot prices:
//
//  1. LAZY-LOADING: Only checks prices for instance type+AZ combinations that actually have running instances
//     (NOT a cartesian product of all types × all AZs)
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
// BUG FIX (2025-11-20):
// Previous implementation created a cartesian product of all instance types × all AZs,
// causing unnecessary API calls for non-existent combinations. Now we only check actual
// instance type+AZ combinations from running instances.
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

	// FIX: Instead of checking cartesian product of all types × all AZs,
	// iterate over actual running instances to get real combinations.
	// Track unique combinations we've already checked to avoid duplicates.
	checkedCombinations := make(map[string]bool)

	instances := r.EC2Cache.GetAllInstances()
	for _, inst := range instances {
		// Create unique key for this combination
		comboKey := fmt.Sprintf("%s:%s:%s", inst.InstanceType, inst.AvailabilityZone, inst.Platform)
		if checkedCombinations[comboKey] {
			continue // Already checked this combination
		}
		checkedCombinations[comboKey] = true

		// Now check if this specific instance type+AZ combination is cached
		instanceType := inst.InstanceType
		az := inst.AvailabilityZone
		accountID := inst.AccountID
		region := inst.Region
		platform := inst.Platform

		// Convert platform to ProductDescription for cache key matching
		productDescription := platformToProductDescription(platform)

		// Build cache key (case-insensitive) - must include ProductDescription
		// Cache keys are lowercase: "m5.xlarge:us-west-2a:linux/unix"
		// ProductDescription is required because Windows and Linux have different spot prices
		key := strings.ToLower(fmt.Sprintf("%s:%s:%s", instanceType, az, productDescription))

		// Check if price exists in cache and if it's stale
		spotPrice, exists := allPricesWithTimestamps[key]
		needsRefresh := false

		if !exists {
			// Exact AZ match not found - check if we have a price for any AZ in same region
			// This handles LocalStack's synthetic data where spot prices may be for different AZs
			// than where instances are placed. In production, AWS returns prices for all AZs,
			// so we'll eventually have the exact match. For now, use any cached price from same region.
			normalizedPD := strings.ToLower(productDescription)

			// Scan cache for any price matching instance type + region + product description
			for cachedKey, cachedPrice := range allPricesWithTimestamps {
				parts := strings.Split(cachedKey, ":")
				if len(parts) == 3 {
					cachedInstanceType := parts[0]
					cachedAZ := parts[1]
					cachedPD := parts[2]
					cachedRegion := cachedAZ[:len(cachedAZ)-1]

					// Found a match for same instance type + region + product description (different AZ)
					if cachedInstanceType == strings.ToLower(instanceType) &&
						cachedRegion == region &&
						cachedPD == normalizedPD {
						// Use this price as a fallback - check if it's stale
						spotPrice = cachedPrice
						exists = true
						r.Log.V(2).Info("using spot price from different AZ in same region (fallback)",
							"instance_type", instanceType,
							"requested_az", az,
							"cached_az", cachedAZ,
							"region", region,
							"product_description", productDescription)
						break
					}
				}
			}

			if !exists {
				// No price found in region at all - need to fetch from AWS
				// This happens when:
				// - First reconciliation (empty cache)
				// - New instance type launched (lazy-loading)
				// - New platform type (e.g., first Windows instance)
				needsRefresh = true
			}
		}

		// If we found a price (either exact match or fallback), check if it's stale
		if exists {
			// Check if price is stale based on FetchedAt timestamp
			// FetchedAt is when WE fetched it (not when AWS recorded it)
			age := time.Since(spotPrice.FetchedAt)
			if age > staleThreshold {
				needsRefresh = true
				// Log at V(2) because this is expected behavior (prices expire hourly)
				r.Log.V(2).Info("spot price is stale, will refresh",
					"instance_type", instanceType,
					"az", az,
					"product_description", productDescription,
					"age_hours", age.Hours(),
					"threshold_hours", staleThreshold.Hours())
			}
		}

		if needsRefresh {
			missing = append(missing, SpotPriceCombination{
				InstanceType:     instanceType,
				AvailabilityZone: az,
				AccountID:        accountID,
				Region:           region,
				Platform:         platform,
			})
		}
	}

	return missing
}

// getMetadataForInstanceType determines the account, region, and platform for a specific
// instance type + AZ combination by looking at running instances in EC2Cache.
// Returns (accountID, region, platform) for the first matching instance.
//
// The platform field is used to determine which ProductDescriptions to query from AWS:
// - Platform "" or aws.PlatformLinux → aws.ProductDescriptionLinuxUnix
// - Platform aws.PlatformWindows → aws.ProductDescriptionWindows
//
// Note: This assumes that within a given AZ, the same instance type will have the same
// platform (which is true in practice - you can't have both Linux and Windows m5.xlarge
// instances in the same AZ with different spot prices).
func (r *SpotPricingReconciler) getMetadataForInstanceType(instanceType, az string) (string, string, string) {
	instances := r.EC2Cache.GetAllInstances()
	for _, inst := range instances {
		if inst.InstanceType == instanceType && inst.AvailabilityZone == az {
			return inst.AccountID, inst.Region, inst.Platform
		}
	}
	// Fallback (should never happen if EC2Cache is populated correctly)
	// Default to Linux since it's the most common platform (~99% of instances)
	return r.Config.AWSAccounts[0].AccountID, r.Config.DefaultRegion, "linux"
}

// platformToProductDescription converts an EC2 Platform value to an AWS Spot Price ProductDescription.
// This mapping is used when querying AWS DescribeSpotPriceHistory API.
//
// EC2 Platform values (from aws.Instance.Platform):
//   - "" (empty)  → aws.ProductDescriptionLinuxUnix (default for non-Windows instances)
//   - "linux"     → aws.ProductDescriptionLinuxUnix
//   - "windows"   → aws.ProductDescriptionWindows
//
// AWS ProductDescription values (for DescribeSpotPriceHistory):
//   - aws.ProductDescriptionLinuxUnix - Most common (~99% of spot instances)
//   - aws.ProductDescriptionWindows   - Windows-based instances
//   - aws.ProductDescriptionSUSE      - SUSE-specific (rarely used, not supported here)
//   - aws.ProductDescriptionRHEL      - RHEL-specific (rarely used, not supported here)
//
// Note: We only support Linux/UNIX and Windows as these cover >99% of use cases.
// SUSE and RHEL instances still return spot prices under "Linux/UNIX" ProductDescription.
func platformToProductDescription(platform string) string {
	normalized := strings.ToLower(strings.TrimSpace(platform))

	// Empty string or "linux" → aws.ProductDescriptionLinuxUnix
	if normalized == "" || normalized == aws.PlatformLinux {
		return aws.ProductDescriptionLinuxUnix
	}

	// "windows" → aws.ProductDescriptionWindows
	if normalized == aws.PlatformWindows {
		return aws.ProductDescriptionWindows
	}

	// Default to Linux/UNIX for any unrecognized values
	// This is safe because AWS returns spot prices for RHEL/SUSE under "Linux/UNIX"
	return aws.ProductDescriptionLinuxUnix
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
	//
	// WHY group by region instead of account+region?
	// - Spot prices are the same across all accounts in the same region
	// - If we have 10 accounts all using m5.xlarge in us-west-2, we only need to query once
	// - This reduces API calls from N (accounts) to 1 per region
	// - Example: 10 accounts × 3 regions = 30 queries → 3 queries (90% reduction)
	//
	// The regionQuery struct tracks which account to use for the API call (arbitrary choice,
	// we pick the first one we encounter) and accumulates all combinations for that region.
	type regionQuery struct {
		accountID    string                 // Account credentials to use for this region's API call
		region       string                 // AWS region (e.g., "us-west-2")
		combinations []SpotPriceCombination // All missing prices for this region
	}
	queries := make(map[string]*regionQuery) // key: region

	// Build the query map by iterating through missing combinations
	for _, combo := range missing {
		if _, exists := queries[combo.Region]; !exists {
			// First time seeing this region - create new query and use this combination's account
			queries[combo.Region] = &regionQuery{
				accountID:    combo.AccountID,
				region:       combo.Region,
				combinations: []SpotPriceCombination{combo},
			}
		} else {
			// Already have a query for this region - just append this combination
			// (we'll use the first account's credentials for all combinations in this region)
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

			// Extract unique instance types from combinations
			instanceTypeSet := make(map[string]bool)
			for _, combo := range combos {
				instanceTypeSet[combo.InstanceType] = true
			}
			instanceTypes := make([]string, 0, len(instanceTypeSet))
			for it := range instanceTypeSet {
				instanceTypes = append(instanceTypes, it)
			}

			// Extract unique platforms and convert to ProductDescriptions for AWS API
			// This allows us to query spot prices for the actual OS types running in the cluster,
			// rather than querying for all possible OS types.
			platformSet := make(map[string]bool)
			for _, combo := range combos {
				platformSet[combo.Platform] = true
			}
			productDescriptions := make([]string, 0, len(platformSet))
			for platform := range platformSet {
				productDescriptions = append(productDescriptions, platformToProductDescription(platform))
			}
			// Ensure at least one ProductDescription (default to Linux/UNIX if none found)
			if len(productDescriptions) == 0 {
				productDescriptions = []string{aws.ProductDescriptionLinuxUnix}
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

// getReconciliationInterval parses the reconciliation interval from config with fallback.
// This method is used by both Reconcile() and Run() to ensure consistent interval parsing.
//
// Default: 15 seconds (fast because lazy-loading = 0 API calls in steady state)
//
// Why 15 seconds (not 15 minutes)?
// - Lazy-loading means steady-state = 0 API calls per cycle
// - Most reconciliation cycles just check cache and return immediately
// - Short intervals provide fast response to new instance types
// - Only makes API calls when new instance types are discovered
func (r *SpotPricingReconciler) getReconciliationInterval(log logr.Logger) time.Duration {
	defaultInterval := 15 * time.Second

	if r.Config.Reconciliation.SpotPricing == "" {
		return defaultInterval
	}

	duration, err := time.ParseDuration(r.Config.Reconciliation.SpotPricing)
	if err != nil {
		log.Error(err, "invalid spot pricing reconciliation interval, using default",
			"configured_interval", r.Config.Reconciliation.SpotPricing,
			"default", defaultInterval.String())
		return defaultInterval
	}

	return duration
}

// scheduleNextReconciliation determines when to run the next reconciliation cycle.
func (r *SpotPricingReconciler) scheduleNextReconciliation(log logr.Logger) ctrl.Result {
	requeueAfter := r.getReconciliationInterval(log)
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

	// Run initial reconciliation with retry logic (BLOCKING)
	// Initial spot pricing load is REQUIRED - we cannot serve accurate costs without it.
	// The CostReconciler waits for SpotPricingReadyChan before starting, so spot prices
	// must be available before any cost calculations begin.
	//
	// Retry logic provides self-healing:
	// - Transient AWS API errors will be retried automatically
	// - Network issues will be retried with exponential backoff
	// - System will eventually become healthy without manual intervention
	log.Info("⏳ running initial spot pricing reconciliation (BLOCKING with retry)")

	err := RetryWithBackoff(ctx, DefaultRetryConfig(), log, "initial spot pricing reconciliation", func() error {
		_, err := r.Reconcile(ctx, ctrl.Request{})
		return err
	})

	if err != nil {
		log.Error(err, "❌ initial spot pricing reconciliation failed permanently")
		return err
	}

	log.Info("✅ initial spot pricing reconciliation completed successfully")

	// Get reconciliation interval from config (uses same logic as Reconcile method)
	interval := r.getReconciliationInterval(log)
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
