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
	"strings"
	"time"

	"github.com/nextdoor/lumina/pkg/aws"
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
	return BuildKey(spRateKeySeparator, spArn, instanceType, region, tenancy, os)
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
//
// PricingCache embeds BaseCache to provide common infrastructure (thread-safety,
// notifications, timestamps). This eliminates ~50 lines of boilerplate code.
type PricingCache struct {
	BaseCache // Provides: Lock/RLock, RegisterUpdateNotifier, NotifyUpdate, MarkUpdated, GetLastUpdate, etc.

	// onDemandPrices stores on-demand pricing keyed by "region:instanceType:os"
	// All keys are lowercase for case-insensitive lookups.
	// Example key: "us-west-2:m5.xlarge:linux" → 0.192
	onDemandPrices map[string]float64

	// spRates stores actual Savings Plan rates keyed by "spArn:instanceType:region"
	// All keys are lowercase for case-insensitive lookups.
	// Example key: "arn:aws:savingsplans::123:savingsplan/abc:m5.xlarge:us-west-2" → 0.0537
	// These are PURCHASE-TIME rates that were locked in when the SP was bought.
	spRates map[string]float64

	// spotPrices stores current spot pricing keyed by "instanceType:availabilityZone:productDescription"
	// All keys are lowercase for case-insensitive lookups.
	// Example key: "m5.xlarge:us-west-2a:linux/unix" → aws.SpotPrice{...}
	// Note: Spot prices vary by ProductDescription (Linux/UNIX vs Windows) and are per-AZ, not per-region.
	// ProductDescription is required because Windows instances have different spot prices than Linux.
	// We store the full SpotPrice struct to preserve individual timestamps per price.
	spotPrices map[string]aws.SpotPrice

	// Domain-specific metadata
	// spRatesLastUpdated tracks when SP rates were last updated (separate from on-demand prices)
	spRatesLastUpdated time.Time
	// spotLastUpdated tracks when spot prices were last updated (separate from on-demand/SP prices)
	spotLastUpdated time.Time
	// isPopulated tracks if on-demand prices have been loaded (separate from BaseCache's lastUpdate)
	isPopulated bool
	// spotIsPopulated tracks if spot prices have been loaded
	spotIsPopulated bool
}

// NewPricingCache creates a new empty pricing cache.
func NewPricingCache() *PricingCache {
	return &PricingCache{
		BaseCache:       NewBaseCache(),
		onDemandPrices:  make(map[string]float64),
		spRates:         make(map[string]float64),
		spotPrices:      make(map[string]aws.SpotPrice),
		isPopulated:     false,
		spotIsPopulated: false,
	}
}

// GetOnDemandPrice returns the on-demand price for an instance type in a region.
// Returns the price and true if found, or 0 and false if not found.
//
// This is an O(1) lookup operation.
//
// All keys are normalized to lowercase for consistent lookups regardless of input casing.
func (c *PricingCache) GetOnDemandPrice(region, instanceType, operatingSystem string) (float64, bool) {
	c.RLock() // From BaseCache
	defer c.RUnlock()

	key := BuildKey(":", region, instanceType, operatingSystem)
	price, exists := c.onDemandPrices[key]
	return price, exists
}

// SetOnDemandPrices replaces all on-demand pricing data in the cache.
// This is typically called by the pricing reconciler after bulk-loading data.
//
// The input map should use keys in the format "region:instanceType:os".
// All keys are normalized to lowercase for case-insensitive lookups.
func (c *PricingCache) SetOnDemandPrices(prices map[string]float64) {
	c.Lock() // From BaseCache
	// Normalize all keys to lowercase for consistent lookups
	normalizedPrices := make(map[string]float64, len(prices))
	for key, price := range prices {
		normalizedPrices[strings.ToLower(key)] = price
	}
	c.onDemandPrices = normalizedPrices
	c.isPopulated = len(prices) > 0
	c.MarkUpdated() // From BaseCache
	c.Unlock()

	// Notify subscribers AFTER releasing the write lock to prevent deadlock
	c.NotifyUpdate() // From BaseCache
}

// RegisterUpdateNotifier is inherited from BaseCache.
// Multiple notifiers can be registered. Callbacks are invoked in separate goroutines
// to prevent blocking cache operations.
//
// This is typically used to trigger cost recalculation when pricing data changes.

// GetAllOnDemandPrices returns a copy of all on-demand prices.
// This is useful for populating CalculationInput.OnDemandPrices.
func (c *PricingCache) GetAllOnDemandPrices() map[string]float64 {
	c.RLock() // From BaseCache
	defer c.RUnlock()

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
	c.RLock() // From BaseCache
	defer c.RUnlock()

	result := make(map[string]float64, len(instances))
	for _, inst := range instances {
		key := BuildKey(":", inst.Region, inst.InstanceType, operatingSystem)
		if price, exists := c.onDemandPrices[key]; exists {
			// Return with simplified key for calculator
			resultKey := BuildKey(":", inst.InstanceType, inst.Region)
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
	c.RLock() // From BaseCache
	defer c.RUnlock()
	return c.isPopulated
}

// LastUpdated returns the timestamp of the last cache update.
// This is an alias for BaseCache.GetLastUpdate() for backward compatibility.
func (c *PricingCache) LastUpdated() time.Time {
	return c.GetLastUpdate() // From BaseCache
}

// GetStats returns statistics about the cached pricing data.
func (c *PricingCache) GetStats() PricingStats {
	c.RLock() // From BaseCache
	defer c.RUnlock()

	// Access lastUpdate directly to avoid double-locking (GetLastUpdate acquires its own lock)
	lastUpdate := c.lastUpdate
	return PricingStats{
		OnDemandPriceCount: len(c.onDemandPrices),
		LastUpdated:        lastUpdate,
		IsPopulated:        c.isPopulated,
		AgeHours:           time.Since(lastUpdate).Hours(),
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
// This overrides BaseCache.IsStale() to also check the isPopulated flag.
func (c *PricingCache) IsStale(maxAge time.Duration) bool {
	c.RLock() // From BaseCache
	defer c.RUnlock()

	if !c.isPopulated {
		return true
	}

	// Access lastUpdate directly to avoid double-locking (BaseCache.IsStale acquires its own lock)
	if c.lastUpdate.IsZero() {
		return true
	}
	return time.Since(c.lastUpdate) > maxAge
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
	c.RLock() // From BaseCache
	defer c.RUnlock()

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

// normalizeProductDescription normalizes AWS spot price ProductDescription values
// by stripping common suffixes and mapping to canonical values.
//
// AWS returns ProductDescription values like:
//   - "Linux/UNIX (Amazon VPC)" → "linux/unix"
//   - "Windows (Amazon VPC)"    → "windows"
//   - "Linux/UNIX"              → "linux/unix"
//   - "Windows"                 → "windows"
//
// This ensures cache keys match regardless of whether AWS includes the VPC suffix.
func normalizeProductDescription(input string) string {
	// Lowercase and trim whitespace
	normalized := strings.ToLower(strings.TrimSpace(input))

	// Strip common suffixes that AWS adds
	// These suffixes don't affect pricing - they're just metadata
	normalized = strings.TrimSuffix(normalized, " (amazon vpc)")
	normalized = strings.TrimSuffix(normalized, " (amazon)")

	return normalized
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
	c.Lock() // From BaseCache
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
	c.Unlock()

	// Notify subscribers AFTER releasing the write lock to prevent deadlock
	// This triggers cost recalculation when new rates are available
	c.NotifyUpdate() // From BaseCache

	return newRatesCount
}

// GetAllSPRates returns a copy of all Savings Plan rates.
// Useful for debugging or exporting cache contents.
func (c *PricingCache) GetAllSPRates() map[string]float64 {
	c.RLock() // From BaseCache
	defer c.RUnlock()

	// Return a copy to prevent external modifications
	copy := make(map[string]float64, len(c.spRates))
	for k, v := range c.spRates {
		copy[k] = v
	}
	return copy
}

// GetSPRateStats returns statistics about the SP rates cache.
func (c *PricingCache) GetSPRateStats() SPRateStats {
	c.RLock() // From BaseCache
	defer c.RUnlock()

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
	c.RLock() // From BaseCache
	defer c.RUnlock()

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
	c.RLock() // From BaseCache
	defer c.RUnlock()

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

// GetSpotPrice returns the current spot price for an instance type in an availability zone with a specific product description.
// Returns the price and true if found, or 0 and false if not found.
//
// This is an O(1) lookup operation.
//
// All keys are normalized to lowercase for consistent lookups regardless of input casing.
// The productDescription parameter should match AWS's ProductDescription values (e.g., "Linux/UNIX", "Windows").
//
// Note: Spot prices are per-AZ (not per-region) and vary by ProductDescription, so you must provide:
//   - Full AZ name (e.g., "us-west-2a" not just "us-west-2")
//   - ProductDescription (e.g., "Linux/UNIX" or "Windows") - different OSes have different spot prices
func (c *PricingCache) GetSpotPrice(instanceType, availabilityZone, productDescription string) (float64, bool) {
	c.RLock() // From BaseCache
	defer c.RUnlock()

	// Normalize product description to match cached keys
	normalizedPD := normalizeProductDescription(productDescription)

	key := BuildKey(":", instanceType, availabilityZone, normalizedPD)
	spotPrice, exists := c.spotPrices[key]
	if !exists {
		return 0, false
	}
	return spotPrice.SpotPrice, true
}

// InsertSpotPrices adds or updates spot prices in the cache without removing existing prices.
// This is typically called by the spot pricing reconciler after querying AWS spot price history.
//
// The input map should use keys in the format "instanceType:availabilityZone:productDescription".
// Example: "m5.xlarge:us-west-2a:linux/unix" → aws.SpotPrice{SpotPrice: 0.0537, ProductDescription: "Linux/UNIX", ...}
// All keys are normalized to lowercase for case-insensitive lookups.
//
// IMPORTANT: The SpotPrice struct must have ProductDescription field populated, as it's used to build the cache key.
// Windows and Linux instances have different spot prices, so ProductDescription is required to differentiate them.
//
// This method merges new prices with existing ones - it does not replace the entire cache.
// To remove prices, use DeleteSpotPrice.
//
// Returns the number of prices that were newly added (not counting updates to existing prices).
func (c *PricingCache) InsertSpotPrices(prices map[string]aws.SpotPrice) int {
	c.Lock() // From BaseCache

	// Initialize cache if it doesn't exist yet
	if c.spotPrices == nil {
		c.spotPrices = make(map[string]aws.SpotPrice)
	}

	// Track how many are new vs updates
	newCount := 0

	// Insert/update each price
	// Note: The key passed in should already include ProductDescription, but we rebuild it
	// from the SpotPrice struct to ensure consistency (the struct is the source of truth)
	for _, price := range prices {
		// Build key from SpotPrice struct fields to ensure it includes ProductDescription
		// Format: "instanceType:availabilityZone:productDescription" (all lowercase)
		normalizedPD := normalizeProductDescription(price.ProductDescription)
		key := BuildKey(":", price.InstanceType, price.AvailabilityZone, normalizedPD)

		if _, exists := c.spotPrices[key]; !exists {
			newCount++
		}
		c.spotPrices[key] = price
	}

	c.spotIsPopulated = len(c.spotPrices) > 0
	c.spotLastUpdated = time.Now()
	c.Unlock()

	// Notify subscribers AFTER releasing the write lock to prevent deadlock
	c.NotifyUpdate() // From BaseCache

	return newCount
}

// DeleteSpotPrice removes a spot price from the cache by its key.
// This is typically called when a spot price becomes permanently unavailable
// (e.g., instance type retired, availability zone no longer exists).
//
// The method takes separate instanceType, availabilityZone, and productDescription parameters
// and constructs the cache key internally in the format "instanceType:availabilityZone:productDescription".
// Example: DeleteSpotPrice("m5.xlarge", "us-west-2a", "Linux/UNIX") removes key "m5.xlarge:us-west-2a:linux/unix"
//
// Lookup is case-insensitive - DeleteSpotPrice("M5.XLARGE", "US-WEST-2A", "LINUX/UNIX") will find
// and delete a price stored as "m5.xlarge:us-west-2a:linux/unix".
//
// Returns:
//   - true: Price was found and successfully deleted from cache
//   - false: Price was not in cache (no-op)
//
// Thread-safety: This method uses write locks to prevent concurrent modification.
func (c *PricingCache) DeleteSpotPrice(instanceType, availabilityZone, productDescription string) bool {
	c.Lock() // From BaseCache - exclusive write access
	defer c.Unlock()

	// Build normalized key (case-insensitive) from components
	// Format: "instanceType:availabilityZone:productDescription" all lowercase
	// Example: "m5.xlarge:us-west-2a:linux/unix"
	normalizedPD := normalizeProductDescription(productDescription)
	key := BuildKey(":", instanceType, availabilityZone, normalizedPD)

	// Check if price exists before attempting deletion
	if _, exists := c.spotPrices[key]; exists {
		// Remove from cache
		delete(c.spotPrices, key)

		// Update populated flag - cache is no longer populated if we deleted the last price
		// This flag is used by SpotIsPopulated() to indicate if cache has any data
		c.spotIsPopulated = len(c.spotPrices) > 0

		return true
	}

	// Price didn't exist - nothing to delete
	return false
}

// GetAllSpotPrices returns a copy of all spot prices (price values only, without timestamps).
// This is useful for cost calculations that only need the price values.
// For debugging with timestamps, use GetAllSpotPricesWithTimestamps().
func (c *PricingCache) GetAllSpotPrices() map[string]float64 {
	c.RLock() // From BaseCache
	defer c.RUnlock()

	// Return a copy with just price values to prevent external modifications
	copy := make(map[string]float64, len(c.spotPrices))
	for k, v := range c.spotPrices {
		copy[k] = v.SpotPrice
	}
	return copy
}

// GetAllSpotPricesWithTimestamps returns a copy of all spot prices with full metadata including timestamps.
// This is useful for debugging and showing data freshness in debug endpoints.
func (c *PricingCache) GetAllSpotPricesWithTimestamps() map[string]aws.SpotPrice {
	c.RLock() // From BaseCache
	defer c.RUnlock()

	// Return a copy to prevent external modifications
	copy := make(map[string]aws.SpotPrice, len(c.spotPrices))
	for k, v := range c.spotPrices {
		copy[k] = v
	}
	return copy
}

// GetSpotPricesForInstances returns spot pricing for specific instances.
// This is more efficient than GetAllSpotPrices when you only need a subset of the data.
//
// Returns a map keyed by "instanceType:availabilityZone" for easier lookup
// by the cost calculator.
func (c *PricingCache) GetSpotPricesForInstances(instances []InstanceKey) map[string]float64 {
	c.RLock() // From BaseCache
	defer c.RUnlock()

	result := make(map[string]float64, len(instances))
	for _, inst := range instances {
		// Note: InstanceKey needs an AvailabilityZone field for spot pricing
		key := BuildKey(":", inst.InstanceType, inst.Region)
		if spotPrice, exists := c.spotPrices[key]; exists {
			// Return with key format expected by calculator
			resultKey := BuildKey(":", inst.InstanceType, inst.Region)
			result[resultKey] = spotPrice.SpotPrice
		}
	}
	return result
}

// SpotIsPopulated returns true if the cache has been populated with spot pricing data.
func (c *PricingCache) SpotIsPopulated() bool {
	c.RLock() // From BaseCache
	defer c.RUnlock()
	return c.spotIsPopulated
}

// GetSpotStats returns statistics about the spot pricing cache.
func (c *PricingCache) GetSpotStats() SpotStats {
	c.RLock() // From BaseCache
	defer c.RUnlock()

	return SpotStats{
		SpotPriceCount: len(c.spotPrices),
		LastUpdated:    c.spotLastUpdated,
		IsPopulated:    c.spotIsPopulated,
		AgeHours:       time.Since(c.spotLastUpdated).Hours(),
	}
}

// SpotStats contains statistics about the spot pricing cache.
type SpotStats struct {
	SpotPriceCount int
	LastUpdated    time.Time
	IsPopulated    bool
	AgeHours       float64
}
