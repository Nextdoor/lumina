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
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/nextdoor/lumina/internal/cache"
	"github.com/nextdoor/lumina/pkg/aws"
	"github.com/nextdoor/lumina/pkg/config"
	"github.com/nextdoor/lumina/pkg/metrics"
)

// TestPricingReconciler_Reconcile_Success tests successful pricing reconciliation.
func TestPricingReconciler_Reconcile_Success(t *testing.T) {
	// Create mock client
	mockClient := aws.NewMockClient()
	ctx := context.Background()

	// Setup pricing client with test data
	pricingClient := mockClient.Pricing(ctx)
	mockPricing := pricingClient.(*aws.MockPricingClient)

	// Add some test prices
	mockPricing.SetOnDemandPrice("us-west-2", "m5.large", "Linux", 0.096)
	mockPricing.SetOnDemandPrice("us-west-2", "m5.xlarge", "Linux", 0.192)
	mockPricing.SetOnDemandPrice("us-east-1", "m5.large", "Linux", 0.096)
	mockPricing.SetOnDemandPrice("us-east-1", "m5.xlarge", "Linux", 0.192)
	mockPricing.SetOnDemandPrice("us-west-2", "m5.large", "Windows", 0.192)
	mockPricing.SetOnDemandPrice("us-west-2", "m5.xlarge", "Windows", 0.384)

	// Create config
	cfg := &config.Config{
		DefaultRegion: "us-east-1",
		Regions:       []string{"us-west-2", "us-east-1"},
	}

	// Create cache and metrics
	pricingCache := cache.NewPricingCache()
	m := metrics.NewMetrics(prometheus.NewRegistry())

	// Create reconciler
	reconciler := &PricingReconciler{
		AWSClient:        mockClient,
		Config:           cfg,
		Cache:            pricingCache,
		Metrics:          m,
		Log:              logr.Discard(),
		Regions:          []string{"us-west-2", "us-east-1"},
		OperatingSystems: []string{"Linux", "Windows"},
	}

	// Run reconciliation
	result, err := reconciler.Reconcile(ctx, ctrl.Request{})

	// Verify no error
	require.NoError(t, err)

	// Verify requeue after 24 hours (default)
	assert.Equal(t, 24*time.Hour, result.RequeueAfter)

	// Verify cache was populated
	stats := pricingCache.GetStats()
	assert.Equal(t, 6, stats.OnDemandPriceCount, "should have 6 prices")
	assert.True(t, stats.IsPopulated, "cache should be populated")
	assert.True(t, stats.AgeHours < 0.1, "cache should be fresh")

	// Verify specific prices
	price, exists := pricingCache.GetOnDemandPrice("us-west-2", "m5.large", "Linux")
	require.True(t, exists, "should have us-west-2 m5.large Linux price")
	assert.Equal(t, 0.096, price)

	price, exists = pricingCache.GetOnDemandPrice("us-west-2", "m5.xlarge", "Windows")
	require.True(t, exists, "should have us-west-2 m5.xlarge Windows price")
	assert.Equal(t, 0.384, price)
}

// TestPricingReconciler_Reconcile_CustomInterval tests custom reconciliation interval.
func TestPricingReconciler_Reconcile_CustomInterval(t *testing.T) {
	// Create mock client
	mockClient := aws.NewMockClient()
	ctx := context.Background()

	// Setup pricing client with test data
	pricingClient := mockClient.Pricing(ctx)
	mockPricing := pricingClient.(*aws.MockPricingClient)
	mockPricing.SetOnDemandPrice("us-west-2", "m5.large", "Linux", 0.096)

	// Create config with custom interval
	cfg := &config.Config{
		DefaultRegion: "us-east-1",
		Regions:       []string{"us-west-2"},
		Reconciliation: config.ReconciliationConfig{
			Pricing: "12h", // Custom 12-hour interval
		},
	}

	// Create cache and metrics
	pricingCache := cache.NewPricingCache()
	m := metrics.NewMetrics(prometheus.NewRegistry())

	// Create reconciler
	reconciler := &PricingReconciler{
		AWSClient:        mockClient,
		Config:           cfg,
		Cache:            pricingCache,
		Metrics:          m,
		Log:              logr.Discard(),
		OperatingSystems: []string{"Linux"},
	}

	// Run reconciliation
	result, err := reconciler.Reconcile(ctx, ctrl.Request{})

	// Verify no error
	require.NoError(t, err)

	// Verify requeue uses custom interval
	assert.Equal(t, 12*time.Hour, result.RequeueAfter)
}

// TestPricingReconciler_Reconcile_InvalidInterval tests handling of invalid interval config.
func TestPricingReconciler_Reconcile_InvalidInterval(t *testing.T) {
	// Create mock client
	mockClient := aws.NewMockClient()
	ctx := context.Background()

	// Setup pricing client with test data
	pricingClient := mockClient.Pricing(ctx)
	mockPricing := pricingClient.(*aws.MockPricingClient)
	mockPricing.SetOnDemandPrice("us-west-2", "m5.large", "Linux", 0.096)

	// Create config with invalid interval (should fall back to default)
	cfg := &config.Config{
		DefaultRegion: "us-east-1",
		Regions:       []string{"us-west-2"},
		Reconciliation: config.ReconciliationConfig{
			Pricing: "invalid-duration",
		},
	}

	// Create cache and metrics
	pricingCache := cache.NewPricingCache()
	m := metrics.NewMetrics(prometheus.NewRegistry())

	// Create reconciler
	reconciler := &PricingReconciler{
		AWSClient:        mockClient,
		Config:           cfg,
		Cache:            pricingCache,
		Metrics:          m,
		Log:              logr.Discard(),
		OperatingSystems: []string{"Linux"},
	}

	// Run reconciliation
	result, err := reconciler.Reconcile(ctx, ctrl.Request{})

	// Verify no error (invalid interval should fall back to default)
	require.NoError(t, err)

	// Verify requeue uses default 24h interval
	assert.Equal(t, 24*time.Hour, result.RequeueAfter)
}

// TestPricingReconciler_Reconcile_DefaultRegions tests region fallback logic.
func TestPricingReconciler_Reconcile_DefaultRegions(t *testing.T) {
	tests := []struct {
		name              string
		configRegions     []string
		reconcilerRegions []string
		expectedCalls     int // Mock doesn't actually paginate, but we verify cache is populated
	}{
		{
			name:              "uses config regions",
			configRegions:     []string{"us-west-2"},
			reconcilerRegions: []string{"us-east-1"},
			expectedCalls:     1, // Should use config regions
		},
		{
			name:              "falls back to reconciler regions",
			configRegions:     nil,
			reconcilerRegions: []string{"us-east-1", "eu-west-1"},
			expectedCalls:     2,
		},
		{
			name:              "falls back to default regions",
			configRegions:     nil,
			reconcilerRegions: nil,
			expectedCalls:     2, // config.DefaultRegions has 2 regions
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock client
			mockClient := aws.NewMockClient()
			ctx := context.Background()

			// Setup pricing client with test data
			pricingClient := mockClient.Pricing(ctx)
			mockPricing := pricingClient.(*aws.MockPricingClient)
			// Add prices for all possible regions
			for _, region := range []string{"us-west-2", "us-east-1", "eu-west-1"} {
				mockPricing.SetOnDemandPrice(region, "m5.large", "Linux", 0.096)
			}

			// Create config
			cfg := &config.Config{
				DefaultRegion: "us-east-1",
				Regions:       tt.configRegions,
			}

			// Create cache and metrics
			pricingCache := cache.NewPricingCache()
			m := metrics.NewMetrics(prometheus.NewRegistry())

			// Create reconciler
			reconciler := &PricingReconciler{
				AWSClient:        mockClient,
				Config:           cfg,
				Cache:            pricingCache,
				Metrics:          m,
				Log:              logr.Discard(),
				Regions:          tt.reconcilerRegions,
				OperatingSystems: []string{"Linux"},
			}

			// Run reconciliation
			result, err := reconciler.Reconcile(ctx, ctrl.Request{})

			// Verify no error
			require.NoError(t, err)
			assert.Equal(t, 24*time.Hour, result.RequeueAfter)

			// Verify cache was populated
			stats := pricingCache.GetStats()
			assert.True(t, stats.IsPopulated, "cache should be populated")
			assert.Greater(t, stats.OnDemandPriceCount, 0, "should have at least one price")
		})
	}
}

// TestPricingReconciler_Reconcile_DefaultOS tests operating system fallback logic.
func TestPricingReconciler_Reconcile_DefaultOS(t *testing.T) {
	// Create mock client
	mockClient := aws.NewMockClient()
	ctx := context.Background()

	// Setup pricing client with test data for both Linux and Windows
	pricingClient := mockClient.Pricing(ctx)
	mockPricing := pricingClient.(*aws.MockPricingClient)
	mockPricing.SetOnDemandPrice("us-west-2", "m5.large", "Linux", 0.096)
	mockPricing.SetOnDemandPrice("us-west-2", "m5.large", "Windows", 0.192)

	// Create config
	cfg := &config.Config{
		DefaultRegion: "us-east-1",
		Regions:       []string{"us-west-2"},
	}

	// Create cache and metrics
	pricingCache := cache.NewPricingCache()
	m := metrics.NewMetrics(prometheus.NewRegistry())

	// Create reconciler with NO operating systems specified (should default to Linux+Windows)
	reconciler := &PricingReconciler{
		AWSClient:        mockClient,
		Config:           cfg,
		Cache:            pricingCache,
		Metrics:          m,
		Log:              logr.Discard(),
		OperatingSystems: nil, // Should default to Linux and Windows
	}

	// Run reconciliation
	result, err := reconciler.Reconcile(ctx, ctrl.Request{})

	// Verify no error
	require.NoError(t, err)
	assert.Equal(t, 24*time.Hour, result.RequeueAfter)

	// Verify cache has both Linux and Windows prices
	linuxPrice, linuxExists := pricingCache.GetOnDemandPrice("us-west-2", "m5.large", "Linux")
	require.True(t, linuxExists, "should have Linux price")
	assert.Equal(t, 0.096, linuxPrice)

	windowsPrice, windowsExists := pricingCache.GetOnDemandPrice("us-west-2", "m5.large", "Windows")
	require.True(t, windowsExists, "should have Windows price")
	assert.Equal(t, 0.192, windowsPrice)
}

// TestPricingReconciler_Reconcile_CacheUpdate tests cache update behavior.
func TestPricingReconciler_Reconcile_CacheUpdate(t *testing.T) {
	// Create mock client
	mockClient := aws.NewMockClient()
	ctx := context.Background()

	// Setup pricing client with initial data
	pricingClient := mockClient.Pricing(ctx)
	mockPricing := pricingClient.(*aws.MockPricingClient)
	mockPricing.SetOnDemandPrice("us-west-2", "m5.large", "Linux", 0.096)

	// Create config
	cfg := &config.Config{
		DefaultRegion: "us-east-1",
		Regions:       []string{"us-west-2"},
	}

	// Create cache and metrics
	pricingCache := cache.NewPricingCache()
	m := metrics.NewMetrics(prometheus.NewRegistry())

	// Create reconciler
	reconciler := &PricingReconciler{
		AWSClient:        mockClient,
		Config:           cfg,
		Cache:            pricingCache,
		Metrics:          m,
		Log:              logr.Discard(),
		OperatingSystems: []string{"Linux"},
	}

	// First reconciliation
	_, err := reconciler.Reconcile(ctx, ctrl.Request{})
	require.NoError(t, err)

	// Verify initial price
	price, exists := pricingCache.GetOnDemandPrice("us-west-2", "m5.large", "Linux")
	require.True(t, exists)
	assert.Equal(t, 0.096, price)

	firstStats := pricingCache.GetStats()
	firstUpdateTime := firstStats.LastUpdated

	// Wait a tiny bit to ensure timestamp changes
	time.Sleep(10 * time.Millisecond)

	// Update pricing data
	mockPricing.SetOnDemandPrice("us-west-2", "m5.large", "Linux", 0.100)
	mockPricing.SetOnDemandPrice("us-west-2", "m5.xlarge", "Linux", 0.200)

	// Second reconciliation
	_, err = reconciler.Reconcile(ctx, ctrl.Request{})
	require.NoError(t, err)

	// Verify updated price
	price, exists = pricingCache.GetOnDemandPrice("us-west-2", "m5.large", "Linux")
	require.True(t, exists)
	assert.Equal(t, 0.100, price)

	// Verify new price was added
	newPrice, newExists := pricingCache.GetOnDemandPrice("us-west-2", "m5.xlarge", "Linux")
	require.True(t, newExists)
	assert.Equal(t, 0.200, newPrice)

	// Verify cache stats were updated
	secondStats := pricingCache.GetStats()
	assert.True(t, secondStats.LastUpdated.After(firstUpdateTime),
		"cache last updated time should be newer")
	assert.Equal(t, 2, secondStats.OnDemandPriceCount,
		"should have 2 prices after update")
}

// TestPricingReconciler_Reconcile_Metrics tests that metrics are updated correctly.
func TestPricingReconciler_Reconcile_Metrics(t *testing.T) {
	// Create mock client
	mockClient := aws.NewMockClient()
	ctx := context.Background()

	// Setup pricing client with test data
	pricingClient := mockClient.Pricing(ctx)
	mockPricing := pricingClient.(*aws.MockPricingClient)
	mockPricing.SetOnDemandPrice("us-west-2", "m5.large", "Linux", 0.096)

	// Create config
	cfg := &config.Config{
		DefaultRegion: "us-east-1",
		Regions:       []string{"us-west-2"},
	}

	// Create cache and metrics
	pricingCache := cache.NewPricingCache()
	registry := prometheus.NewRegistry()
	m := metrics.NewMetrics(registry)

	// Create reconciler
	reconciler := &PricingReconciler{
		AWSClient:        mockClient,
		Config:           cfg,
		Cache:            pricingCache,
		Metrics:          m,
		Log:              logr.Discard(),
		OperatingSystems: []string{"Linux"},
	}

	// Run reconciliation
	_, err := reconciler.Reconcile(ctx, ctrl.Request{})
	require.NoError(t, err)

	// Wait for background goroutine to update the freshness metric
	// The metric is updated every second, so wait slightly more than 1 second
	time.Sleep(1200 * time.Millisecond)

	// Gather metrics
	metricFamilies, err := registry.Gather()
	require.NoError(t, err)

	// Verify data_last_success metric
	foundSuccess := false
	foundFreshness := false

	for _, mf := range metricFamilies {
		if *mf.Name == "lumina_data_last_success" {
			for _, metric := range mf.Metric {
				// Check for pricing metric (empty account_id and region labels)
				var dataType string
				for _, label := range metric.Label {
					if *label.Name == "data_type" {
						dataType = *label.Value
					}
				}
				if dataType == "pricing" {
					foundSuccess = true
					assert.Equal(t, float64(1), *metric.Gauge.Value,
						"data_last_success should be 1 for successful load")
				}
			}
		}

		if *mf.Name == "lumina_data_freshness_seconds" {
			for _, metric := range mf.Metric {
				// Check for pricing metric
				var dataType string
				for _, label := range metric.Label {
					if *label.Name == "data_type" {
						dataType = *label.Value
					}
				}
				if dataType == "pricing" {
					foundFreshness = true
					// DataFreshness now stores age in seconds (auto-updated every second)
					// We waited ~1.2 seconds, so age should be approximately 1-2 seconds
					assert.GreaterOrEqual(t, *metric.Gauge.Value, float64(1.0),
						"data_freshness should be at least 1 second (we waited 1.2s)")
					assert.Less(t, *metric.Gauge.Value, float64(2.0),
						"data_freshness should be less than 2 seconds")
				}
			}
		}
	}

	assert.True(t, foundSuccess, "should have data_last_success metric for pricing")
	assert.True(t, foundFreshness, "should have data_freshness metric for pricing")
}

// TestPricingReconciler_Reconcile_TestData tests using test data instead of API.
func TestPricingReconciler_Reconcile_TestData(t *testing.T) {
	// Create mock client (won't be used since we have test data)
	mockClient := aws.NewMockClient()
	ctx := context.Background()

	// Create config with test pricing data
	cfg := &config.Config{
		DefaultRegion: "us-east-1",
		Regions:       []string{"us-west-2"},
		TestData: &config.TestData{
			PricingFlat: map[string]float64{
				"us-west-2:m5.large:Linux":   0.096,
				"us-west-2:m5.xlarge:Linux":  0.192,
				"us-west-2:m5.2xlarge:Linux": 0.384,
			},
		},
	}

	// Create cache and metrics
	pricingCache := cache.NewPricingCache()
	m := metrics.NewMetrics(prometheus.NewRegistry())

	// Create reconciler
	reconciler := &PricingReconciler{
		AWSClient:        mockClient,
		Config:           cfg,
		Cache:            pricingCache,
		Metrics:          m,
		Log:              logr.Discard(),
		OperatingSystems: []string{"Linux"},
	}

	// Run reconciliation
	result, err := reconciler.Reconcile(ctx, ctrl.Request{})

	// Verify no error
	require.NoError(t, err)
	assert.Equal(t, 24*time.Hour, result.RequeueAfter)

	// Verify cache was populated with test data
	stats := pricingCache.GetStats()
	assert.Equal(t, 3, stats.OnDemandPriceCount, "should have 3 test prices")
	assert.True(t, stats.IsPopulated, "cache should be populated")

	// Verify specific test prices
	price, exists := pricingCache.GetOnDemandPrice("us-west-2", "m5.large", "Linux")
	require.True(t, exists, "should have us-west-2 m5.large Linux price")
	assert.Equal(t, 0.096, price)

	price, exists = pricingCache.GetOnDemandPrice("us-west-2", "m5.xlarge", "Linux")
	require.True(t, exists, "should have us-west-2 m5.xlarge Linux price")
	assert.Equal(t, 0.192, price)

	price, exists = pricingCache.GetOnDemandPrice("us-west-2", "m5.2xlarge", "Linux")
	require.True(t, exists, "should have us-west-2 m5.2xlarge Linux price")
	assert.Equal(t, 0.384, price)

	// Verify mock client was NOT called (test data used instead)
	mockPricing := mockClient.PricingClientInstance
	assert.Equal(t, 0, mockPricing.GetOnDemandPricesCallCount,
		"should not call pricing API when test data is configured")
}
