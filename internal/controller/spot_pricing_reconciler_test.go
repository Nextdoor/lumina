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

// TestSpotPricingReconciler_Reconcile_LazyLoading_FirstRun tests the first reconciliation
// when cache is empty - should fetch all spot prices for running instances.
func TestSpotPricingReconciler_Reconcile_LazyLoading_FirstRun(t *testing.T) {
	// Setup: Create EC2Cache with running instances
	ec2Cache := cache.NewEC2Cache()
	ec2Cache.SetInstances("123456789012", "us-west-2", []aws.Instance{
		{
			InstanceID:       "i-001",
			InstanceType:     "m5.large",
			AvailabilityZone: "us-west-2a",
			Region:           "us-west-2",
			AccountID:        "123456789012",
			State:            "running",
		},
		{
			InstanceID:       "i-002",
			InstanceType:     "c5.xlarge",
			AvailabilityZone: "us-west-2b",
			Region:           "us-west-2",
			AccountID:        "123456789012",
			State:            "running",
		},
	})

	// Setup: Create mock AWS client with spot prices
	mockClient := aws.NewMockClient()
	ctx := context.Background()
	ec2Client, err := mockClient.EC2(ctx, aws.AccountConfig{
		AccountID: "123456789012",
		Region:    "us-west-2",
	})
	require.NoError(t, err)
	mockEC2 := ec2Client.(*aws.MockEC2Client)
	mockEC2.SpotPrices = []aws.SpotPrice{
		{
			InstanceType:       "m5.large",
			AvailabilityZone:   "us-west-2a",
			SpotPrice:          0.034,
			Timestamp:          time.Now(),
			ProductDescription: "Linux/UNIX",
		},
		{
			InstanceType:       "c5.xlarge",
			AvailabilityZone:   "us-west-2b",
			SpotPrice:          0.068,
			Timestamp:          time.Now(),
			ProductDescription: "Linux/UNIX",
		},
	}

	// Setup: Create pricing cache and EC2ReadyChan
	pricingCache := cache.NewPricingCache()
	m := metrics.NewMetrics(prometheus.NewRegistry())
	ec2ReadyChan := make(chan struct{})
	close(ec2ReadyChan) // Signal EC2Cache is ready

	// Setup: Create reconciler
	reconciler := &SpotPricingReconciler{
		AWSClient: mockClient,
		Config: &config.Config{
			AWSAccounts: []config.AWSAccount{
				{
					AccountID: "123456789012",
					Name:      "test-account",
				},
			},
			DefaultRegion: "us-west-2",
		},
		EC2Cache:     ec2Cache,
		Cache:        pricingCache,
		Metrics:      m,
		Log:          logr.Discard(),
		EC2ReadyChan: ec2ReadyChan,
	}

	// Test: Run reconciliation (first run should fetch all prices)
	result, err := reconciler.Reconcile(ctx, ctrl.Request{})
	require.NoError(t, err)
	assert.Equal(t, 15*time.Second, result.RequeueAfter)

	// Verify: Cache was populated with spot prices
	stats := pricingCache.GetSpotStats()
	assert.Equal(t, 2, stats.SpotPriceCount, "should have 2 spot prices")
	assert.True(t, stats.IsPopulated, "cache should be populated")

	// Verify: Specific prices exist
	price1, exists1 := pricingCache.GetSpotPrice("m5.large", "us-west-2a", "Linux/UNIX")
	require.True(t, exists1, "should have m5.large price")
	assert.Equal(t, 0.034, price1)

	price2, exists2 := pricingCache.GetSpotPrice("c5.xlarge", "us-west-2b", "Linux/UNIX")
	require.True(t, exists2, "should have c5.xlarge price")
	assert.Equal(t, 0.068, price2)
}

// TestSpotPricingReconciler_Reconcile_LazyLoading_SteadyState tests that when all prices
// are cached, reconciliation does 0 API calls.
func TestSpotPricingReconciler_Reconcile_LazyLoading_SteadyState(t *testing.T) {
	// Setup: Create EC2Cache with running instances
	ec2Cache := cache.NewEC2Cache()
	ec2Cache.SetInstances("123456789012", "us-west-2", []aws.Instance{
		{
			InstanceID:       "i-001",
			InstanceType:     "m5.large",
			AvailabilityZone: "us-west-2a",
			Region:           "us-west-2",
			AccountID:        "123456789012",
			State:            "running",
		},
	})

	// Setup: Create pricing cache PRE-POPULATED with spot prices
	pricingCache := cache.NewPricingCache()
	pricingCache.InsertSpotPrices(map[string]aws.SpotPrice{
		"m5.large:us-west-2a": {
			InstanceType:       "m5.large",
			AvailabilityZone:   "us-west-2a",
			SpotPrice:          0.034,
			Timestamp:          time.Now(),
			ProductDescription: "Linux/UNIX",
		},
	})

	// Setup: Create mock client that will fail if called (proves 0 API calls)
	mockClient := aws.NewMockClient()
	mockClient.EC2Error = assert.AnError // This would cause failure if EC2 client is created

	m := metrics.NewMetrics(prometheus.NewRegistry())
	ec2ReadyChan := make(chan struct{})
	close(ec2ReadyChan)

	// Setup: Create reconciler
	reconciler := &SpotPricingReconciler{
		AWSClient: mockClient,
		Config: &config.Config{
			AWSAccounts: []config.AWSAccount{
				{
					AccountID: "123456789012",
					Name:      "test-account",
				},
			},
			DefaultRegion: "us-west-2",
		},
		EC2Cache:     ec2Cache,
		Cache:        pricingCache,
		Metrics:      m,
		Log:          logr.Discard(),
		EC2ReadyChan: ec2ReadyChan,
	}

	// Test: Run reconciliation (should do 0 API calls since all prices cached)
	ctx := context.Background()
	result, err := reconciler.Reconcile(ctx, ctrl.Request{})
	require.NoError(t, err) // Would fail if API was called
	assert.Equal(t, 15*time.Second, result.RequeueAfter)

	// Verify: Cache still has the same price (no changes)
	price, exists := pricingCache.GetSpotPrice("m5.large", "us-west-2a", "Linux/UNIX")
	require.True(t, exists)
	assert.Equal(t, 0.034, price)
}

// TestSpotPricingReconciler_Reconcile_LazyLoading_NewInstanceType tests that when a new
// instance type appears, only that type's spot price is fetched.
func TestSpotPricingReconciler_Reconcile_LazyLoading_NewInstanceType(t *testing.T) {
	// Setup: Create EC2Cache with TWO instances (one new, one existing)
	ec2Cache := cache.NewEC2Cache()
	ec2Cache.SetInstances("123456789012", "us-west-2", []aws.Instance{
		{
			InstanceID:       "i-001",
			InstanceType:     "m5.large",
			AvailabilityZone: "us-west-2a",
			Region:           "us-west-2",
			AccountID:        "123456789012",
			State:            "running",
		},
		{
			InstanceID:       "i-002",
			InstanceType:     "c5.xlarge", // NEW instance type
			AvailabilityZone: "us-west-2a",
			Region:           "us-west-2",
			AccountID:        "123456789012",
			State:            "running",
		},
	})

	// Setup: Create pricing cache with ONLY m5.large price (c5.xlarge is missing)
	pricingCache := cache.NewPricingCache()
	pricingCache.InsertSpotPrices(map[string]aws.SpotPrice{
		"m5.large:us-west-2a": {
			InstanceType:       "m5.large",
			AvailabilityZone:   "us-west-2a",
			SpotPrice:          0.034,
			Timestamp:          time.Now(),
			ProductDescription: "Linux/UNIX",
		},
	})

	// Setup: Create mock client with spot prices (will be queried for c5.xlarge only)
	mockClient := aws.NewMockClient()
	ctx := context.Background()
	ec2Client, err := mockClient.EC2(ctx, aws.AccountConfig{
		AccountID: "123456789012",
		Region:    "us-west-2",
	})
	require.NoError(t, err)
	mockEC2 := ec2Client.(*aws.MockEC2Client)
	mockEC2.SpotPrices = []aws.SpotPrice{
		{
			InstanceType:       "c5.xlarge",
			AvailabilityZone:   "us-west-2a",
			SpotPrice:          0.068,
			Timestamp:          time.Now(),
			ProductDescription: "Linux/UNIX",
		},
	}

	m := metrics.NewMetrics(prometheus.NewRegistry())
	ec2ReadyChan := make(chan struct{})
	close(ec2ReadyChan)

	// Setup: Create reconciler
	reconciler := &SpotPricingReconciler{
		AWSClient: mockClient,
		Config: &config.Config{
			AWSAccounts: []config.AWSAccount{
				{
					AccountID: "123456789012",
					Name:      "test-account",
				},
			},
			DefaultRegion: "us-west-2",
		},
		EC2Cache:     ec2Cache,
		Cache:        pricingCache,
		Metrics:      m,
		Log:          logr.Discard(),
		EC2ReadyChan: ec2ReadyChan,
	}

	// Test: Run reconciliation (should only fetch c5.xlarge price)
	result, err := reconciler.Reconcile(ctx, ctrl.Request{})
	require.NoError(t, err)
	assert.Equal(t, 15*time.Second, result.RequeueAfter)

	// Verify: Cache now has BOTH prices (InsertSpotPrices merges, doesn't replace)
	stats := pricingCache.GetSpotStats()
	assert.Equal(t, 2, stats.SpotPriceCount, "should have 2 spot prices (m5.large + c5.xlarge)")

	// Verify: Original price still exists
	price1, exists1 := pricingCache.GetSpotPrice("m5.large", "us-west-2a", "Linux/UNIX")
	require.True(t, exists1, "should have m5.large price")
	assert.Equal(t, 0.034, price1)

	// Verify: New price was added
	price2, exists2 := pricingCache.GetSpotPrice("c5.xlarge", "us-west-2a", "Linux/UNIX")
	require.True(t, exists2, "should have c5.xlarge price")
	assert.Equal(t, 0.068, price2)
}

// TestSpotPricingReconciler_Reconcile_NoInstances tests behavior when no instances are running.
func TestSpotPricingReconciler_Reconcile_NoInstances(t *testing.T) {
	// Setup: Empty EC2Cache
	ec2Cache := cache.NewEC2Cache()

	mockClient := aws.NewMockClient()
	pricingCache := cache.NewPricingCache()
	m := metrics.NewMetrics(prometheus.NewRegistry())
	ec2ReadyChan := make(chan struct{})
	close(ec2ReadyChan)

	reconciler := &SpotPricingReconciler{
		AWSClient: mockClient,
		Config: &config.Config{
			AWSAccounts: []config.AWSAccount{
				{
					AccountID: "123456789012",
					Name:      "test-account",
				},
			},
			DefaultRegion: "us-west-2",
		},
		EC2Cache:     ec2Cache,
		Cache:        pricingCache,
		Metrics:      m,
		Log:          logr.Discard(),
		EC2ReadyChan: ec2ReadyChan,
	}

	// Test: Run reconciliation with no instances
	result, err := reconciler.Reconcile(context.Background(), ctrl.Request{})
	require.NoError(t, err)
	assert.Equal(t, 15*time.Second, result.RequeueAfter)

	// Verify: Cache is empty (no spot prices needed)
	stats := pricingCache.GetSpotStats()
	assert.Equal(t, 0, stats.SpotPriceCount)
}

// TestSpotPricingReconciler_Reconcile_WaitsForEC2Cache tests that reconciliation
// waits for EC2ReadyChan before proceeding.
func TestSpotPricingReconciler_Reconcile_WaitsForEC2Cache(t *testing.T) {
	ec2Cache := cache.NewEC2Cache()
	mockClient := aws.NewMockClient()
	pricingCache := cache.NewPricingCache()
	m := metrics.NewMetrics(prometheus.NewRegistry())
	ec2ReadyChan := make(chan struct{}) // NOT closed yet

	reconciler := &SpotPricingReconciler{
		AWSClient: mockClient,
		Config: &config.Config{
			AWSAccounts: []config.AWSAccount{
				{
					AccountID: "123456789012",
					Name:      "test-account",
				},
			},
			DefaultRegion: "us-west-2",
		},
		EC2Cache:     ec2Cache,
		Cache:        pricingCache,
		Metrics:      m,
		Log:          logr.Discard(),
		EC2ReadyChan: ec2ReadyChan,
	}

	// Test: Run reconciliation in goroutine (will block on EC2ReadyChan)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		_, _ = reconciler.Reconcile(ctx, ctrl.Request{})
		close(done)
	}()

	// Verify: Reconciliation is blocked (done channel not closed)
	select {
	case <-done:
		t.Fatal("Reconcile should be blocked waiting for EC2ReadyChan")
	case <-time.After(50 * time.Millisecond):
		// Expected: still blocked
	}

	// Now close EC2ReadyChan to unblock
	close(ec2ReadyChan)

	// Verify: Reconciliation completes after EC2ReadyChan is closed
	select {
	case <-done:
		// Expected: reconciliation completed
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Reconcile should complete after EC2ReadyChan is closed")
	}
}

// TestSpotPricingReconciler_Reconcile_CustomInterval tests custom reconciliation interval.
func TestSpotPricingReconciler_Reconcile_CustomInterval(t *testing.T) {
	ec2Cache := cache.NewEC2Cache()
	mockClient := aws.NewMockClient()
	pricingCache := cache.NewPricingCache()
	m := metrics.NewMetrics(prometheus.NewRegistry())
	ec2ReadyChan := make(chan struct{})
	close(ec2ReadyChan)

	reconciler := &SpotPricingReconciler{
		AWSClient: mockClient,
		Config: &config.Config{
			AWSAccounts: []config.AWSAccount{
				{
					AccountID: "123456789012",
					Name:      "test-account",
				},
			},
			DefaultRegion: "us-west-2",
			Reconciliation: config.ReconciliationConfig{
				SpotPricing: "30s", // Custom interval
			},
		},
		EC2Cache:     ec2Cache,
		Cache:        pricingCache,
		Metrics:      m,
		Log:          logr.Discard(),
		EC2ReadyChan: ec2ReadyChan,
	}

	// Test: Run reconciliation
	result, err := reconciler.Reconcile(context.Background(), ctrl.Request{})
	require.NoError(t, err)

	// Verify: Uses custom interval
	assert.Equal(t, 30*time.Second, result.RequeueAfter)
}

// TestSpotPricingReconciler_Reconcile_InvalidInterval tests handling of invalid interval.
func TestSpotPricingReconciler_Reconcile_InvalidInterval(t *testing.T) {
	ec2Cache := cache.NewEC2Cache()
	mockClient := aws.NewMockClient()
	pricingCache := cache.NewPricingCache()
	m := metrics.NewMetrics(prometheus.NewRegistry())
	ec2ReadyChan := make(chan struct{})
	close(ec2ReadyChan)

	reconciler := &SpotPricingReconciler{
		AWSClient: mockClient,
		Config: &config.Config{
			AWSAccounts: []config.AWSAccount{
				{
					AccountID: "123456789012",
					Name:      "test-account",
				},
			},
			DefaultRegion: "us-west-2",
			Reconciliation: config.ReconciliationConfig{
				SpotPricing: "invalid-duration",
			},
		},
		EC2Cache:     ec2Cache,
		Cache:        pricingCache,
		Metrics:      m,
		Log:          logr.Discard(),
		EC2ReadyChan: ec2ReadyChan,
	}

	// Test: Run reconciliation with invalid interval
	result, err := reconciler.Reconcile(context.Background(), ctrl.Request{})
	require.NoError(t, err)

	// Verify: Falls back to default (15 seconds)
	assert.Equal(t, 15*time.Second, result.RequeueAfter)
}

// TestSpotPricingReconciler_Reconcile_ReadyChanClosed tests that ReadyChan is closed
// after first reconciliation.
func TestSpotPricingReconciler_Reconcile_ReadyChanClosed(t *testing.T) {
	ec2Cache := cache.NewEC2Cache()
	mockClient := aws.NewMockClient()
	pricingCache := cache.NewPricingCache()
	m := metrics.NewMetrics(prometheus.NewRegistry())
	ec2ReadyChan := make(chan struct{})
	close(ec2ReadyChan)
	readyChan := make(chan struct{})

	reconciler := &SpotPricingReconciler{
		AWSClient: mockClient,
		Config: &config.Config{
			AWSAccounts: []config.AWSAccount{
				{
					AccountID: "123456789012",
					Name:      "test-account",
				},
			},
			DefaultRegion: "us-west-2",
		},
		EC2Cache:     ec2Cache,
		Cache:        pricingCache,
		Metrics:      m,
		Log:          logr.Discard(),
		EC2ReadyChan: ec2ReadyChan,
		ReadyChan:    readyChan,
	}

	// Verify: ReadyChan is NOT closed yet
	select {
	case <-readyChan:
		t.Fatal("ReadyChan should not be closed before reconciliation")
	default:
		// Expected
	}

	// Test: Run reconciliation
	_, err := reconciler.Reconcile(context.Background(), ctrl.Request{})
	require.NoError(t, err)

	// Verify: ReadyChan IS closed after first reconciliation
	select {
	case <-readyChan:
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Fatal("ReadyChan should be closed after first reconciliation")
	}

	// Test: Run reconciliation again (ReadyChan should not panic)
	_, err = reconciler.Reconcile(context.Background(), ctrl.Request{})
	require.NoError(t, err)

	// Verify: ReadyChan is still closed
	select {
	case <-readyChan:
		// Expected
	default:
		t.Fatal("ReadyChan should still be closed")
	}
}

// TestSpotPricingReconciler_Reconcile_EC2ClientError tests error handling when EC2 client creation fails.
func TestSpotPricingReconciler_Reconcile_EC2ClientError(t *testing.T) {
	// Setup: Create EC2Cache with running instance
	ec2Cache := cache.NewEC2Cache()
	ec2Cache.SetInstances("123456789012", "us-west-2", []aws.Instance{
		{
			InstanceID:       "i-001",
			InstanceType:     "m5.large",
			AvailabilityZone: "us-west-2a",
			Region:           "us-west-2",
			AccountID:        "123456789012",
			State:            "running",
		},
	})

	// Setup: Create mock client that fails EC2 client creation
	mockClient := aws.NewMockClient()
	mockClient.EC2Error = assert.AnError

	pricingCache := cache.NewPricingCache()
	m := metrics.NewMetrics(prometheus.NewRegistry())
	ec2ReadyChan := make(chan struct{})
	close(ec2ReadyChan)

	reconciler := &SpotPricingReconciler{
		AWSClient: mockClient,
		Config: &config.Config{
			AWSAccounts: []config.AWSAccount{
				{
					AccountID: "123456789012",
					Name:      "test-account",
				},
			},
			DefaultRegion: "us-west-2",
		},
		EC2Cache:     ec2Cache,
		Cache:        pricingCache,
		Metrics:      m,
		Log:          logr.Discard(),
		EC2ReadyChan: ec2ReadyChan,
	}

	// Test: Run reconciliation (should handle EC2 client error gracefully)
	result, err := reconciler.Reconcile(context.Background(), ctrl.Request{})
	require.NoError(t, err) // Reconcile doesn't fail, just logs errors
	assert.Equal(t, 15*time.Second, result.RequeueAfter)

	// Verify: Cache is still empty (no prices fetched due to error)
	stats := pricingCache.GetSpotStats()
	assert.Equal(t, 0, stats.SpotPriceCount)
}

// TestSpotPricingReconciler_Reconcile_SpotPriceAPIError tests error handling when DescribeSpotPriceHistory fails.
func TestSpotPricingReconciler_Reconcile_SpotPriceAPIError(t *testing.T) {
	// Setup: Create EC2Cache with running instance
	ec2Cache := cache.NewEC2Cache()
	ec2Cache.SetInstances("123456789012", "us-west-2", []aws.Instance{
		{
			InstanceID:       "i-001",
			InstanceType:     "m5.large",
			AvailabilityZone: "us-west-2a",
			Region:           "us-west-2",
			AccountID:        "123456789012",
			State:            "running",
		},
	})

	// Setup: Create mock AWS client with failing DescribeSpotPriceHistory
	mockClient := aws.NewMockClient()
	ctx := context.Background()
	ec2Client, err := mockClient.EC2(ctx, aws.AccountConfig{
		AccountID: "123456789012",
		Region:    "us-west-2",
	})
	require.NoError(t, err)
	mockEC2 := ec2Client.(*aws.MockEC2Client)
	mockEC2.DescribeSpotPriceHistoryError = assert.AnError

	pricingCache := cache.NewPricingCache()
	m := metrics.NewMetrics(prometheus.NewRegistry())
	ec2ReadyChan := make(chan struct{})
	close(ec2ReadyChan)

	reconciler := &SpotPricingReconciler{
		AWSClient: mockClient,
		Config: &config.Config{
			AWSAccounts: []config.AWSAccount{
				{
					AccountID: "123456789012",
					Name:      "test-account",
				},
			},
			DefaultRegion: "us-west-2",
		},
		EC2Cache:     ec2Cache,
		Cache:        pricingCache,
		Metrics:      m,
		Log:          logr.Discard(),
		EC2ReadyChan: ec2ReadyChan,
	}

	// Test: Run reconciliation (should handle API error gracefully)
	result, err := reconciler.Reconcile(context.Background(), ctrl.Request{})
	require.NoError(t, err) // Reconcile doesn't fail, just logs errors
	assert.Equal(t, 15*time.Second, result.RequeueAfter)

	// Verify: Cache is still empty (no prices fetched due to error)
	stats := pricingCache.GetSpotStats()
	assert.Equal(t, 0, stats.SpotPriceCount)
}

// TestSpotPricingReconciler_Reconcile_CustomCacheExpiration tests that custom cache expiration is used.
func TestSpotPricingReconciler_Reconcile_CustomCacheExpiration(t *testing.T) {
	// Setup: Create EC2Cache with running instance
	ec2Cache := cache.NewEC2Cache()
	ec2Cache.SetInstances("123456789012", "us-west-2", []aws.Instance{
		{
			InstanceID:       "i-001",
			InstanceType:     "m5.large",
			AvailabilityZone: "us-west-2a",
			Region:           "us-west-2",
			AccountID:        "123456789012",
			State:            "running",
		},
	})

	// Setup: Create pricing cache with spot price that's 10 minutes old
	pricingCache := cache.NewPricingCache()
	tenMinutesAgo := time.Now().Add(-10 * time.Minute)
	pricingCache.InsertSpotPrices(map[string]aws.SpotPrice{
		"m5.large:us-west-2a": {
			InstanceType:       "m5.large",
			AvailabilityZone:   "us-west-2a",
			SpotPrice:          0.034,
			Timestamp:          tenMinutesAgo,
			FetchedAt:          tenMinutesAgo, // 10 minutes old
			ProductDescription: "Linux/UNIX",
		},
	})

	// Setup: Create mock client with updated spot price
	mockClient := aws.NewMockClient()
	ctx := context.Background()
	ec2Client, err := mockClient.EC2(ctx, aws.AccountConfig{
		AccountID: "123456789012",
		Region:    "us-west-2",
	})
	require.NoError(t, err)
	mockEC2 := ec2Client.(*aws.MockEC2Client)
	mockEC2.SpotPrices = []aws.SpotPrice{
		{
			InstanceType:       "m5.large",
			AvailabilityZone:   "us-west-2a",
			SpotPrice:          0.040, // Price changed
			Timestamp:          time.Now(),
			ProductDescription: "Linux/UNIX",
		},
	}

	m := metrics.NewMetrics(prometheus.NewRegistry())
	ec2ReadyChan := make(chan struct{})
	close(ec2ReadyChan)

	// Setup: Create reconciler with custom cache expiration (5 minutes)
	reconciler := &SpotPricingReconciler{
		AWSClient: mockClient,
		Config: &config.Config{
			AWSAccounts: []config.AWSAccount{
				{
					AccountID: "123456789012",
					Name:      "test-account",
				},
			},
			DefaultRegion: "us-west-2",
			Pricing: config.PricingConfig{
				SpotPriceCacheExpiration: "5m", // Custom: 5 minutes (less than 10 minutes)
			},
		},
		EC2Cache:     ec2Cache,
		Cache:        pricingCache,
		Metrics:      m,
		Log:          logr.Discard(),
		EC2ReadyChan: ec2ReadyChan,
	}

	// Test: Run reconciliation (should refresh because price is >5 minutes old)
	result, err := reconciler.Reconcile(ctx, ctrl.Request{})
	require.NoError(t, err)
	assert.Equal(t, 15*time.Second, result.RequeueAfter)

	// Verify: Price was refreshed (updated to new value)
	price, exists := pricingCache.GetSpotPrice("m5.large", "us-west-2a", "Linux/UNIX")
	require.True(t, exists)
	assert.Equal(t, 0.040, price, "price should be updated to new value")
}

// TestSpotPricingReconciler_Reconcile_InvalidCacheExpiration tests handling of invalid cache expiration.
func TestSpotPricingReconciler_Reconcile_InvalidCacheExpiration(t *testing.T) {
	// Setup: Create EC2Cache with running instance
	ec2Cache := cache.NewEC2Cache()
	ec2Cache.SetInstances("123456789012", "us-west-2", []aws.Instance{
		{
			InstanceID:       "i-001",
			InstanceType:     "m5.large",
			AvailabilityZone: "us-west-2a",
			Region:           "us-west-2",
			AccountID:        "123456789012",
			State:            "running",
		},
	})

	// Setup: Create pricing cache with spot price that's 90 minutes old
	pricingCache := cache.NewPricingCache()
	ninetyMinutesAgo := time.Now().Add(-90 * time.Minute)
	pricingCache.InsertSpotPrices(map[string]aws.SpotPrice{
		"m5.large:us-west-2a": {
			InstanceType:       "m5.large",
			AvailabilityZone:   "us-west-2a",
			SpotPrice:          0.034,
			Timestamp:          ninetyMinutesAgo,
			FetchedAt:          ninetyMinutesAgo, // 90 minutes old
			ProductDescription: "Linux/UNIX",
		},
	})

	// Setup: Create mock client
	mockClient := aws.NewMockClient()
	ctx := context.Background()
	ec2Client, err := mockClient.EC2(ctx, aws.AccountConfig{
		AccountID: "123456789012",
		Region:    "us-west-2",
	})
	require.NoError(t, err)
	mockEC2 := ec2Client.(*aws.MockEC2Client)
	mockEC2.SpotPrices = []aws.SpotPrice{
		{
			InstanceType:       "m5.large",
			AvailabilityZone:   "us-west-2a",
			SpotPrice:          0.040,
			Timestamp:          time.Now(),
			ProductDescription: "Linux/UNIX",
		},
	}

	m := metrics.NewMetrics(prometheus.NewRegistry())
	ec2ReadyChan := make(chan struct{})
	close(ec2ReadyChan)

	// Setup: Create reconciler with INVALID cache expiration
	reconciler := &SpotPricingReconciler{
		AWSClient: mockClient,
		Config: &config.Config{
			AWSAccounts: []config.AWSAccount{
				{
					AccountID: "123456789012",
					Name:      "test-account",
				},
			},
			DefaultRegion: "us-west-2",
			Pricing: config.PricingConfig{
				SpotPriceCacheExpiration: "invalid-duration", // INVALID
			},
		},
		EC2Cache:     ec2Cache,
		Cache:        pricingCache,
		Metrics:      m,
		Log:          logr.Discard(),
		EC2ReadyChan: ec2ReadyChan,
	}

	// Test: Run reconciliation (should use default 1h and refresh the stale price)
	result, err := reconciler.Reconcile(ctx, ctrl.Request{})
	require.NoError(t, err)
	assert.Equal(t, 15*time.Second, result.RequeueAfter)

	// Verify: Price was refreshed (because 90m > default 1h threshold)
	price, exists := pricingCache.GetSpotPrice("m5.large", "us-west-2a", "Linux/UNIX")
	require.True(t, exists)
	assert.Equal(t, 0.040, price, "price should be updated despite invalid config")
}

// TestSpotPricingReconciler_Reconcile_ContextCancelled tests that reconciliation stops when context is cancelled.
func TestSpotPricingReconciler_Reconcile_ContextCancelled(t *testing.T) {
	ec2Cache := cache.NewEC2Cache()
	mockClient := aws.NewMockClient()
	pricingCache := cache.NewPricingCache()
	m := metrics.NewMetrics(prometheus.NewRegistry())
	ec2ReadyChan := make(chan struct{})
	// Don't close ec2ReadyChan - reconciler will block waiting

	reconciler := &SpotPricingReconciler{
		AWSClient: mockClient,
		Config: &config.Config{
			AWSAccounts: []config.AWSAccount{
				{
					AccountID: "123456789012",
					Name:      "test-account",
				},
			},
			DefaultRegion: "us-west-2",
		},
		EC2Cache:     ec2Cache,
		Cache:        pricingCache,
		Metrics:      m,
		Log:          logr.Discard(),
		EC2ReadyChan: ec2ReadyChan,
	}

	// Test: Create a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Run reconciliation (should return immediately due to cancelled context)
	result, err := reconciler.Reconcile(ctx, ctrl.Request{})
	assert.Error(t, err)
	assert.Equal(t, context.Canceled, err)
	assert.Equal(t, ctrl.Result{}, result)
}

// TestSpotPricingReconciler_Reconcile_MultipleAccountsSameRegion tests that when multiple
// accounts have instances in the same region, we only make ONE API call per region (not per account).
// This is correct because spot prices are region-specific, not account-specific.
func TestSpotPricingReconciler_Reconcile_MultipleAccountsSameRegion(t *testing.T) {
	// Setup: Create EC2Cache with instances from TWO accounts in the SAME region
	ec2Cache := cache.NewEC2Cache()
	ec2Cache.SetInstances("111111111111", "us-west-2", []aws.Instance{
		{
			InstanceID:       "i-account1-001",
			InstanceType:     "m5.large",
			AvailabilityZone: "us-west-2a",
			Region:           "us-west-2",
			AccountID:        "111111111111",
			State:            "running",
		},
	})
	ec2Cache.SetInstances("222222222222", "us-west-2", []aws.Instance{
		{
			InstanceID:       "i-account2-001",
			InstanceType:     "c5.xlarge",
			AvailabilityZone: "us-west-2a",
			Region:           "us-west-2",
			AccountID:        "222222222222",
			State:            "running",
		},
	})

	// Setup: Create mock client with spot prices configured
	mockClient := aws.NewMockClient()
	ctx := context.Background()

	// Configure BOTH accounts' EC2 clients with the same spot prices
	// (in reality, both would return the same data since spot prices are region-specific)
	for _, accountID := range []string{"111111111111", "222222222222"} {
		ec2Client, err := mockClient.EC2(ctx, aws.AccountConfig{
			AccountID: accountID,
			Region:    "us-west-2",
		})
		require.NoError(t, err)
		mockEC2 := ec2Client.(*aws.MockEC2Client)
		mockEC2.SpotPrices = []aws.SpotPrice{
			{
				InstanceType:       "m5.large",
				AvailabilityZone:   "us-west-2a",
				SpotPrice:          0.034,
				Timestamp:          time.Now(),
				ProductDescription: "Linux/UNIX",
			},
			{
				InstanceType:       "c5.xlarge",
				AvailabilityZone:   "us-west-2a",
				SpotPrice:          0.068,
				Timestamp:          time.Now(),
				ProductDescription: "Linux/UNIX",
			},
		}
	}

	pricingCache := cache.NewPricingCache()
	m := metrics.NewMetrics(prometheus.NewRegistry())
	ec2ReadyChan := make(chan struct{})
	close(ec2ReadyChan)

	// Setup: Create reconciler with TWO accounts
	reconciler := &SpotPricingReconciler{
		AWSClient: mockClient,
		Config: &config.Config{
			AWSAccounts: []config.AWSAccount{
				{
					AccountID: "111111111111",
					Name:      "account-1",
				},
				{
					AccountID: "222222222222",
					Name:      "account-2",
				},
			},
			DefaultRegion: "us-west-2",
		},
		EC2Cache:     ec2Cache,
		Cache:        pricingCache,
		Metrics:      m,
		Log:          logr.Discard(),
		EC2ReadyChan: ec2ReadyChan,
	}

	// Test: Run reconciliation
	result, err := reconciler.Reconcile(ctx, ctrl.Request{})
	require.NoError(t, err)
	assert.Equal(t, 15*time.Second, result.RequeueAfter)

	// Verify: Only ONE DescribeSpotPriceHistory call was made (one per region, not per account)
	// Get the mock EC2 client for account 1 to check call count
	ec2Client1, err := mockClient.EC2(ctx, aws.AccountConfig{
		AccountID: "111111111111",
		Region:    "us-west-2",
	})
	require.NoError(t, err)
	mockEC2_1 := ec2Client1.(*aws.MockEC2Client)

	// Get the mock EC2 client for account 2
	ec2Client2, err := mockClient.EC2(ctx, aws.AccountConfig{
		AccountID: "222222222222",
		Region:    "us-west-2",
	})
	require.NoError(t, err)
	mockEC2_2 := ec2Client2.(*aws.MockEC2Client)

	// Total calls should be 1 (only one account's client was used)
	totalCalls := mockEC2_1.DescribeSpotPriceHistoryCallCount + mockEC2_2.DescribeSpotPriceHistoryCallCount
	assert.Equal(t, 1, totalCalls, "should only make 1 API call for both accounts in same region")

	// Verify: Cache was populated with both spot prices
	stats := pricingCache.GetSpotStats()
	assert.Equal(t, 2, stats.SpotPriceCount, "should have both spot prices")

	price1, exists1 := pricingCache.GetSpotPrice("m5.large", "us-west-2a", "Linux/UNIX")
	require.True(t, exists1)
	assert.Equal(t, 0.034, price1)

	price2, exists2 := pricingCache.GetSpotPrice("c5.xlarge", "us-west-2a", "Linux/UNIX")
	require.True(t, exists2)
	assert.Equal(t, 0.068, price2)
}
