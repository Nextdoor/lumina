/*
Copyright 2025 Lumina Contributors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cache

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	// OSLinux represents the normalized Linux operating system value.
	// This matches EC2 instances with empty Platform field and SP rates for Linux/UNIX, RHEL, SUSE.
	OSLinux = "linux"

	// OSWindows represents the normalized Windows operating system value.
	// This matches EC2 instances with Platform="windows" and SP rates for Windows.
	OSWindows = "windows"

	// SPRateNotAvailable is a sentinel value stored in the cache to indicate that
	// a specific rate combination (SP ARN + instance type + region + tenancy + OS)
	// was queried from AWS but no rate was returned. This prevents repeated API calls
	// for rate combinations that don't exist (e.g., Windows rates for a Linux-only SP).
	//
	// Why -1.0? This value:
	// - Is invalid as a real hourly rate (rates are always >= 0)
	// - Won't be confused with $0/hour free-tier pricing
	// - Makes it obvious in debug output that this is a marker value
	SPRateNotAvailable = -1.0
)

// SP rate cache keys use comma-separated format to avoid ARN colon conflicts:
// Format: "spArn,instanceType,region,tenancy,os"
// Example: "arn:aws:savingsplans::123:savingsplan/abc,m5.xlarge,us-west-2,default,linux"
//
// All keys are stored in lowercase for case-insensitive lookups.
const (
	spRateKeySeparator = ","
	spRateKeyParts     = 5 // ARN, instanceType, region, tenancy, OS
)

// BuildSPRateKey constructs a cache key for an SP rate lookup.
// Returns a lowercase, comma-separated key for consistent cache access.
//
// This function is exported so the SP rates reconciler can build keys consistently
// when constructing rate maps to add to the cache.
func BuildSPRateKey(spArn, instanceType, region, tenancy, os string) string {
	return strings.ToLower(fmt.Sprintf("%s%s%s%s%s%s%s%s%s",
		spArn, spRateKeySeparator,
		instanceType, spRateKeySeparator,
		region, spRateKeySeparator,
		tenancy, spRateKeySeparator,
		os))
}

// parseSPRateKey splits a cache key into its components.
// Returns (instanceType, region, tenancy, os, ok).
// The SP ARN is not returned as it's typically already known from context.
func parseSPRateKey(key string) (instanceType, region, tenancy, os string, ok bool) {
	parts := strings.Split(key, spRateKeySeparator)
	if len(parts) != spRateKeyParts {
		return "", "", "", "", false
	}
	// parts[0] is spArn (caller typically doesn't need it)
	return parts[1], parts[2], parts[3], parts[4], true
}

// PricingCache provides thread-safe access to AWS pricing data.
// It stores on-demand pricing for EC2 instances and Savings Plan offering rates.
//
// The cache is populated by:
//   - On-demand prices: Pricing reconciler via bulk loading (every 24h)
//   - SP rates: SP Rates reconciler via lazy loading (every 1-2m)
//
// All keys are normalized to lowercase for case-insensitive lookups. This ensures
// consistent behavior regardless of the input casing from AWS APIs or configuration.
//
// Thread-safety: All methods are safe for concurrent access.
type PricingCache struct {
	mu sync.RWMutex

	// onDemandPrices stores on-demand pricing keyed by "region:instanceType:os"
	// All keys are lowercase for case-insensitive lookups.
	// Example key: "us-west-2:m5.xlarge:linux" → 0.192
	onDemandPrices map[string]float64

	// spRates stores actual Savings Plan rates keyed by "spArn:instanceType:region"
	// All keys are lowercase for case-insensitive lookups.
	// Example key: "arn:aws:savingsplans::123:savingsplan/abc:m5.xlarge:us-west-2" → 0.0537
	// These are PURCHASE-TIME rates that were locked in when the SP was bought.
	spRates map[string]float64

	// Metadata
	lastUpdated        time.Time
	spRatesLastUpdated time.Time
	isPopulated        bool

	// notifiers are callbacks invoked after cache updates
	// Separate mutex to prevent deadlock during notification
	notifyMu  sync.RWMutex
	notifiers []UpdateNotifier
}

// NewPricingCache creates a new empty pricing cache.
func NewPricingCache() *PricingCache {
	return &PricingCache{
		onDemandPrices: make(map[string]float64),
		spRates:        make(map[string]float64),
		isPopulated:    false,
	}
}

// GetOnDemandPrice returns the on-demand price for an instance type in a region.
// Returns the price and true if found, or 0 and false if not found.
//
// This is an O(1) lookup operation.
//
// All keys are normalized to lowercase for consistent lookups regardless of input casing.
func (c *PricingCache) GetOnDemandPrice(region, instanceType, operatingSystem string) (float64, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Normalize to lowercase for case-insensitive lookups
	key := strings.ToLower(fmt.Sprintf("%s:%s:%s", region, instanceType, operatingSystem))
	price, exists := c.onDemandPrices[key]
	return price, exists
}

// SetOnDemandPrices replaces all on-demand pricing data in the cache.
// This is typically called by the pricing reconciler after bulk-loading data.
//
// The input map should use keys in the format "region:instanceType:os".
// All keys are normalized to lowercase for case-insensitive lookups.
func (c *PricingCache) SetOnDemandPrices(prices map[string]float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Normalize all keys to lowercase for consistent lookups
	normalizedPrices := make(map[string]float64, len(prices))
	for key, price := range prices {
		normalizedPrices[strings.ToLower(key)] = price
	}
	c.onDemandPrices = normalizedPrices
	c.lastUpdated = time.Now()
	c.isPopulated = len(prices) > 0

	// Notify subscribers after releasing the write lock
	c.notifyUpdate()
}

// RegisterUpdateNotifier adds a callback to be invoked when cache data changes.
// Multiple notifiers can be registered. Callbacks are invoked in separate goroutines
// to prevent blocking cache operations.
//
// This is typically used to trigger cost recalculation when pricing data changes.
func (c *PricingCache) RegisterUpdateNotifier(fn UpdateNotifier) {
	c.notifyMu.Lock()
	defer c.notifyMu.Unlock()
	c.notifiers = append(c.notifiers, fn)
}

// notifyUpdate invokes all registered notifiers in separate goroutines.
// This method should be called after cache modifications, outside of the main mutex lock.
func (c *PricingCache) notifyUpdate() {
	c.notifyMu.RLock()
	defer c.notifyMu.RUnlock()

	for _, fn := range c.notifiers {
		// Run in goroutine to prevent blocking cache operations
		// This means notifiers must be thread-safe
		go fn()
	}
}

// GetAllOnDemandPrices returns a copy of all on-demand prices.
// This is useful for populating CalculationInput.OnDemandPrices.
func (c *PricingCache) GetAllOnDemandPrices() map[string]float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Return a copy to prevent external modifications
	copy := make(map[string]float64, len(c.onDemandPrices))
	for k, v := range c.onDemandPrices {
		copy[k] = v
	}
	return copy
}

// GetOnDemandPricesForInstances returns pricing data only for the specified
// instance types and regions. This is more efficient than GetAllOnDemandPrices
// when you only need a subset of the data.
//
// Returns a map keyed by "instanceType:region" (without OS) for easier lookup
// by the cost calculator.
func (c *PricingCache) GetOnDemandPricesForInstances(
	instances []InstanceKey,
	operatingSystem string,
) map[string]float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make(map[string]float64, len(instances))
	for _, inst := range instances {
		// Normalize to lowercase for case-insensitive lookups
		key := strings.ToLower(fmt.Sprintf("%s:%s:%s", inst.Region, inst.InstanceType, operatingSystem))
		if price, exists := c.onDemandPrices[key]; exists {
			// Return with simplified key for calculator
			resultKey := fmt.Sprintf("%s:%s", inst.InstanceType, inst.Region)
			result[resultKey] = price
		}
	}
	return result
}

// InstanceKey represents an instance type and region combination.
type InstanceKey struct {
	InstanceType string
	Region       string
}

// IsPopulated returns true if the cache has been populated with pricing data.
func (c *PricingCache) IsPopulated() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.isPopulated
}

// LastUpdated returns the timestamp of the last cache update.
func (c *PricingCache) LastUpdated() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastUpdated
}

// GetStats returns statistics about the cached pricing data.
func (c *PricingCache) GetStats() PricingStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return PricingStats{
		OnDemandPriceCount: len(c.onDemandPrices),
		LastUpdated:        c.lastUpdated,
		IsPopulated:        c.isPopulated,
		AgeHours:           time.Since(c.lastUpdated).Hours(),
	}
}

// PricingStats contains statistics about the pricing cache.
type PricingStats struct {
	OnDemandPriceCount int
	LastUpdated        time.Time
	IsPopulated        bool
	AgeHours           float64
}

// IsStale returns true if the cache hasn't been updated in more than the specified duration.
// AWS pricing typically changes monthly, so 24-48 hours is a reasonable staleness threshold.
func (c *PricingCache) IsStale(maxAge time.Duration) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.isPopulated {
		return true
	}

	return time.Since(c.lastUpdated) > maxAge
}

// GetSPRate returns the Savings Plan rate for a specific SP ARN, instance type, region, tenancy, and OS.
// Returns the rate and true if found, or 0 and false if not found.
//
// This is an O(1) lookup operation.
//
// All keys are normalized to lowercase for consistent lookups regardless of input casing.
// The tenancy parameter should be "default" (shared), "dedicated", or "host".
// The operatingSystem parameter should be normalized ("linux" or "windows").
//
// Note: The cache may contain sentinel values (SPRateNotAvailable) for rate combinations that
// were queried but don't exist. These are treated as "not found" to prevent cost calculation errors.
func (c *PricingCache) GetSPRate(spArn, instanceType, region, tenancy, operatingSystem string) (float64, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Normalize operating system to match cached values
	// Empty string (from EC2 instances with no Platform field) should normalize to "linux"
	normalizedOS := normalizeOS(operatingSystem)

	// Build cache key (lowercase, comma-separated)
	key := BuildSPRateKey(spArn, instanceType, region, tenancy, normalizedOS)
	rate, exists := c.spRates[key]

	// Sentinel values indicate this combination was queried but doesn't exist in AWS.
	// Treat these as "not found" to prevent errors in cost calculations.
	if exists && rate == SPRateNotAvailable {
		return 0, false
	}

	return rate, exists
}

// normalizeOS normalizes operating system values to a consistent format.
// This ensures EC2 Platform values and Savings Plans ProductDescription values
// map to the same normalized value for cache lookups.
//
// EC2 Platform values:
//   - ""        → "linux" (most common: ~99% of instances)
//   - "linux"   → "linux"
//   - "windows" → "windows"
//
// Savings Plans ProductDescription values:
//   - "Linux/UNIX", "Red Hat Enterprise Linux", "SUSE Linux" → "linux"
//   - "Windows" → "windows"
func normalizeOS(input string) string {
	normalized := strings.ToLower(strings.TrimSpace(input))

	// Empty string from EC2 instances = Linux
	if normalized == "" || normalized == OSLinux {
		return OSLinux
	}

	// Direct match
	if normalized == OSWindows {
		return OSWindows
	}

	// Contains checks for Savings Plans ProductDescription values
	if strings.Contains(normalized, "linux") || strings.Contains(normalized, "unix") {
		return OSLinux
	}
	if strings.Contains(normalized, "windows") {
		return OSWindows
	}

	// Default to linux (most common case)
	return OSLinux
}

// AddSPRates adds new Savings Plan rates to the cache without removing existing rates.
// This is used by the SP rates reconciler to add rates for each Savings Plan.
//
// The input map should use keys in the format "spArn,instanceType,region,tenancy,os" (comma-separated).
// Example: "arn:aws:savingsplans::123:savingsplan/abc,m5.xlarge,us-west-2,default,linux" -> 0.0537
// All keys are normalized to lowercase for case-insensitive lookups.
//
// Returns the number of NEW rates added (doesn't count updates to existing rates).
func (c *PricingCache) AddSPRates(rates map[string]float64) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	newRatesCount := 0
	for key, rate := range rates {
		normalizedKey := strings.ToLower(key)
		// Count as new if the key didn't exist before
		if _, exists := c.spRates[normalizedKey]; !exists {
			newRatesCount++
		}
		c.spRates[normalizedKey] = rate
	}

	c.spRatesLastUpdated = time.Now()

	// Notify subscribers after adding rates
	// This triggers cost recalculation when new rates are available
	c.notifyUpdate()

	return newRatesCount
}

// GetAllSPRates returns a copy of all Savings Plan rates.
// Useful for debugging or exporting cache contents.
func (c *PricingCache) GetAllSPRates() map[string]float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Return a copy to prevent external modifications
	copy := make(map[string]float64, len(c.spRates))
	for k, v := range c.spRates {
		copy[k] = v
	}
	return copy
}

// GetSPRateStats returns statistics about the SP rates cache.
func (c *PricingCache) GetSPRateStats() SPRateStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return SPRateStats{
		TotalRates:  len(c.spRates),
		LastUpdated: c.spRatesLastUpdated,
		AgeHours:    time.Since(c.spRatesLastUpdated).Hours(),
	}
}

// SPRateStats contains statistics about the SP rates cache.
type SPRateStats struct {
	TotalRates  int
	LastUpdated time.Time
	AgeHours    float64
}

// HasAnySPRate checks if ANY rate exists for the given Savings Plan ARN.
// This is more efficient than GetAllSPRates() when you just need to know if rates exist.
// Returns true if at least one rate is cached for this SP ARN.
//
// This is used by the SP Rates Reconciler to determine which SPs need rate fetching.
func (c *PricingCache) HasAnySPRate(spArn string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Normalize SP ARN to lowercase for case-insensitive comparison
	normalizedArn := strings.ToLower(spArn)
	arnPrefix := normalizedArn + spRateKeySeparator

	// Check if any key starts with this SP ARN
	// Keys are formatted as "spArn,instanceType,region,tenancy" (comma-separated)
	for key := range c.spRates {
		if strings.HasPrefix(key, arnPrefix) {
			return true
		}
	}

	return false
}

// GetMissingSPRatesForInstances returns the instance types that are missing from the cache
// for a given Savings Plan. This enables incremental rate fetching for newly discovered
// instance types without refetching rates that already exist.
//
// Parameters:
//   - spArn: The Savings Plan ARN to check
//   - instanceTypes: List of instance types to check (e.g., ["m5.xlarge", "r8g.large"])
//   - regions: List of regions to check (e.g., ["us-west-2", "us-east-1"])
//   - tenancies: List of tenancies to check (e.g., ["default", "dedicated"])
//   - operatingSystems: List of OS types to check (e.g., ["linux", "windows"])
//
// Returns three slices containing the unique values that need fetching:
//   - missingInstanceTypes: Instance types not found in cache
//   - missingRegions: Regions not found in cache
//   - missingTenancies: Tenancies not found in cache
//   - missingOS: OS types not found in cache
func (c *PricingCache) GetMissingSPRatesForInstances(
	spArn string,
	instanceTypes []string,
	regions []string,
	tenancies []string,
	operatingSystems []string,
) ([]string, []string, []string, []string) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Normalize SP ARN to lowercase for case-insensitive comparison
	normalizedArn := strings.ToLower(spArn)

	// Track which combinations exist in cache
	// We need to check all combinations to find what's missing
	type cacheKey struct {
		instanceType string
		region       string
		tenancy      string
		os           string
	}
	existingKeys := make(map[cacheKey]bool)

	// Build prefix for this SP ARN
	arnPrefix := normalizedArn + spRateKeySeparator

	// Scan cache for existing rates for this SP
	for key := range c.spRates {
		if strings.HasPrefix(key, arnPrefix) {
			// Parse the cache key to extract components
			instanceType, region, tenancy, os, ok := parseSPRateKey(key)
			if ok {
				cKey := cacheKey{
					instanceType: instanceType,
					region:       region,
					tenancy:      tenancy,
					os:           os,
				}
				existingKeys[cKey] = true
			}
		}
	}

	// Find missing instance types, regions, tenancies, and OS
	missingInstanceTypes := make(map[string]bool)
	missingRegions := make(map[string]bool)
	missingTenancies := make(map[string]bool)
	missingOS := make(map[string]bool)

	// Check each combination to see if it exists in cache
	// Normalize all keys to lowercase for case-insensitive comparison (cache keys are stored lowercase)
	for _, instanceType := range instanceTypes {
		for _, region := range regions {
			for _, tenancy := range tenancies {
				for _, os := range operatingSystems {
					key := cacheKey{
						instanceType: strings.ToLower(instanceType),
						region:       strings.ToLower(region),
						tenancy:      strings.ToLower(tenancy),
						os:           strings.ToLower(os),
					}
					if !existingKeys[key] {
						// This combination is missing
						missingInstanceTypes[instanceType] = true
						missingRegions[region] = true
						missingTenancies[tenancy] = true
						missingOS[os] = true
					}
				}
			}
		}
	}

	// Convert maps to slices
	missingInstanceTypesSlice := make([]string, 0, len(missingInstanceTypes))
	for it := range missingInstanceTypes {
		missingInstanceTypesSlice = append(missingInstanceTypesSlice, it)
	}

	missingRegionsSlice := make([]string, 0, len(missingRegions))
	for r := range missingRegions {
		missingRegionsSlice = append(missingRegionsSlice, r)
	}

	missingTenanciesSlice := make([]string, 0, len(missingTenancies))
	for t := range missingTenancies {
		missingTenanciesSlice = append(missingTenanciesSlice, t)
	}

	missingOSSlice := make([]string, 0, len(missingOS))
	for o := range missingOS {
		missingOSSlice = append(missingOSSlice, o)
	}

	return missingInstanceTypesSlice, missingRegionsSlice, missingTenanciesSlice, missingOSSlice
}
