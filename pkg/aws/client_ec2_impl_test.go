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
