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

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSPUtilizationCacheGetFresh(t *testing.T) {
	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	cache := NewSPUtilizationCache()

	_, ok := cache.GetFresh(now, 3*time.Hour)
	assert.False(t, ok)

	cache.UpdateComputeUnusedCommitment(1.25, now.Add(-time.Hour))
	observation, ok := cache.GetFresh(now, time.Hour)
	require.True(t, ok, "an observation at the exact freshness boundary should be accepted")
	assert.Equal(t, 1.25, observation.UnusedCommitment)

	_, ok = cache.GetFresh(now, 30*time.Minute)
	assert.False(t, ok)
}
