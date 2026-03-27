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
	"fmt"
	"net/http"
	"strings"
	"sync"
)

// ReconcilerHealthTracker tracks the health of reconciler goroutines.
// When a reconciler's Run() method exits (e.g., after exhausting retries on initial load),
// it marks itself as failed in this tracker. The readiness probe checks this tracker
// to detect dead reconcilers and trigger a pod restart via Kubernetes.
//
// Thread-safe: all methods can be called concurrently from multiple goroutines.
type ReconcilerHealthTracker struct {
	mu     sync.RWMutex
	failed map[string]error
}

// NewReconcilerHealthTracker creates a new health tracker.
func NewReconcilerHealthTracker() *ReconcilerHealthTracker {
	return &ReconcilerHealthTracker{
		failed: make(map[string]error),
	}
}

// MarkFailed records that a reconciler has permanently failed.
// This is called when a reconciler's Run() method exits after exhausting retries.
// Once marked failed, the readiness probe will fail, causing Kubernetes to restart the pod.
func (t *ReconcilerHealthTracker) MarkFailed(name string, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.failed[name] = err
}

// Check implements the healthz.Checker interface for use as a readiness probe.
// Returns nil if all reconcilers are healthy, or an error listing all failed reconcilers.
func (t *ReconcilerHealthTracker) Check(_ *http.Request) error {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if len(t.failed) == 0 {
		return nil
	}

	// Build a descriptive error listing all failed reconcilers
	var failures []string
	for name, err := range t.failed {
		failures = append(failures, fmt.Sprintf("%s: %v", name, err))
	}

	return fmt.Errorf("reconcilers failed: %s", strings.Join(failures, "; "))
}

// GetFailed returns a copy of the current failure map.
// Useful for debugging and logging.
func (t *ReconcilerHealthTracker) GetFailed() map[string]error {
	t.mu.RLock()
	defer t.mu.RUnlock()

	result := make(map[string]error, len(t.failed))
	for k, v := range t.failed {
		result[k] = v
	}
	return result
}
