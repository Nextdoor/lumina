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

// Package cache provides thread-safe in-memory caches for RI/SP data.
//
// The cache supports atomic updates with graceful degradation - if an update
// fails, the old data is retained. This ensures the controller continues
// operating with potentially stale but valid data.
package cache

import (
	"time"

	"github.com/nextdoor/lumina/pkg/aws"
)

// RISPCache stores Reserved Instances and Savings Plans data with thread-safe access.
// Data is updated atomically on each refresh cycle. If a refresh fails, old data
// is retained to allow graceful degradation.
//
// RISPCache embeds BaseCache to provide common infrastructure (thread-safety,
// notifications, timestamps). This eliminates ~50 lines of boilerplate code.
type RISPCache struct {
	BaseCache // Provides: Lock/RLock, RegisterUpdateNotifier, NotifyUpdate, MarkUpdated, GetLastUpdate, etc.

	// Reserved Instances indexed by region then account ID
	// Structure: map[region]map[accountID][]ReservedInstance
	reservedInstances map[string]map[string][]aws.ReservedInstance

	// Savings Plans indexed by account ID
	// Savings Plans are organization-wide (not regional)
	// Structure: map[accountID][]SavingsPlan
	savingsPlans map[string][]aws.SavingsPlan

	// Freshness tracking per account/region
	// This is domain-specific (tracks per-account/region updates) vs BaseCache's global lastUpdate
	freshness map[string]time.Time
}

// NewRISPCache creates a new empty RI/SP cache.
func NewRISPCache() *RISPCache {
	return &RISPCache{
		BaseCache:         NewBaseCache(),
		reservedInstances: make(map[string]map[string][]aws.ReservedInstance),
		savingsPlans:      make(map[string][]aws.SavingsPlan),
		freshness:         make(map[string]time.Time),
	}
}

// UpdateReservedInstances atomically replaces all RI data for a region/account.
// This should be called after successfully querying AWS APIs.
func (c *RISPCache) UpdateReservedInstances(region, accountID string, ris []aws.ReservedInstance) {
	c.Lock() // From BaseCache
	defer c.Unlock()

	// Initialize region map if needed
	if c.reservedInstances[region] == nil {
		c.reservedInstances[region] = make(map[string][]aws.ReservedInstance)
	}

	// Replace data for this region/account
	c.reservedInstances[region][accountID] = ris

	// Update freshness
	key := BuildKey(":", region, accountID, "ri")
	c.freshness[key] = time.Now()
	c.MarkUpdated() // From BaseCache

	// Notify subscribers after releasing the write lock
	c.NotifyUpdate() // From BaseCache
}

// UpdateSavingsPlans atomically replaces all SP data for an account.
// This should be called after successfully querying AWS APIs.
func (c *RISPCache) UpdateSavingsPlans(accountID string, sps []aws.SavingsPlan) {
	c.Lock() // From BaseCache
	defer c.Unlock()

	// Replace data for this account
	c.savingsPlans[accountID] = sps

	// Update freshness
	key := BuildKey(":", accountID, "sp")
	c.freshness[key] = time.Now()
	c.MarkUpdated() // From BaseCache

	// Notify subscribers after releasing the write lock
	c.NotifyUpdate() // From BaseCache
}

// RegisterUpdateNotifier is inherited from BaseCache.
// Multiple notifiers can be registered. Callbacks are invoked in separate goroutines
// to prevent blocking cache operations.
//
// This is typically used to trigger cost recalculation when RI/SP data changes.

// GetReservedInstances returns all RIs for a specific region/account.
// Returns empty slice if no data exists (never returns nil).
func (c *RISPCache) GetReservedInstances(region, accountID string) []aws.ReservedInstance {
	c.RLock() // From BaseCache
	defer c.RUnlock()

	if regionMap, ok := c.reservedInstances[region]; ok {
		if ris, ok := regionMap[accountID]; ok {
			// Return a copy to prevent external modification
			result := make([]aws.ReservedInstance, len(ris))
			copy(result, ris)
			return result
		}
	}

	return []aws.ReservedInstance{}
}

// GetAllReservedInstances returns all RIs across all regions and accounts.
// Returns empty slice if no data exists.
func (c *RISPCache) GetAllReservedInstances() []aws.ReservedInstance {
	c.RLock() // From BaseCache
	defer c.RUnlock()

	var all []aws.ReservedInstance
	for _, regionMap := range c.reservedInstances {
		for _, ris := range regionMap {
			all = append(all, ris...)
		}
	}

	return all
}

// GetSavingsPlans returns all SPs for a specific account.
// Returns empty slice if no data exists (never returns nil).
func (c *RISPCache) GetSavingsPlans(accountID string) []aws.SavingsPlan {
	c.RLock() // From BaseCache
	defer c.RUnlock()

	if sps, ok := c.savingsPlans[accountID]; ok {
		// Return a copy to prevent external modification
		result := make([]aws.SavingsPlan, len(sps))
		copy(result, sps)
		return result
	}

	return []aws.SavingsPlan{}
}

// GetAllSavingsPlans returns all SPs across all accounts.
// Returns empty slice if no data exists.
func (c *RISPCache) GetAllSavingsPlans() []aws.SavingsPlan {
	c.RLock() // From BaseCache
	defer c.RUnlock()

	var all []aws.SavingsPlan
	for _, sps := range c.savingsPlans {
		all = append(all, sps...)
	}

	return all
}

// GetFreshness returns the last update time for a specific data type.
// Returns zero time if never updated.
// This is domain-specific freshness tracking (per-account/region) vs BaseCache's global timestamp.
func (c *RISPCache) GetFreshness(key string) time.Time {
	c.RLock() // From BaseCache
	defer c.RUnlock()

	if t, ok := c.freshness[key]; ok {
		return t
	}

	return time.Time{}
}

// GetLastUpdate is inherited from BaseCache and returns the global last update timestamp.
// For per-account/region freshness, use GetFreshness instead.

// GetStats returns cache statistics for monitoring.
func (c *RISPCache) GetStats() CacheStats {
	c.RLock() // From BaseCache
	defer c.RUnlock()

	riCount := 0
	for _, regionMap := range c.reservedInstances {
		for _, ris := range regionMap {
			riCount += len(ris)
		}
	}

	spCount := 0
	for _, sps := range c.savingsPlans {
		spCount += len(sps)
	}

	return CacheStats{
		ReservedInstanceCount: riCount,
		SavingsPlanCount:      spCount,
		LastUpdate:            c.GetLastUpdate(), // From BaseCache
		RegionCount:           len(c.reservedInstances),
		AccountCount:          len(c.savingsPlans),
	}
}

// CacheStats contains statistics about the cache contents.
type CacheStats struct {
	ReservedInstanceCount int
	SavingsPlanCount      int
	LastUpdate            time.Time
	RegionCount           int
	AccountCount          int
}
