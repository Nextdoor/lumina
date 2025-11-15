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

package cache

import (
	"testing"
	"time"

	"github.com/nextdoor/lumina/pkg/aws"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewRISPCache verifies cache initialization.
func TestNewRISPCache(t *testing.T) {
	cache := NewRISPCache()

	assert.NotNil(t, cache)
	assert.NotNil(t, cache.reservedInstances)
	assert.NotNil(t, cache.savingsPlans)
	assert.NotNil(t, cache.freshness)
	assert.True(t, cache.lastUpdate.IsZero(), "lastUpdate should be zero time initially")

	// Verify empty state
	stats := cache.GetStats()
	assert.Equal(t, 0, stats.ReservedInstanceCount)
	assert.Equal(t, 0, stats.SavingsPlanCount)
	assert.True(t, stats.LastUpdate.IsZero())
}

// TestUpdateReservedInstances verifies RI updates.
func TestUpdateReservedInstances(t *testing.T) {
	cache := NewRISPCache()

	// Create test RIs
	ris := []aws.ReservedInstance{
		{
			ReservedInstanceID: "ri-12345",
			InstanceType:       "m5.large",
			Region:             "us-west-2",
			AccountID:          "111111111111",
			InstanceCount:      2,
		},
		{
			ReservedInstanceID: "ri-67890",
			InstanceType:       "m5.xlarge",
			Region:             "us-west-2",
			AccountID:          "111111111111",
			InstanceCount:      1,
		},
	}

	// Update cache
	beforeUpdate := time.Now()
	cache.UpdateReservedInstances("us-west-2", "111111111111", ris)
	afterUpdate := time.Now()

	// Verify data stored correctly
	retrieved := cache.GetReservedInstances("us-west-2", "111111111111")
	require.Len(t, retrieved, 2)
	assert.Equal(t, "ri-12345", retrieved[0].ReservedInstanceID)
	assert.Equal(t, "ri-67890", retrieved[1].ReservedInstanceID)

	// Verify freshness tracking
	freshness := cache.GetFreshness("us-west-2:111111111111:ri")
	assert.False(t, freshness.IsZero())
	assert.True(t, freshness.After(beforeUpdate) || freshness.Equal(beforeUpdate))
	assert.True(t, freshness.Before(afterUpdate) || freshness.Equal(afterUpdate))

	// Verify last update time
	lastUpdate := cache.GetLastUpdate()
	assert.False(t, lastUpdate.IsZero())
}

// TestUpdateReservedInstances_MultipleRegions verifies handling multiple regions.
func TestUpdateReservedInstances_MultipleRegions(t *testing.T) {
	cache := NewRISPCache()

	// Add RIs for different regions
	ris1 := []aws.ReservedInstance{
		{ReservedInstanceID: "ri-uswest", Region: "us-west-2", AccountID: "111111111111"},
	}
	ris2 := []aws.ReservedInstance{
		{ReservedInstanceID: "ri-useast", Region: "us-east-1", AccountID: "111111111111"},
	}

	cache.UpdateReservedInstances("us-west-2", "111111111111", ris1)
	cache.UpdateReservedInstances("us-east-1", "111111111111", ris2)

	// Verify both regions stored separately
	west := cache.GetReservedInstances("us-west-2", "111111111111")
	east := cache.GetReservedInstances("us-east-1", "111111111111")

	assert.Len(t, west, 1)
	assert.Equal(t, "ri-uswest", west[0].ReservedInstanceID)

	assert.Len(t, east, 1)
	assert.Equal(t, "ri-useast", east[0].ReservedInstanceID)

	// Verify GetAllReservedInstances returns both
	all := cache.GetAllReservedInstances()
	assert.Len(t, all, 2)
}

// TestUpdateReservedInstances_Replace verifies updates replace old data.
func TestUpdateReservedInstances_Replace(t *testing.T) {
	cache := NewRISPCache()

	// Initial data
	initial := []aws.ReservedInstance{
		{ReservedInstanceID: "ri-old", Region: "us-west-2", AccountID: "111111111111"},
	}
	cache.UpdateReservedInstances("us-west-2", "111111111111", initial)

	// Verify initial data
	retrieved := cache.GetReservedInstances("us-west-2", "111111111111")
	assert.Len(t, retrieved, 1)
	assert.Equal(t, "ri-old", retrieved[0].ReservedInstanceID)

	// Update with new data
	updated := []aws.ReservedInstance{
		{ReservedInstanceID: "ri-new1", Region: "us-west-2", AccountID: "111111111111"},
		{ReservedInstanceID: "ri-new2", Region: "us-west-2", AccountID: "111111111111"},
	}
	cache.UpdateReservedInstances("us-west-2", "111111111111", updated)

	// Verify old data replaced
	retrieved = cache.GetReservedInstances("us-west-2", "111111111111")
	assert.Len(t, retrieved, 2)
	assert.Equal(t, "ri-new1", retrieved[0].ReservedInstanceID)
	assert.Equal(t, "ri-new2", retrieved[1].ReservedInstanceID)
}

// TestGetReservedInstances_NotFound verifies behavior when no data exists.
func TestGetReservedInstances_NotFound(t *testing.T) {
	cache := NewRISPCache()

	// Query non-existent data
	retrieved := cache.GetReservedInstances("us-west-2", "111111111111")

	// Should return empty slice, not nil
	assert.NotNil(t, retrieved)
	assert.Len(t, retrieved, 0)
}

// TestGetReservedInstances_Immutability verifies returned data is a copy.
func TestGetReservedInstances_Immutability(t *testing.T) {
	cache := NewRISPCache()

	// Store data
	original := []aws.ReservedInstance{
		{ReservedInstanceID: "ri-123", InstanceCount: 1},
	}
	cache.UpdateReservedInstances("us-west-2", "111111111111", original)

	// Get data and modify it
	retrieved := cache.GetReservedInstances("us-west-2", "111111111111")
	retrieved[0].InstanceCount = 999

	// Verify cache data unchanged
	fromCache := cache.GetReservedInstances("us-west-2", "111111111111")
	assert.Equal(t, int32(1), fromCache[0].InstanceCount, "cache data should not be modified")
}

// TestUpdateSavingsPlans verifies SP updates.
func TestUpdateSavingsPlans(t *testing.T) {
	cache := NewRISPCache()

	// Create test SPs
	sps := []aws.SavingsPlan{
		{
			SavingsPlanARN:  "arn:aws:savingsplans::111111111111:savingsplan/sp-12345",
			SavingsPlanType: "Compute",
			Commitment:      100.0,
			AccountID:       "111111111111",
		},
		{
			SavingsPlanARN:  "arn:aws:savingsplans::111111111111:savingsplan/sp-67890",
			SavingsPlanType: "EC2Instance",
			Commitment:      50.0,
			AccountID:       "111111111111",
		},
	}

	// Update cache
	beforeUpdate := time.Now()
	cache.UpdateSavingsPlans("111111111111", sps)
	afterUpdate := time.Now()

	// Verify data stored correctly
	retrieved := cache.GetSavingsPlans("111111111111")
	require.Len(t, retrieved, 2)
	assert.Equal(t, "arn:aws:savingsplans::111111111111:savingsplan/sp-12345", retrieved[0].SavingsPlanARN)
	assert.Equal(t, "arn:aws:savingsplans::111111111111:savingsplan/sp-67890", retrieved[1].SavingsPlanARN)

	// Verify freshness tracking
	freshness := cache.GetFreshness("111111111111:sp")
	assert.False(t, freshness.IsZero())
	assert.True(t, freshness.After(beforeUpdate) || freshness.Equal(beforeUpdate))
	assert.True(t, freshness.Before(afterUpdate) || freshness.Equal(afterUpdate))
}

// TestUpdateSavingsPlans_Replace verifies updates replace old data.
func TestUpdateSavingsPlans_Replace(t *testing.T) {
	cache := NewRISPCache()

	// Initial data
	initial := []aws.SavingsPlan{
		{SavingsPlanARN: "arn:old", AccountID: "111111111111"},
	}
	cache.UpdateSavingsPlans("111111111111", initial)

	// Update with new data
	updated := []aws.SavingsPlan{
		{SavingsPlanARN: "arn:new", AccountID: "111111111111"},
	}
	cache.UpdateSavingsPlans("111111111111", updated)

	// Verify old data replaced
	retrieved := cache.GetSavingsPlans("111111111111")
	assert.Len(t, retrieved, 1)
	assert.Equal(t, "arn:new", retrieved[0].SavingsPlanARN)
}

// TestGetSavingsPlans_NotFound verifies behavior when no data exists.
func TestGetSavingsPlans_NotFound(t *testing.T) {
	cache := NewRISPCache()

	// Query non-existent data
	retrieved := cache.GetSavingsPlans("111111111111")

	// Should return empty slice, not nil
	assert.NotNil(t, retrieved)
	assert.Len(t, retrieved, 0)
}

// TestGetSavingsPlans_Immutability verifies returned data is a copy.
func TestGetSavingsPlans_Immutability(t *testing.T) {
	cache := NewRISPCache()

	// Store data
	original := []aws.SavingsPlan{
		{SavingsPlanARN: "arn:test", Commitment: 100.0},
	}
	cache.UpdateSavingsPlans("111111111111", original)

	// Get data and modify it
	retrieved := cache.GetSavingsPlans("111111111111")
	retrieved[0].Commitment = 999.0

	// Verify cache data unchanged
	fromCache := cache.GetSavingsPlans("111111111111")
	assert.Equal(t, 100.0, fromCache[0].Commitment, "cache data should not be modified")
}

// TestGetAllSavingsPlans verifies retrieving all SPs across accounts.
func TestGetAllSavingsPlans(t *testing.T) {
	cache := NewRISPCache()

	// Add SPs for different accounts
	sp1 := []aws.SavingsPlan{
		{SavingsPlanARN: "arn:sp1", AccountID: "111111111111"},
	}
	sp2 := []aws.SavingsPlan{
		{SavingsPlanARN: "arn:sp2", AccountID: "222222222222"},
	}

	cache.UpdateSavingsPlans("111111111111", sp1)
	cache.UpdateSavingsPlans("222222222222", sp2)

	// Verify GetAllSavingsPlans returns both
	all := cache.GetAllSavingsPlans()
	assert.Len(t, all, 2)
}

// TestGetStats verifies cache statistics.
func TestGetStats(t *testing.T) {
	cache := NewRISPCache()

	// Initially empty
	stats := cache.GetStats()
	assert.Equal(t, 0, stats.ReservedInstanceCount)
	assert.Equal(t, 0, stats.SavingsPlanCount)
	assert.Equal(t, 0, stats.RegionCount)
	assert.Equal(t, 0, stats.AccountCount)

	// Add some data
	cache.UpdateReservedInstances("us-west-2", "111111111111", []aws.ReservedInstance{
		{ReservedInstanceID: "ri-1"},
		{ReservedInstanceID: "ri-2"},
	})
	cache.UpdateReservedInstances("us-east-1", "111111111111", []aws.ReservedInstance{
		{ReservedInstanceID: "ri-3"},
	})
	cache.UpdateSavingsPlans("111111111111", []aws.SavingsPlan{
		{SavingsPlanARN: "arn:sp1"},
	})
	cache.UpdateSavingsPlans("222222222222", []aws.SavingsPlan{
		{SavingsPlanARN: "arn:sp2"},
		{SavingsPlanARN: "arn:sp3"},
	})

	// Verify stats
	stats = cache.GetStats()
	assert.Equal(t, 3, stats.ReservedInstanceCount)
	assert.Equal(t, 3, stats.SavingsPlanCount)
	assert.Equal(t, 2, stats.RegionCount)
	assert.Equal(t, 2, stats.AccountCount)
	assert.False(t, stats.LastUpdate.IsZero())
}

// TestGetFreshness_NotFound verifies behavior when freshness not tracked.
func TestGetFreshness_NotFound(t *testing.T) {
	cache := NewRISPCache()

	freshness := cache.GetFreshness("nonexistent:key")
	assert.True(t, freshness.IsZero(), "should return zero time for non-existent key")
}

// TestConcurrentAccess verifies thread-safety of cache operations.
func TestConcurrentAccess(t *testing.T) {
	cache := NewRISPCache()

	// Run concurrent updates and reads
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(id int) {
			// Concurrent writes
			ri := []aws.ReservedInstance{
				{ReservedInstanceID: "ri-concurrent"},
			}
			cache.UpdateReservedInstances("us-west-2", "111111111111", ri)

			sp := []aws.SavingsPlan{
				{SavingsPlanARN: "arn:concurrent"},
			}
			cache.UpdateSavingsPlans("111111111111", sp)

			// Concurrent reads
			_ = cache.GetReservedInstances("us-west-2", "111111111111")
			_ = cache.GetSavingsPlans("111111111111")
			_ = cache.GetStats()
			_ = cache.GetFreshness("us-west-2:111111111111:ri")

			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify cache still functional after concurrent access
	stats := cache.GetStats()
	assert.NotNil(t, stats)
}
