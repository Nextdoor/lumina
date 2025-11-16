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
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

const (
	testLocalStackEndpoint = "http://localhost:4566"
)

// isLocalStackAvailable checks if LocalStack is running and accessible.
func isLocalStackAvailable() bool {
	client := &http.Client{
		Timeout: 1 * time.Second,
	}
	resp, err := client.Get(testLocalStackEndpoint)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode < 500
}

// TestNewRealEC2Client tests that NewRealEC2Client creates a valid client.
func TestNewRealEC2Client(t *testing.T) {
	ctx := context.Background()
	creds := credentials.StaticCredentialsProvider{
		Value: aws.Credentials{
			AccessKeyID:     "test",
			SecretAccessKey: "test",
		},
	}

	client, err := NewRealEC2Client(ctx, "123456789012", testRegion, creds, "")
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
		t.Error("expected non-nil EC2 SDK client")
	}
}

// TestNewRealEC2ClientWithEndpoint tests client creation with custom endpoint.
func TestNewRealEC2ClientWithEndpoint(t *testing.T) {
	ctx := context.Background()
	creds := credentials.StaticCredentialsProvider{
		Value: aws.Credentials{
			AccessKeyID:     "test",
			SecretAccessKey: "test",
		},
	}

	endpoint := testLocalStackEndpoint
	client, err := NewRealEC2Client(ctx, "123456789012", "us-east-1", creds, endpoint)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

// TestRealEC2ClientDescribeInstances tests the DescribeInstances method.
// Since the implementation is a stub, we just verify it returns without error.
func TestRealEC2ClientDescribeInstances(t *testing.T) {
	ctx := context.Background()
	creds := credentials.StaticCredentialsProvider{
		Value: aws.Credentials{
			AccessKeyID:     "test",
			SecretAccessKey: "test",
		},
	}

	client, err := NewRealEC2Client(ctx, "123456789012", "us-west-2", creds, "")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	// Call the stub implementation
	instances, err := client.DescribeInstances(ctx, []string{"us-west-2"})
	if err != nil {
		t.Errorf("expected no error from stub, got: %v", err)
	}

	// Stub returns empty slice
	if len(instances) != 0 {
		t.Errorf("expected empty instances from stub, got %d", len(instances))
	}
}

// TestRealEC2ClientDescribeReservedInstances tests the DescribeReservedInstances method.
// This test requires LocalStack to be running.
func TestRealEC2ClientDescribeReservedInstances(t *testing.T) {
	if !isLocalStackAvailable() {
		t.Skip("Skipping test: LocalStack is not available at " + testLocalStackEndpoint)
	}

	ctx := context.Background()
	creds := credentials.StaticCredentialsProvider{
		Value: aws.Credentials{
			AccessKeyID:     "test",
			SecretAccessKey: "test",
		},
	}

	client, err := NewRealEC2Client(ctx, "123456789012", "us-west-2", creds, testLocalStackEndpoint)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ris, err := client.DescribeReservedInstances(ctx, []string{"us-west-2"})
	if err != nil {
		t.Errorf("expected no error from stub, got: %v", err)
	}

	if len(ris) != 0 {
		t.Errorf("expected empty RIs from stub, got %d", len(ris))
	}
}

// TestRealEC2ClientDescribeSpotPriceHistory tests the DescribeSpotPriceHistory method.
func TestRealEC2ClientDescribeSpotPriceHistory(t *testing.T) {
	ctx := context.Background()
	creds := credentials.StaticCredentialsProvider{
		Value: aws.Credentials{
			AccessKeyID:     "test",
			SecretAccessKey: "test",
		},
	}

	client, err := NewRealEC2Client(ctx, "123456789012", "us-west-2", creds, "")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	prices, err := client.DescribeSpotPriceHistory(ctx, []string{"us-west-2"}, []string{"t3.micro"})
	if err != nil {
		t.Errorf("expected no error from stub, got: %v", err)
	}

	if len(prices) != 0 {
		t.Errorf("expected empty spot prices from stub, got %d", len(prices))
	}
}

// TestRealEC2ClientGetInstanceByID tests the GetInstanceByID method.
func TestRealEC2ClientGetInstanceByID(t *testing.T) {
	ctx := context.Background()
	creds := credentials.StaticCredentialsProvider{
		Value: aws.Credentials{
			AccessKeyID:     "test",
			SecretAccessKey: "test",
		},
	}

	client, err := NewRealEC2Client(ctx, "123456789012", "us-west-2", creds, "")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	instance, err := client.GetInstanceByID(ctx, "us-west-2", "i-12345678")
	if err != nil {
		t.Errorf("expected no error from stub, got: %v", err)
	}

	// Stub returns nil
	if instance != nil {
		t.Errorf("expected nil instance from stub, got %v", instance)
	}
}

// TestConvertReservedInstance tests the convertReservedInstance function.
func TestConvertReservedInstance(t *testing.T) {
	// Test with complete RI data
	t.Run("complete data", func(t *testing.T) {
		startTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		endTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

		awsRI := types.ReservedInstances{
			ReservedInstancesId: aws.String("ri-12345678"),
			InstanceType:        types.InstanceTypeM5Large,
			AvailabilityZone:    aws.String("us-west-2a"),
			InstanceCount:       aws.Int32(3),
			State:               types.ReservedInstanceStateActive,
			Start:               &startTime,
			End:                 &endTime,
			OfferingClass:       types.OfferingClassTypeStandard,
			OfferingType:        types.OfferingTypeValuesAllUpfront,
			ProductDescription:  types.RIProductDescriptionLinuxUnix,
		}

		result := convertReservedInstance(awsRI, "us-west-2", "123456789012")

		if result.ReservedInstanceID != "ri-12345678" {
			t.Errorf("expected ReservedInstanceID ri-12345678, got %s", result.ReservedInstanceID)
		}
		if result.InstanceType != "m5.large" {
			t.Errorf("expected InstanceType m5.large, got %s", result.InstanceType)
		}
		if result.AvailabilityZone != "us-west-2a" {
			t.Errorf("expected AvailabilityZone us-west-2a, got %s", result.AvailabilityZone)
		}
		if result.Region != "us-west-2" {
			t.Errorf("expected Region us-west-2, got %s", result.Region)
		}
		if result.InstanceCount != 3 {
			t.Errorf("expected InstanceCount 3, got %d", result.InstanceCount)
		}
		if result.State != "active" {
			t.Errorf("expected State active, got %s", result.State)
		}
		if !result.Start.Equal(startTime) {
			t.Errorf("expected Start %v, got %v", startTime, result.Start)
		}
		if !result.End.Equal(endTime) {
			t.Errorf("expected End %v, got %v", endTime, result.End)
		}
		if result.OfferingClass != "standard" {
			t.Errorf("expected OfferingClass standard, got %s", result.OfferingClass)
		}
		if result.OfferingType != "All Upfront" {
			t.Errorf("expected OfferingType 'All Upfront', got %s", result.OfferingType)
		}
		if result.Platform != "Linux/UNIX" {
			t.Errorf("expected Platform Linux/UNIX, got %s", result.Platform)
		}
		if result.AccountID != "123456789012" {
			t.Errorf("expected AccountID 123456789012, got %s", result.AccountID)
		}
	})

	// Test with minimal RI data (nil pointers)
	t.Run("minimal data with nil pointers", func(t *testing.T) {
		awsRI := types.ReservedInstances{
			ReservedInstancesId: nil, // Test nil handling
			InstanceType:        types.InstanceTypeT3Micro,
			InstanceCount:       nil, // Test nil handling
			State:               types.ReservedInstanceStateActive,
		}

		result := convertReservedInstance(awsRI, "us-east-1", "987654321098")

		if result.ReservedInstanceID != "" {
			t.Errorf("expected empty ReservedInstanceID, got %s", result.ReservedInstanceID)
		}
		if result.InstanceType != "t3.micro" {
			t.Errorf("expected InstanceType t3.micro, got %s", result.InstanceType)
		}
		if result.Region != "us-east-1" {
			t.Errorf("expected Region us-east-1, got %s", result.Region)
		}
		if result.InstanceCount != 0 {
			t.Errorf("expected InstanceCount 0, got %d", result.InstanceCount)
		}
		if result.AccountID != "987654321098" {
			t.Errorf("expected AccountID 987654321098, got %s", result.AccountID)
		}
		// Check zero times for nil Start/End
		if !result.Start.IsZero() {
			t.Errorf("expected zero Start time, got %v", result.Start)
		}
		if !result.End.IsZero() {
			t.Errorf("expected zero End time, got %v", result.End)
		}
	})
}
