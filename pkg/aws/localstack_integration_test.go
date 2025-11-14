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

//go:build localstack
// +build localstack

package aws_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/nextdoor/lumina/pkg/aws"
)

const (
	// LocalStack endpoint for testing
	localstackEndpoint = "http://localhost:4566"

	// Test account IDs (LocalStack uses 000000000000 as default)
	testAccountID        = "000000000000"
	testStagingAccountID = "111111111111"

	// Test IAM role ARNs (created by init script)
	testRoleARN        = "arn:aws:iam::000000000000:role/lumina/LuminaTestRole"
	testStagingRoleARN = "arn:aws:iam::111111111111:role/lumina/LuminaStagingRole"
)

// TestLocalStackConnection verifies that LocalStack is running and accessible.
// This test should be run first to ensure the test environment is ready.
func TestLocalStackConnection(t *testing.T) {
	// Check if LocalStack health endpoint is responding
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", localstackEndpoint+"/_localstack/health", nil)
	if err != nil {
		t.Fatalf("failed to create health check request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("LocalStack is not running at %s. Start it with: cd test/localstack && docker-compose up -d\nError: %v",
			localstackEndpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("LocalStack health check failed with status %d", resp.StatusCode)
	}

	t.Logf("LocalStack is running and healthy at %s", localstackEndpoint)
}

// TestRealClientCreation tests that we can create a RealClient configured
// to use LocalStack as the endpoint.
func TestRealClientCreation(t *testing.T) {
	ctx := context.Background()

	// Create client with LocalStack endpoint
	client, err := aws.NewClientWithEndpoint(aws.ClientConfig{
		DefaultRegion: "us-west-2",
	}, localstackEndpoint)

	if err != nil {
		t.Fatalf("failed to create AWS client: %v", err)
	}

	if client == nil {
		t.Fatal("expected non-nil client")
	}

	// Verify we can get a Pricing client (doesn't require credentials)
	pricingClient := client.Pricing(ctx)
	if pricingClient == nil {
		t.Fatal("expected non-nil pricing client")
	}
}

// TestSTSAssumeRole tests that we can perform a real STS AssumeRole operation
// against LocalStack. This is the core end-to-end test for AssumeRole functionality.
func TestSTSAssumeRole(t *testing.T) {
	tests := []struct {
		name      string
		accountID string
		roleARN   string
		region    string
	}{
		{
			name:      "AssumeProductionRole",
			accountID: testAccountID,
			roleARN:   testRoleARN,
			region:    "us-west-2",
		},
		{
			name:      "AssumeStagingRole",
			accountID: testStagingAccountID,
			roleARN:   testStagingRoleARN,
			region:    "us-east-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			// Create client with LocalStack endpoint
			client, err := aws.NewClientWithEndpoint(aws.ClientConfig{
				DefaultRegion: tt.region,
			}, localstackEndpoint)
			if err != nil {
				t.Fatalf("failed to create AWS client: %v", err)
			}

			// Create account configuration with AssumeRole ARN
			accountConfig := aws.AccountConfig{
				AccountID:     tt.accountID,
				Name:          fmt.Sprintf("Test-%s", tt.accountID),
				AssumeRoleARN: tt.roleARN,
				Region:        tt.region,
			}

			// Get EC2 client (this should trigger AssumeRole)
			ec2Client, err := client.EC2(ctx, accountConfig)
			if err != nil {
				t.Fatalf("failed to get EC2 client (AssumeRole may have failed): %v", err)
			}

			if ec2Client == nil {
				t.Fatal("expected non-nil EC2 client")
			}

			// Get SavingsPlans client (this should also trigger AssumeRole or use cached creds)
			spClient, err := client.SavingsPlans(ctx, accountConfig)
			if err != nil {
				t.Fatalf("failed to get SavingsPlans client: %v", err)
			}

			if spClient == nil {
				t.Fatal("expected non-nil SavingsPlans client")
			}

			t.Logf("Successfully assumed role %s for account %s", tt.roleARN, tt.accountID)
		})
	}
}

// TestMultiAccountAccess tests that we can access multiple accounts using
// different AssumeRole operations within the same client.
func TestMultiAccountAccess(t *testing.T) {
	ctx := context.Background()

	// Create a single client
	client, err := aws.NewClientWithEndpoint(aws.ClientConfig{
		DefaultRegion: "us-west-2",
	}, localstackEndpoint)
	if err != nil {
		t.Fatalf("failed to create AWS client: %v", err)
	}

	// Access first account
	prodAccount := aws.AccountConfig{
		AccountID:     testAccountID,
		Name:          "Production",
		AssumeRoleARN: testRoleARN,
		Region:        "us-west-2",
	}

	prodEC2, err := client.EC2(ctx, prodAccount)
	if err != nil {
		t.Fatalf("failed to get production EC2 client: %v", err)
	}
	if prodEC2 == nil {
		t.Fatal("expected non-nil production EC2 client")
	}

	// Access second account
	stagingAccount := aws.AccountConfig{
		AccountID:     testStagingAccountID,
		Name:          "Staging",
		AssumeRoleARN: testStagingRoleARN,
		Region:        "us-east-1",
	}

	stagingEC2, err := client.EC2(ctx, stagingAccount)
	if err != nil {
		t.Fatalf("failed to get staging EC2 client: %v", err)
	}
	if stagingEC2 == nil {
		t.Fatal("expected non-nil staging EC2 client")
	}

	// Verify both clients are different (they should be cached separately)
	if prodEC2 == stagingEC2 {
		t.Error("expected different EC2 clients for different accounts")
	}

	// Access the first account again - should get cached client
	prodEC2Again, err := client.EC2(ctx, prodAccount)
	if err != nil {
		t.Fatalf("failed to get production EC2 client (second time): %v", err)
	}

	// This should be the same cached instance
	if prodEC2 != prodEC2Again {
		t.Error("expected cached EC2 client for same account on second access")
	}

	t.Log("Successfully accessed multiple accounts with cached credentials")
}

// TestAssumeRoleWithoutARN tests that we can create clients without AssumeRole
// when no ARN is provided (falls back to default credentials).
func TestAssumeRoleWithoutARN(t *testing.T) {
	ctx := context.Background()

	client, err := aws.NewClientWithEndpoint(aws.ClientConfig{
		DefaultRegion: "us-west-2",
	}, localstackEndpoint)
	if err != nil {
		t.Fatalf("failed to create AWS client: %v", err)
	}

	// Create account config WITHOUT AssumeRole ARN
	accountConfig := aws.AccountConfig{
		AccountID: testAccountID,
		Name:      "DirectAccess",
		Region:    "us-west-2",
		// AssumeRoleARN is intentionally empty
	}

	// Should still be able to get clients (using default credentials)
	ec2Client, err := client.EC2(ctx, accountConfig)
	if err != nil {
		t.Fatalf("failed to get EC2 client without AssumeRole: %v", err)
	}

	if ec2Client == nil {
		t.Fatal("expected non-nil EC2 client")
	}

	t.Log("Successfully created client without AssumeRole ARN")
}

// TestClientCaching tests that the client properly caches per-account clients
// to avoid redundant AssumeRole operations.
func TestClientCaching(t *testing.T) {
	ctx := context.Background()

	client, err := aws.NewClientWithEndpoint(aws.ClientConfig{
		DefaultRegion: "us-west-2",
	}, localstackEndpoint)
	if err != nil {
		t.Fatalf("failed to create AWS client: %v", err)
	}

	accountConfig := aws.AccountConfig{
		AccountID:     testAccountID,
		Name:          "CacheTest",
		AssumeRoleARN: testRoleARN,
		Region:        "us-west-2",
	}

	// First access - should create new client and cache it
	ec2Client1, err := client.EC2(ctx, accountConfig)
	if err != nil {
		t.Fatalf("first EC2 access failed: %v", err)
	}

	// Second access - should return cached client
	ec2Client2, err := client.EC2(ctx, accountConfig)
	if err != nil {
		t.Fatalf("second EC2 access failed: %v", err)
	}

	// Verify it's the same instance (cached)
	if ec2Client1 != ec2Client2 {
		t.Error("expected same cached EC2 client instance, but got different instances")
	}

	// Same for SavingsPlans client
	spClient1, err := client.SavingsPlans(ctx, accountConfig)
	if err != nil {
		t.Fatalf("first SavingsPlans access failed: %v", err)
	}

	spClient2, err := client.SavingsPlans(ctx, accountConfig)
	if err != nil {
		t.Fatalf("second SavingsPlans access failed: %v", err)
	}

	if spClient1 != spClient2 {
		t.Error("expected same cached SavingsPlans client instance")
	}

	t.Log("Client caching is working correctly")
}
