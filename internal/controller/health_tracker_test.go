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

package controller

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewReconcilerHealthTracker(t *testing.T) {
	tracker := NewReconcilerHealthTracker()
	require.NotNil(t, tracker)
	assert.Empty(t, tracker.GetFailed())
}

func TestReconcilerHealthTracker_CheckHealthy(t *testing.T) {
	tracker := NewReconcilerHealthTracker()
	err := tracker.Check(&http.Request{})
	assert.NoError(t, err)
}

func TestReconcilerHealthTracker_CheckNilRequest(t *testing.T) {
	tracker := NewReconcilerHealthTracker()
	err := tracker.Check(nil)
	assert.NoError(t, err)
}

func TestReconcilerHealthTracker_MarkFailed(t *testing.T) {
	tracker := NewReconcilerHealthTracker()

	tracker.MarkFailed("pricing", fmt.Errorf("API rate limit exceeded"))

	err := tracker.Check(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pricing")
	assert.Contains(t, err.Error(), "API rate limit exceeded")
}

func TestReconcilerHealthTracker_MultipleFailures(t *testing.T) {
	tracker := NewReconcilerHealthTracker()

	tracker.MarkFailed("pricing", fmt.Errorf("pricing error"))
	tracker.MarkFailed("risp", fmt.Errorf("risp error"))

	err := tracker.Check(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pricing")
	assert.Contains(t, err.Error(), "risp")

	failed := tracker.GetFailed()
	assert.Len(t, failed, 2)
	assert.Contains(t, failed["pricing"].Error(), "pricing error")
	assert.Contains(t, failed["risp"].Error(), "risp error")
}

func TestReconcilerHealthTracker_GetFailedReturnsCopy(t *testing.T) {
	tracker := NewReconcilerHealthTracker()
	tracker.MarkFailed("pricing", fmt.Errorf("error"))

	// Get a copy and modify it
	failed := tracker.GetFailed()
	failed["fake"] = fmt.Errorf("fake error")

	// Original should be unchanged
	original := tracker.GetFailed()
	assert.Len(t, original, 1)
	assert.NotContains(t, original, "fake")
}

func TestReconcilerHealthTracker_ConcurrentAccess(t *testing.T) {
	tracker := NewReconcilerHealthTracker()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := fmt.Sprintf("reconciler-%d", i)
			tracker.MarkFailed(name, fmt.Errorf("error %d", i))
			_ = tracker.Check(nil)
			_ = tracker.GetFailed()
		}(i)
	}
	wg.Wait()

	failed := tracker.GetFailed()
	assert.Len(t, failed, 100)
}

func TestReconcilerHealthTracker_ErrorMessageFormat(t *testing.T) {
	tracker := NewReconcilerHealthTracker()
	tracker.MarkFailed("pricing", fmt.Errorf("throttling"))

	err := tracker.Check(nil)
	require.Error(t, err)
	assert.True(t, strings.HasPrefix(err.Error(), "reconcilers failed:"))
}
