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

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
)

// TestNewRealSPClient tests that NewRealSPClient creates a valid client.
func TestNewRealSPClient(t *testing.T) {
	ctx := context.Background()
	creds := credentials.StaticCredentialsProvider{
		Value: aws.Credentials{
			AccessKeyID:     "test",
			SecretAccessKey: "test",
		},
	}

	client, err := NewRealSPClient(ctx, testRegion, creds, "")
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
	client, err := NewRealSPClient(ctx, "us-east-1", creds, endpoint)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

// TestRealSPClientDescribeSavingsPlans tests the DescribeSavingsPlans method.
func TestRealSPClientDescribeSavingsPlans(t *testing.T) {
	ctx := context.Background()
	creds := credentials.StaticCredentialsProvider{
		Value: aws.Credentials{
			AccessKeyID:     "test",
			SecretAccessKey: "test",
		},
	}

	client, err := NewRealSPClient(ctx, testRegion, creds, "")
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
func TestRealSPClientGetSavingsPlanByARN(t *testing.T) {
	ctx := context.Background()
	creds := credentials.StaticCredentialsProvider{
		Value: aws.Credentials{
			AccessKeyID:     "test",
			SecretAccessKey: "test",
		},
	}

	client, err := NewRealSPClient(ctx, testRegion, creds, "")
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
