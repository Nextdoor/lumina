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
	"testing"
	"time"

	"github.com/nextdoor/lumina/pkg/aws"
)

// TestNewPricingCache tests cache initialization.
func TestNewPricingCache(t *testing.T) {
	cache := NewPricingCache()

	if cache == nil {
		t.Fatal("expected non-nil cache")
	}

	if cache.IsPopulated() {
		t.Error("new cache should not be populated")
	}

	if cache.LastUpdated().IsZero() == false {
		t.Error("new cache should have zero last updated time")
	}

	stats := cache.GetStats()
	if stats.OnDemandPriceCount != 0 {
		t.Errorf("expected 0 prices, got %d", stats.OnDemandPriceCount)
	}
}

// TestGetSetOnDemandPrice tests basic get/set operations.
func TestGetSetOnDemandPrice(t *testing.T) {
	cache := NewPricingCache()

	// Get from empty cache
	price, exists := cache.GetOnDemandPrice("us-west-2", "m5.xlarge", "Linux")
	if exists {
		t.Error("expected price not to exist in empty cache")
	}
	if price != 0 {
		t.Errorf("expected price 0, got %.4f", price)
	}

	// Set prices
	prices := map[string]float64{
		"us-west-2:m5.xlarge:Linux":  0.192,
		"us-west-2:c5.2xlarge:Linux": 0.34,
		"us-east-1:m5.xlarge:Linux":  0.192,
	}
	cache.SetOnDemandPrices(prices)

	// Verify cache is populated
	if !cache.IsPopulated() {
		t.Error("cache should be populated after setting prices")
	}

	// Get existing price
	price, exists = cache.GetOnDemandPrice("us-west-2", "m5.xlarge", "Linux")
	if !exists {
		t.Error("expected price to exist")
	}
	if price != 0.192 {
		t.Errorf("expected price 0.192, got %.4f", price)
	}

	// Get non-existent price
	_, exists = cache.GetOnDemandPrice("eu-west-1", "m5.xlarge", "Linux")
	if exists {
		t.Error("expected price not to exist")
	}

	// Verify stats
	stats := cache.GetStats()
	if stats.OnDemandPriceCount != 3 {
		t.Errorf("expected 3 prices, got %d", stats.OnDemandPriceCount)
	}
	if !stats.IsPopulated {
		t.Error("stats should show cache is populated")
	}
	if time.Since(stats.LastUpdated) > time.Second {
		t.Error("last updated should be recent")
	}
}

// TestGetAllOnDemandPrices tests retrieving all prices.
func TestGetAllOnDemandPrices(t *testing.T) {
	cache := NewPricingCache()

	prices := map[string]float64{
		"us-west-2:m5.xlarge:Linux":  0.192,
		"us-west-2:c5.2xlarge:Linux": 0.34,
	}
	cache.SetOnDemandPrices(prices)

	allPrices := cache.GetAllOnDemandPrices()
	if len(allPrices) != 2 {
		t.Errorf("expected 2 prices, got %d", len(allPrices))
	}

	// Verify it's a copy (modifications don't affect cache)
	allPrices["test:key:os"] = 999.99
	if _, exists := cache.GetOnDemandPrice("test", "key", "os"); exists {
		t.Error("external modifications should not affect cache")
	}
}

// TestGetOnDemandPricesForInstances tests filtered price retrieval.
func TestGetOnDemandPricesForInstances(t *testing.T) {
	cache := NewPricingCache()

	prices := map[string]float64{
		"us-west-2:m5.xlarge:Linux":   0.192,
		"us-west-2:c5.2xlarge:Linux":  0.34,
		"us-east-1:m5.xlarge:Linux":   0.192,
		"us-east-1:m5.xlarge:Windows": 0.384, // Different OS
	}
	cache.SetOnDemandPrices(prices)

	instances := []OnDemandKey{
		{InstanceType: "m5.xlarge", Region: "us-west-2"},
		{InstanceType: "c5.2xlarge", Region: "us-west-2"},
		{InstanceType: "r5.large", Region: "us-west-2"}, // Not in cache
	}

	filtered := cache.GetOnDemandPricesForInstances(instances, "Linux")

	if len(filtered) != 2 {
		t.Errorf("expected 2 filtered prices, got %d", len(filtered))
	}

	// Check result key format (instanceType:region)
	if price, exists := filtered["m5.xlarge:us-west-2"]; !exists || price != 0.192 {
		t.Errorf("expected m5.xlarge price 0.192, got %.4f (exists: %v)", price, exists)
	}

	if price, exists := filtered["c5.2xlarge:us-west-2"]; !exists || price != 0.34 {
		t.Errorf("expected c5.2xlarge price 0.34, got %.4f (exists: %v)", price, exists)
	}

	// Should not include r5.large (not in cache)
	if _, exists := filtered["r5.large:us-west-2"]; exists {
		t.Error("should not include instances not in cache")
	}
}

// TestGetSpotPricesForInstances tests filtered spot price retrieval.
func TestGetSpotPricesForInstances(t *testing.T) {
	cache := NewPricingCache()

	// Set up spot prices with different zones and product descriptions
	priceMap := map[string]float64{
		"m5.xlarge:us-west-2a:Linux/UNIX":  0.05,
		"m5.xlarge:us-west-2b:Linux/UNIX":  0.06,
		"m5.xlarge:us-west-2a:Windows":     0.08,
		"c5.2xlarge:us-west-2a:Linux/UNIX": 0.10,
	}
	cache.InsertSpotPrices(spotPriceMapFromFloats(priceMap))

	// Test retrieving specific instances
	instances := []SpotPriceKey{
		{
			InstanceType:       "m5.xlarge",
			AvailabilityZone:   "us-west-2a",
			ProductDescription: "Linux/UNIX",
		},
		{
			InstanceType:       "c5.2xlarge",
			AvailabilityZone:   "us-west-2a",
			ProductDescription: "Linux/UNIX",
		},
		{
			// Not in cache
			InstanceType:       "r5.large",
			AvailabilityZone:   "us-west-2a",
			ProductDescription: "Linux/UNIX",
		},
	}

	filtered := cache.GetSpotPricesForInstances(instances)

	// Should find 2 matches
	if len(filtered) != 2 {
		t.Errorf("expected 2 filtered prices, got %d", len(filtered))
	}

	// Check result key format (instanceType:availabilityZone:productDescription)
	expectedKey1 := "m5.xlarge:us-west-2a:linux/unix"
	if price, exists := filtered[expectedKey1]; !exists || price != 0.05 {
		t.Errorf("expected m5.xlarge price 0.05, got %.4f (exists: %v)", price, exists)
	}

	expectedKey2 := "c5.2xlarge:us-west-2a:linux/unix"
	if price, exists := filtered[expectedKey2]; !exists || price != 0.10 {
		t.Errorf("expected c5.2xlarge price 0.10, got %.4f (exists: %v)", price, exists)
	}

	// Should not include r5.large (not in cache)
	notFoundKey := "r5.large:us-west-2a:linux/unix"
	if _, exists := filtered[notFoundKey]; exists {
		t.Error("should not include instances not in cache")
	}

	// Verify different zones have different prices
	instancesZoneB := []SpotPriceKey{
		{
			InstanceType:       "m5.xlarge",
			AvailabilityZone:   "us-west-2b",
			ProductDescription: "Linux/UNIX",
		},
	}
	filteredZoneB := cache.GetSpotPricesForInstances(instancesZoneB)
	expectedKeyZoneB := "m5.xlarge:us-west-2b:linux/unix"
	if price, exists := filteredZoneB[expectedKeyZoneB]; !exists || price != 0.06 {
		t.Errorf("expected m5.xlarge zone b price 0.06, got %.4f (exists: %v)", price, exists)
	}

	// Verify different product descriptions have different prices
	instancesWindows := []SpotPriceKey{
		{
			InstanceType:       "m5.xlarge",
			AvailabilityZone:   "us-west-2a",
			ProductDescription: "Windows",
		},
	}
	filteredWindows := cache.GetSpotPricesForInstances(instancesWindows)
	expectedKeyWindows := "m5.xlarge:us-west-2a:windows"
	if price, exists := filteredWindows[expectedKeyWindows]; !exists || price != 0.08 {
		t.Errorf("expected m5.xlarge windows price 0.08, got %.4f (exists: %v)", price, exists)
	}
}

// TestGetSpotPricesForInstancesWithNormalization tests that product description normalization works.
func TestGetSpotPricesForInstancesWithNormalization(t *testing.T) {
	cache := NewPricingCache()

	// Set up spot prices with AWS VPC suffix (as AWS returns them)
	priceMap := map[string]float64{
		"m5.xlarge:us-west-2a:Linux/UNIX (Amazon VPC)": 0.05,
	}
	cache.InsertSpotPrices(spotPriceMapFromFloats(priceMap))

	// Query with normalized product description (without VPC suffix)
	instances := []SpotPriceKey{
		{
			InstanceType:       "m5.xlarge",
			AvailabilityZone:   "us-west-2a",
			ProductDescription: "Linux/UNIX",
		},
	}

	filtered := cache.GetSpotPricesForInstances(instances)

	// Should find the price despite different ProductDescription formatting
	if len(filtered) != 1 {
		t.Errorf("expected 1 price with normalization, got %d", len(filtered))
	}

	expectedKey := "m5.xlarge:us-west-2a:linux/unix"
	if price, exists := filtered[expectedKey]; !exists || price != 0.05 {
		t.Errorf("expected normalized price 0.05, got %.4f (exists: %v)", price, exists)
	}
}

// TestGetSpotPricesForInstancesEmpty tests empty results.
func TestGetSpotPricesForInstancesEmpty(t *testing.T) {
	cache := NewPricingCache()

	// Empty cache
	instances := []SpotPriceKey{
		{
			InstanceType:       "m5.xlarge",
			AvailabilityZone:   "us-west-2a",
			ProductDescription: "Linux/UNIX",
		},
	}

	filtered := cache.GetSpotPricesForInstances(instances)
	if len(filtered) != 0 {
		t.Errorf("expected 0 prices from empty cache, got %d", len(filtered))
	}

	// Empty instance list
	priceMap := map[string]float64{
		"m5.xlarge:us-west-2a:Linux/UNIX": 0.05,
	}
	cache.InsertSpotPrices(spotPriceMapFromFloats(priceMap))

	filteredEmpty := cache.GetSpotPricesForInstances([]SpotPriceKey{})
	if len(filteredEmpty) != 0 {
		t.Errorf("expected 0 prices from empty instance list, got %d", len(filteredEmpty))
	}
}

// TestIsStale tests staleness detection.
func TestIsStale(t *testing.T) {
	cache := NewPricingCache()

	// Empty cache is stale
	if !cache.IsStale(24 * time.Hour) {
		t.Error("empty cache should be stale")
	}

	// Populate cache
	cache.SetOnDemandPrices(map[string]float64{
		"us-west-2:m5.xlarge:Linux": 0.192,
	})

	// Fresh cache is not stale
	if cache.IsStale(24 * time.Hour) {
		t.Error("fresh cache should not be stale")
	}

	// Simulate old cache by manipulating lastUpdate (from BaseCache)
	cache.Lock()
	cache.lastUpdate = time.Now().Add(-25 * time.Hour)
	cache.Unlock()

	// Old cache is stale
	if !cache.IsStale(24 * time.Hour) {
		t.Error("old cache should be stale")
	}
}

// TestLastUpdated tests the timestamp tracking.
func TestLastUpdated(t *testing.T) {
	cache := NewPricingCache()

	// New cache has zero time
	if !cache.LastUpdated().IsZero() {
		t.Error("new cache should have zero last updated time")
	}

	// After setting prices, should have recent timestamp
	before := time.Now()
	cache.SetOnDemandPrices(map[string]float64{
		"us-west-2:m5.xlarge:Linux": 0.192,
	})
	after := time.Now()

	lastUpdated := cache.LastUpdated()
	if lastUpdated.Before(before) || lastUpdated.After(after) {
		t.Error("last updated timestamp should be between before and after")
	}
}

// TestPricingGetStats tests statistics retrieval.
func TestPricingGetStats(t *testing.T) {
	cache := NewPricingCache()

	// Empty cache stats
	stats := cache.GetStats()
	if stats.OnDemandPriceCount != 0 {
		t.Errorf("expected 0 prices, got %d", stats.OnDemandPriceCount)
	}
	if stats.IsPopulated {
		t.Error("empty cache should not be populated")
	}

	// Populate cache
	cache.SetOnDemandPrices(map[string]float64{
		"us-west-2:m5.xlarge:Linux":  0.192,
		"us-west-2:c5.2xlarge:Linux": 0.34,
		"us-east-1:m5.xlarge:Linux":  0.192,
	})

	stats = cache.GetStats()
	if stats.OnDemandPriceCount != 3 {
		t.Errorf("expected 3 prices, got %d", stats.OnDemandPriceCount)
	}
	if !stats.IsPopulated {
		t.Error("populated cache should report as populated")
	}
	if stats.AgeHours < 0 {
		t.Errorf("age should be non-negative, got %.2f", stats.AgeHours)
	}
	if stats.AgeHours > 1 {
		t.Errorf("fresh cache should be < 1 hour old, got %.2f", stats.AgeHours)
	}
}

// TestConcurrency tests thread-safety.
func TestConcurrency(t *testing.T) {
	cache := NewPricingCache()

	// Populate initial data
	initialPrices := make(map[string]float64)
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("us-west-2:m5.%dx:Linux", i)
		initialPrices[key] = float64(i) * 0.1
	}
	cache.SetOnDemandPrices(initialPrices)

	// Run concurrent reads and writes
	// IMPORTANT: Keep iteration counts LOW to ensure tests run quickly (< 1 second)
	// The goal is to catch data races, not stress test the cache
	done := make(chan bool)

	// Concurrent readers (5 goroutines, 10 iterations each)
	for i := 0; i < 5; i++ {
		go func() {
			for j := 0; j < 10; j++ {
				cache.GetOnDemandPrice("us-west-2", "m5.xlarge", "Linux")
				cache.GetAllOnDemandPrices()
				cache.GetStats()
				cache.IsStale(24 * time.Hour)
			}
			done <- true
		}()
	}

	// Concurrent writers (2 goroutines, 5 iterations each)
	for i := 0; i < 2; i++ {
		go func(id int) {
			for j := 0; j < 5; j++ {
				newPrices := make(map[string]float64)
				for k := 0; k < 50; k++ {
					key := fmt.Sprintf("us-east-1:m5.%dx:Linux", k)
					newPrices[key] = float64(id*100+k) * 0.01
				}
				cache.SetOnDemandPrices(newPrices)
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines (5 readers + 2 writers = 7 total)
	for i := 0; i < 7; i++ {
		<-done
	}

	// Verify cache is still in valid state
	stats := cache.GetStats()
	if stats.OnDemandPriceCount < 0 {
		t.Error("concurrent access resulted in invalid state")
	}
}

// TestAddSPRates tests adding Savings Plan rates to the cache.
func TestAddSPRates(t *testing.T) {
	cache := NewPricingCache()

	// Add initial SP rates (key format: spArn,instanceType,region,tenancy,os)
	rates := map[string]float64{
		"arn:aws:savingsplans::123:savingsplan/abc,m5.xlarge,us-west-2,default,linux":   0.0537,
		"arn:aws:savingsplans::123:savingsplan/abc,m5.2xlarge,us-west-2,default,linux":  0.1074,
		"arn:aws:savingsplans::456:savingsplan/def,c5.xlarge,us-east-1,default,linux":   0.0450,
		"arn:aws:savingsplans::123:savingsplan/abc,m5.xlarge,us-west-2,dedicated,linux": 0.1708, // Same instance, different tenancy
		"arn:aws:savingsplans::123:savingsplan/abc,m5.xlarge,us-west-2,default,windows": 0.1074, // Same instance, different OS
	}

	newCount := cache.AddSPRates(rates)
	if newCount != 5 {
		t.Errorf("expected 5 new rates, got %d", newCount)
	}

	// Verify rates can be retrieved with tenancy and OS
	rate, exists := cache.GetSPRate("arn:aws:savingsplans::123:savingsplan/abc", "m5.xlarge", "us-west-2", "default", "linux")
	if !exists {
		t.Error("expected SP rate to exist for default tenancy, linux")
	}
	if rate != 0.0537 {
		t.Errorf("expected rate 0.0537, got %.4f", rate)
	}

	// Verify dedicated tenancy rate is different
	rate, exists = cache.GetSPRate("arn:aws:savingsplans::123:savingsplan/abc", "m5.xlarge", "us-west-2", "dedicated", "linux")
	if !exists {
		t.Error("expected SP rate to exist for dedicated tenancy, linux")
	}
	if rate != 0.1708 {
		t.Errorf("expected rate 0.1708, got %.4f", rate)
	}

	// Verify windows rate is different
	rate, exists = cache.GetSPRate("arn:aws:savingsplans::123:savingsplan/abc", "m5.xlarge", "us-west-2", "default", "windows")
	if !exists {
		t.Error("expected SP rate to exist for default tenancy, windows")
	}
	if rate != 0.1074 {
		t.Errorf("expected rate 0.1074, got %.4f", rate)
	}

	// Add overlapping rates (should not count as new)
	rates2 := map[string]float64{
		"arn:aws:savingsplans::123:savingsplan/abc,m5.xlarge,us-west-2,default,linux": 0.0537, // Duplicate
		"arn:aws:savingsplans::123:savingsplan/abc,r5.xlarge,us-west-2,default,linux": 0.0600, // New
	}

	newCount = cache.AddSPRates(rates2)
	if newCount != 1 {
		t.Errorf("expected 1 new rate, got %d", newCount)
	}

	stats := cache.GetSPRateStats()
	if stats.TotalRates != 6 {
		t.Errorf("expected 6 total rates, got %d", stats.TotalRates)
	}
}

// TestGetSPRate tests checking for specific SP rate existence.
func TestGetSPRate(t *testing.T) {
	cache := NewPricingCache()

	// Empty cache
	if _, exists := cache.GetSPRate("arn:aws:savingsplans::123:savingsplan/abc", "m5.xlarge", "us-west-2", "default", "linux"); exists {
		t.Error("expected rate not to exist in empty cache")
	}

	// Add rates with different tenancies and OS
	rates := map[string]float64{
		"arn:aws:savingsplans::123:savingsplan/abc,m5.xlarge,us-west-2,default,linux":   0.0537,
		"arn:aws:savingsplans::123:savingsplan/abc,m5.2xlarge,us-west-2,default,linux":  0.1074,
		"arn:aws:savingsplans::123:savingsplan/abc,m5.xlarge,us-west-2,dedicated,linux": 0.1708,
		"arn:aws:savingsplans::123:savingsplan/abc,m5.xlarge,us-west-2,default,windows": 0.1074,
	}
	cache.AddSPRates(rates)

	// Check existing rate with default tenancy, linux
	if rate, exists := cache.GetSPRate("arn:aws:savingsplans::123:savingsplan/abc", "m5.xlarge", "us-west-2", "default", "linux"); !exists || rate != 0.0537 {
		t.Error("expected rate 0.0537 to exist for default tenancy, linux")
	}

	// Check existing rate with dedicated tenancy, linux
	if rate, exists := cache.GetSPRate("arn:aws:savingsplans::123:savingsplan/abc", "m5.xlarge", "us-west-2", "dedicated", "linux"); !exists || rate != 0.1708 {
		t.Error("expected rate 0.1708 to exist for dedicated tenancy, linux")
	}

	// Check existing rate with default tenancy, windows
	if rate, exists := cache.GetSPRate("arn:aws:savingsplans::123:savingsplan/abc", "m5.xlarge", "us-west-2", "default", "windows"); !exists || rate != 0.1074 {
		t.Error("expected rate 0.1074 to exist for default tenancy, windows")
	}

	// Check non-existent rate (wrong tenancy)
	if _, exists := cache.GetSPRate("arn:aws:savingsplans::123:savingsplan/abc", "m5.xlarge", "us-west-2", "host", "linux"); exists {
		t.Error("expected rate not to exist for host tenancy")
	}

	// Check non-existent rate (wrong OS)
	if _, exists := cache.GetSPRate("arn:aws:savingsplans::123:savingsplan/abc", "m5.xlarge", "us-west-2", "dedicated", "windows"); exists {
		t.Error("expected rate not to exist for dedicated tenancy, windows")
	}

	// Check non-existent rate (different instance type)
	if _, exists := cache.GetSPRate("arn:aws:savingsplans::123:savingsplan/abc", "c5.xlarge", "us-west-2", "default", "linux"); exists {
		t.Error("expected rate not to exist")
	}

	// Check non-existent rate (different SP)
	if _, exists := cache.GetSPRate("arn:aws:savingsplans::456:savingsplan/def", "m5.xlarge", "us-west-2", "default", "linux"); exists {
		t.Error("expected rate not to exist")
	}
}

// TestHasAnySPRate tests the efficient SP rate existence check.
func TestHasAnySPRate(t *testing.T) {
	cache := NewPricingCache()

	spArn1 := "arn:aws:savingsplans::123:savingsplan/abc"
	spArn2 := "arn:aws:savingsplans::456:savingsplan/def"
	spArn3 := "arn:aws:savingsplans::789:savingsplan/ghi"

	// Empty cache - no rates exist
	if cache.HasAnySPRate(spArn1) {
		t.Error("expected no rates for SP in empty cache")
	}

	// Add rates for SP1 and SP2 (with tenancy and OS)
	rates := map[string]float64{
		spArn1 + ",m5.xlarge,us-west-2,default,linux":   0.0537,
		spArn1 + ",m5.2xlarge,us-west-2,default,linux":  0.1074,
		spArn1 + ",c5.xlarge,us-east-1,default,linux":   0.0450,
		spArn1 + ",m5.xlarge,us-west-2,dedicated,linux": 0.1708, // Same instance, different tenancy
		spArn1 + ",m5.xlarge,us-west-2,default,windows": 0.1074, // Same instance, different OS
		spArn2 + ",r5.xlarge,us-west-2,default,linux":   0.0600,
	}
	cache.AddSPRates(rates)

	// SP1 has rates
	if !cache.HasAnySPRate(spArn1) {
		t.Error("expected SP1 to have rates")
	}

	// SP2 has rates
	if !cache.HasAnySPRate(spArn2) {
		t.Error("expected SP2 to have rates")
	}

	// SP3 has no rates
	if cache.HasAnySPRate(spArn3) {
		t.Error("expected SP3 to have no rates")
	}

	// Test case-insensitive matching
	spArnUpper := "ARN:AWS:SAVINGSPLANS::123:SAVINGSPLAN/ABC"
	if !cache.HasAnySPRate(spArnUpper) {
		t.Error("expected case-insensitive match for SP1")
	}
}

// TestGetAllSPRates tests retrieving all SP rates.
func TestGetAllSPRates(t *testing.T) {
	cache := NewPricingCache()

	rates := map[string]float64{
		"arn:aws:savingsplans::123:savingsplan/abc,m5.xlarge,us-west-2,default,linux":   0.0537,
		"arn:aws:savingsplans::456:savingsplan/def,c5.xlarge,us-east-1,default,linux":   0.0450,
		"arn:aws:savingsplans::123:savingsplan/abc,m5.xlarge,us-west-2,dedicated,linux": 0.1708,
		"arn:aws:savingsplans::123:savingsplan/abc,m5.xlarge,us-west-2,default,windows": 0.1074,
	}
	cache.AddSPRates(rates)

	allRates := cache.GetAllSPRates()
	if len(allRates) != 4 {
		t.Errorf("expected 4 rates, got %d", len(allRates))
	}

	// Verify it's a copy (modifications don't affect cache)
	allRates["test,key,region,tenancy,os"] = 999.99
	if _, exists := cache.GetSPRate("test", "key", "region", "tenancy", "os"); exists {
		t.Error("external modifications should not affect cache")
	}
}

// TestSPRateStats tests SP rate statistics.
func TestSPRateStats(t *testing.T) {
	cache := NewPricingCache()

	// Empty cache stats
	stats := cache.GetSPRateStats()
	if stats.TotalRates != 0 {
		t.Errorf("expected 0 rates, got %d", stats.TotalRates)
	}

	// Add rates (with tenancy and OS)
	rates := map[string]float64{
		"arn:aws:savingsplans::123:savingsplan/abc,m5.xlarge,us-west-2,default,linux":   0.0537,
		"arn:aws:savingsplans::123:savingsplan/abc,m5.2xlarge,us-west-2,default,linux":  0.1074,
		"arn:aws:savingsplans::456:savingsplan/def,c5.xlarge,us-east-1,default,linux":   0.0450,
		"arn:aws:savingsplans::123:savingsplan/abc,m5.xlarge,us-west-2,dedicated,linux": 0.1708,
		"arn:aws:savingsplans::123:savingsplan/abc,m5.xlarge,us-west-2,default,windows": 0.1074,
	}
	cache.AddSPRates(rates)

	stats = cache.GetSPRateStats()
	if stats.TotalRates != 5 {
		t.Errorf("expected 5 rates, got %d", stats.TotalRates)
	}
	if stats.AgeHours < 0 {
		t.Errorf("age should be non-negative, got %.2f", stats.AgeHours)
	}
	if stats.AgeHours > 1 {
		t.Errorf("fresh cache should be < 1 hour old, got %.2f", stats.AgeHours)
	}
}

// TestGetMissingSPRatesForInstances_NoCachedRates tests finding missing rates
// when no rates exist in cache.
func TestGetMissingSPRatesForInstances_NoCachedRates(t *testing.T) {
	cache := NewPricingCache()

	// Check for missing rates when cache is empty
	missingInstanceTypes, missingRegions, missingTenancies, missingOS := cache.GetMissingSPRatesForInstances(
		"arn:aws:savingsplans::123:savingsplan/abc",
		[]string{"m5.xlarge", "r5.2xlarge"},
		[]string{"us-west-2", "us-east-1"},
		[]string{"default", "dedicated"},
		[]string{"linux", "windows"},
	)

	// All should be missing since cache is empty
	if len(missingInstanceTypes) != 2 {
		t.Errorf("expected 2 missing instance types, got %d", len(missingInstanceTypes))
	}
	if len(missingRegions) != 2 {
		t.Errorf("expected 2 missing regions, got %d", len(missingRegions))
	}
	if len(missingTenancies) != 2 {
		t.Errorf("expected 2 missing tenancies, got %d", len(missingTenancies))
	}
	if len(missingOS) != 2 {
		t.Errorf("expected 2 missing OS, got %d", len(missingOS))
	}
}

// TestGetMissingSPRatesForInstances_AllCached tests finding missing rates
// when all rates exist in cache.
func TestGetMissingSPRatesForInstances_AllCached(t *testing.T) {
	cache := NewPricingCache()

	// Add all combinations for m5.xlarge in us-west-2
	rates := map[string]float64{
		"arn:aws:savingsplans::123:savingsplan/abc,m5.xlarge,us-west-2,default,linux":     0.0537,
		"arn:aws:savingsplans::123:savingsplan/abc,m5.xlarge,us-west-2,default,windows":   0.1074,
		"arn:aws:savingsplans::123:savingsplan/abc,m5.xlarge,us-west-2,dedicated,linux":   0.1708,
		"arn:aws:savingsplans::123:savingsplan/abc,m5.xlarge,us-west-2,dedicated,windows": 0.2148,
	}
	cache.AddSPRates(rates)

	// Check for missing rates - should find none
	missingInstanceTypes, missingRegions, missingTenancies, missingOS := cache.GetMissingSPRatesForInstances(
		"arn:aws:savingsplans::123:savingsplan/abc",
		[]string{"m5.xlarge"},
		[]string{"us-west-2"},
		[]string{"default", "dedicated"},
		[]string{"linux", "windows"},
	)

	// Nothing should be missing
	if len(missingInstanceTypes) != 0 {
		t.Errorf("expected 0 missing instance types, got %d: %v", len(missingInstanceTypes), missingInstanceTypes)
	}
	if len(missingRegions) != 0 {
		t.Errorf("expected 0 missing regions, got %d: %v", len(missingRegions), missingRegions)
	}
	if len(missingTenancies) != 0 {
		t.Errorf("expected 0 missing tenancies, got %d: %v", len(missingTenancies), missingTenancies)
	}
	if len(missingOS) != 0 {
		t.Errorf("expected 0 missing OS, got %d: %v", len(missingOS), missingOS)
	}
}

// TestGetMissingSPRatesForInstances_PartiallyCached tests finding missing rates
// when some rates exist in cache but others are missing.
func TestGetMissingSPRatesForInstances_PartiallyCached(t *testing.T) {
	cache := NewPricingCache()

	// Add rates for m5.xlarge and r5.xlarge in us-west-2, default tenancy only
	rates := map[string]float64{
		"arn:aws:savingsplans::123:savingsplan/abc,m5.xlarge,us-west-2,default,linux":   0.0537,
		"arn:aws:savingsplans::123:savingsplan/abc,m5.xlarge,us-west-2,default,windows": 0.1074,
		"arn:aws:savingsplans::123:savingsplan/abc,r5.xlarge,us-west-2,default,linux":   0.0600,
	}
	cache.AddSPRates(rates)

	// Check for missing rates - requesting r8g.large (new instance type)
	// and dedicated tenancy (new tenancy)
	missingInstanceTypes, missingRegions, missingTenancies, missingOS := cache.GetMissingSPRatesForInstances(
		"arn:aws:savingsplans::123:savingsplan/abc",
		[]string{"m5.xlarge", "r5.xlarge", "r8g.large"}, // r8g.large is new
		[]string{"us-west-2"},
		[]string{"default", "dedicated"}, // dedicated is new
		[]string{"linux", "windows"},
	)

	// All instance types should be missing because dedicated tenancy is missing for all of them
	// r8g.large is completely missing, m5.xlarge and r5.xlarge are missing dedicated+windows combinations
	if len(missingInstanceTypes) != 3 {
		t.Errorf("expected 3 missing instance types, got %d: %v", len(missingInstanceTypes), missingInstanceTypes)
	}

	// us-west-2 should be in missing regions because dedicated tenancy is missing
	if len(missingRegions) != 1 || missingRegions[0] != "us-west-2" {
		t.Errorf("expected missing regions [us-west-2], got %v", missingRegions)
	}

	// Both tenancies should be missing:
	// - default is missing for r8g.large + windows
	// - dedicated is missing for all combinations
	if len(missingTenancies) != 2 {
		t.Errorf("expected 2 missing tenancies, got %d: %v", len(missingTenancies), missingTenancies)
	}

	// Both OS should be missing (because dedicated tenancy is missing and r8g.large is completely missing)
	if len(missingOS) != 2 {
		t.Errorf("expected 2 missing OS, got %d: %v", len(missingOS), missingOS)
	}
}

// TestGetMissingSPRatesForInstances_MultipleRegions tests finding missing rates
// across multiple regions.
func TestGetMissingSPRatesForInstances_MultipleRegions(t *testing.T) {
	cache := NewPricingCache()

	// Add rates for m5.xlarge in us-west-2 only
	rates := map[string]float64{
		"arn:aws:savingsplans::123:savingsplan/abc,m5.xlarge,us-west-2,default,linux": 0.0537,
	}
	cache.AddSPRates(rates)

	// Check for missing rates in two regions
	missingInstanceTypes, missingRegions, missingTenancies, missingOS := cache.GetMissingSPRatesForInstances(
		"arn:aws:savingsplans::123:savingsplan/abc",
		[]string{"m5.xlarge"},
		[]string{"us-west-2", "us-east-1"}, // us-east-1 is new
		[]string{"default"},
		[]string{"linux"},
	)

	// m5.xlarge should still be in missing because us-east-1 is missing
	if len(missingInstanceTypes) != 1 || missingInstanceTypes[0] != "m5.xlarge" {
		t.Errorf("expected missing instance types [m5.xlarge], got %v", missingInstanceTypes)
	}

	// us-east-1 should be missing
	if len(missingRegions) != 1 || missingRegions[0] != "us-east-1" {
		t.Errorf("expected missing regions [us-east-1], got %v", missingRegions)
	}

	// default should be in missing (for us-east-1)
	if len(missingTenancies) != 1 || missingTenancies[0] != "default" {
		t.Errorf("expected missing tenancies [default], got %v", missingTenancies)
	}

	// linux should be in missing (for us-east-1)
	if len(missingOS) != 1 || missingOS[0] != "linux" {
		t.Errorf("expected missing OS [linux], got %v", missingOS)
	}
}

// TestGetMissingSPRatesForInstances_DifferentSP tests that missing rates
// are correctly identified per Savings Plan ARN.
func TestGetMissingSPRatesForInstances_DifferentSP(t *testing.T) {
	cache := NewPricingCache()

	// Add rates for SP abc
	rates := map[string]float64{
		"arn:aws:savingsplans::123:savingsplan/abc,m5.xlarge,us-west-2,default,linux": 0.0537,
	}
	cache.AddSPRates(rates)

	// Check for missing rates for a different SP (def)
	missingInstanceTypes, _, _, _ := cache.GetMissingSPRatesForInstances(
		"arn:aws:savingsplans::123:savingsplan/def", // Different SP
		[]string{"m5.xlarge"},
		[]string{"us-west-2"},
		[]string{"default"},
		[]string{"linux"},
	)

	// Should be missing because it's a different SP
	if len(missingInstanceTypes) != 1 {
		t.Errorf("expected 1 missing instance type for different SP, got %d", len(missingInstanceTypes))
	}
}

// TestParseSPRateKey_ValidKeys tests parsing valid SP rate keys.
func TestParseSPRateKey_ValidKeys(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		wantInstance string
		wantRegion   string
		wantTenancy  string
		wantOS       string
		wantOK       bool
	}{
		{
			name:         "standard linux key",
			key:          "arn:aws:savingsplans::123:savingsplan/abc,m5.xlarge,us-west-2,default,linux",
			wantInstance: "m5.xlarge",
			wantRegion:   "us-west-2",
			wantTenancy:  "default",
			wantOS:       "linux",
			wantOK:       true,
		},
		{
			name:         "windows key",
			key:          "arn:aws:savingsplans::123:savingsplan/def,c5.2xlarge,us-east-1,dedicated,windows",
			wantInstance: "c5.2xlarge",
			wantRegion:   "us-east-1",
			wantTenancy:  "dedicated",
			wantOS:       "windows",
			wantOK:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			instance, region, tenancy, os, ok := parseSPRateKey(tt.key)
			if ok != tt.wantOK {
				t.Errorf("parseSPRateKey() ok = %v, want %v", ok, tt.wantOK)
			}
			if instance != tt.wantInstance {
				t.Errorf("parseSPRateKey() instance = %v, want %v", instance, tt.wantInstance)
			}
			if region != tt.wantRegion {
				t.Errorf("parseSPRateKey() region = %v, want %v", region, tt.wantRegion)
			}
			if tenancy != tt.wantTenancy {
				t.Errorf("parseSPRateKey() tenancy = %v, want %v", tenancy, tt.wantTenancy)
			}
			if os != tt.wantOS {
				t.Errorf("parseSPRateKey() os = %v, want %v", os, tt.wantOS)
			}
		})
	}
}

// TestParseSPRateKey_InvalidKeys tests parsing invalid SP rate keys.
func TestParseSPRateKey_InvalidKeys(t *testing.T) {
	tests := []struct {
		name string
		key  string
	}{
		{
			name: "too few parts",
			key:  "arn:aws:savingsplans::123:savingsplan/abc,m5.xlarge,us-west-2",
		},
		{
			name: "too many parts",
			key:  "arn:aws:savingsplans::123:savingsplan/abc,m5.xlarge,us-west-2,default,linux,extra",
		},
		{
			name: "empty key",
			key:  "",
		},
		{
			name: "no separators",
			key:  "invalid-key-format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, _, _, ok := parseSPRateKey(tt.key)
			if ok {
				t.Errorf("parseSPRateKey() should return ok=false for invalid key %q", tt.key)
			}
		})
	}
}

// TestRegisterUpdateNotifier tests registering and triggering update notifiers.
func TestRegisterUpdateNotifier(t *testing.T) {
	cache := NewPricingCache()

	// Track notifications with a channel
	notified := make(chan int, 1)

	// Register a notifier
	cache.RegisterUpdateNotifier(func() {
		notified <- 1
	})

	// Trigger notification by setting prices
	prices := map[string]float64{
		"us-west-2:m5.xlarge:Linux": 0.192,
	}
	cache.SetOnDemandPrices(prices)

	// Wait for notification (with timeout)
	select {
	case <-notified:
		// Success
	case <-time.After(100 * time.Millisecond):
		t.Error("expected notifier to be called after SetOnDemandPrices")
	}
}

// TestRegisterUpdateNotifier_MultipleNotifiers tests multiple registered notifiers.
func TestRegisterUpdateNotifier_MultipleNotifiers(t *testing.T) {
	cache := NewPricingCache()

	// Track notifications from multiple notifiers with channels
	notified1 := make(chan int, 1)
	notified2 := make(chan int, 1)

	cache.RegisterUpdateNotifier(func() {
		notified1 <- 1
	})
	cache.RegisterUpdateNotifier(func() {
		notified2 <- 1
	})

	// Trigger notification
	prices := map[string]float64{
		"us-west-2:m5.xlarge:Linux": 0.192,
	}
	cache.SetOnDemandPrices(prices)

	// Wait for both notifiers (with timeout)
	timeout := time.After(100 * time.Millisecond)
	notifier1Called := false
	notifier2Called := false

	for i := 0; i < 2; i++ {
		select {
		case <-notified1:
			notifier1Called = true
		case <-notified2:
			notifier2Called = true
		case <-timeout:
			if !notifier1Called {
				t.Error("expected notifier 1 to be called")
			}
			if !notifier2Called {
				t.Error("expected notifier 2 to be called")
			}
			return
		}
	}

	if !notifier1Called {
		t.Error("expected notifier 1 to be called")
	}
	if !notifier2Called {
		t.Error("expected notifier 2 to be called")
	}
}

// TestGetSPRate_WithSentinelValues tests GetSPRate with sentinel values.
func TestGetSPRate_WithSentinelValues(t *testing.T) {
	cache := NewPricingCache()

	// Add a real rate and a sentinel value
	rates := map[string]float64{
		"arn:aws:savingsplans::123:savingsplan/abc,m5.xlarge,us-west-2,default,linux":   0.0537,
		"arn:aws:savingsplans::123:savingsplan/abc,m5.xlarge,us-west-2,default,windows": SPRateNotAvailable, // Sentinel
	}
	cache.AddSPRates(rates)

	// Test getting real rate
	rate, exists := cache.GetSPRate(
		"arn:aws:savingsplans::123:savingsplan/abc",
		"m5.xlarge",
		"us-west-2",
		"default",
		"linux",
	)
	if !exists {
		t.Error("expected rate to exist")
	}
	if rate != 0.0537 {
		t.Errorf("expected rate 0.0537, got %.4f", rate)
	}

	// Test getting sentinel value - should return exists=false and rate=0
	rate, exists = cache.GetSPRate(
		"arn:aws:savingsplans::123:savingsplan/abc",
		"m5.xlarge",
		"us-west-2",
		"default",
		"windows",
	)
	if exists {
		t.Error("expected sentinel value to return exists=false")
	}
	if rate != 0 {
		t.Errorf("expected sentinel value to return rate=0, got %.4f", rate)
	}

	// Test getting non-existent rate - should also return exists=false
	rate, exists = cache.GetSPRate(
		"arn:aws:savingsplans::123:savingsplan/abc",
		"m5.xlarge",
		"us-east-1",
		"default",
		"linux",
	)
	if exists {
		t.Error("expected non-existent rate to return exists=false")
	}
	if rate != 0 {
		t.Errorf("expected non-existent rate to return rate=0, got %.4f", rate)
	}
}

// TestNormalizeOS_EdgeCases tests normalizeOS with RHEL, SUSE, and other edge cases.
func TestNormalizeOS_EdgeCases(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "RHEL variant",
			input: "Red Hat Enterprise Linux",
			want:  "linux",
		},
		{
			name:  "RHEL lowercase",
			input: "red hat",
			want:  "linux",
		},
		{
			name:  "SUSE variant",
			input: "SUSE Linux Enterprise",
			want:  "linux",
		},
		{
			name:  "suse lowercase",
			input: "suse",
			want:  "linux",
		},
		{
			name:  "Linux/UNIX AWS format",
			input: "Linux/UNIX",
			want:  "linux",
		},
		{
			name:  "Unix variant",
			input: "UNIX",
			want:  "linux",
		},
		{
			name:  "Windows with extra spaces",
			input: "  Windows  ",
			want:  "windows",
		},
		{
			name:  "Windows Server",
			input: "Windows Server 2019",
			want:  "windows",
		},
		{
			name:  "empty string",
			input: "",
			want:  "linux", // Default fallback
		},
		{
			name:  "unknown OS",
			input: "FreeBSD",
			want:  "linux", // Default fallback
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeOS(tt.input)
			if got != tt.want {
				t.Errorf("normalizeOS(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// spotPriceMapFromFloats is a test helper that converts a map of instance:az -> price
// into a map of instance:az -> aws.SpotPrice struct with current timestamps.
func spotPriceMapFromFloats(prices map[string]float64) map[string]aws.SpotPrice {
	spotPrices := make(map[string]aws.SpotPrice, len(prices))
	now := time.Now()

	for key, price := range prices {
		// Parse key format "instanceType:availabilityZone:productDescription"
		// For backward compatibility, also support "instanceType:availabilityZone" (defaults to Linux/UNIX)
		parts := strings.Split(key, ":")
		instanceType := ""
		az := ""
		productDescription := "Linux/UNIX" // Default

		if len(parts) >= 2 {
			instanceType = parts[0]
			az = parts[1]
		}
		if len(parts) >= 3 {
			productDescription = parts[2]
		}

		spotPrices[key] = aws.SpotPrice{
			InstanceType:       instanceType,
			AvailabilityZone:   az,
			SpotPrice:          price,
			Timestamp:          now, // When AWS recorded the price
			FetchedAt:          now, // When we fetched it
			ProductDescription: productDescription,
		}
	}

	return spotPrices
}

// TestGetSetSpotPrice tests basic spot price get/set operations.
func TestGetSetSpotPrice(t *testing.T) {
	cache := NewPricingCache()

	// Get from empty cache
	price, exists := cache.GetSpotPrice("m5.xlarge", "us-west-2a", "Linux/UNIX")
	if exists {
		t.Error("expected price not to exist in empty cache")
	}
	if price != 0 {
		t.Errorf("expected price 0, got %.4f", price)
	}

	// Set spot prices
	priceMap := map[string]float64{
		"m5.xlarge:us-west-2a":  0.0537,
		"c5.2xlarge:us-west-2b": 0.068,
		"m5.xlarge:us-east-1a":  0.0521,
	}
	cache.InsertSpotPrices(spotPriceMapFromFloats(priceMap))

	// Verify cache is populated
	if !cache.SpotIsPopulated() {
		t.Error("cache should be populated after setting spot prices")
	}

	// Get existing price
	price, exists = cache.GetSpotPrice("m5.xlarge", "us-west-2a", "Linux/UNIX")
	if !exists {
		t.Error("expected price to exist")
	}
	if price != 0.0537 {
		t.Errorf("expected price 0.0537, got %.4f", price)
	}

	// Get non-existent price
	_, exists = cache.GetSpotPrice("r5.large", "eu-west-1a", "Linux/UNIX")
	if exists {
		t.Error("expected price not to exist")
	}

	// Verify stats
	stats := cache.GetSpotStats()
	if stats.SpotPriceCount != 3 {
		t.Errorf("expected 3 prices, got %d", stats.SpotPriceCount)
	}
	if !stats.IsPopulated {
		t.Error("stats should show cache is populated")
	}
	if time.Since(stats.LastUpdated) > time.Second {
		t.Error("last updated should be recent")
	}
}

// TestGetAllSpotPrices tests retrieving all spot prices.
func TestGetAllSpotPrices(t *testing.T) {
	cache := NewPricingCache()

	priceMap := map[string]float64{
		"m5.xlarge:us-west-2a":  0.0537,
		"c5.2xlarge:us-west-2b": 0.068,
	}
	cache.InsertSpotPrices(spotPriceMapFromFloats(priceMap))

	allPrices := cache.GetAllSpotPrices()
	if len(allPrices) != 2 {
		t.Errorf("expected 2 prices, got %d", len(allPrices))
	}

	// Verify it's a copy (modifications don't affect cache)
	allPrices["test:key:linux/unix"] = 999.99
	if _, exists := cache.GetSpotPrice("test", "key", "Linux/UNIX"); exists {
		t.Error("external modifications should not affect cache")
	}
}

// TestSpotIsPopulated tests spot cache population tracking.
func TestSpotIsPopulated(t *testing.T) {
	cache := NewPricingCache()

	// Empty cache is not populated
	if cache.SpotIsPopulated() {
		t.Error("empty cache should not be populated")
	}

	// Populate cache
	cache.InsertSpotPrices(spotPriceMapFromFloats(map[string]float64{
		"m5.xlarge:us-west-2a": 0.0537,
	}))

	// Cache is now populated
	if !cache.SpotIsPopulated() {
		t.Error("cache should be populated after setting prices")
	}
}

// TestSpotStats tests spot pricing statistics.
func TestSpotStats(t *testing.T) {
	cache := NewPricingCache()

	// Empty cache stats
	stats := cache.GetSpotStats()
	if stats.SpotPriceCount != 0 {
		t.Errorf("expected 0 prices, got %d", stats.SpotPriceCount)
	}
	if stats.IsPopulated {
		t.Error("empty cache should not be populated")
	}

	// Populate cache
	cache.InsertSpotPrices(spotPriceMapFromFloats(map[string]float64{
		"m5.xlarge:us-west-2a":  0.0537,
		"c5.2xlarge:us-west-2b": 0.068,
		"m5.xlarge:us-east-1a":  0.0521,
	}))

	stats = cache.GetSpotStats()
	if stats.SpotPriceCount != 3 {
		t.Errorf("expected 3 prices, got %d", stats.SpotPriceCount)
	}
	if !stats.IsPopulated {
		t.Error("populated cache should report as populated")
	}
	if stats.AgeHours < 0 {
		t.Errorf("age should be non-negative, got %.2f", stats.AgeHours)
	}
	if stats.AgeHours > 1 {
		t.Errorf("fresh cache should be < 1 hour old, got %.2f", stats.AgeHours)
	}
}

// TestSpotPriceCaseInsensitive tests case-insensitive spot price lookups.
func TestSpotPriceCaseInsensitive(t *testing.T) {
	cache := NewPricingCache()

	// Set prices with mixed case
	cache.InsertSpotPrices(spotPriceMapFromFloats(map[string]float64{
		"M5.Xlarge:US-WEST-2A": 0.0537,
	}))

	// Should find price with different casing
	price, exists := cache.GetSpotPrice("m5.xlarge", "us-west-2a", "Linux/UNIX")
	if !exists {
		t.Error("expected price to exist (case-insensitive)")
	}
	if price != 0.0537 {
		t.Errorf("expected price 0.0537, got %.4f", price)
	}

	// Also test uppercase query
	price, exists = cache.GetSpotPrice("M5.XLARGE", "US-WEST-2A", "LINUX/UNIX")
	if !exists {
		t.Error("expected price to exist (uppercase query)")
	}
	if price != 0.0537 {
		t.Errorf("expected price 0.0537, got %.4f", price)
	}
}

// TestInsertSpotPrices_EmptyMap tests that inserting an empty map doesn't affect existing prices.
func TestInsertSpotPrices_EmptyMap(t *testing.T) {
	cache := NewPricingCache()

	// Populate first
	cache.InsertSpotPrices(spotPriceMapFromFloats(map[string]float64{
		"m5.xlarge:us-west-2a": 0.0537,
	}))

	if !cache.SpotIsPopulated() {
		t.Error("cache should be populated")
	}

	// Insert empty map (should not clear cache - InsertSpotPrices merges, doesn't replace)
	newCount := cache.InsertSpotPrices(spotPriceMapFromFloats(map[string]float64{}))

	// Cache should still be populated with original price
	if !cache.SpotIsPopulated() {
		t.Error("cache should still be populated after inserting empty map")
	}

	if newCount != 0 {
		t.Errorf("expected 0 new prices from empty insert, got %d", newCount)
	}

	stats := cache.GetSpotStats()
	if stats.SpotPriceCount != 1 {
		t.Errorf("expected 1 price (original), got %d", stats.SpotPriceCount)
	}
}

// TestSpotPriceNotification tests that spot price updates trigger notifications.
func TestSpotPriceNotification(t *testing.T) {
	cache := NewPricingCache()

	// Track notifications with a channel
	notified := make(chan int, 1)

	// Register a notifier
	cache.RegisterUpdateNotifier(func() {
		notified <- 1
	})

	// Trigger notification by setting spot prices
	priceMap := map[string]float64{
		"m5.xlarge:us-west-2a": 0.0537,
	}
	cache.InsertSpotPrices(spotPriceMapFromFloats(priceMap))

	// Wait for notification (with timeout)
	select {
	case <-notified:
		// Success
	case <-time.After(100 * time.Millisecond):
		t.Error("expected notifier to be called after InsertSpotPrices")
	}
}

// TestInsertSpotPrices_Merge tests that InsertSpotPrices merges with existing prices.
func TestInsertSpotPrices_Merge(t *testing.T) {
	cache := NewPricingCache()

	// Insert initial prices
	initial := map[string]float64{
		"m5.xlarge:us-west-2a":  0.0537,
		"c5.2xlarge:us-west-2b": 0.068,
	}
	newCount := cache.InsertSpotPrices(spotPriceMapFromFloats(initial))
	if newCount != 2 {
		t.Errorf("expected 2 new prices, got %d", newCount)
	}

	// Insert more prices (some new, some updates)
	additional := map[string]float64{
		"m5.xlarge:us-west-2a": 0.0540, // Update existing
		"r5.large:us-west-2a":  0.0300, // New
	}
	newCount = cache.InsertSpotPrices(spotPriceMapFromFloats(additional))
	if newCount != 1 {
		t.Errorf("expected 1 new price, got %d", newCount)
	}

	// Verify all 3 prices exist
	stats := cache.GetSpotStats()
	if stats.SpotPriceCount != 3 {
		t.Errorf("expected 3 total prices, got %d", stats.SpotPriceCount)
	}

	// Verify the updated price
	price, exists := cache.GetSpotPrice("m5.xlarge", "us-west-2a", "Linux/UNIX")
	if !exists {
		t.Error("expected updated price to exist")
	}
	if price != 0.0540 {
		t.Errorf("expected updated price 0.0540, got %.4f", price)
	}

	// Verify the original price that wasn't updated
	price, exists = cache.GetSpotPrice("c5.2xlarge", "us-west-2b", "Linux/UNIX")
	if !exists {
		t.Error("expected original price to still exist")
	}
	if price != 0.068 {
		t.Errorf("expected original price 0.068, got %.4f", price)
	}
}

// TestDeleteSpotPrice tests removing spot prices from the cache.
func TestDeleteSpotPrice(t *testing.T) {
	cache := NewPricingCache()

	// Populate cache
	prices := map[string]float64{
		"m5.xlarge:us-west-2a":  0.0537,
		"c5.2xlarge:us-west-2b": 0.068,
		"r5.large:us-east-1a":   0.0300,
	}
	cache.InsertSpotPrices(spotPriceMapFromFloats(prices))

	// Delete existing price
	deleted := cache.DeleteSpotPrice("m5.xlarge", "us-west-2a", "Linux/UNIX")
	if !deleted {
		t.Error("expected delete to return true for existing price")
	}

	// Verify it's gone
	_, exists := cache.GetSpotPrice("m5.xlarge", "us-west-2a", "Linux/UNIX")
	if exists {
		t.Error("expected price to be deleted")
	}

	// Verify other prices still exist
	stats := cache.GetSpotStats()
	if stats.SpotPriceCount != 2 {
		t.Errorf("expected 2 remaining prices, got %d", stats.SpotPriceCount)
	}

	// Try to delete non-existent price
	deleted = cache.DeleteSpotPrice("t3.micro", "us-west-1a", "Linux/UNIX")
	if deleted {
		t.Error("expected delete to return false for non-existent price")
	}

	// Delete all remaining prices
	cache.DeleteSpotPrice("c5.2xlarge", "us-west-2b", "Linux/UNIX")
	cache.DeleteSpotPrice("r5.large", "us-east-1a", "Linux/UNIX")

	// Cache should no longer be populated
	if cache.SpotIsPopulated() {
		t.Error("cache should not be populated after deleting all prices")
	}
}

// TestDeleteSpotPrice_CaseInsensitive tests case-insensitive deletion.
func TestDeleteSpotPrice_CaseInsensitive(t *testing.T) {
	cache := NewPricingCache()

	// Insert with lowercase
	cache.InsertSpotPrices(spotPriceMapFromFloats(map[string]float64{
		"m5.xlarge:us-west-2a": 0.0537,
	}))

	// Delete with different casing
	deleted := cache.DeleteSpotPrice("M5.XLARGE", "US-WEST-2A", "LINUX/UNIX")
	if !deleted {
		t.Error("expected case-insensitive delete to work")
	}

	// Verify it's gone
	_, exists := cache.GetSpotPrice("m5.xlarge", "us-west-2a", "Linux/UNIX")
	if exists {
		t.Error("expected price to be deleted")
	}
}
