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

package aws

import (
	"context"
	"testing"
)

// TestNewRealPricingClient tests that NewRealPricingClient creates a valid client.
func TestNewRealPricingClient(t *testing.T) {
	client := NewRealPricingClient()

	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

// TestRealPricingClientGetOnDemandPrice tests the GetOnDemandPrice method.
func TestRealPricingClientGetOnDemandPrice(t *testing.T) {
	ctx := context.Background()
	client := NewRealPricingClient()

	// Call the stub implementation
	price, err := client.GetOnDemandPrice(ctx, "us-west-2", "t3.micro", "Linux")
	if err != nil {
		t.Errorf("expected no error from stub, got: %v", err)
	}

	// Stub returns nil
	if price != nil {
		t.Errorf("expected nil price from stub, got %v", price)
	}
}

// TestRealPricingClientGetOnDemandPrices tests the GetOnDemandPrices method.
func TestRealPricingClientGetOnDemandPrices(t *testing.T) {
	ctx := context.Background()
	client := NewRealPricingClient()

	// Call the stub implementation
	prices, err := client.GetOnDemandPrices(ctx, "us-west-2", []string{"t3.micro", "t3.small"}, "Linux")
	if err != nil {
		t.Errorf("expected no error from stub, got: %v", err)
	}

	// Stub returns empty slice
	if len(prices) != 0 {
		t.Errorf("expected empty prices from stub, got %d", len(prices))
	}
}
