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
)

// RealPricingClient is a production implementation of PricingClient that makes
// real API calls to AWS Pricing using the AWS SDK v2.
//
// Note: Pricing API does not require account-specific credentials as pricing
// information is publicly available.
type RealPricingClient struct {
	// TODO: Add AWS Pricing SDK client when implementing pricing logic
}

// NewRealPricingClient creates a new Pricing client.
func NewRealPricingClient() *RealPricingClient {
	return &RealPricingClient{}
}

// GetOnDemandPrice returns the on-demand price for an instance type in a region.
func (c *RealPricingClient) GetOnDemandPrice(
	_ context.Context,
	_ string,
	_ string,
	_ string,
) (*OnDemandPrice, error) {
	// TODO: Implement real Pricing API call
	return nil, nil
}

// GetOnDemandPrices returns on-demand prices for multiple instance types.
func (c *RealPricingClient) GetOnDemandPrices(
	_ context.Context,
	_ string,
	_ []string,
	_ string,
) ([]OnDemandPrice, error) {
	// TODO: Implement real Pricing API bulk call
	return []OnDemandPrice{}, nil
}
