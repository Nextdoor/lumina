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
)

// TestBaseCache_NewBaseCache verifies initialization.
func TestBaseCache_NewBaseCache(t *testing.T) {
	cache := NewBaseCache()

	if !cache.lastUpdate.IsZero() {
		t.Errorf("Expected zero lastUpdate, got %v", cache.lastUpdate)
	}

	if cache.notifiers == nil {
		t.Error("Expected notifiers to be initialized")
	}

	if len(cache.notifiers) != 0 {
		t.Errorf("Expected 0 notifiers, got %d", len(cache.notifiers))
	}
}

// TestBaseCache_LockUnlock verifies basic lock/unlock operations.
func TestBaseCache_LockUnlock(t *testing.T) {
	cache := NewBaseCache()

	// Should not panic - write lock
	cache.Lock()
	cache.lastUpdate = time.Now() // Do something while holding lock
	cache.Unlock()

	// Should not panic - read lock
	cache.RLock()
	_ = cache.lastUpdate // Read something while holding lock
	cache.RUnlock()
}

// TestBaseCache_ConcurrentAccess tests thread-safety with concurrent reads/writes.
func TestBaseCache_ConcurrentAccess(t *testing.T) {
	cache := NewBaseCache()

	const numGoroutines = 100
	const numOperations = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines * 2) // readers + writers

	// Concurrent writers
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				cache.Lock()
				cache.MarkUpdated()
				cache.Unlock()
			}
		}()
	}

	// Concurrent readers
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				cache.RLock()
				_ = cache.lastUpdate
				cache.RUnlock()
			}
		}()
	}

	wg.Wait()
}

// TestBaseCache_MarkUpdated verifies timestamp is set correctly.
func TestBaseCache_MarkUpdated(t *testing.T) {
	cache := NewBaseCache()

	before := time.Now()
	cache.Lock()
	cache.MarkUpdated()
	cache.Unlock()
	after := time.Now()

	lastUpdate := cache.GetLastUpdate()
	if lastUpdate.Before(before) || lastUpdate.After(after) {
		t.Errorf("Expected lastUpdate between %v and %v, got %v", before, after, lastUpdate)
	}
}

// TestBaseCache_GetLastUpdate verifies thread-safe timestamp retrieval.
func TestBaseCache_GetLastUpdate(t *testing.T) {
	cache := NewBaseCache()

	// Initially zero
	if !cache.GetLastUpdate().IsZero() {
		t.Error("Expected zero lastUpdate initially")
	}

	// After update
	cache.Lock()
	cache.MarkUpdated()
	cache.Unlock()

	lastUpdate := cache.GetLastUpdate()
	if lastUpdate.IsZero() {
		t.Error("Expected non-zero lastUpdate after MarkUpdated")
	}
}

// TestBaseCache_IsStale verifies staleness checking.
func TestBaseCache_IsStale(t *testing.T) {
	cache := NewBaseCache()

	// Never updated - should be stale
	if !cache.IsStale(1 * time.Second) {
		t.Error("Expected cache to be stale when never updated")
	}

	// Just updated - should not be stale
	cache.Lock()
	cache.MarkUpdated()
	cache.Unlock()

	if cache.IsStale(1 * time.Hour) {
		t.Error("Expected cache to not be stale immediately after update")
	}

	// Old update - should be stale
	cache.Lock()
	cache.lastUpdate = time.Now().Add(-2 * time.Hour)
	cache.Unlock()

	if !cache.IsStale(1 * time.Hour) {
		t.Error("Expected cache to be stale after 2 hours with 1 hour maxAge")
	}
}

// TestBaseCache_GetAge verifies age calculation.
func TestBaseCache_GetAge(t *testing.T) {
	cache := NewBaseCache()

	// Never updated - age should be 0
	if cache.GetAge() != 0 {
		t.Errorf("Expected age 0 for never updated cache, got %v", cache.GetAge())
	}

	// Just updated - age should be very small
	cache.Lock()
	cache.MarkUpdated()
	cache.Unlock()

	age := cache.GetAge()
	if age > 1*time.Second {
		t.Errorf("Expected age < 1s, got %v", age)
	}

	// Old update - age should be > 1 hour
	cache.Lock()
	cache.lastUpdate = time.Now().Add(-2 * time.Hour)
	cache.Unlock()

	age = cache.GetAge()
	if age < 2*time.Hour {
		t.Errorf("Expected age >= 2h, got %v", age)
	}
}

// TestBaseCache_RegisterUpdateNotifier verifies notifier registration.
func TestBaseCache_RegisterUpdateNotifier(t *testing.T) {
	cache := NewBaseCache()

	var mu sync.Mutex
	called := false
	notifier := func() {
		mu.Lock()
		called = true
		mu.Unlock()
	}

	cache.RegisterUpdateNotifier(notifier)

	// Verify notifier was added
	cache.notifyMu.RLock()
	if len(cache.notifiers) != 1 {
		t.Errorf("Expected 1 notifier, got %d", len(cache.notifiers))
	}
	cache.notifyMu.RUnlock()

	// Trigger notification
	cache.NotifyUpdate()

	// Wait a bit for goroutine to execute
	time.Sleep(10 * time.Millisecond)

	mu.Lock()
	wasCalled := called
	mu.Unlock()

	if !wasCalled {
		t.Error("Expected notifier to be called")
	}
}

// TestBaseCache_RegisterMultipleNotifiers verifies multiple notifiers work.
func TestBaseCache_RegisterMultipleNotifiers(t *testing.T) {
	cache := NewBaseCache()

	var mu sync.Mutex
	callCount := 0

	for i := 0; i < 5; i++ {
		cache.RegisterUpdateNotifier(func() {
			mu.Lock()
			callCount++
			mu.Unlock()
		})
	}

	cache.NotifyUpdate()

	// Wait for all goroutines to complete
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if callCount != 5 {
		t.Errorf("Expected 5 notifier calls, got %d", callCount)
	}
}

// TestBaseCache_NotifyUpdateAsync verifies notifications are async.
func TestBaseCache_NotifyUpdateAsync(t *testing.T) {
	cache := NewBaseCache()

	blockChan := make(chan struct{})
	cache.RegisterUpdateNotifier(func() {
		<-blockChan // Block until we allow it
	})

	// This should not block
	done := make(chan struct{})
	go func() {
		cache.NotifyUpdate()
		close(done)
	}()

	// NotifyUpdate should return quickly
	select {
	case <-done:
		// Good - NotifyUpdate returned
	case <-time.After(100 * time.Millisecond):
		t.Error("NotifyUpdate blocked, expected async execution")
	}

	// Unblock the notifier
	close(blockChan)
}

// TestCacheKeyBuilder_Build verifies key building.
func TestCacheKeyBuilder_Build(t *testing.T) {
	tests := []struct {
		name      string
		separator string
		parts     []string
		expected  string
	}{
		{
			name:      "simple colon-separated",
			separator: ":",
			parts:     []string{"us-west-2", "m5.xlarge", "linux"},
			expected:  "us-west-2:m5.xlarge:linux",
		},
		{
			name:      "comma-separated with ARN",
			separator: ",",
			parts:     []string{"arn:aws:sp::123:sp/abc", "m5.xlarge", "us-west-2"},
			expected:  "arn:aws:sp::123:sp/abc,m5.xlarge,us-west-2",
		},
		{
			name:      "single part",
			separator: ":",
			parts:     []string{"single"},
			expected:  "single",
		},
		{
			name:      "empty parts",
			separator: ":",
			parts:     []string{},
			expected:  "",
		},
		{
			name:      "mixed case - normalized to lowercase",
			separator: ":",
			parts:     []string{"US-WEST-2", "M5.XLARGE", "Linux"},
			expected:  "us-west-2:m5.xlarge:linux",
		},
		{
			name:      "parts with whitespace",
			separator: ":",
			parts:     []string{" us-west-2 ", " m5.xlarge", "linux "},
			expected:  "us-west-2:m5.xlarge:linux",
		},
		{
			name:      "empty strings skipped",
			separator: ":",
			parts:     []string{"us-west-2", "", "linux"},
			expected:  "us-west-2:linux",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewCacheKey(tt.separator)
			result := builder.Add(tt.parts...).Build()

			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

// TestBuildKey verifies convenience function.
func TestBuildKey(t *testing.T) {
	result := BuildKey(":", "us-west-2", "m5.xlarge", "linux")
	expected := "us-west-2:m5.xlarge:linux"

	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}
}

// TestParseKey verifies key parsing.
func TestParseKey(t *testing.T) {
	tests := []struct {
		name      string
		key       string
		separator string
		expected  []string
		expectOK  bool
	}{
		{
			name:      "valid colon-separated",
			key:       "us-west-2:m5.xlarge:linux",
			separator: ":",
			expected:  []string{"us-west-2", "m5.xlarge", "linux"},
			expectOK:  true,
		},
		{
			name:      "valid comma-separated",
			key:       "arn:aws:sp::123:sp/abc,m5.xlarge,us-west-2",
			separator: ",",
			expected:  []string{"arn:aws:sp::123:sp/abc", "m5.xlarge", "us-west-2"},
			expectOK:  true,
		},
		{
			name:      "single part",
			key:       "single",
			separator: ":",
			expected:  []string{"single"},
			expectOK:  true,
		},
		{
			name:      "empty key",
			key:       "",
			separator: ":",
			expected:  nil,
			expectOK:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parts, ok := ParseKey(tt.key, tt.separator)

			if ok != tt.expectOK {
				t.Errorf("Expected ok=%v, got %v", tt.expectOK, ok)
			}

			if !ok {
				return
			}

			if len(parts) != len(tt.expected) {
				t.Errorf("Expected %d parts, got %d", len(tt.expected), len(parts))
				return
			}

			for i, part := range parts {
				if part != tt.expected[i] {
					t.Errorf("Part %d: expected %q, got %q", i, tt.expected[i], part)
				}
			}
		})
	}
}

// TestParseKeyN verifies key parsing with part count validation.
func TestParseKeyN(t *testing.T) {
	tests := []struct {
		name          string
		key           string
		separator     string
		expectedParts int
		expected      []string
		expectOK      bool
	}{
		{
			name:          "correct part count",
			key:           "us-west-2:m5.xlarge:linux",
			separator:     ":",
			expectedParts: 3,
			expected:      []string{"us-west-2", "m5.xlarge", "linux"},
			expectOK:      true,
		},
		{
			name:          "too few parts",
			key:           "us-west-2:m5.xlarge",
			separator:     ":",
			expectedParts: 3,
			expected:      nil,
			expectOK:      false,
		},
		{
			name:          "too many parts",
			key:           "us-west-2:m5.xlarge:linux:extra",
			separator:     ":",
			expectedParts: 3,
			expected:      nil,
			expectOK:      false,
		},
		{
			name:          "empty key",
			key:           "",
			separator:     ":",
			expectedParts: 3,
			expected:      nil,
			expectOK:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parts, ok := ParseKeyN(tt.key, tt.separator, tt.expectedParts)

			if ok != tt.expectOK {
				t.Errorf("Expected ok=%v, got %v", tt.expectOK, ok)
			}

			if !ok {
				return
			}

			if len(parts) != len(tt.expected) {
				t.Errorf("Expected %d parts, got %d", len(tt.expected), len(parts))
				return
			}

			for i, part := range parts {
				if part != tt.expected[i] {
					t.Errorf("Part %d: expected %q, got %q", i, tt.expected[i], part)
				}
			}
		})
	}
}

// TestTimestampedFloat64_IsStale verifies staleness checking.
func TestTimestampedFloat64_IsStale(t *testing.T) {
	// Fresh entry
	fresh := TimestampedFloat64{
		Value:     1.23,
		Timestamp: time.Now(),
	}

	if fresh.IsStale(1 * time.Hour) {
		t.Error("Expected fresh entry to not be stale")
	}

	// Stale entry
	stale := TimestampedFloat64{
		Value:     4.56,
		Timestamp: time.Now().Add(-2 * time.Hour),
	}

	if !stale.IsStale(1 * time.Hour) {
		t.Error("Expected stale entry to be stale")
	}
}

// TestTimestampedFloat64_Age verifies age calculation.
func TestTimestampedFloat64_Age(t *testing.T) {
	entry := TimestampedFloat64{
		Value:     1.23,
		Timestamp: time.Now().Add(-1 * time.Hour),
	}

	age := entry.Age()
	if age < 1*time.Hour || age > 1*time.Hour+10*time.Second {
		t.Errorf("Expected age ~1h, got %v", age)
	}
}

// TestMapCache_NewMapCache verifies initialization.
func TestMapCache_NewMapCache(t *testing.T) {
	cache := NewMapCache[float64]()

	if cache.data == nil {
		t.Error("Expected data to be initialized")
	}

	if cache.Len() != 0 {
		t.Errorf("Expected length 0, got %d", cache.Len())
	}
}

// TestMapCache_GetSet verifies basic get/set operations.
func TestMapCache_GetSet(t *testing.T) {
	cache := NewMapCache[float64]()

	// Get non-existent key
	_, ok := cache.Get("key1")
	if ok {
		t.Error("Expected Get to return false for non-existent key")
	}

	// Set and get
	cache.Set("key1", 1.23)

	value, ok := cache.Get("key1")
	if !ok {
		t.Error("Expected Get to return true for existing key")
	}

	if value != 1.23 {
		t.Errorf("Expected value 1.23, got %v", value)
	}
}

// TestMapCache_SetBatch verifies batch operations.
func TestMapCache_SetBatch(t *testing.T) {
	cache := NewMapCache[float64]()

	updates := map[string]float64{
		"key1": 1.23,
		"key2": 4.56,
		"key3": 7.89,
	}

	cache.SetBatch(updates)

	if cache.Len() != 3 {
		t.Errorf("Expected length 3, got %d", cache.Len())
	}

	for key, expected := range updates {
		value, ok := cache.Get(key)
		if !ok {
			t.Errorf("Expected key %q to exist", key)
		}
		if value != expected {
			t.Errorf("Key %q: expected %v, got %v", key, expected, value)
		}
	}
}

// TestMapCache_Delete verifies deletion.
func TestMapCache_Delete(t *testing.T) {
	cache := NewMapCache[float64]()

	cache.Set("key1", 1.23)
	cache.Set("key2", 4.56)

	cache.Delete("key1")

	if cache.Len() != 1 {
		t.Errorf("Expected length 1, got %d", cache.Len())
	}

	_, ok := cache.Get("key1")
	if ok {
		t.Error("Expected key1 to be deleted")
	}

	_, ok = cache.Get("key2")
	if !ok {
		t.Error("Expected key2 to still exist")
	}
}

// TestMapCache_Clear verifies clearing all entries.
func TestMapCache_Clear(t *testing.T) {
	cache := NewMapCache[float64]()

	cache.Set("key1", 1.23)
	cache.Set("key2", 4.56)

	cache.Clear()

	if cache.Len() != 0 {
		t.Errorf("Expected length 0 after Clear, got %d", cache.Len())
	}
}

// TestMapCache_GetAll verifies getting all entries.
func TestMapCache_GetAll(t *testing.T) {
	cache := NewMapCache[float64]()

	expected := map[string]float64{
		"key1": 1.23,
		"key2": 4.56,
		"key3": 7.89,
	}

	cache.SetBatch(expected)

	all := cache.GetAll()

	if len(all) != len(expected) {
		t.Errorf("Expected %d entries, got %d", len(expected), len(all))
	}

	for key, expectedValue := range expected {
		value, ok := all[key]
		if !ok {
			t.Errorf("Expected key %q to exist", key)
		}
		if value != expectedValue {
			t.Errorf("Key %q: expected %v, got %v", key, expectedValue, value)
		}
	}
}

// TestMapCache_Has verifies key existence checking.
func TestMapCache_Has(t *testing.T) {
	cache := NewMapCache[float64]()

	cache.Set("key1", 1.23)

	if !cache.Has("key1") {
		t.Error("Expected Has to return true for existing key")
	}

	if cache.Has("key2") {
		t.Error("Expected Has to return false for non-existent key")
	}
}

// TestMapCache_ConcurrentAccess tests thread-safety.
func TestMapCache_ConcurrentAccess(t *testing.T) {
	cache := NewMapCache[int]()

	const numGoroutines = 50
	const numOperations = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines * 3) // writers + readers + deleters

	// Concurrent writers
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				key := BuildKey(":", "key", "writer", string(rune(id)), string(rune(j)))
				cache.Set(key, id*100+j)
			}
		}(i)
	}

	// Concurrent readers
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				key := BuildKey(":", "key", "writer", string(rune(id)), string(rune(j)))
				_, _ = cache.Get(key)
			}
		}(i)
	}

	// Concurrent deleters
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				key := BuildKey(":", "key", "writer", string(rune(id)), string(rune(j)))
				cache.Delete(key)
			}
		}(i)
	}

	wg.Wait()
}

// TestMapCache_UpdateNotifications verifies notifications are triggered.
func TestMapCache_UpdateNotifications(t *testing.T) {
	cache := NewMapCache[float64]()

	var mu sync.Mutex
	callCount := 0

	cache.RegisterUpdateNotifier(func() {
		mu.Lock()
		callCount++
		mu.Unlock()
	})

	// Set should trigger notification
	cache.Set("key1", 1.23)
	time.Sleep(10 * time.Millisecond)

	mu.Lock()
	if callCount != 1 {
		t.Errorf("Expected 1 notification after Set, got %d", callCount)
	}
	mu.Unlock()

	// SetBatch should trigger notification
	cache.SetBatch(map[string]float64{"key2": 4.56})
	time.Sleep(10 * time.Millisecond)

	mu.Lock()
	if callCount != 2 {
		t.Errorf("Expected 2 notifications after SetBatch, got %d", callCount)
	}
	mu.Unlock()

	// Delete should trigger notification
	cache.Delete("key1")
	time.Sleep(10 * time.Millisecond)

	mu.Lock()
	if callCount != 3 {
		t.Errorf("Expected 3 notifications after Delete, got %d", callCount)
	}
	mu.Unlock()

	// Clear should trigger notification
	cache.Clear()
	time.Sleep(10 * time.Millisecond)

	mu.Lock()
	if callCount != 4 {
		t.Errorf("Expected 4 notifications after Clear, got %d", callCount)
	}
	mu.Unlock()
}
