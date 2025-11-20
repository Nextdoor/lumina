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
	"fmt"
	"time"

	"github.com/go-logr/logr"
)

// RetryConfig configures retry behavior for reconciler operations.
type RetryConfig struct {
	// MaxRetries is the maximum number of attempts (default: 10)
	MaxRetries int

	// InitialDelay is the initial delay between retries (default: 5s)
	InitialDelay time.Duration

	// MaxDelay is the maximum delay between retries (default: 60s)
	// Delays are capped at this value even with exponential backoff
	MaxDelay time.Duration

	// Multiplier is the backoff multiplier (default: 2.0 for exponential backoff)
	Multiplier float64
}

// DefaultRetryConfig returns sensible defaults for retry behavior.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:   10,
		InitialDelay: 5 * time.Second,
		MaxDelay:     60 * time.Second,
		Multiplier:   2.0,
	}
}

// RetryWithBackoff executes an operation with exponential backoff retry logic.
// This provides self-healing behavior for transient errors (network issues, API rate limits, etc.).
//
// The operation is retried up to config.MaxRetries times with exponential backoff.
// If all retries are exhausted, returns an error.
//
// Parameters:
//   - ctx: Context for cancellation support
//   - config: Retry configuration (use DefaultRetryConfig() for sensible defaults)
//   - log: Logger for structured logging of retry attempts
//   - operationName: Human-readable name for the operation (used in logs and errors)
//   - operation: Function to execute (should return error on failure, nil on success)
//
// Example usage:
//
//	err := RetryWithBackoff(ctx, DefaultRetryConfig(), log, "initial data load", func() error {
//	    return reconciler.loadData()
//	})
func RetryWithBackoff(
	ctx context.Context,
	config RetryConfig,
	log logr.Logger,
	operationName string,
	operation func() error,
) error {
	retryDelay := config.InitialDelay

	for attempt := 1; attempt <= config.MaxRetries; attempt++ {
		err := operation()
		if err == nil {
			// Success!
			if attempt > 1 {
				log.Info("operation succeeded after retries",
					"operation", operationName,
					"attempts", attempt)
			}
			return nil
		}

		// Operation failed
		log.Error(err, "operation failed",
			"operation", operationName,
			"attempt", attempt,
			"max_retries", config.MaxRetries,
			"next_retry_delay", retryDelay)

		// If this was the last attempt, return error
		if attempt == config.MaxRetries {
			return fmt.Errorf("%s failed after %d attempts: %w", operationName, config.MaxRetries, err)
		}

		// Wait before retrying (with context cancellation support)
		select {
		case <-time.After(retryDelay):
			// Exponential backoff with cap
			retryDelay = time.Duration(float64(retryDelay) * config.Multiplier)
			if retryDelay > config.MaxDelay {
				retryDelay = config.MaxDelay
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	// Should never reach here, but return error anyway
	return fmt.Errorf("%s failed after %d attempts", operationName, config.MaxRetries)
}
