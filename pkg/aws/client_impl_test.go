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

// TestNewRealClient tests that NewRealClient creates a valid client instance.
// This test ensures the basic client initialization works without errors.
func TestNewRealClient(t *testing.T) {
	ctx := context.Background()
	cfg := ClientConfig{
		DefaultRegion: "us-west-2",
	}

	// Create client without endpoint (will use real AWS - but we won't call any APIs)
	client, err := NewRealClient(ctx, cfg, "")
	if err != nil {
		t.Fatalf("expected no error creating real client, got: %v", err)
	}

	if client == nil {
		t.Fatal("expected non-nil client")
	}

	// Verify client has initialized fields
	if client.config.DefaultRegion != "us-west-2" {
		t.Errorf("expected DefaultRegion us-west-2, got %s", client.config.DefaultRegion)
	}

	if client.stsClient == nil {
		t.Error("expected non-nil STS client")
	}

	if client.ec2Clients == nil {
		t.Error("expected initialized ec2Clients map")
	}

	if client.spClients == nil {
		t.Error("expected initialized spClients map")
	}
}

// TestNewRealClientWithEndpoint tests client creation with a custom endpoint.
// This is used for LocalStack testing.
func TestNewRealClientWithEndpoint(t *testing.T) {
	ctx := context.Background()
	cfg := ClientConfig{
		DefaultRegion: "us-east-1",
	}

	endpoint := testLocalStackEndpoint
	client, err := NewRealClient(ctx, cfg, endpoint)
	if err != nil {
		t.Fatalf("expected no error creating client with endpoint, got: %v", err)
	}

	if client == nil {
		t.Fatal("expected non-nil client")
	}

	if client.endpointURL != endpoint {
		t.Errorf("expected endpointURL %s, got %s", endpoint, client.endpointURL)
	}
}

// TestRealClientPricing tests that Pricing() returns a valid PricingClient.
func TestRealClientPricing(t *testing.T) {
	ctx := context.Background()
	cfg := ClientConfig{
		DefaultRegion: "us-west-2",
	}

	client, err := NewRealClient(ctx, cfg, "")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	// Get pricing client
	pricingClient := client.Pricing(ctx)
	if pricingClient == nil {
		t.Fatal("expected non-nil pricing client")
	}

	// Call again - should return cached instance
	pricingClient2 := client.Pricing(ctx)
	if pricingClient != pricingClient2 {
		t.Error("expected same cached pricing client instance")
	}
}

// TestNewClientWithEndpointFunction tests the exported NewClientWithEndpoint function.
func TestNewClientWithEndpointFunction(t *testing.T) {
	cfg := ClientConfig{
		DefaultRegion: "us-west-2",
	}

	// Test with empty endpoint
	client, err := NewClientWithEndpoint(cfg, "")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}

	// Test with LocalStack endpoint
	client2, err := NewClientWithEndpoint(cfg, testLocalStackEndpoint)
	if err != nil {
		t.Fatalf("expected no error with endpoint, got: %v", err)
	}
	if client2 == nil {
		t.Fatal("expected non-nil client with endpoint")
	}
}

// TestNewClientFunction tests the exported NewClient function.
func TestNewClientFunction(t *testing.T) {
	cfg := ClientConfig{
		DefaultRegion: "us-west-2",
	}

	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

// TestRealClientEC2 tests the EC2 method with caching.
func TestRealClientEC2(t *testing.T) {
	ctx := context.Background()
	cfg := ClientConfig{
		DefaultRegion: "us-west-2",
	}

	client, err := NewRealClient(ctx, cfg, "")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	// Create account config without AssumeRole
	accountConfig := AccountConfig{
		AccountID: "123456789012",
		Name:      "Test",
		Region:    "us-west-2",
	}

	// First call - should create and cache EC2 client
	ec2Client1, err := client.EC2(ctx, accountConfig)
	if err != nil {
		t.Fatalf("failed to get EC2 client: %v", err)
	}
	if ec2Client1 == nil {
		t.Fatal("expected non-nil EC2 client")
	}

	// Second call - should return cached client
	ec2Client2, err := client.EC2(ctx, accountConfig)
	if err != nil {
		t.Fatalf("failed to get EC2 client (cached): %v", err)
	}

	// Verify it's the same cached instance
	if ec2Client1 != ec2Client2 {
		t.Error("expected same cached EC2 client instance")
	}
}

// TestRealClientSavingsPlans tests the SavingsPlans method with caching.
func TestRealClientSavingsPlans(t *testing.T) {
	ctx := context.Background()
	cfg := ClientConfig{
		DefaultRegion: "us-west-2",
	}

	client, err := NewRealClient(ctx, cfg, "")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	// Create account config without AssumeRole
	accountConfig := AccountConfig{
		AccountID: "123456789012",
		Name:      "Test",
		Region:    "us-west-2",
	}

	// First call - should create and cache SavingsPlans client
	spClient1, err := client.SavingsPlans(ctx, accountConfig)
	if err != nil {
		t.Fatalf("failed to get SavingsPlans client: %v", err)
	}
	if spClient1 == nil {
		t.Fatal("expected non-nil SavingsPlans client")
	}

	// Second call - should return cached client
	spClient2, err := client.SavingsPlans(ctx, accountConfig)
	if err != nil {
		t.Fatalf("failed to get SavingsPlans client (cached): %v", err)
	}

	// Verify it's the same cached instance
	if spClient1 != spClient2 {
		t.Error("expected same cached SavingsPlans client instance")
	}
}

// TestRealClientGetCredentialsWithoutAssumeRole tests getCredentials without AssumeRole.
// When no AssumeRoleARN is provided, getCredentials should return the default
// credential provider from the AWS SDK credential chain (environment variables,
// shared credentials file, IAM role, etc.).
func TestRealClientGetCredentialsWithoutAssumeRole(t *testing.T) {
	ctx := context.Background()
	cfg := ClientConfig{
		DefaultRegion: "us-west-2",
	}

	client, err := NewRealClient(ctx, cfg, "")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	// Create account config without AssumeRole ARN
	accountConfig := AccountConfig{
		AccountID: "123456789012",
		Name:      "Test",
	}

	// Get credentials - should return the default credential provider
	credsProvider := client.getCredentials(accountConfig)

	// Verify we can retrieve credentials (they should come from environment/config)
	// We don't check specific values since they depend on the test environment,
	// but we verify that the provider is functional and returns non-empty credentials.
	credValue, err := credsProvider.Retrieve(ctx)
	if err != nil {
		t.Fatalf("failed to retrieve credentials: %v", err)
	}

	// Verify credentials are not empty (actual values depend on test environment)
	if credValue.AccessKeyID == "" {
		t.Error("expected non-empty AccessKeyID from default credential provider")
	}

	if credValue.SecretAccessKey == "" {
		t.Error("expected non-empty SecretAccessKey from default credential provider")
	}

	// Verify the returned provider is the same as the client's default provider
	if credsProvider != client.defaultCredsProvider {
		t.Error("expected getCredentials to return the same default credential provider instance")
	}
}
