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
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
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
	config               ClientConfig
	stsClient            *sts.Client
	defaultCredsProvider aws.CredentialsProvider   // Default credential provider from credential chain
	defaultAccountConfig AccountConfig             // Account config for non-account-specific calls (pricing, etc)
	mu                   sync.RWMutex              // Protects ec2Clients and spClients maps
	ec2Clients           map[string]*RealEC2Client // Cached per-account EC2 clients
	spClients            map[string]*RealSPClient  // Cached per-account Savings Plans clients
	pricingCache         *RealPricingClient        // Shared pricing client (region-independent)
	endpointURL          string                    // Optional endpoint URL (for LocalStack testing)
}

// NewRealClient creates a new RealClient with the specified configuration.
// The client uses the AWS SDK default credential chain for authentication.
//
// The defaultAccountConfig specifies which account to use for non-account-specific
// API calls (e.g., pricing data). This ensures all AWS calls use assumed role credentials.
//
// For LocalStack testing, set endpointURL to "http://localhost:4566".
func NewRealClient(
	ctx context.Context,
	cfg ClientConfig,
	defaultAccountConfig AccountConfig,
	endpointURL string,
) (*RealClient, error) {
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
		config:               cfg,
		stsClient:            stsClient,
		defaultCredsProvider: awsCfg.Credentials,
		defaultAccountConfig: defaultAccountConfig,
		ec2Clients:           make(map[string]*RealEC2Client),
		spClients:            make(map[string]*RealSPClient),
		pricingCache:         nil, // Will be initialized on first Pricing() call
		endpointURL:          endpointURL,
	}, nil
}

// EC2 returns an EC2Client for the specified account configuration.
// If accountConfig.AssumeRoleARN is set, it will assume that role using STS.
// The client is cached per-account to avoid repeated AssumeRole calls.
func (c *RealClient) EC2(ctx context.Context, accountConfig AccountConfig) (EC2Client, error) {
	// Check cache first (read lock)
	cacheKey := accountConfig.AccountID + ":" + accountConfig.Region
	c.mu.RLock()
	if client, ok := c.ec2Clients[cacheKey]; ok {
		c.mu.RUnlock()
		return client, nil
	}
	c.mu.RUnlock()

	// Get credentials (potentially via AssumeRole)
	creds := c.getCredentials(accountConfig)

	// Create EC2 client with assumed credentials
	client, err := NewRealEC2Client(
		ctx, accountConfig.AccountID, accountConfig.Name,
		accountConfig.Region, creds, c.endpointURL,
	)
	if err != nil { // coverage:ignore - AWS SDK config errors are difficult to trigger in unit tests
		return nil, err
	}

	// Cache the client (write lock)
	c.mu.Lock()
	c.ec2Clients[cacheKey] = client
	c.mu.Unlock()
	return client, nil
}

// SavingsPlans returns a SavingsPlansClient for the specified account.
// If accountConfig.AssumeRoleARN is set, it will assume that role using STS.
// The client is cached per-account to avoid repeated AssumeRole calls.
func (c *RealClient) SavingsPlans(ctx context.Context, accountConfig AccountConfig) (SavingsPlansClient, error) {
	// Check cache first (read lock)
	cacheKey := accountConfig.AccountID + ":" + accountConfig.Region
	c.mu.RLock()
	if client, ok := c.spClients[cacheKey]; ok {
		c.mu.RUnlock()
		return client, nil
	}
	c.mu.RUnlock()

	// Get credentials (potentially via AssumeRole)
	creds := c.getCredentials(accountConfig)

	// Create Savings Plans client with assumed credentials
	client, err := NewRealSPClient(
		ctx, accountConfig.AccountID, accountConfig.Name,
		accountConfig.Region, creds, c.endpointURL,
	)
	if err != nil { // coverage:ignore - AWS SDK config errors are difficult to trigger in unit tests
		return nil, err
	}

	// Cache the client (write lock)
	c.mu.Lock()
	c.spClients[cacheKey] = client
	c.mu.Unlock()
	return client, nil
}

// Pricing returns a PricingClient. Pricing data is shared across all accounts,
// but we use the default account's assumed role credentials for authentication.
//
// The pricing client is lazily initialized on first call and then cached
// for subsequent requests. This avoids AWS SDK configuration overhead
// during client initialization.
//
// Note: Unlike EC2 and SavingsPlans clients which are per-account, the pricing
// client is shared because pricing data is the same for all accounts. We use
// the default account's credentials (via AssumeRole) to make the API calls,
// ensuring all AWS calls use assumed role credentials rather than the pod's credentials.
func (c *RealClient) Pricing(ctx context.Context) PricingClient {
	if c.pricingCache == nil {
		// Get credentials for the default account (will use AssumeRole if configured)
		creds := c.getCredentials(c.defaultAccountConfig)

		// Initialize pricing client with default account credentials (uses us-east-1 for pricing API)
		client, err := NewRealPricingClient(ctx, creds, c.endpointURL)
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

// getCredentials returns a credential provider for the specified account.
// If AssumeRoleARN is set, it returns an AssumeRoleProvider that automatically
// refreshes credentials before expiration. Otherwise, it returns the default
// credential provider from the AWS SDK credential chain.
//
// The returned provider is wrapped in a CredentialsCache which handles:
//   - Automatic credential refresh before expiration
//   - Thread-safe credential caching
//   - Exponential backoff on errors
//
// This approach follows AWS SDK best practices and prevents credential
// expiration issues that would occur with manual AssumeRole calls.
func (c *RealClient) getCredentials(accountConfig AccountConfig) aws.CredentialsProvider {
	// If no AssumeRoleARN, use default credentials from the credential chain
	if accountConfig.AssumeRoleARN == "" {
		return c.defaultCredsProvider
	}

	// Use AWS SDK's AssumeRoleProvider for automatic credential refresh.
	// This provider handles:
	// - Automatic refresh before credentials expire (typically ~1 hour)
	// - Thread-safe credential caching
	// - Retry logic with exponential backoff
	// - Proper session naming for AWS CloudTrail audit logs
	//
	// This path is tested in localstack_integration_test.go with the -tags=localstack build tag
	// which tests real STS AssumeRole operations against LocalStack.
	// coverage:ignore - AssumeRole path tested in integration tests with LocalStack
	provider := stscreds.NewAssumeRoleProvider(c.stsClient, accountConfig.AssumeRoleARN,
		func(o *stscreds.AssumeRoleOptions) {
			// Set session name for CloudTrail audit logging
			o.RoleSessionName = "lumina-" + accountConfig.AccountID
		})

	// Wrap in CredentialsCache for automatic refresh before expiration.
	// The cache will transparently refresh credentials when they're close to expiring,
	// preventing the "Request has expired" errors that plagued the previous implementation.
	return aws.NewCredentialsCache(provider)
}
