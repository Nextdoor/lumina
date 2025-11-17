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

package aws_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nextdoor/lumina/pkg/aws"
	"github.com/nextdoor/lumina/pkg/aws/testdata"
	"github.com/nextdoor/lumina/pkg/config"
)

// TestAccountValidator_Integration tests the account validator with mock AWS clients
// using realistic test scenarios.
func TestAccountValidator_Integration(t *testing.T) {
	tests := []struct {
		name          string
		scenario      testdata.Scenario
		accountID     string
		expectError   bool
		errorContains string
	}{
		{
			name:        "SimpleScenario - successful validation",
			scenario:    testdata.SimpleScenario,
			accountID:   "111111111111",
			expectError: false,
		},
		{
			name:        "ComplexScenario - production account validation",
			scenario:    testdata.ComplexScenario,
			accountID:   "111111111111",
			expectError: false,
		},
		{
			name:        "ComplexScenario - staging account validation",
			scenario:    testdata.ComplexScenario,
			accountID:   "222222222222",
			expectError: false,
		},
		{
			name:        "ComplexScenario - development account validation",
			scenario:    testdata.ComplexScenario,
			accountID:   "333333333333",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create and load mock client with scenario data
			client := &aws.MockClient{
				EC2Clients:          make(map[string]*aws.MockEC2Client),
				SavingsPlansClients: make(map[string]*aws.MockSavingsPlansClient),
				PricingClientInstance: &aws.MockPricingClient{
					OnDemandPrices: make(map[string]*aws.OnDemandPrice),
				},
			}
			testdata.LoadScenario(tt.scenario, client)

			// Find the account in the scenario
			var account *testdata.Account
			for i := range tt.scenario.Accounts {
				if tt.scenario.Accounts[i].ID == tt.accountID {
					account = &tt.scenario.Accounts[i]
					break
				}
			}
			if account == nil {
				t.Fatalf("account %s not found in scenario", tt.accountID)
			}

			// Create validator
			validator := aws.NewAccountValidator(client)

			// Validate account access
			accountConfig := aws.AccountConfig{
				AccountID:     account.ID,
				AssumeRoleARN: "arn:aws:iam::" + account.ID + ":role/lumina-controller",
				Region:        account.Region,
			}
			err := validator.ValidateAccountAccess(context.Background(), accountConfig)

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

// TestHealthChecker_Integration tests the health checker with mock AWS clients
// using realistic test scenarios.
func TestHealthChecker_Integration(t *testing.T) {
	tests := []struct {
		name          string
		scenario      testdata.Scenario
		expectError   bool
		errorContains string
	}{
		{
			name:        "SimpleScenario - all accounts accessible",
			scenario:    testdata.SimpleScenario,
			expectError: false,
		},
		{
			name:        "ComplexScenario - all accounts accessible",
			scenario:    testdata.ComplexScenario,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create and load mock client with scenario data
			client := &aws.MockClient{
				EC2Clients:          make(map[string]*aws.MockEC2Client),
				SavingsPlansClients: make(map[string]*aws.MockSavingsPlansClient),
				PricingClientInstance: &aws.MockPricingClient{
					OnDemandPrices: make(map[string]*aws.OnDemandPrice),
				},
			}
			testdata.LoadScenario(tt.scenario, client)

			// Convert scenario accounts to config accounts
			var configAccounts []config.AWSAccount
			for _, account := range tt.scenario.Accounts {
				configAccounts = append(configAccounts, config.AWSAccount{
					AccountID:     account.ID,
					Name:          account.Name,
					AssumeRoleARN: "arn:aws:iam::" + account.ID + ":role/lumina-controller",
					Region:        account.Region,
				})
			}

			// Create validator, monitor, and health checker
			validator := aws.NewAccountValidator(client)
			monitor := aws.NewCredentialMonitor(validator, configAccounts, 10*time.Minute)

			// Run initial check (instead of starting background monitor)
			monitor.CheckAllAccounts()

			healthChecker := aws.NewHealthChecker(monitor)

			// Create test HTTP request
			req := httptest.NewRequest(http.MethodGet, "/readyz", nil)

			// Run health check (reads from monitor cache)
			err := healthChecker.Check(req)

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
