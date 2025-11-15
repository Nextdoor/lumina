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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nextdoor/lumina/pkg/config"
)

// MockValidator is a test double for the Validator interface
type MockValidator struct {
	ValidateAccountAccessFunc func(ctx context.Context, accountConfig AccountConfig) error
}

func (m *MockValidator) ValidateAccountAccess(ctx context.Context, accountConfig AccountConfig) error {
	if m.ValidateAccountAccessFunc != nil {
		return m.ValidateAccountAccessFunc(ctx, accountConfig)
	}
	return nil
}

func TestHealthChecker_Name(t *testing.T) {
	checker := NewHealthChecker(&MockValidator{}, []config.AWSAccount{})
	if name := checker.Name(); name != "aws-account-access" {
		t.Errorf("expected name 'aws-account-access', got %q", name)
	}
}

func TestHealthChecker_Check(t *testing.T) {
	tests := []struct {
		name          string
		accounts      []config.AWSAccount
		validatorFunc func(ctx context.Context, cfg AccountConfig) error
		expectError   bool
		errorContains string
	}{
		{
			name:     "no accounts configured - healthy",
			accounts: []config.AWSAccount{},
			validatorFunc: func(ctx context.Context, cfg AccountConfig) error {
				t.Error("validator should not be called when no accounts are configured")
				return nil
			},
			expectError: false,
		},
		{
			name: "single account - successful validation",
			accounts: []config.AWSAccount{
				{
					AccountID:     "123456789012",
					Name:          "Production",
					AssumeRoleARN: "arn:aws:iam::123456789012:role/test-role",
					Region:        "us-west-2",
				},
			},
			validatorFunc: func(ctx context.Context, cfg AccountConfig) error {
				return nil
			},
			expectError: false,
		},
		{
			name: "single account - validation failure",
			accounts: []config.AWSAccount{
				{
					AccountID:     "123456789012",
					Name:          "Production",
					AssumeRoleARN: "arn:aws:iam::123456789012:role/test-role",
					Region:        "us-west-2",
				},
			},
			validatorFunc: func(ctx context.Context, cfg AccountConfig) error {
				return errors.New("AssumeRole denied")
			},
			expectError:   true,
			errorContains: "failed to validate access to 1/1 AWS accounts",
		},
		{
			name: "multiple accounts - all successful",
			accounts: []config.AWSAccount{
				{
					AccountID:     "123456789012",
					Name:          "Production",
					AssumeRoleARN: "arn:aws:iam::123456789012:role/test-role",
					Region:        "us-west-2",
				},
				{
					AccountID:     "987654321098",
					Name:          "Staging",
					AssumeRoleARN: "arn:aws:iam::987654321098:role/test-role",
					Region:        "us-east-1",
				},
			},
			validatorFunc: func(ctx context.Context, cfg AccountConfig) error {
				return nil
			},
			expectError: false,
		},
		{
			name: "multiple accounts - partial failure",
			accounts: []config.AWSAccount{
				{
					AccountID:     "123456789012",
					Name:          "Production",
					AssumeRoleARN: "arn:aws:iam::123456789012:role/test-role",
					Region:        "us-west-2",
				},
				{
					AccountID:     "987654321098",
					Name:          "Staging",
					AssumeRoleARN: "arn:aws:iam::987654321098:role/test-role",
					Region:        "us-east-1",
				},
			},
			validatorFunc: func(ctx context.Context, cfg AccountConfig) error {
				// Fail only for the staging account
				if cfg.AccountID == "987654321098" {
					return errors.New("network timeout")
				}
				return nil
			},
			expectError:   true,
			errorContains: "failed to validate access to 1/2 AWS accounts",
		},
		{
			name: "multiple accounts - all failed",
			accounts: []config.AWSAccount{
				{
					AccountID:     "123456789012",
					Name:          "Production",
					AssumeRoleARN: "arn:aws:iam::123456789012:role/test-role",
					Region:        "us-west-2",
				},
				{
					AccountID:     "987654321098",
					Name:          "Staging",
					AssumeRoleARN: "arn:aws:iam::987654321098:role/test-role",
					Region:        "us-east-1",
				},
			},
			validatorFunc: func(ctx context.Context, cfg AccountConfig) error {
				return errors.New("invalid credentials")
			},
			expectError:   true,
			errorContains: "failed to validate access to 2/2 AWS accounts",
		},
		{
			name: "validation error includes account details",
			accounts: []config.AWSAccount{
				{
					AccountID:     "123456789012",
					Name:          "Production",
					AssumeRoleARN: "arn:aws:iam::123456789012:role/test-role",
					Region:        "us-west-2",
				},
			},
			validatorFunc: func(ctx context.Context, cfg AccountConfig) error {
				return errors.New("AssumeRole denied")
			},
			expectError:   true,
			errorContains: "Production (123456789012)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock validator
			mockValidator := &MockValidator{
				ValidateAccountAccessFunc: tt.validatorFunc,
			}

			// Create health checker
			checker := NewHealthChecker(mockValidator, tt.accounts)

			// Create a test HTTP request with context
			req := httptest.NewRequest(http.MethodGet, "/readyz", nil)

			// Run health check
			err := checker.Check(req)

			// Verify results
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
