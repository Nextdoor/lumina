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
	"errors"
	"testing"
)

// mockEC2ClientWithError wraps MockEC2Client and injects errors for DescribeInstances
type mockEC2ClientWithError struct {
	*MockEC2Client
	describeInstancesError error
}

func (m *mockEC2ClientWithError) DescribeInstances(ctx context.Context, regions []string) ([]Instance, error) {
	if m.describeInstancesError != nil {
		return nil, m.describeInstancesError
	}
	return m.MockEC2Client.DescribeInstances(ctx, regions)
}

func TestAccountValidator_ValidateAccountAccess(t *testing.T) {
	tests := []struct {
		name          string
		accountConfig AccountConfig
		setupMock     func(*MockClient)
		expectError   bool
		errorContains string
	}{
		{
			name: "successful validation",
			accountConfig: AccountConfig{
				AccountID:     "123456789012",
				AssumeRoleARN: "arn:aws:iam::123456789012:role/test-role",
				Region:        "us-west-2",
			},
			setupMock: func(m *MockClient) {
				// No errors, will succeed
			},
			expectError: false,
		},
		{
			name: "failed to create EC2 client - AssumeRole denied",
			accountConfig: AccountConfig{
				AccountID:     "123456789012",
				AssumeRoleARN: "arn:aws:iam::123456789012:role/test-role",
				Region:        "us-west-2",
			},
			setupMock: func(m *MockClient) {
				m.EC2Error = errors.New("AccessDenied: User is not authorized to perform: sts:AssumeRole")
			},
			expectError:   true,
			errorContains: "failed to create EC2 client for account 123456789012",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock client
			mockClient := NewMockClient()
			tt.setupMock(mockClient)

			// Create validator
			validator := NewAccountValidator(mockClient)

			// Run validation
			err := validator.ValidateAccountAccess(context.Background(), tt.accountConfig)

			// Check results
			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got nil")
				} else if tt.errorContains != "" && !contains(err.Error(), tt.errorContains) {
					t.Errorf("expected error to contain %q, got %q", tt.errorContains, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

// TestAccountValidator_ValidateAccountAccess_DescribeInstancesError specifically tests
// the scenario where EC2 client creation succeeds but the API call fails.
// This uses a custom mock that wraps MockEC2Client.
func TestAccountValidator_ValidateAccountAccess_DescribeInstancesError(t *testing.T) {
	// Create a custom validator that uses a client with failing DescribeInstances
	mockClient := &mockClientWithFailingDescribeInstances{
		MockClient: NewMockClient(),
	}

	validator := NewAccountValidator(mockClient)

	err := validator.ValidateAccountAccess(context.Background(), AccountConfig{
		AccountID:     "123456789012",
		AssumeRoleARN: "arn:aws:iam::123456789012:role/test-role",
		Region:        "us-west-2",
	})

	if err == nil {
		t.Errorf("expected error but got nil")
	} else if !contains(err.Error(), "failed to validate AWS API access") {
		t.Errorf("expected error to contain 'failed to validate AWS API access', got %q", err.Error())
	}
}

// mockClientWithFailingDescribeInstances wraps MockClient and returns EC2 clients
// that fail on DescribeInstances calls
type mockClientWithFailingDescribeInstances struct {
	*MockClient
}

func (m *mockClientWithFailingDescribeInstances) EC2(
	ctx context.Context,
	accountConfig AccountConfig,
) (EC2Client, error) {
	if m.EC2Error != nil {
		return nil, m.EC2Error
	}

	// Create a normal EC2 client
	ec2Client, err := m.MockClient.EC2(ctx, accountConfig)
	if err != nil {
		return nil, err
	}

	// Wrap it with error injection
	return &mockEC2ClientWithError{
		MockEC2Client:          ec2Client.(*MockEC2Client),
		describeInstancesError: errors.New("RequestError: network timeout"),
	}, nil
}

// contains checks if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
