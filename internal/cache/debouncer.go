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
	"time"
)

// Debouncer accumulates rapid events and triggers a callback only after a period
// of quiet. This prevents "thundering herd" scenarios where multiple data sources
// update simultaneously (e.g., EC2, RISP, and Pricing caches all refresh on startup).
//
// Example: If events arrive at T+0ms, T+100ms, and T+200ms with a 1-second delay,
// the callback fires once at T+1200ms (1 second after the LAST event).
//
// Thread-safe for concurrent Trigger() calls.
type Debouncer struct {
	mu       sync.Mutex
	timer    *time.Timer
	fn       func()
	duration time.Duration
}

// NewDebouncer creates a debouncer that waits 'duration' after the last trigger
// before invoking the callback function.
//
// The callback is invoked in a separate goroutine, so it won't block the Trigger() caller.
func NewDebouncer(duration time.Duration, fn func()) *Debouncer {
	return &Debouncer{
		duration: duration,
		fn:       fn,
	}
}

// Trigger records an event. If multiple events arrive in rapid succession,
// the callback fires only once after 'duration' of quiet.
//
// This method is thread-safe and non-blocking.
func (d *Debouncer) Trigger() {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Cancel any pending timer
	if d.timer != nil {
		d.timer.Stop()
	}

	// Schedule callback to fire after the delay
	d.timer = time.AfterFunc(d.duration, d.fn)
}

// Stop cancels any pending callback. This should be called during shutdown
// to prevent callbacks from firing after cleanup.
func (d *Debouncer) Stop() {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
}
