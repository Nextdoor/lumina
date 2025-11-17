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
	"time"
)

// Client is the main interface for interacting with AWS services.
// It provides access to EC2, Savings Plans, and pricing APIs with
// built-in support for cross-account AssumeRole operations.
type Client interface {
	// EC2 returns an EC2Client for the specified account configuration.
	// If accountConfig.AssumeRoleARN is set, it will assume that role.
	// Otherwise, it uses the default credential chain.
	EC2(ctx context.Context, accountConfig AccountConfig) (EC2Client, error)

	// SavingsPlans returns a SavingsPlansClient for the specified account.
	SavingsPlans(ctx context.Context, accountConfig AccountConfig) (SavingsPlansClient, error)

	// Pricing returns a PricingClient (does not require account-specific credentials)
	Pricing(ctx context.Context) PricingClient
}

// EC2Client provides access to EC2 API operations needed for cost calculation.
type EC2Client interface {
	// DescribeInstances returns all running EC2 instances in the specified regions.
	// If regions is empty, queries all regions.
	DescribeInstances(ctx context.Context, regions []string) ([]Instance, error)

	// DescribeReservedInstances returns all active Reserved Instances in the specified regions.
	// If regions is empty, queries all regions.
	DescribeReservedInstances(ctx context.Context, regions []string) ([]ReservedInstance, error)

	// DescribeSpotPriceHistory returns current spot prices for the specified instance types.
	// If instanceTypes is empty, returns prices for all instance types.
	// If regions is empty, queries all regions.
	DescribeSpotPriceHistory(ctx context.Context, regions []string, instanceTypes []string) ([]SpotPrice, error)

	// GetInstanceByID returns a specific instance by ID.
	// Returns nil if the instance is not found.
	GetInstanceByID(ctx context.Context, region string, instanceID string) (*Instance, error)
}

// SavingsPlansClient provides access to AWS Savings Plans API operations.
type SavingsPlansClient interface {
	// DescribeSavingsPlans returns all active Savings Plans for the account.
	// This API is not region-specific (operates on the global Savings Plans data).
	DescribeSavingsPlans(ctx context.Context) ([]SavingsPlan, error)

	// GetSavingsPlanByARN returns a specific Savings Plan by ARN.
	// Returns nil if the Savings Plan is not found.
	GetSavingsPlanByARN(ctx context.Context, arn string) (*SavingsPlan, error)
}

// PricingClient provides access to AWS Pricing API operations.
// This client does not require account-specific credentials as pricing
// information is publicly available.
type PricingClient interface {
	// GetOnDemandPrice returns the on-demand price for an instance type in a region.
	GetOnDemandPrice(
		ctx context.Context,
		region string,
		instanceType string,
		operatingSystem string,
	) (*OnDemandPrice, error)

	// GetOnDemandPrices returns on-demand prices for multiple instance types.
	// This is more efficient than calling GetOnDemandPrice multiple times.
	GetOnDemandPrices(
		ctx context.Context,
		region string,
		instanceTypes []string,
		operatingSystem string,
	) ([]OnDemandPrice, error)

	// LoadAllPricing bulk-loads pricing data for the specified regions and operating systems.
	// This is the most efficient way to preload pricing data at startup.
	// Results are cached internally and used by GetOnDemandPrice() calls.
	//
	// Parameters:
	//   - regions: List of AWS regions to load pricing for (e.g., ["us-west-2", "us-east-1"])
	//   - operatingSystems: List of OS types to load (e.g., ["Linux", "Windows"])
	//
	// Returns a map of "region:instanceType:os" -> price ($/hour) for all loaded prices.
	// This allows callers to populate their own caches if needed.
	LoadAllPricing(
		ctx context.Context,
		regions []string,
		operatingSystems []string,
	) (map[string]float64, error)
}

// ClientConfig configures the AWS client creation.
type ClientConfig struct {
	// DefaultRegion is the default AWS region for API calls
	DefaultRegion string

	// DefaultAccount is the account configuration to use for non-account-specific
	// API calls (e.g., pricing data). This ensures all AWS calls use assumed role
	// credentials rather than the pod's service account credentials.
	DefaultAccount AccountConfig

	// MaxRetries is the maximum number of retries for AWS API calls
	// Default: 3
	MaxRetries int

	// RetryDelay is the initial delay between retries (exponential backoff)
	// Default: 1 second
	RetryDelay time.Duration

	// HTTPTimeout is the timeout for HTTP requests to AWS APIs
	// Default: 30 seconds
	HTTPTimeout time.Duration

	// EnableMetrics enables AWS SDK metrics collection
	// Default: false
	EnableMetrics bool
}

// NewClient creates a new AWS client with the specified configuration.
// The client handles credential management, AssumeRole operations,
// retries, and rate limiting automatically.
//
// For production use, this creates a RealClient that connects to actual AWS APIs.
// For testing with LocalStack, use NewClientWithEndpoint instead.
func NewClient(config ClientConfig) (Client, error) {
	// Create a real AWS client with no custom endpoint (production use)
	return NewClientWithEndpoint(config, "")
}

// NewClientWithEndpoint creates a new AWS client with a custom endpoint URL.
// This is primarily used for testing with LocalStack.
//
// For production use, pass an empty endpointURL or use NewClient instead.
// For LocalStack testing, pass "http://localhost:4566" as endpointURL.
func NewClientWithEndpoint(config ClientConfig, endpointURL string) (Client, error) {
	// Create a real AWS client with the specified endpoint
	return NewRealClient(context.Background(), config, config.DefaultAccount, endpointURL)
}
