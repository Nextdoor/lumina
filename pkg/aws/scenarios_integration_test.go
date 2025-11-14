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
	"testing"

	"github.com/nextdoor/lumina/pkg/aws"
	"github.com/nextdoor/lumina/pkg/aws/testdata"
)

// TestScenarioLoading validates that test scenarios can be loaded into MockClient
// and that the expected number of resources are present.
func TestScenarioLoading(t *testing.T) {
	tests := []struct {
		name               string
		scenario           testdata.Scenario
		expectedAccounts   int
		expectedInstances  map[string]int // accountID -> instance count
		expectedRIs        map[string]int // accountID -> RI count
		expectedSPs        map[string]int // accountID -> SP count
	}{
		{
			name:              "SimpleScenario",
			scenario:          testdata.SimpleScenario,
			expectedAccounts:  1,
			expectedInstances: map[string]int{"111111111111": 10},
			expectedRIs:       map[string]int{"111111111111": 2},
			expectedSPs:       map[string]int{"111111111111": 1},
		},
		{
			name:              "ComplexScenario",
			scenario:          testdata.ComplexScenario,
			expectedAccounts:  3,
			expectedInstances: map[string]int{
				"111111111111": 33, // production
				"222222222222": 4,  // staging
				"333333333333": 15, // development
			},
			expectedRIs: map[string]int{
				"111111111111": 3, // production
				"222222222222": 1, // staging
				"333333333333": 0, // development
			},
			expectedSPs: map[string]int{
				"111111111111": 2, // production
				"222222222222": 0, // staging
				"333333333333": 0, // development
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create and load client
			client := &aws.MockClient{
				EC2Clients:          make(map[string]*aws.MockEC2Client),
				SavingsPlansClients: make(map[string]*aws.MockSavingsPlansClient),
				PricingClientInstance: &aws.MockPricingClient{
					OnDemandPrices: make(map[string]*aws.OnDemandPrice),
				},
			}
			testdata.LoadScenario(tt.scenario, client)

			// Validate account count
			if len(tt.scenario.Accounts) != tt.expectedAccounts {
				t.Errorf("expected %d accounts, got %d",
					tt.expectedAccounts, len(tt.scenario.Accounts))
			}

			// Validate resources per account
			for accountID, expectedCount := range tt.expectedInstances {
				account := findAccount(tt.scenario, accountID)
				if account == nil {
					t.Fatalf("account %s not found", accountID)
				}

				// Get EC2 client and validate instance count
				ec2, err := client.EC2(context.Background(), aws.AccountConfig{
					AccountID: account.ID,
					Name:      account.Name,
					Region:    account.Region,
				})
				if err != nil {
					t.Fatalf("failed to get EC2 client: %v", err)
				}
				mockEC2 := ec2.(*aws.MockEC2Client)
				if len(mockEC2.Instances) != expectedCount {
					t.Errorf("account %s: expected %d instances, got %d",
						accountID, expectedCount, len(mockEC2.Instances))
				}

				// Validate RI count
				if expectedRI, ok := tt.expectedRIs[accountID]; ok {
					if len(mockEC2.ReservedInstances) != expectedRI {
						t.Errorf("account %s: expected %d RIs, got %d",
							accountID, expectedRI, len(mockEC2.ReservedInstances))
					}
				}

				// Validate SP count
				if expectedSP, ok := tt.expectedSPs[accountID]; ok {
					sp, err := client.SavingsPlans(context.Background(), aws.AccountConfig{
						AccountID: account.ID,
						Name:      account.Name,
						Region:    account.Region,
					})
					if err != nil {
						t.Fatalf("failed to get SavingsPlans client: %v", err)
					}
					mockSP := sp.(*aws.MockSavingsPlansClient)
					if len(mockSP.SavingsPlans) != expectedSP {
						t.Errorf("account %s: expected %d SPs, got %d",
							accountID, expectedSP, len(mockSP.SavingsPlans))
					}
				}
			}
		})
	}
}

// TestScenarioExpectedOutcomes validates that scenarios have expected outcome definitions.
// These will be used for cost calculation validation once that logic is implemented.
func TestScenarioExpectedOutcomes(t *testing.T) {
	tests := []struct {
		name     string
		scenario testdata.Scenario
	}{
		{"SimpleScenario", testdata.SimpleScenario},
		{"ComplexScenario", testdata.ComplexScenario},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Validate that expected outcomes are defined
			if tt.scenario.Expected.TotalMonthlyCost <= 0 {
				t.Error("TotalMonthlyCost should be defined")
			}

			// For ComplexScenario, validate cost breakdown exists
			if tt.name == "ComplexScenario" {
				if len(tt.scenario.Expected.CostByAccount) == 0 {
					t.Error("CostByAccount should be defined for complex scenario")
				}
				if len(tt.scenario.Expected.CostByRegion) == 0 {
					t.Error("CostByRegion should be defined for complex scenario")
				}
			}
		})
	}
}

// findAccount is a helper to find an account in a scenario by ID.
func findAccount(scenario testdata.Scenario, accountID string) *testdata.Account {
	for i := range scenario.Accounts {
		if scenario.Accounts[i].ID == accountID {
			return &scenario.Accounts[i]
		}
	}
	return nil
}
