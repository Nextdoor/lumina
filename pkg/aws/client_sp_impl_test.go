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
	"net/http"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/savingsplans/types"
)

const (
	testLocalStackEndpointSP = "http://localhost:4566"
)

// isLocalStackAvailable checks if LocalStack is running and accessible.
func isLocalStackAvailableSP() bool {
	client := &http.Client{
		Timeout: 1 * time.Second,
	}
	resp, err := client.Get(testLocalStackEndpointSP)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode < 500
}

// TestNewRealSPClient tests that NewRealSPClient creates a valid client.
func TestNewRealSPClient(t *testing.T) {
	ctx := context.Background()
	creds := credentials.StaticCredentialsProvider{
		Value: aws.Credentials{
			AccessKeyID:     "test",
			SecretAccessKey: "test",
		},
	}

	client, err := NewRealSPClient(ctx, "123456789012", testRegion, creds, "")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if client == nil {
		t.Fatal("expected non-nil client")
	}

	if client.region != testRegion {
		t.Errorf("expected region us-west-2, got %s", client.region)
	}

	if client.client == nil {
		t.Error("expected non-nil SavingsPlans SDK client")
	}
}

// TestNewRealSPClientWithEndpoint tests client creation with custom endpoint.
func TestNewRealSPClientWithEndpoint(t *testing.T) {
	ctx := context.Background()
	creds := credentials.StaticCredentialsProvider{
		Value: aws.Credentials{
			AccessKeyID:     "test",
			SecretAccessKey: "test",
		},
	}

	endpoint := testLocalStackEndpoint
	client, err := NewRealSPClient(ctx, "123456789012", "us-east-1", creds, endpoint)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

// TestRealSPClientDescribeSavingsPlans tests the DescribeSavingsPlans method.
// This test requires LocalStack to be running.
func TestRealSPClientDescribeSavingsPlans(t *testing.T) {
	if !isLocalStackAvailableSP() {
		t.Skip("Skipping test: LocalStack is not available at " + testLocalStackEndpointSP)
	}

	ctx := context.Background()
	creds := credentials.StaticCredentialsProvider{
		Value: aws.Credentials{
			AccessKeyID:     "test",
			SecretAccessKey: "test",
		},
	}

	client, err := NewRealSPClient(ctx, "123456789012", testRegion, creds, testLocalStackEndpointSP)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	// Call the stub implementation
	plans, err := client.DescribeSavingsPlans(ctx)
	if err != nil {
		t.Errorf("expected no error from stub, got: %v", err)
	}

	// Stub returns empty slice
	if len(plans) != 0 {
		t.Errorf("expected empty plans from stub, got %d", len(plans))
	}
}

// TestRealSPClientGetSavingsPlanByARN tests the GetSavingsPlanByARN method.
// This test requires LocalStack to be running.
func TestRealSPClientGetSavingsPlanByARN(t *testing.T) {
	if !isLocalStackAvailableSP() {
		t.Skip("Skipping test: LocalStack is not available at " + testLocalStackEndpointSP)
	}

	ctx := context.Background()
	creds := credentials.StaticCredentialsProvider{
		Value: aws.Credentials{
			AccessKeyID:     "test",
			SecretAccessKey: "test",
		},
	}

	client, err := NewRealSPClient(ctx, "123456789012", testRegion, creds, testLocalStackEndpointSP)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	testARN := "arn:aws:savingsplans::123456789012:savingsplan/12345678-1234-1234-1234-123456789012"
	plan, err := client.GetSavingsPlanByARN(ctx, testARN)
	if err != nil {
		t.Errorf("expected no error from stub, got: %v", err)
	}

	// Stub returns nil
	if plan != nil {
		t.Errorf("expected nil plan from stub, got %v", plan)
	}
}

// TestConvertSavingsPlan tests the convertSavingsPlan function.
func TestConvertSavingsPlan(t *testing.T) {
	// Test Compute Savings Plan
	t.Run("compute savings plan", func(t *testing.T) {
		start := "2024-01-01T00:00:00Z"
		end := "2025-01-01T00:00:00Z"
		commitment := "1.50"

		awsSP := types.SavingsPlan{
			SavingsPlanArn:  aws.String("arn:aws:savingsplans::123456789012:savingsplan/sp-compute"),
			SavingsPlanId:   aws.String("sp-123456"),
			SavingsPlanType: types.SavingsPlanTypeCompute,
			State:           types.SavingsPlanStateActive,
			Commitment:      &commitment,
			Region:          aws.String("us-west-2"), // Should be overridden to "all" for Compute
			Start:           &start,
			End:             &end,
		}

		result := convertSavingsPlan(awsSP, "123456789012")

		if result.SavingsPlanARN != "arn:aws:savingsplans::123456789012:savingsplan/sp-compute" {
			t.Errorf("expected SavingsPlanARN sp-compute, got %s", result.SavingsPlanARN)
		}
		if result.SavingsPlanID != "sp-123456" {
			t.Errorf("expected SavingsPlanID sp-123456, got %s", result.SavingsPlanID)
		}
		if result.SavingsPlanType != "Compute" {
			t.Errorf("expected SavingsPlanType Compute, got %s", result.SavingsPlanType)
		}
		if result.State != "active" {
			t.Errorf("expected State active, got %s", result.State)
		}
		if result.Commitment != 1.50 {
			t.Errorf("expected Commitment 1.50, got %f", result.Commitment)
		}
		if result.Region != "all" {
			t.Errorf("expected Region all (Compute SPs apply globally), got %s", result.Region)
		}
		if result.AccountID != "123456789012" {
			t.Errorf("expected AccountID 123456789012, got %s", result.AccountID)
		}
		if result.InstanceFamily != "" {
			t.Errorf("expected empty InstanceFamily for Compute SP, got %s", result.InstanceFamily)
		}

		// Verify times
		expectedStart, _ := time.Parse(time.RFC3339, start)
		if !result.Start.Equal(expectedStart) {
			t.Errorf("expected Start %v, got %v", expectedStart, result.Start)
		}
		expectedEnd, _ := time.Parse(time.RFC3339, end)
		if !result.End.Equal(expectedEnd) {
			t.Errorf("expected End %v, got %v", expectedEnd, result.End)
		}
	})

	// Test EC2 Instance Savings Plan
	t.Run("ec2 instance savings plan", func(t *testing.T) {
		commitment := "2.75"

		awsSP := types.SavingsPlan{
			SavingsPlanArn:    aws.String("arn:aws:savingsplans::987654321:savingsplan/sp-ec2"),
			SavingsPlanId:     aws.String("sp-789012"),
			SavingsPlanType:   types.SavingsPlanType("EC2Instance"), // Use string value
			State:             types.SavingsPlanStateActive,
			Commitment:        &commitment,
			Region:            aws.String("us-east-1"),
			Ec2InstanceFamily: aws.String("m5"),
		}

		result := convertSavingsPlan(awsSP, "987654321")

		if result.SavingsPlanType != "EC2Instance" {
			t.Errorf("expected SavingsPlanType EC2Instance, got %s", result.SavingsPlanType)
		}
		if result.Region != "us-east-1" {
			t.Errorf("expected Region us-east-1, got %s", result.Region)
		}
		if result.InstanceFamily != "m5" {
			t.Errorf("expected InstanceFamily m5, got %s", result.InstanceFamily)
		}
		if result.EC2InstanceFamily != "m5" {
			t.Errorf("expected EC2InstanceFamily m5, got %s", result.EC2InstanceFamily)
		}
		if result.Commitment != 2.75 {
			t.Errorf("expected Commitment 2.75, got %f", result.Commitment)
		}
	})

	// Test minimal data with nil pointers
	t.Run("minimal data with nils", func(t *testing.T) {
		awsSP := types.SavingsPlan{
			SavingsPlanArn:  nil,
			SavingsPlanId:   nil,
			SavingsPlanType: types.SavingsPlanTypeCompute,
			State:           types.SavingsPlanStateActive,
			Commitment:      nil,
			Region:          nil,
			Start:           nil,
			End:             nil,
		}

		result := convertSavingsPlan(awsSP, "111111111111")

		if result.SavingsPlanARN != "" {
			t.Errorf("expected empty SavingsPlanARN, got %s", result.SavingsPlanARN)
		}
		if result.Commitment != 0.0 {
			t.Errorf("expected Commitment 0.0, got %f", result.Commitment)
		}
		if result.Region != "all" {
			t.Errorf("expected Region all (Compute SP default), got %s", result.Region)
		}
		if !result.Start.IsZero() {
			t.Errorf("expected zero Start time, got %v", result.Start)
		}
		if !result.End.IsZero() {
			t.Errorf("expected zero End time, got %v", result.End)
		}
	})

	// Test invalid time parsing
	t.Run("invalid time strings", func(t *testing.T) {
		invalidTime := "not-a-valid-time"
		awsSP := types.SavingsPlan{
			SavingsPlanArn:  aws.String("arn:test"),
			SavingsPlanType: types.SavingsPlanTypeCompute,
			State:           types.SavingsPlanStateActive,
			Start:           &invalidTime,
			End:             &invalidTime,
		}

		result := convertSavingsPlan(awsSP, "123456789012")

		// Should result in zero times when parsing fails
		if !result.Start.IsZero() {
			t.Errorf("expected zero Start time for invalid parse, got %v", result.Start)
		}
		if !result.End.IsZero() {
			t.Errorf("expected zero End time for invalid parse, got %v", result.End)
		}
	})

	// Test invalid commitment parsing
	t.Run("invalid commitment string", func(t *testing.T) {
		invalidCommitment := "not-a-number"
		awsSP := types.SavingsPlan{
			SavingsPlanArn:  aws.String("arn:test"),
			SavingsPlanType: types.SavingsPlanTypeCompute,
			State:           types.SavingsPlanStateActive,
			Commitment:      &invalidCommitment,
		}

		result := convertSavingsPlan(awsSP, "123456789012")

		// Should result in 0.0 commitment when parsing fails
		if result.Commitment != 0.0 {
			t.Errorf("expected Commitment 0.0 for invalid parse, got %f", result.Commitment)
		}
	})
}
