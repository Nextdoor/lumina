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
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRetryWithBackoff_Success tests successful operation on first attempt.
func TestRetryWithBackoff_Success(t *testing.T) {
	ctx := context.Background()
	config := DefaultRetryConfig()
	log := logr.Discard()

	callCount := 0
	operation := func() error {
		callCount++
		return nil
	}

	err := RetryWithBackoff(ctx, config, log, "test-operation", operation)

	require.NoError(t, err)
	assert.Equal(t, 1, callCount, "operation should be called once")
}

// TestRetryWithBackoff_SuccessAfterRetries tests successful operation after several failures.
func TestRetryWithBackoff_SuccessAfterRetries(t *testing.T) {
	ctx := context.Background()
	config := DefaultRetryConfig()
	config.InitialDelay = 10 * time.Millisecond // Speed up test
	config.MaxDelay = 20 * time.Millisecond
	log := logr.Discard()

	callCount := 0
	operation := func() error {
		callCount++
		if callCount < 3 {
			return errors.New("temporary failure")
		}
		return nil
	}

	startTime := time.Now()
	err := RetryWithBackoff(ctx, config, log, "test-operation", operation)
	duration := time.Since(startTime)

	require.NoError(t, err)
	assert.Equal(t, 3, callCount, "operation should be called 3 times")
	// Should have delayed: 10ms + 20ms = 30ms (first delay, second delay capped at max)
	assert.GreaterOrEqual(t, duration, 30*time.Millisecond, "should have delayed at least 30ms")
}

// TestRetryWithBackoff_ExhaustedRetries tests permanent failure after exhausting retries.
func TestRetryWithBackoff_ExhaustedRetries(t *testing.T) {
	ctx := context.Background()
	config := DefaultRetryConfig()
	config.MaxRetries = 3
	config.InitialDelay = 10 * time.Millisecond // Speed up test
	config.MaxDelay = 20 * time.Millisecond
	log := logr.Discard()

	callCount := 0
	expectedErr := errors.New("persistent failure")
	operation := func() error {
		callCount++
		return expectedErr
	}

	err := RetryWithBackoff(ctx, config, log, "test-operation", operation)

	require.Error(t, err)
	assert.Equal(t, 3, callCount, "operation should be called MaxRetries times")
	assert.Contains(t, err.Error(), "test-operation failed after 3 attempts")
	assert.ErrorIs(t, err, expectedErr, "should wrap original error")
}

// TestRetryWithBackoff_ContextCanceled tests that context cancellation stops retries.
func TestRetryWithBackoff_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	config := DefaultRetryConfig()
	config.InitialDelay = 100 * time.Millisecond // Long enough for us to cancel
	log := logr.Discard()

	callCount := 0
	operation := func() error {
		callCount++
		return errors.New("will fail")
	}

	// Cancel context after first failure
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := RetryWithBackoff(ctx, config, log, "test-operation", operation)

	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 1, callCount, "operation should be called once before cancellation")
}

// TestRetryWithBackoff_ContextTimeout tests that context timeout stops retries.
func TestRetryWithBackoff_ContextTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	config := DefaultRetryConfig()
	config.InitialDelay = 100 * time.Millisecond // Longer than context timeout
	log := logr.Discard()

	callCount := 0
	operation := func() error {
		callCount++
		return errors.New("will fail")
	}

	err := RetryWithBackoff(ctx, config, log, "test-operation", operation)

	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Equal(t, 1, callCount, "operation should be called once before timeout")
}

// TestRetryWithBackoff_ExponentialBackoff tests that delays increase exponentially.
func TestRetryWithBackoff_ExponentialBackoff(t *testing.T) {
	ctx := context.Background()
	config := RetryConfig{
		MaxRetries:   4,
		InitialDelay: 10 * time.Millisecond,
		MaxDelay:     100 * time.Millisecond,
		Multiplier:   2.0,
	}
	log := logr.Discard()

	var delays []time.Duration
	lastCallTime := time.Now()

	callCount := 0
	operation := func() error {
		callCount++
		if callCount > 1 {
			delay := time.Since(lastCallTime)
			delays = append(delays, delay)
		}
		lastCallTime = time.Now()
		return errors.New("will fail")
	}

	_ = RetryWithBackoff(ctx, config, log, "test-operation", operation)

	require.Equal(t, 4, callCount, "should retry 4 times")
	require.Len(t, delays, 3, "should have 3 delays")

	// Verify exponential backoff: 10ms, 20ms, 40ms
	// Allow some tolerance due to test timing
	assert.InDelta(t, 10*time.Millisecond, delays[0], float64(5*time.Millisecond), "first delay should be ~10ms")
	assert.InDelta(t, 20*time.Millisecond, delays[1], float64(5*time.Millisecond), "second delay should be ~20ms")
	assert.InDelta(t, 40*time.Millisecond, delays[2], float64(10*time.Millisecond), "third delay should be ~40ms")
}

// TestRetryWithBackoff_MaxDelayCap tests that delays are capped at MaxDelay.
func TestRetryWithBackoff_MaxDelayCap(t *testing.T) {
	ctx := context.Background()
	config := RetryConfig{
		MaxRetries:   5,
		InitialDelay: 10 * time.Millisecond,
		MaxDelay:     25 * time.Millisecond, // Cap at 25ms
		Multiplier:   2.0,
	}
	log := logr.Discard()

	var delays []time.Duration
	lastCallTime := time.Now()

	callCount := 0
	operation := func() error {
		callCount++
		if callCount > 1 {
			delay := time.Since(lastCallTime)
			delays = append(delays, delay)
		}
		lastCallTime = time.Now()
		return errors.New("will fail")
	}

	_ = RetryWithBackoff(ctx, config, log, "test-operation", operation)

	require.Equal(t, 5, callCount, "should retry 5 times")
	require.Len(t, delays, 4, "should have 4 delays")

	// Verify: 10ms, 20ms, 25ms (capped), 25ms (capped)
	assert.InDelta(t, 10*time.Millisecond, delays[0], float64(5*time.Millisecond), "first delay should be ~10ms")
	assert.InDelta(t, 20*time.Millisecond, delays[1], float64(5*time.Millisecond), "second delay should be ~20ms")
	assert.InDelta(t, 25*time.Millisecond, delays[2], float64(5*time.Millisecond), "third delay should be capped at ~25ms")
	assert.InDelta(t, 25*time.Millisecond, delays[3], float64(5*time.Millisecond), "fourth delay should be capped at ~25ms")
}

// TestDefaultRetryConfig tests that default config has sensible values.
func TestDefaultRetryConfig(t *testing.T) {
	config := DefaultRetryConfig()

	assert.Equal(t, 10, config.MaxRetries, "default MaxRetries should be 10")
	assert.Equal(t, 5*time.Second, config.InitialDelay, "default InitialDelay should be 5s")
	assert.Equal(t, 60*time.Second, config.MaxDelay, "default MaxDelay should be 60s")
	assert.Equal(t, 2.0, config.Multiplier, "default Multiplier should be 2.0")
}

// TestRetryWithBackoff_CustomMultiplier tests custom backoff multiplier.
func TestRetryWithBackoff_CustomMultiplier(t *testing.T) {
	ctx := context.Background()
	config := RetryConfig{
		MaxRetries:   3,
		InitialDelay: 10 * time.Millisecond,
		MaxDelay:     100 * time.Millisecond,
		Multiplier:   3.0, // Triple each time instead of double
	}
	log := logr.Discard()

	var delays []time.Duration
	lastCallTime := time.Now()

	callCount := 0
	operation := func() error {
		callCount++
		if callCount > 1 {
			delay := time.Since(lastCallTime)
			delays = append(delays, delay)
		}
		lastCallTime = time.Now()
		return errors.New("will fail")
	}

	_ = RetryWithBackoff(ctx, config, log, "test-operation", operation)

	require.Equal(t, 3, callCount, "should retry 3 times")
	require.Len(t, delays, 2, "should have 2 delays")

	// Verify: 10ms, 30ms (3.0 multiplier)
	assert.InDelta(t, 10*time.Millisecond, delays[0], float64(5*time.Millisecond), "first delay should be ~10ms")
	assert.InDelta(t, 30*time.Millisecond, delays[1], float64(10*time.Millisecond), "second delay should be ~30ms (tripled)")
}
