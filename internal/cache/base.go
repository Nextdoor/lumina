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

// Package cache provides common infrastructure for thread-safe in-memory caches.
//
// This file implements BaseCache, which provides:
// - Thread-safety with RWMutex
// - Update notification system
// - Timestamp tracking and staleness checks
// - Common key building and parsing utilities
// - Generic MapCache for simple key-value storage
//
// Caches can embed BaseCache to eliminate boilerplate code for infrastructure
// while maintaining domain-specific storage and query patterns.
package cache

import (
	"strings"
	"sync"
	"time"
)

// BaseCache provides common cache infrastructure: thread-safety, notifications, and metadata.
// It does NOT store the actual data - that's handled by the embedding struct.
//
// This eliminates code duplication for:
// - RWMutex management
// - Update notification system
// - Metadata tracking (timestamps, staleness checks)
//
// Usage: Embed in your cache struct and use the provided methods.
//
// Example:
//
//	type MyCache struct {
//	    BaseCache
//	    data map[string]MyData
//	}
//
//	func (c *MyCache) Update(data map[string]MyData) {
//	    c.Lock()
//	    defer c.Unlock()
//	    c.data = data
//	    c.MarkUpdated()
//	    c.NotifyUpdate()
//	}
type BaseCache struct {
	// Separate mutexes to prevent deadlock during notification
	// mu protects the embedding struct's data fields
	mu sync.RWMutex

	// notifyMu protects the notifiers slice
	// Separate to prevent deadlock when notifications trigger cache reads
	notifyMu sync.RWMutex

	notifiers  []UpdateNotifier
	lastUpdate time.Time
}

// NewBaseCache creates a new base cache with infrastructure initialized.
func NewBaseCache() BaseCache {
	return BaseCache{
		lastUpdate: time.Time{}, // Zero time indicates never updated
		notifiers:  make([]UpdateNotifier, 0),
	}
}

// Lock acquires the write lock. Use when modifying cache data.
// Must be paired with Unlock().
func (b *BaseCache) Lock() {
	b.mu.Lock()
}

// Unlock releases the write lock.
func (b *BaseCache) Unlock() {
	b.mu.Unlock()
}

// RLock acquires the read lock. Use when reading cache data.
// Multiple readers can hold the lock simultaneously.
// Must be paired with RUnlock().
func (b *BaseCache) RLock() {
	b.mu.RLock()
}

// RUnlock releases the read lock.
func (b *BaseCache) RUnlock() {
	b.mu.RUnlock()
}

// RegisterUpdateNotifier adds a callback invoked when cache data changes.
// Multiple notifiers can be registered. Callbacks are invoked in separate goroutines
// to prevent blocking cache operations.
//
// This is typically used to trigger cost recalculation when cache data changes.
// Thread-safe.
func (b *BaseCache) RegisterUpdateNotifier(fn UpdateNotifier) {
	b.notifyMu.Lock()
	defer b.notifyMu.Unlock()
	b.notifiers = append(b.notifiers, fn)
}

// NotifyUpdate invokes all registered notifiers in separate goroutines.
// This method should be called after cache modifications.
//
// IMPORTANT: Call this AFTER releasing the main data lock to prevent deadlock.
// Notifiers may read from caches, so they need to acquire read locks.
//
// Example:
//
//	func (c *MyCache) Update(data map[string]MyData) {
//	    c.Lock()
//	    c.data = data
//	    c.MarkUpdated()
//	    c.Unlock()           // Release lock BEFORE notifying
//	    c.NotifyUpdate()     // Safe to call now
//	}
func (b *BaseCache) NotifyUpdate() {
	b.notifyMu.RLock()
	defer b.notifyMu.RUnlock()

	for _, fn := range b.notifiers {
		// Run in goroutine to prevent blocking cache operations
		// This means notifiers must be thread-safe
		go fn()
	}
}

// MarkUpdated sets the last update timestamp to now.
// Call this after successful data modifications.
//
// IMPORTANT: Caller must hold the write lock when calling this method.
func (b *BaseCache) MarkUpdated() {
	b.lastUpdate = time.Now()
}

// GetLastUpdate returns when the cache was last modified.
// Returns zero time if never updated.
// Thread-safe.
func (b *BaseCache) GetLastUpdate() time.Time {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.lastUpdate
}

// IsStale returns true if the cache hasn't been updated within maxAge.
// Returns true if the cache has never been updated (zero time).
// Thread-safe.
func (b *BaseCache) IsStale(maxAge time.Duration) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.lastUpdate.IsZero() {
		return true // Never updated
	}
	return time.Since(b.lastUpdate) > maxAge
}

// GetAge returns the duration since the last update.
// Returns 0 if never updated.
// Thread-safe.
func (b *BaseCache) GetAge() time.Duration {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.lastUpdate.IsZero() {
		return 0
	}
	return time.Since(b.lastUpdate)
}

// CacheKeyBuilder provides fluent API for building normalized cache keys.
// All keys are automatically lowercased for case-insensitive lookups.
//
// Example:
//
//	key := NewCacheKey(":").Add("us-west-2", "m5.xlarge", "linux").Build()
//	// Returns: "us-west-2:m5.xlarge:linux"
type CacheKeyBuilder struct {
	separator string
	parts     []string
}

// NewCacheKey creates a new key builder with the specified separator.
// Common separators: ":" for simple keys, "," for keys containing ARNs.
func NewCacheKey(separator string) *CacheKeyBuilder {
	return &CacheKeyBuilder{
		separator: separator,
		parts:     make([]string, 0, 4), // Pre-allocate for common case
	}
}

// Add appends one or more parts to the key.
// Each part is trimmed and normalized to lowercase.
// Empty parts are skipped.
func (b *CacheKeyBuilder) Add(parts ...string) *CacheKeyBuilder {
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			b.parts = append(b.parts, strings.ToLower(trimmed))
		}
	}
	return b
}

// Build constructs the final key string.
// Returns a lowercase, separator-joined key.
func (b *CacheKeyBuilder) Build() string {
	return strings.Join(b.parts, b.separator)
}

// BuildKey is a convenience function for simple key construction.
// Equivalent to NewCacheKey(sep).Add(parts...).Build().
//
// Example:
//
//	key := BuildKey(":", "us-west-2", "m5.xlarge", "linux")
//	// Returns: "us-west-2:m5.xlarge:linux"
func BuildKey(separator string, parts ...string) string {
	return NewCacheKey(separator).Add(parts...).Build()
}

// ParseKey splits a cache key into its components.
// Returns the parts and true if successful, or empty slice and false if key is invalid.
//
// Example:
//
//	parts, ok := ParseKey("us-west-2:m5.xlarge:linux", ":")
//	// parts = ["us-west-2", "m5.xlarge", "linux"], ok = true
func ParseKey(key, separator string) ([]string, bool) {
	if key == "" {
		return nil, false
	}
	parts := strings.Split(key, separator)
	if len(parts) == 0 {
		return nil, false
	}
	return parts, true
}

// ParseKeyN splits a cache key and validates it has exactly N parts.
// Returns the parts and true if successful, or empty slice and false if invalid.
//
// Example:
//
//	parts, ok := ParseKeyN("us-west-2:m5.xlarge:linux", ":", 3)
//	// parts = ["us-west-2", "m5.xlarge", "linux"], ok = true
//
//	parts, ok := ParseKeyN("us-west-2:m5.xlarge", ":", 3)
//	// parts = nil, ok = false (expected 3 parts, got 2)
func ParseKeyN(key, separator string, expectedParts int) ([]string, bool) {
	parts, ok := ParseKey(key, separator)
	if !ok || len(parts) != expectedParts {
		return nil, false
	}
	return parts, true
}

// TimestampedFloat64 wraps a float64 value with its fetch timestamp.
// Used for cache entries that need individual freshness tracking (e.g., spot prices).
//
// The cache-level lastUpdate tracks bulk operations, while TimestampedFloat64
// tracks individual entry freshness for incremental refresh strategies.
type TimestampedFloat64 struct {
	Value     float64
	Timestamp time.Time
}

// IsStale returns true if this entry is older than maxAge.
func (t TimestampedFloat64) IsStale(maxAge time.Duration) bool {
	return time.Since(t.Timestamp) > maxAge
}

// Age returns how old this entry is.
func (t TimestampedFloat64) Age() time.Duration {
	return time.Since(t.Timestamp)
}

// MapCache provides common operations for map[string]T caches.
// Use this for simple key-value caches to reduce boilerplate.
//
// This is optional - caches with complex storage patterns (nested maps,
// specialized queries) can skip this and use BaseCache directly.
//
// Example:
//
//	cache := NewMapCache[float64]()
//	cache.Set("price:m5.large", 0.096)
//	price, ok := cache.Get("price:m5.large")
type MapCache[T any] struct {
	BaseCache
	data map[string]T
}

// NewMapCache creates a new map-based cache.
func NewMapCache[T any]() *MapCache[T] {
	return &MapCache[T]{
		BaseCache: NewBaseCache(),
		data:      make(map[string]T),
	}
}

// Get retrieves a value by key.
// Returns the value and true if found, or zero value and false if not found.
// Thread-safe.
func (m *MapCache[T]) Get(key string) (T, bool) {
	m.RLock()
	defer m.RUnlock()
	val, exists := m.data[key]
	return val, exists
}

// Set stores a value for a key.
// Thread-safe. Notifies update listeners.
func (m *MapCache[T]) Set(key string, value T) {
	m.Lock()
	m.data[key] = value
	m.MarkUpdated()
	m.Unlock()

	m.NotifyUpdate()
}

// SetBatch stores multiple key-value pairs atomically.
// Thread-safe. Notifies update listeners once after all updates.
func (m *MapCache[T]) SetBatch(updates map[string]T) {
	m.Lock()
	for key, value := range updates {
		m.data[key] = value
	}
	m.MarkUpdated()
	m.Unlock()

	m.NotifyUpdate()
}

// Delete removes a value by key.
// Thread-safe. Notifies update listeners.
func (m *MapCache[T]) Delete(key string) {
	m.Lock()
	delete(m.data, key)
	m.MarkUpdated()
	m.Unlock()

	m.NotifyUpdate()
}

// Clear removes all entries.
// Thread-safe. Notifies update listeners.
func (m *MapCache[T]) Clear() {
	m.Lock()
	m.data = make(map[string]T)
	m.MarkUpdated()
	m.Unlock()

	m.NotifyUpdate()
}

// GetAll returns a copy of all entries.
// Thread-safe.
func (m *MapCache[T]) GetAll() map[string]T {
	m.RLock()
	defer m.RUnlock()

	copy := make(map[string]T, len(m.data))
	for k, v := range m.data {
		copy[k] = v
	}
	return copy
}

// Len returns the number of entries.
// Thread-safe.
func (m *MapCache[T]) Len() int {
	m.RLock()
	defer m.RUnlock()
	return len(m.data)
}

// Has checks if a key exists.
// Thread-safe.
func (m *MapCache[T]) Has(key string) bool {
	m.RLock()
	defer m.RUnlock()
	_, exists := m.data[key]
	return exists
}
