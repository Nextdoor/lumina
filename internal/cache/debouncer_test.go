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

	"github.com/stretchr/testify/assert"
)

func TestDebouncer_SingleTrigger(t *testing.T) {
	callCount := 0
	var mu sync.Mutex

	debouncer := NewDebouncer(50*time.Millisecond, func() {
		mu.Lock()
		callCount++
		mu.Unlock()
	})
	defer debouncer.Stop()

	// Single trigger
	debouncer.Trigger()

	// Wait for callback
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	assert.Equal(t, 1, callCount, "callback should fire once")
	mu.Unlock()
}

func TestDebouncer_MultipleTriggers(t *testing.T) {
	callCount := 0
	var mu sync.Mutex

	debouncer := NewDebouncer(50*time.Millisecond, func() {
		mu.Lock()
		callCount++
		mu.Unlock()
	})
	defer debouncer.Stop()

	// Trigger multiple times in rapid succession
	debouncer.Trigger()
	time.Sleep(10 * time.Millisecond)
	debouncer.Trigger()
	time.Sleep(10 * time.Millisecond)
	debouncer.Trigger()

	// Wait for callback (should only fire once)
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	assert.Equal(t, 1, callCount, "callback should fire once after multiple triggers")
	mu.Unlock()
}

func TestDebouncer_ConcurrentTriggers(t *testing.T) {
	callCount := 0
	var mu sync.Mutex

	debouncer := NewDebouncer(50*time.Millisecond, func() {
		mu.Lock()
		callCount++
		mu.Unlock()
	})
	defer debouncer.Stop()

	// Trigger from multiple goroutines simultaneously
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			debouncer.Trigger()
		}()
	}
	wg.Wait()

	// Wait for callback
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	assert.Equal(t, 1, callCount, "callback should fire once despite concurrent triggers")
	mu.Unlock()
}

func TestDebouncer_Stop(t *testing.T) {
	callCount := 0
	var mu sync.Mutex

	debouncer := NewDebouncer(50*time.Millisecond, func() {
		mu.Lock()
		callCount++
		mu.Unlock()
	})

	// Trigger and immediately stop
	debouncer.Trigger()
	debouncer.Stop()

	// Wait to ensure callback doesn't fire
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	assert.Equal(t, 0, callCount, "callback should not fire after Stop()")
	mu.Unlock()
}

func TestDebouncer_RepeatedTriggersWithGaps(t *testing.T) {
	callCount := 0
	var mu sync.Mutex

	debouncer := NewDebouncer(50*time.Millisecond, func() {
		mu.Lock()
		callCount++
		mu.Unlock()
	})
	defer debouncer.Stop()

	// First trigger
	debouncer.Trigger()
	time.Sleep(100 * time.Millisecond) // Wait for callback to fire

	// Second trigger after callback fired
	debouncer.Trigger()
	time.Sleep(100 * time.Millisecond) // Wait for callback to fire again

	mu.Lock()
	assert.Equal(t, 2, callCount, "callback should fire twice for triggers with gaps")
	mu.Unlock()
}
