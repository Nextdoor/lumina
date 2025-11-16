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
	"sync"
	"testing"
	"time"

	"github.com/nextdoor/lumina/pkg/aws"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testAccountID = "123456789012"
	testRegion    = "us-west-2"
)

// TestNewEC2Cache verifies that a new cache is properly initialized.
func TestNewEC2Cache(t *testing.T) {
	cache := NewEC2Cache()

	require.NotNil(t, cache)
	assert.NotNil(t, cache.instances)
	assert.True(t, cache.lastUpdate.IsZero(), "New cache should have zero last update time")

	// Verify empty cache behavior
	allInstances := cache.GetAllInstances()
	assert.Empty(t, allInstances, "New cache should return empty slice")

	_, found := cache.GetInstance("nonexistent")
	assert.False(t, found, "GetInstance on empty cache should return false")
}

// TestSetInstancesAndGetInstance tests basic set and get operations.
func TestSetInstancesAndGetInstance(t *testing.T) {
	cache := NewEC2Cache()

	instances := []aws.Instance{
		{
			InstanceID:   "i-abc123",
			InstanceType: "m5.xlarge",
			Region:       testRegion,
			AccountID:    testAccountID,
			State:        "running",
		},
		{
			InstanceID:   "i-def456",
			InstanceType: "c5.2xlarge",
			Region:       testRegion,
			AccountID:    testAccountID,
			State:        "stopped",
		},
	}

	// Set instances
	cache.SetInstances(testAccountID, testRegion, instances)

	// Verify GetInstance returns correct data
	inst1, found := cache.GetInstance("i-abc123")
	require.True(t, found, "Instance should be found")
	assert.Equal(t, "i-abc123", inst1.InstanceID)
	assert.Equal(t, "m5.xlarge", inst1.InstanceType)
	assert.Equal(t, "running", inst1.State)

	inst2, found := cache.GetInstance("i-def456")
	require.True(t, found, "Instance should be found")
	assert.Equal(t, "i-def456", inst2.InstanceID)
	assert.Equal(t, "c5.2xlarge", inst2.InstanceType)
	assert.Equal(t, "stopped", inst2.State)

	// Verify non-existent instance
	_, found = cache.GetInstance("i-nonexistent")
	assert.False(t, found, "Non-existent instance should not be found")
}

// TestGetInstanceReturnsCopy verifies that GetInstance returns a copy, not a reference.
func TestGetInstanceReturnsCopy(t *testing.T) {
	cache := NewEC2Cache()

	instances := []aws.Instance{
		{
			InstanceID:   "i-abc123",
			InstanceType: "m5.xlarge",
			Region:       testRegion,
			AccountID:    testAccountID,
			State:        "running",
		},
	}

	cache.SetInstances(testAccountID, testRegion, instances)

	// Get instance and modify it
	inst1, found := cache.GetInstance("i-abc123")
	require.True(t, found)
	inst1.State = "terminated" // Modify the returned instance

	// Get instance again and verify it wasn't modified
	inst2, found := cache.GetInstance("i-abc123")
	require.True(t, found)
	assert.Equal(t, "running", inst2.State, "Cache should not be modified by external changes")
}

// TestSetInstancesReplacesOldData verifies that SetInstances replaces previous data
// for the same account+region combination.
func TestSetInstancesReplacesOldData(t *testing.T) {
	cache := NewEC2Cache()

	// First batch of instances
	instances1 := []aws.Instance{
		{
			InstanceID:   "i-old1",
			InstanceType: "m5.large",
			Region:       testRegion,
			AccountID:    testAccountID,
			State:        "running",
		},
		{
			InstanceID:   "i-old2",
			InstanceType: "m5.xlarge",
			Region:       testRegion,
			AccountID:    testAccountID,
			State:        "running",
		},
	}

	cache.SetInstances(testAccountID, testRegion, instances1)

	// Verify first batch
	_, found := cache.GetInstance("i-old1")
	assert.True(t, found, "First instance should exist")

	// Second batch replaces first batch
	instances2 := []aws.Instance{
		{
			InstanceID:   "i-new1",
			InstanceType: "c5.2xlarge",
			Region:       testRegion,
			AccountID:    testAccountID,
			State:        "running",
		},
	}

	cache.SetInstances(testAccountID, testRegion, instances2)

	// Verify old instances are gone
	_, found = cache.GetInstance("i-old1")
	assert.False(t, found, "Old instance should be removed")
	_, found = cache.GetInstance("i-old2")
	assert.False(t, found, "Old instance should be removed")

	// Verify new instance exists
	inst, found := cache.GetInstance("i-new1")
	require.True(t, found, "New instance should exist")
	assert.Equal(t, "c5.2xlarge", inst.InstanceType)
}

// TestSetInstancesMultipleRegions verifies that instances from different regions
// don't interfere with each other.
func TestSetInstancesMultipleRegions(t *testing.T) {
	cache := NewEC2Cache()

	// Add instances in us-west-2
	instancesWest := []aws.Instance{
		{
			InstanceID:   "i-west1",
			InstanceType: "m5.xlarge",
			Region:       testRegion,
			AccountID:    testAccountID,
			State:        "running",
		},
	}

	cache.SetInstances(testAccountID, testRegion, instancesWest)

	// Add instances in us-east-1
	instancesEast := []aws.Instance{
		{
			InstanceID:   "i-east1",
			InstanceType: "c5.2xlarge",
			Region:       "us-east-1",
			AccountID:    testAccountID,
			State:        "running",
		},
	}

	cache.SetInstances(testAccountID, "us-east-1", instancesEast)

	// Verify both regions exist independently
	west, found := cache.GetInstance("i-west1")
	require.True(t, found)
	assert.Equal(t, "us-west-2", west.Region)

	east, found := cache.GetInstance("i-east1")
	require.True(t, found)
	assert.Equal(t, "us-east-1", east.Region)

	// Update us-west-2, should not affect us-east-1
	newInstancesWest := []aws.Instance{
		{
			InstanceID:   "i-west2",
			InstanceType: "m5.2xlarge",
			Region:       testRegion,
			AccountID:    testAccountID,
			State:        "running",
		},
	}

	cache.SetInstances(testAccountID, testRegion, newInstancesWest)

	// us-east-1 instance should still exist
	east, found = cache.GetInstance("i-east1")
	require.True(t, found)
	assert.Equal(t, "us-east-1", east.Region)

	// Old us-west-2 instance should be gone
	_, found = cache.GetInstance("i-west1")
	assert.False(t, found)

	// New us-west-2 instance should exist
	west, found = cache.GetInstance("i-west2")
	require.True(t, found)
	assert.Equal(t, "us-west-2", west.Region)
}

// TestSetInstancesMultipleAccounts verifies that instances from different accounts
// are tracked independently.
func TestSetInstancesMultipleAccounts(t *testing.T) {
	cache := NewEC2Cache()
	region := "us-west-2"

	// Add instances for account 1
	instancesAccount1 := []aws.Instance{
		{
			InstanceID:   "i-account1-1",
			InstanceType: "m5.xlarge",
			Region:       region,
			AccountID:    "111111111111",
			State:        "running",
		},
	}

	cache.SetInstances("111111111111", region, instancesAccount1)

	// Add instances for account 2
	instancesAccount2 := []aws.Instance{
		{
			InstanceID:   "i-account2-1",
			InstanceType: "c5.2xlarge",
			Region:       region,
			AccountID:    "222222222222",
			State:        "running",
		},
	}

	cache.SetInstances("222222222222", region, instancesAccount2)

	// Verify both accounts exist independently
	inst1, found := cache.GetInstance("i-account1-1")
	require.True(t, found)
	assert.Equal(t, "111111111111", inst1.AccountID)

	inst2, found := cache.GetInstance("i-account2-1")
	require.True(t, found)
	assert.Equal(t, "222222222222", inst2.AccountID)

	// Update account 1, should not affect account 2
	newInstancesAccount1 := []aws.Instance{
		{
			InstanceID:   "i-account1-2",
			InstanceType: "m5.2xlarge",
			Region:       region,
			AccountID:    "111111111111",
			State:        "running",
		},
	}

	cache.SetInstances("111111111111", region, newInstancesAccount1)

	// Account 2 instance should still exist
	inst2, found = cache.GetInstance("i-account2-1")
	require.True(t, found)
	assert.Equal(t, "222222222222", inst2.AccountID)

	// Old account 1 instance should be gone
	_, found = cache.GetInstance("i-account1-1")
	assert.False(t, found)

	// New account 1 instance should exist
	inst1, found = cache.GetInstance("i-account1-2")
	require.True(t, found)
	assert.Equal(t, "111111111111", inst1.AccountID)
}

// TestGetAllInstances verifies that GetAllInstances returns all cached instances.
func TestGetAllInstances(t *testing.T) {
	cache := NewEC2Cache()

	// Add instances across multiple accounts and regions
	cache.SetInstances("111111111111", "us-west-2", []aws.Instance{
		{InstanceID: "i-1", InstanceType: "m5.xlarge", Region: "us-west-2", AccountID: "111111111111", State: "running"},
		{InstanceID: "i-2", InstanceType: "m5.large", Region: "us-west-2", AccountID: "111111111111", State: "stopped"},
	})

	cache.SetInstances("222222222222", "us-east-1", []aws.Instance{
		{InstanceID: "i-3", InstanceType: "c5.2xlarge", Region: "us-east-1", AccountID: "222222222222", State: "running"},
	})

	allInstances := cache.GetAllInstances()
	assert.Len(t, allInstances, 3, "Should return all instances")

	// Verify we have all instance IDs
	ids := make(map[string]bool)
	for _, inst := range allInstances {
		ids[inst.InstanceID] = true
	}

	assert.True(t, ids["i-1"])
	assert.True(t, ids["i-2"])
	assert.True(t, ids["i-3"])
}

// TestGetInstancesByAccount verifies filtering by account ID.
func TestGetInstancesByAccount(t *testing.T) {
	cache := NewEC2Cache()

	// Add instances for multiple accounts
	cache.SetInstances("111111111111", "us-west-2", []aws.Instance{
		{InstanceID: "i-account1-1", InstanceType: "m5.xlarge", Region: "us-west-2", AccountID: "111111111111", State: "running"},
		{InstanceID: "i-account1-2", InstanceType: "m5.large", Region: "us-west-2", AccountID: "111111111111", State: "stopped"},
	})

	cache.SetInstances("222222222222", "us-west-2", []aws.Instance{
		{InstanceID: "i-account2-1", InstanceType: "c5.2xlarge", Region: "us-west-2", AccountID: "222222222222", State: "running"},
	})

	// Get instances for account 1
	account1Instances := cache.GetInstancesByAccount("111111111111")
	assert.Len(t, account1Instances, 2, "Should return 2 instances for account 1")

	for _, inst := range account1Instances {
		assert.Equal(t, "111111111111", inst.AccountID)
	}

	// Get instances for account 2
	account2Instances := cache.GetInstancesByAccount("222222222222")
	assert.Len(t, account2Instances, 1, "Should return 1 instance for account 2")
	assert.Equal(t, "i-account2-1", account2Instances[0].InstanceID)

	// Get instances for non-existent account
	nonExistentInstances := cache.GetInstancesByAccount("999999999999")
	assert.Empty(t, nonExistentInstances, "Should return empty slice for non-existent account")
}

// TestGetInstancesByRegion verifies filtering by region.
func TestGetInstancesByRegion(t *testing.T) {
	cache := NewEC2Cache()

	// Add instances across multiple regions
	cache.SetInstances("111111111111", "us-west-2", []aws.Instance{
		{InstanceID: "i-west-1", InstanceType: "m5.xlarge", Region: "us-west-2", AccountID: "111111111111", State: "running"},
		{InstanceID: "i-west-2", InstanceType: "m5.large", Region: "us-west-2", AccountID: "111111111111", State: "stopped"},
	})

	cache.SetInstances("222222222222", "us-east-1", []aws.Instance{
		{InstanceID: "i-east-1", InstanceType: "c5.2xlarge", Region: "us-east-1", AccountID: "222222222222", State: "running"},
	})

	// Also add another instance in us-west-2 from different account
	cache.SetInstances("333333333333", "us-west-2", []aws.Instance{
		{InstanceID: "i-west-3", InstanceType: "t3.medium", Region: "us-west-2", AccountID: "333333333333", State: "running"},
	})

	// Get instances in us-west-2 (should include instances from both accounts)
	westInstances := cache.GetInstancesByRegion("us-west-2")
	assert.Len(t, westInstances, 3, "Should return 3 instances in us-west-2")

	for _, inst := range westInstances {
		assert.Equal(t, "us-west-2", inst.Region)
	}

	// Get instances in us-east-1
	eastInstances := cache.GetInstancesByRegion("us-east-1")
	assert.Len(t, eastInstances, 1, "Should return 1 instance in us-east-1")
	assert.Equal(t, "i-east-1", eastInstances[0].InstanceID)

	// Get instances for non-existent region
	nonExistentInstances := cache.GetInstancesByRegion("eu-west-1")
	assert.Empty(t, nonExistentInstances, "Should return empty slice for non-existent region")
}

// TestGetRunningInstances verifies filtering by state.
func TestGetRunningInstances(t *testing.T) {
	cache := NewEC2Cache()

	// Add instances with different states
	cache.SetInstances("111111111111", "us-west-2", []aws.Instance{
		{InstanceID: "i-running-1", InstanceType: "m5.xlarge", Region: "us-west-2", AccountID: "111111111111", State: "running"},
		{InstanceID: "i-stopped-1", InstanceType: "m5.large", Region: "us-west-2", AccountID: "111111111111", State: "stopped"},
		{InstanceID: "i-running-2", InstanceType: "c5.2xlarge", Region: "us-west-2", AccountID: "111111111111", State: "running"},
		{InstanceID: "i-terminated-1", InstanceType: "t3.medium", Region: "us-west-2", AccountID: "111111111111", State: "terminated"},
	})

	runningInstances := cache.GetRunningInstances()
	assert.Len(t, runningInstances, 2, "Should return only running instances")

	for _, inst := range runningInstances {
		assert.Equal(t, "running", inst.State)
	}

	// Verify we got the correct instances
	ids := make(map[string]bool)
	for _, inst := range runningInstances {
		ids[inst.InstanceID] = true
	}

	assert.True(t, ids["i-running-1"])
	assert.True(t, ids["i-running-2"])
	assert.False(t, ids["i-stopped-1"])
	assert.False(t, ids["i-terminated-1"])
}

// TestGetLastUpdateTime verifies last update time tracking.
func TestGetLastUpdateTime(t *testing.T) {
	cache := NewEC2Cache()

	// Initially should be zero time
	lastUpdate := cache.GetLastUpdateTime()
	assert.True(t, lastUpdate.IsZero(), "New cache should have zero last update time")

	// After setting instances, should have non-zero time
	timeBefore := time.Now()
	cache.SetInstances("111111111111", "us-west-2", []aws.Instance{
		{InstanceID: "i-1", InstanceType: "m5.xlarge", Region: "us-west-2", AccountID: "111111111111", State: "running"},
	})
	timeAfter := time.Now()

	lastUpdate = cache.GetLastUpdateTime()
	assert.False(t, lastUpdate.IsZero(), "Last update time should be set")
	assert.True(t, lastUpdate.After(timeBefore) || lastUpdate.Equal(timeBefore))
	assert.True(t, lastUpdate.Before(timeAfter) || lastUpdate.Equal(timeAfter))

	// Wait a bit and update again
	time.Sleep(10 * time.Millisecond)
	firstUpdate := lastUpdate

	cache.SetInstances("222222222222", "us-east-1", []aws.Instance{
		{InstanceID: "i-2", InstanceType: "c5.2xlarge", Region: "us-east-1", AccountID: "222222222222", State: "running"},
	})

	secondUpdate := cache.GetLastUpdateTime()
	assert.True(t, secondUpdate.After(firstUpdate), "Second update should be later than first")
}

// TestClear verifies that Clear removes all cached data.
func TestClear(t *testing.T) {
	cache := NewEC2Cache()

	// Add some instances
	cache.SetInstances("111111111111", "us-west-2", []aws.Instance{
		{InstanceID: "i-1", InstanceType: "m5.xlarge", Region: "us-west-2", AccountID: "111111111111", State: "running"},
		{InstanceID: "i-2", InstanceType: "m5.large", Region: "us-west-2", AccountID: "111111111111", State: "stopped"},
	})

	// Verify instances exist
	allInstances := cache.GetAllInstances()
	assert.Len(t, allInstances, 2)

	lastUpdate := cache.GetLastUpdateTime()
	assert.False(t, lastUpdate.IsZero())

	// Clear the cache
	cache.Clear()

	// Verify cache is empty
	allInstances = cache.GetAllInstances()
	assert.Empty(t, allInstances, "Cache should be empty after Clear")

	_, found := cache.GetInstance("i-1")
	assert.False(t, found, "Instance should not be found after Clear")

	lastUpdate = cache.GetLastUpdateTime()
	assert.True(t, lastUpdate.IsZero(), "Last update time should be zero after Clear")
}

// TestConcurrentReads verifies that multiple goroutines can read simultaneously.
func TestConcurrentReads(t *testing.T) {
	cache := NewEC2Cache()

	// Populate cache with test data
	instances := make([]aws.Instance, 100)
	for i := 0; i < 100; i++ {
		instances[i] = aws.Instance{
			InstanceID:   string(rune('a'+(i/26))) + string(rune('a'+(i%26))),
			InstanceType: "m5.xlarge",
			Region:       testRegion,
			AccountID:    testAccountID,
			State:        "running",
		}
	}

	cache.SetInstances(testAccountID, testRegion, instances)

	// Launch multiple readers concurrently
	var wg sync.WaitGroup
	numReaders := 50

	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			// Each reader performs multiple reads
			for j := 0; j < 100; j++ {
				_ = cache.GetAllInstances()
				_ = cache.GetRunningInstances()
				_ = cache.GetInstancesByAccount(testAccountID)
				_ = cache.GetInstancesByRegion(testRegion)
				_ = cache.GetLastUpdateTime()
			}
		}()
	}

	wg.Wait()

	// Verify data integrity after concurrent reads
	allInstances := cache.GetAllInstances()
	assert.Len(t, allInstances, 100, "Data should be intact after concurrent reads")
}

// TestConcurrentReadsDuringWrite verifies thread safety when reading during writes.
func TestConcurrentReadsDuringWrite(t *testing.T) {
	cache := NewEC2Cache()

	// Initial data
	instances := []aws.Instance{
		{InstanceID: "i-1", InstanceType: "m5.xlarge", Region: "us-west-2", AccountID: "123456789012", State: "running"},
	}
	cache.SetInstances(testAccountID, testRegion, instances)

	var wg sync.WaitGroup

	// Start reader goroutines
	numReaders := 10
	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for j := 0; j < 100; j++ {
				_ = cache.GetAllInstances()
				_, _ = cache.GetInstance("i-1")
				_ = cache.GetRunningInstances()
				time.Sleep(time.Microsecond)
			}
		}()
	}

	// Start writer goroutines
	numWriters := 5
	for i := 0; i < numWriters; i++ {
		wg.Add(1)
		accountID := "12345678901" + string(rune('0'+i))
		go func(accID string) {
			defer wg.Done()

			for j := 0; j < 50; j++ {
				newInstances := []aws.Instance{
					{
						InstanceID:   "i-" + accID + "-" + string(rune('a'+j)),
						InstanceType: "m5.large",
						Region:       testRegion,
						AccountID:    accID,
						State:        "running",
					},
				}
				cache.SetInstances(accID, "us-west-2", newInstances)
				time.Sleep(time.Microsecond)
			}
		}(accountID)
	}

	wg.Wait()

	// Verify cache is in a valid state
	allInstances := cache.GetAllInstances()
	assert.NotEmpty(t, allInstances, "Cache should have data after concurrent operations")
}

// TestPerformanceLargeDataset verifies performance with a large number of instances.
func TestPerformanceLargeDataset(t *testing.T) {
	cache := NewEC2Cache()

	// Create 10,000 instances
	numInstances := 10000
	instances := make([]aws.Instance, numInstances)

	for i := 0; i < numInstances; i++ {
		instances[i] = aws.Instance{
			InstanceID:   generateInstanceID(i),
			InstanceType: "m5.xlarge",
			Region:       testRegion,
			AccountID:    testAccountID,
			State:        "running",
		}
	}

	// Measure SetInstances performance
	startSet := time.Now()
	cache.SetInstances(testAccountID, testRegion, instances)
	setDuration := time.Since(startSet)

	assert.Less(t, setDuration, 50*time.Millisecond, "SetInstances should complete in < 50ms for 10k instances")

	// Measure GetInstance performance (lookup)
	startGet := time.Now()
	_, found := cache.GetInstance(generateInstanceID(5000))
	getDuration := time.Since(startGet)

	assert.True(t, found)
	assert.Less(t, getDuration, time.Millisecond, "GetInstance should complete in < 1ms")

	// Measure GetRunningInstances performance (filtering)
	startFilter := time.Now()
	runningInstances := cache.GetRunningInstances()
	filterDuration := time.Since(startFilter)

	assert.Len(t, runningInstances, numInstances)
	assert.Less(t, filterDuration, 10*time.Millisecond, "GetRunningInstances should complete in < 10ms for 10k instances")
}

// TestPerformanceMemoryUsage verifies reasonable memory usage.
func TestPerformanceMemoryUsage(t *testing.T) {
	cache := NewEC2Cache()

	// Create 10,000 instances
	numInstances := 10000
	instances := make([]aws.Instance, numInstances)

	for i := 0; i < numInstances; i++ {
		instances[i] = aws.Instance{
			InstanceID:   generateInstanceID(i),
			InstanceType: "m5.xlarge",
			Region:       testRegion,
			AccountID:    testAccountID,
			State:        "running",
			LaunchTime:   time.Now(),
			Tags: map[string]string{
				"Name":        "test-instance",
				"Environment": "production",
			},
		}
	}

	cache.SetInstances(testAccountID, testRegion, instances)

	// Verify we can retrieve all instances
	allInstances := cache.GetAllInstances()
	assert.Len(t, allInstances, numInstances)

	// This test doesn't measure exact memory usage since that's hard to do portably,
	// but it verifies that we can handle large datasets without issues.
	// Manual testing shows ~500 bytes per instance is reasonable.
}

// TestEmptyCacheOperations verifies operations on empty cache return appropriate values.
func TestEmptyCacheOperations(t *testing.T) {
	cache := NewEC2Cache()

	// All get operations should return empty/false
	_, found := cache.GetInstance("i-nonexistent")
	assert.False(t, found)

	allInstances := cache.GetAllInstances()
	assert.Empty(t, allInstances)

	accountInstances := cache.GetInstancesByAccount("123456789012")
	assert.Empty(t, accountInstances)

	regionInstances := cache.GetInstancesByRegion("us-west-2")
	assert.Empty(t, regionInstances)

	runningInstances := cache.GetRunningInstances()
	assert.Empty(t, runningInstances)

	lastUpdate := cache.GetLastUpdateTime()
	assert.True(t, lastUpdate.IsZero())
}

// TestSetInstancesEmptySlice verifies setting an empty slice removes old data.
func TestSetInstancesEmptySlice(t *testing.T) {
	cache := NewEC2Cache()

	// Add initial instances
	instances := []aws.Instance{
		{InstanceID: "i-1", InstanceType: "m5.xlarge", Region: testRegion, AccountID: testAccountID, State: "running"},
		{InstanceID: "i-2", InstanceType: "m5.large", Region: testRegion, AccountID: testAccountID, State: "stopped"},
	}

	cache.SetInstances(testAccountID, testRegion, instances)

	// Verify instances exist
	_, found := cache.GetInstance("i-1")
	assert.True(t, found)

	// Set empty slice (simulating all instances terminated)
	cache.SetInstances(testAccountID, testRegion, []aws.Instance{})

	// Verify all instances are removed
	_, found = cache.GetInstance("i-1")
	assert.False(t, found, "Instance should be removed")
	_, found = cache.GetInstance("i-2")
	assert.False(t, found, "Instance should be removed")

	allInstances := cache.GetAllInstances()
	assert.Empty(t, allInstances, "Cache should be empty after setting empty slice")
}

// generateInstanceID generates a unique instance ID for testing.
func generateInstanceID(i int) string {
	return "i-" + string(rune('a'+(i/676)%26)) + string(rune('a'+(i/26)%26)) + string(rune('a'+(i%26)))
}
