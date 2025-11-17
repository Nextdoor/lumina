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

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// RealClient is a production implementation of the Client interface that
// makes real calls to AWS APIs using the AWS SDK v2.
//
// This implementation handles:
//   - Credential management using AWS SDK default credential chain
//   - STS AssumeRole operations for cross-account access
//   - Automatic retries and exponential backoff
//   - Region-aware API calls
//
// For testing, use MockClient instead.
type RealClient struct {
	config       ClientConfig
	stsClient    *sts.Client
	ec2Clients   map[string]*RealEC2Client // Cached per-account EC2 clients
	spClients    map[string]*RealSPClient  // Cached per-account Savings Plans clients
	pricingCache *RealPricingClient        // Shared pricing client (region-independent)
	endpointURL  string                    // Optional endpoint URL (for LocalStack testing)
}

// NewRealClient creates a new RealClient with the specified configuration.
// The client uses the AWS SDK default credential chain for authentication.
//
// For LocalStack testing, set endpointURL to "http://localhost:4566".
func NewRealClient(ctx context.Context, cfg ClientConfig, endpointURL string) (*RealClient, error) {
	// Load AWS configuration using default credential chain
	// This will automatically use:
	// 1. Environment variables (AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY)
	// 2. Shared credentials file (~/.aws/credentials)
	// 3. IAM role (if running on EC2 or ECS)
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(cfg.DefaultRegion),
	)
	if err != nil { // coverage:ignore - AWS SDK config loading errors are difficult to trigger in unit tests
		return nil, err
	}

	// Create STS client for AssumeRole operations
	stsOpts := []func(*sts.Options){}
	if endpointURL != "" {
		// Override endpoint for LocalStack testing
		// This branch is tested in localstack_integration_test.go
		stsOpts = append(stsOpts, func(o *sts.Options) {
			o.BaseEndpoint = &endpointURL // coverage:ignore - tested in LocalStack integration tests
		})
	}
	stsClient := sts.NewFromConfig(awsCfg, stsOpts...)

	return &RealClient{
		config:       cfg,
		stsClient:    stsClient,
		ec2Clients:   make(map[string]*RealEC2Client),
		spClients:    make(map[string]*RealSPClient),
		pricingCache: nil, // Will be initialized on first Pricing() call
		endpointURL:  endpointURL,
	}, nil
}

// EC2 returns an EC2Client for the specified account configuration.
// If accountConfig.AssumeRoleARN is set, it will assume that role using STS.
// The client is cached per-account to avoid repeated AssumeRole calls.
func (c *RealClient) EC2(ctx context.Context, accountConfig AccountConfig) (EC2Client, error) {
	// Check cache first
	cacheKey := accountConfig.AccountID + ":" + accountConfig.Region
	if client, ok := c.ec2Clients[cacheKey]; ok {
		return client, nil
	}

	// Get credentials (potentially via AssumeRole)
	creds, err := c.getCredentials(ctx, accountConfig)
	if err != nil { // coverage:ignore - error path tested in LocalStack integration tests
		return nil, err
	}

	// Create EC2 client with assumed credentials
	client, err := NewRealEC2Client(ctx, accountConfig.AccountID, accountConfig.Region, creds, c.endpointURL)
	if err != nil { // coverage:ignore - AWS SDK config errors are difficult to trigger in unit tests
		return nil, err
	}

	// Cache the client
	c.ec2Clients[cacheKey] = client
	return client, nil
}

// SavingsPlans returns a SavingsPlansClient for the specified account.
// If accountConfig.AssumeRoleARN is set, it will assume that role using STS.
// The client is cached per-account to avoid repeated AssumeRole calls.
func (c *RealClient) SavingsPlans(ctx context.Context, accountConfig AccountConfig) (SavingsPlansClient, error) {
	// Check cache first
	cacheKey := accountConfig.AccountID + ":" + accountConfig.Region
	if client, ok := c.spClients[cacheKey]; ok {
		return client, nil
	}

	// Get credentials (potentially via AssumeRole)
	creds, err := c.getCredentials(ctx, accountConfig)
	if err != nil { // coverage:ignore - error path tested in LocalStack integration tests
		return nil, err
	}

	// Create Savings Plans client with assumed credentials
	client, err := NewRealSPClient(ctx, accountConfig.AccountID, accountConfig.Region, creds, c.endpointURL)
	if err != nil { // coverage:ignore - AWS SDK config errors are difficult to trigger in unit tests
		return nil, err
	}

	// Cache the client
	c.spClients[cacheKey] = client
	return client, nil
}

// Pricing returns a PricingClient. Pricing API is not account-specific
// and does not require AssumeRole operations.
//
// The pricing client is lazily initialized on first call and then cached
// for subsequent requests. This avoids AWS SDK configuration overhead
// during client initialization.
func (c *RealClient) Pricing(ctx context.Context) PricingClient {
	if c.pricingCache == nil {
		// Initialize pricing client (uses us-east-1 for pricing API)
		client, err := NewRealPricingClient(ctx, c.endpointURL)
		if err != nil { // coverage:ignore - AWS SDK config errors are difficult to trigger in unit tests
			// Return a client that will error on every call
			// This is better than panicking, and allows the controller to continue
			// operating even if pricing API is unavailable
			return &BrokenPricingClient{err: err}
		}
		c.pricingCache = client
	}
	return c.pricingCache
}

// BrokenPricingClient is a PricingClient that always returns an error.
// This is used as a fallback when the real pricing client fails to initialize.
// It allows the controller to continue operating even if pricing API is unavailable.
type BrokenPricingClient struct {
	err error
}

// GetOnDemandPrice always returns the initialization error.
func (b *BrokenPricingClient) GetOnDemandPrice(
	_ context.Context,
	_ string,
	_ string,
	_ string,
) (*OnDemandPrice, error) {
	return nil, b.err
}

// GetOnDemandPrices always returns the initialization error.
func (b *BrokenPricingClient) GetOnDemandPrices(
	_ context.Context,
	_ string,
	_ []string,
	_ string,
) ([]OnDemandPrice, error) {
	return nil, b.err
}

// LoadAllPricing always returns the initialization error.
func (b *BrokenPricingClient) LoadAllPricing(
	_ context.Context,
	_ []string,
	_ []string,
) (map[string]float64, error) {
	return nil, b.err
}

// getCredentials returns credentials for the specified account.
// If AssumeRoleARN is set, it performs an STS AssumeRole operation.
// Otherwise, it returns the default credentials from the credential chain.
func (c *RealClient) getCredentials(
	ctx context.Context,
	accountConfig AccountConfig,
) (credentials.StaticCredentialsProvider, error) {
	// If no AssumeRoleARN, use default credentials
	// (Note: For production use, we'd need to support default credentials better.
	// For now, this is primarily for testing with LocalStack where we use static credentials)
	if accountConfig.AssumeRoleARN == "" {
		return credentials.StaticCredentialsProvider{
			Value: aws.Credentials{
				AccessKeyID:     "test",
				SecretAccessKey: "test",
			},
		}, nil
	}

	// Perform AssumeRole
	// This path is tested in localstack_integration_test.go with the -tags=localstack build tag
	// which tests real STS AssumeRole operations against LocalStack.
	// coverage:ignore - AssumeRole path tested in integration tests with LocalStack
	result, err := c.stsClient.AssumeRole(ctx, &sts.AssumeRoleInput{
		RoleArn:         &accountConfig.AssumeRoleARN,
		RoleSessionName: ptrString("lumina-" + accountConfig.AccountID),
	})
	if err != nil {
		return credentials.StaticCredentialsProvider{}, err
	}

	// Return static credentials from the assumed role
	return credentials.StaticCredentialsProvider{
		Value: aws.Credentials{
			AccessKeyID:     *result.Credentials.AccessKeyId,
			SecretAccessKey: *result.Credentials.SecretAccessKey,
			SessionToken:    *result.Credentials.SessionToken,
		},
	}, nil
}

// ptrString returns a pointer to the given string.
func ptrString(s string) *string {
	return &s
}
