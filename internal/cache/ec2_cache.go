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

// Package cache provides thread-safe in-memory caches for AWS resource data.
//
// This file implements EC2Cache for storing EC2 instance inventory data.
// For Reserved Instance and Savings Plan data, see risp_cache.go.
//
// EC2Cache stores EC2 instance data collected by the reconciler (5-minute refresh cycle).
// The cache is queried by:
// - Cost calculation algorithms (Phase 6) to determine actual instance costs
// - Metrics emission (Phase 5) to expose instance inventory and coverage data
//
// The cache design follows these principles:
// - Instance IDs are globally unique across AWS regions/accounts, enabling simple map lookup
// - Instances are stored with their account and region metadata for filtering
// - All instance states are cached (running, stopped, terminated) to track transitions
// - Updates are atomic per account+region to allow partial refreshes without clearing the cache
// - Thread-safe with sync.RWMutex allowing multiple concurrent readers
package cache

import (
	"time"

	"github.com/nextdoor/lumina/pkg/aws"
)

// EC2Cache stores EC2 instance data with thread-safe access.
// Instance IDs are globally unique across AWS, so we use them as the primary key.
// Each instance stores its own AccountID and Region for filtering operations.
//
// The cache supports partial updates via SetInstances(accountID, region, instances),
// which replaces only the instances for that specific account+region combination.
// This allows the reconciler to update data incrementally without clearing the entire cache.
//
// EC2Cache embeds BaseCache to provide common infrastructure (thread-safety,
// notifications, timestamps). This eliminates ~50 lines of boilerplate code.
type EC2Cache struct {
	BaseCache // Provides: Lock/RLock, RegisterUpdateNotifier, NotifyUpdate, MarkUpdated, GetLastUpdate, etc.

	// instances maps instance ID to instance data
	// Key: instance ID (e.g., "i-1234567890abcdef0")
	// Value: pointer to Instance struct (includes AccountID and Region fields)
	instances map[string]*aws.Instance
}

// NewEC2Cache creates a new empty EC2 instance cache.
func NewEC2Cache() *EC2Cache {
	return &EC2Cache{
		BaseCache: NewBaseCache(),
		instances: make(map[string]*aws.Instance),
	}
}

// SetInstances atomically replaces all instances for a specific account+region.
// This should be called after successfully querying the AWS DescribeInstances API.
//
// The method removes any existing instances for the given account+region combination
// before adding the new instances. This ensures old/terminated instances don't
// persist in the cache if they no longer appear in AWS API responses.
//
// Example usage in reconciler:
//
//	instances, err := client.DescribeInstances(ctx, "us-west-2", false)
//	if err == nil {
//	    cache.SetInstances("123456789012", "us-west-2", instances)
//	}
func (c *EC2Cache) SetInstances(accountID, region string, instances []aws.Instance) {
	c.Lock() // From BaseCache
	defer c.Unlock()

	// Remove existing instances for this account+region combination
	// This is more efficient than clearing the entire cache and prevents
	// old instances from persisting if they're terminated or deleted
	for id, inst := range c.instances {
		if inst.AccountID == accountID && inst.Region == region {
			delete(c.instances, id)
		}
	}

	// Add new instances
	// We store pointers to avoid copying large structs during Get operations
	for i := range instances {
		c.instances[instances[i].InstanceID] = &instances[i]
	}

	c.MarkUpdated() // From BaseCache

	// Notify subscribers after releasing the write lock
	// This prevents deadlock if notifiers try to read from the cache
	c.NotifyUpdate() // From BaseCache
}

// RegisterUpdateNotifier is inherited from BaseCache.
// Multiple notifiers can be registered. Callbacks are invoked in separate goroutines
// to prevent blocking cache operations.
//
// This is typically used to trigger cost recalculation when EC2 inventory changes.

// GetInstance returns a specific instance by its ID.
// Returns a copy of the instance and true if found, or nil and false if not found.
//
// The returned instance is a copy to prevent external code from modifying cache data.
func (c *EC2Cache) GetInstance(instanceID string) (*aws.Instance, bool) {
	c.RLock() // From BaseCache
	defer c.RUnlock()

	if inst, ok := c.instances[instanceID]; ok {
		// Return a copy to prevent external modification
		instCopy := *inst
		return &instCopy, true
	}

	return nil, false
}

// GetInstancesByAccount returns all instances belonging to a specific AWS account.
// Returns empty slice if no instances exist for that account.
//
// This is useful for calculating per-account costs or generating account-level metrics.
func (c *EC2Cache) GetInstancesByAccount(accountID string) []aws.Instance {
	c.RLock() // From BaseCache
	defer c.RUnlock()

	var result []aws.Instance
	for _, inst := range c.instances {
		if inst.AccountID == accountID {
			result = append(result, *inst)
		}
	}

	return result
}

// GetInstancesByRegion returns all instances in a specific AWS region across all accounts.
// Returns empty slice if no instances exist in that region.
//
// This is useful for region-specific cost analysis or detecting regional capacity issues.
func (c *EC2Cache) GetInstancesByRegion(region string) []aws.Instance {
	c.RLock() // From BaseCache
	defer c.RUnlock()

	var result []aws.Instance
	for _, inst := range c.instances {
		if inst.Region == region {
			result = append(result, *inst)
		}
	}

	return result
}

// GetAllInstances returns all cached instances across all accounts and regions.
// Returns empty slice if cache is empty.
//
// This is useful for organization-wide cost analysis and inventory reporting.
func (c *EC2Cache) GetAllInstances() []aws.Instance {
	c.RLock() // From BaseCache
	defer c.RUnlock()

	result := make([]aws.Instance, 0, len(c.instances))
	for _, inst := range c.instances {
		result = append(result, *inst)
	}

	return result
}

// GetRunningInstances returns only instances in "running" state.
// Returns empty slice if no running instances exist.
//
// Cost calculations typically only care about running instances since
// stopped instances don't incur compute charges (only EBS volume charges).
// This method filters the cache to running instances only, avoiding the need
// for callers to manually filter state.
func (c *EC2Cache) GetRunningInstances() []aws.Instance {
	c.RLock() // From BaseCache
	defer c.RUnlock()

	var result []aws.Instance
	for _, inst := range c.instances {
		if inst.State == "running" {
			result = append(result, *inst)
		}
	}

	return result
}

// GetLastUpdateTime returns when the cache was last updated.
// Returns zero time if cache has never been updated.
//
// This is useful for monitoring cache freshness and detecting stale data.
// This is an alias for BaseCache.GetLastUpdate() for backward compatibility.
func (c *EC2Cache) GetLastUpdateTime() time.Time {
	return c.GetLastUpdate() // From BaseCache
}

// Clear removes all cached data and resets the lastUpdate timestamp.
// This should only be called when completely resetting the cache,
// such as during shutdown or major reconfiguration.
//
// Under normal operation, use SetInstances to replace data incrementally
// rather than clearing the entire cache.
func (c *EC2Cache) Clear() {
	c.Lock() // From BaseCache
	defer c.Unlock()

	c.instances = make(map[string]*aws.Instance)
	// Reset lastUpdate to zero to indicate cache has never been populated
	c.lastUpdate = time.Time{}
}
