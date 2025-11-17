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
	"testing"
	"time"
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

	instances := []InstanceKey{
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

	// Simulate old cache by manipulating lastUpdated
	cache.mu.Lock()
	cache.lastUpdated = time.Now().Add(-25 * time.Hour)
	cache.mu.Unlock()

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
	done := make(chan bool)

	// Concurrent readers
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				cache.GetOnDemandPrice("us-west-2", "m5.xlarge", "Linux")
				cache.GetAllOnDemandPrices()
				cache.GetStats()
				cache.IsStale(24 * time.Hour)
			}
			done <- true
		}()
	}

	// Concurrent writers
	for i := 0; i < 3; i++ {
		go func(id int) {
			for j := 0; j < 10; j++ {
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

	// Wait for all goroutines
	for i := 0; i < 13; i++ {
		<-done
	}

	// Verify cache is still in valid state
	stats := cache.GetStats()
	if stats.OnDemandPriceCount < 0 {
		t.Error("concurrent access resulted in invalid state")
	}
}
