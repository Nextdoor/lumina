// Copyright 2025 Nextdoor, Inc.
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

import "time"

// SPUtilizationObservation is an organization-level Cost Explorer observation.
type SPUtilizationObservation struct {
	UnusedCommitment float64
	PeriodEnd        time.Time
}

// SPUtilizationCache stores the observed organization-level unused Compute
// Savings Plans commitment reported by AWS Cost Explorer.
type SPUtilizationCache struct {
	BaseCache
	observation *SPUtilizationObservation
}

// NewSPUtilizationCache creates an empty Savings Plans utilization cache.
func NewSPUtilizationCache() *SPUtilizationCache {
	return &SPUtilizationCache{BaseCache: NewBaseCache()}
}

// UpdateComputeUnusedCommitment replaces the current observation.
func (c *SPUtilizationCache) UpdateComputeUnusedCommitment(unused float64, periodEnd time.Time) {
	c.Lock()
	c.observation = &SPUtilizationObservation{
		UnusedCommitment: unused,
		PeriodEnd:        periodEnd,
	}
	c.MarkUpdated()
	c.Unlock()
	c.NotifyUpdate()
}

// GetFresh returns the observation when it is no older than maxAge.
func (c *SPUtilizationCache) GetFresh(now time.Time, maxAge time.Duration) (SPUtilizationObservation, bool) {
	c.RLock()
	defer c.RUnlock()

	if c.observation == nil || c.observation.PeriodEnd.IsZero() ||
		now.Sub(c.observation.PeriodEnd) > maxAge {
		return SPUtilizationObservation{}, false
	}
	return *c.observation, true
}
