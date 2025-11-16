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
	"time"
)

func TestMockClient_EC2(t *testing.T) {
	tests := []struct {
		name            string
		accountConfig   AccountConfig
		setupMock       func(*MockClient)
		expectError     bool
		checkAssumeRole bool
	}{
		{
			name: "basic EC2 client creation",
			accountConfig: AccountConfig{
				AccountID: "111111111111",
				Name:      "Test Account",
				Region:    "us-west-2",
			},
			expectError: false,
		},
		{
			name: "EC2 client with AssumeRole",
			accountConfig: AccountConfig{
				AccountID:     "222222222222",
				Name:          "Production",
				AssumeRoleARN: "arn:aws:iam::222222222222:role/lumina-controller",
				SessionName:   "test-session",
				Region:        "us-east-1",
			},
			expectError:     false,
			checkAssumeRole: true,
		},
		{
			name: "EC2 client error",
			accountConfig: AccountConfig{
				AccountID: "333333333333",
			},
			setupMock: func(m *MockClient) {
				m.EC2Error = context.DeadlineExceeded
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := NewMockClient()

			if tt.setupMock != nil {
				tt.setupMock(mockClient)
			}

			ctx := context.Background()
			ec2Client, err := mockClient.EC2(ctx, tt.accountConfig)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if ec2Client == nil {
				t.Errorf("expected EC2Client but got nil")
				return
			}

			// Check AssumeRole was tracked
			if tt.checkAssumeRole {
				if len(mockClient.AssumeRoleCalls) != 1 {
					t.Errorf("expected 1 AssumeRole call, got %d", len(mockClient.AssumeRoleCalls))
					return
				}

				call := mockClient.AssumeRoleCalls[0]
				if call.AccountID != tt.accountConfig.AccountID {
					t.Errorf("expected AccountID %s, got %s", tt.accountConfig.AccountID, call.AccountID)
				}
				if call.AssumeRoleARN != tt.accountConfig.AssumeRoleARN {
					t.Errorf("expected AssumeRoleARN %s, got %s", tt.accountConfig.AssumeRoleARN, call.AssumeRoleARN)
				}
			}
		})
	}
}

func TestMockClient_SavingsPlans(t *testing.T) {
	tests := []struct {
		name            string
		accountConfig   AccountConfig
		setupMock       func(*MockClient)
		expectError     bool
		checkAssumeRole bool
	}{
		{
			name: "basic SavingsPlans client creation",
			accountConfig: AccountConfig{
				AccountID: "111111111111",
				Name:      "Test Account",
				Region:    "us-west-2",
			},
			expectError: false,
		},
		{
			name: "SavingsPlans client with AssumeRole",
			accountConfig: AccountConfig{
				AccountID:     "222222222222",
				Name:          "Production",
				AssumeRoleARN: "arn:aws:iam::222222222222:role/lumina-controller",
				SessionName:   "test-session",
				Region:        "us-east-1",
			},
			expectError:     false,
			checkAssumeRole: true,
		},
		{
			name: "SavingsPlans client returns error",
			accountConfig: AccountConfig{
				AccountID: "333333333333",
				Name:      "Test Account",
				Region:    "us-west-2",
			},
			setupMock: func(m *MockClient) {
				m.SavingsPlansError = errors.New("mock error")
			},
			expectError: true,
		},
		{
			name: "reuse existing SavingsPlans client",
			accountConfig: AccountConfig{
				AccountID: "444444444444",
				Name:      "Test Account",
				Region:    "us-west-2",
			},
			setupMock: func(m *MockClient) {
				// Pre-create a client for this account
				m.SavingsPlansClients["444444444444"] = NewMockSavingsPlansClient()
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewMockClient()
			if tt.setupMock != nil {
				tt.setupMock(client)
			}

			ctx := context.Background()
			spClient, err := client.SavingsPlans(ctx, tt.accountConfig)

			if tt.expectError && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			if !tt.expectError {
				if spClient == nil {
					t.Error("expected non-nil SavingsPlans client")
				}

				// Verify client is stored in map
				if _, exists := client.SavingsPlansClients[tt.accountConfig.AccountID]; !exists {
					t.Error("SavingsPlans client not stored in map")
				}
			}

			// Check AssumeRole tracking
			if tt.checkAssumeRole {
				if len(client.AssumeRoleCalls) != 1 {
					t.Errorf("expected 1 AssumeRole call, got %d", len(client.AssumeRoleCalls))
				}
				if len(client.AssumeRoleCalls) > 0 {
					call := client.AssumeRoleCalls[0]
					if call.AccountID != tt.accountConfig.AccountID {
						t.Errorf("expected AccountID %s, got %s", tt.accountConfig.AccountID, call.AccountID)
					}
					if call.AssumeRoleARN != tt.accountConfig.AssumeRoleARN {
						t.Errorf("expected AssumeRoleARN %s, got %s", tt.accountConfig.AssumeRoleARN, call.AssumeRoleARN)
					}
				}
			}
		})
	}
}

func TestMockEC2Client_DescribeInstances(t *testing.T) {
	tests := []struct {
		name            string
		instances       []Instance
		regions         []string
		expectedCount   int
		expectedRegions []string
	}{
		{
			name: "all instances, no region filter",
			instances: []Instance{
				{InstanceID: "i-1", Region: "us-west-2", InstanceType: "m5.xlarge", State: "running"},
				{InstanceID: "i-2", Region: "us-east-1", InstanceType: "c5.2xlarge", State: "running"},
				{InstanceID: "i-3", Region: "us-west-2", InstanceType: "r5.large", State: "running"},
			},
			regions:       []string{},
			expectedCount: 3,
		},
		{
			name: "filter by single region",
			instances: []Instance{
				{InstanceID: "i-1", Region: "us-west-2", InstanceType: "m5.xlarge", State: "running"},
				{InstanceID: "i-2", Region: "us-east-1", InstanceType: "c5.2xlarge", State: "running"},
				{InstanceID: "i-3", Region: "us-west-2", InstanceType: "r5.large", State: "running"},
			},
			regions:         []string{"us-west-2"},
			expectedCount:   2,
			expectedRegions: []string{"us-west-2", "us-west-2"},
		},
		{
			name: "filter by multiple regions",
			instances: []Instance{
				{InstanceID: "i-1", Region: "us-west-2", InstanceType: "m5.xlarge", State: "running"},
				{InstanceID: "i-2", Region: "us-east-1", InstanceType: "c5.2xlarge", State: "running"},
				{InstanceID: "i-3", Region: "eu-west-1", InstanceType: "r5.large", State: "running"},
			},
			regions:       []string{"us-west-2", "us-east-1"},
			expectedCount: 2,
		},
		{
			name:          "empty instance list",
			instances:     []Instance{},
			regions:       []string{},
			expectedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockEC2 := NewMockEC2Client()
			mockEC2.Instances = tt.instances

			ctx := context.Background()
			result, err := mockEC2.DescribeInstances(ctx, tt.regions)

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if len(result) != tt.expectedCount {
				t.Errorf("expected %d instances, got %d", tt.expectedCount, len(result))
				return
			}

			if tt.expectedRegions != nil {
				for i, instance := range result {
					if instance.Region != tt.expectedRegions[i] {
						t.Errorf("expected region %s at index %d, got %s", tt.expectedRegions[i], i, instance.Region)
					}
				}
			}

			// Verify call count
			if mockEC2.DescribeInstancesCallCount != 1 {
				t.Errorf("expected 1 DescribeInstances call, got %d", mockEC2.DescribeInstancesCallCount)
			}
		})
	}
}

func TestMockEC2Client_DescribeReservedInstances(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name          string
		reservedInsts []ReservedInstance
		regions       []string
		expectedCount int
	}{
		{
			name: "all RIs, no region filter",
			reservedInsts: []ReservedInstance{
				{
					ReservedInstanceID: "ri-1",
					Region:             "us-west-2",
					InstanceType:       "m5.xlarge",
					State:              "active",
					Start:              now,
					End:                now.Add(365 * 24 * time.Hour),
				},
				{
					ReservedInstanceID: "ri-2",
					Region:             "us-east-1",
					InstanceType:       "c5.2xlarge",
					State:              "active",
					Start:              now,
					End:                now.Add(365 * 24 * time.Hour),
				},
			},
			regions:       []string{},
			expectedCount: 2,
		},
		{
			name: "filter by region",
			reservedInsts: []ReservedInstance{
				{
					ReservedInstanceID: "ri-1",
					Region:             "us-west-2",
					InstanceType:       "m5.xlarge",
					State:              "active",
					Start:              now,
					End:                now.Add(365 * 24 * time.Hour),
				},
				{
					ReservedInstanceID: "ri-2",
					Region:             "us-east-1",
					InstanceType:       "c5.2xlarge",
					State:              "active",
					Start:              now,
					End:                now.Add(365 * 24 * time.Hour),
				},
			},
			regions:       []string{"us-west-2"},
			expectedCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockEC2 := NewMockEC2Client()
			mockEC2.ReservedInstances = tt.reservedInsts

			ctx := context.Background()
			result, err := mockEC2.DescribeReservedInstances(ctx, tt.regions)

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if len(result) != tt.expectedCount {
				t.Errorf("expected %d RIs, got %d", tt.expectedCount, len(result))
			}
		})
	}
}

func TestMockEC2Client_DescribeSpotPriceHistory(t *testing.T) {
	tests := []struct {
		name          string
		spotPrices    []SpotPrice
		regions       []string
		instanceTypes []string
		expectedCount int
	}{
		{
			name: "all spot prices",
			spotPrices: []SpotPrice{
				{InstanceType: "m5.xlarge", AvailabilityZone: "us-west-2a", SpotPrice: 0.034, Timestamp: time.Now()},
				{InstanceType: "c5.2xlarge", AvailabilityZone: "us-east-1b", SpotPrice: 0.068, Timestamp: time.Now()},
			},
			regions:       []string{},
			instanceTypes: []string{},
			expectedCount: 2,
		},
		{
			name: "filter by region",
			spotPrices: []SpotPrice{
				{InstanceType: "m5.xlarge", AvailabilityZone: "us-west-2a", SpotPrice: 0.034, Timestamp: time.Now()},
				{InstanceType: "c5.2xlarge", AvailabilityZone: "us-east-1b", SpotPrice: 0.068, Timestamp: time.Now()},
			},
			regions:       []string{"us-west-2"},
			instanceTypes: []string{},
			expectedCount: 1,
		},
		{
			name: "filter by instance type",
			spotPrices: []SpotPrice{
				{InstanceType: "m5.xlarge", AvailabilityZone: "us-west-2a", SpotPrice: 0.034, Timestamp: time.Now()},
				{InstanceType: "c5.2xlarge", AvailabilityZone: "us-west-2b", SpotPrice: 0.068, Timestamp: time.Now()},
			},
			regions:       []string{},
			instanceTypes: []string{"m5.xlarge"},
			expectedCount: 1,
		},
		{
			name: "filter by region and instance type",
			spotPrices: []SpotPrice{
				{InstanceType: "m5.xlarge", AvailabilityZone: "us-west-2a", SpotPrice: 0.034, Timestamp: time.Now()},
				{InstanceType: "m5.xlarge", AvailabilityZone: "us-east-1b", SpotPrice: 0.035, Timestamp: time.Now()},
				{InstanceType: "c5.2xlarge", AvailabilityZone: "us-west-2b", SpotPrice: 0.068, Timestamp: time.Now()},
			},
			regions:       []string{"us-west-2"},
			instanceTypes: []string{"m5.xlarge"},
			expectedCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockEC2 := NewMockEC2Client()
			mockEC2.SpotPrices = tt.spotPrices

			ctx := context.Background()
			result, err := mockEC2.DescribeSpotPriceHistory(ctx, tt.regions, tt.instanceTypes)

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if len(result) != tt.expectedCount {
				t.Errorf("expected %d spot prices, got %d", tt.expectedCount, len(result))
			}
		})
	}
}

func TestMockEC2Client_GetInstanceByID(t *testing.T) {
	tests := []struct {
		name       string
		instances  []Instance
		region     string
		instanceID string
		expectNil  bool
	}{
		{
			name: "instance found",
			instances: []Instance{
				{InstanceID: "i-abc123", Region: "us-west-2", InstanceType: "m5.xlarge", State: "running"},
			},
			region:     "us-west-2",
			instanceID: "i-abc123",
			expectNil:  false,
		},
		{
			name: "instance not found - wrong region",
			instances: []Instance{
				{InstanceID: "i-abc123", Region: "us-west-2", InstanceType: "m5.xlarge", State: "running"},
			},
			region:     "us-east-1",
			instanceID: "i-abc123",
			expectNil:  true,
		},
		{
			name: "instance not found - wrong ID",
			instances: []Instance{
				{InstanceID: "i-abc123", Region: "us-west-2", InstanceType: "m5.xlarge", State: "running"},
			},
			region:     "us-west-2",
			instanceID: "i-xyz789",
			expectNil:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockEC2 := NewMockEC2Client()
			mockEC2.Instances = tt.instances

			ctx := context.Background()
			result, err := mockEC2.GetInstanceByID(ctx, tt.region, tt.instanceID)

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if tt.expectNil && result != nil {
				t.Errorf("expected nil result, got instance %s", result.InstanceID)
			}

			if !tt.expectNil && result == nil {
				t.Errorf("expected instance, got nil")
			}
		})
	}
}

func TestMockSavingsPlansClient_DescribeSavingsPlans(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name          string
		savingsPlans  []SavingsPlan
		expectedCount int
	}{
		{
			name: "multiple savings plans",
			savingsPlans: []SavingsPlan{
				{
					SavingsPlanARN:  "arn:aws:savingsplans::111111111111:savingsplan/sp-1",
					SavingsPlanType: "EC2Instance",
					Commitment:      150.00,
					Region:          "us-west-2",
					InstanceFamily:  "m5",
					State:           "active",
					Start:           now,
					End:             now.Add(365 * 24 * time.Hour),
				},
				{
					SavingsPlanARN:  "arn:aws:savingsplans::111111111111:savingsplan/sp-2",
					SavingsPlanType: "Compute",
					Commitment:      300.00,
					Region:          "all",
					State:           "active",
					Start:           now,
					End:             now.Add(365 * 24 * time.Hour),
				},
			},
			expectedCount: 2,
		},
		{
			name:          "no savings plans",
			savingsPlans:  []SavingsPlan{},
			expectedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSP := NewMockSavingsPlansClient()
			mockSP.SavingsPlans = tt.savingsPlans

			ctx := context.Background()
			result, err := mockSP.DescribeSavingsPlans(ctx)

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if len(result) != tt.expectedCount {
				t.Errorf("expected %d savings plans, got %d", tt.expectedCount, len(result))
			}
		})
	}
}

func TestMockSavingsPlansClient_GetSavingsPlanByARN(t *testing.T) {
	now := time.Now()

	sp1 := SavingsPlan{
		SavingsPlanARN:  "arn:aws:savingsplans::111111111111:savingsplan/sp-1",
		SavingsPlanType: "EC2Instance",
		Commitment:      150.00,
		State:           "active",
		Start:           now,
		End:             now.Add(365 * 24 * time.Hour),
	}

	tests := []struct {
		name         string
		savingsPlans []SavingsPlan
		arn          string
		expectNil    bool
	}{
		{
			name:         "savings plan found",
			savingsPlans: []SavingsPlan{sp1},
			arn:          sp1.SavingsPlanARN,
			expectNil:    false,
		},
		{
			name:         "savings plan not found",
			savingsPlans: []SavingsPlan{sp1},
			arn:          "arn:aws:savingsplans::111111111111:savingsplan/sp-999",
			expectNil:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSP := NewMockSavingsPlansClient()
			mockSP.SavingsPlans = tt.savingsPlans

			ctx := context.Background()
			result, err := mockSP.GetSavingsPlanByARN(ctx, tt.arn)

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if tt.expectNil && result != nil {
				t.Errorf("expected nil result, got savings plan %s", result.SavingsPlanARN)
			}

			if !tt.expectNil && result == nil {
				t.Errorf("expected savings plan, got nil")
			}
		})
	}
}

func TestMockPricingClient_GetOnDemandPrice(t *testing.T) {
	tests := []struct {
		name            string
		setupPrices     func(*MockPricingClient)
		region          string
		instanceType    string
		operatingSystem string
		expectError     bool
		expectedPrice   float64
	}{
		{
			name: "price found",
			setupPrices: func(m *MockPricingClient) {
				m.SetOnDemandPrice("us-west-2", "m5.xlarge", "Linux", 0.192)
			},
			region:          "us-west-2",
			instanceType:    "m5.xlarge",
			operatingSystem: "Linux",
			expectError:     false,
			expectedPrice:   0.192,
		},
		{
			name: "price not found",
			setupPrices: func(m *MockPricingClient) {
				m.SetOnDemandPrice("us-west-2", "m5.xlarge", "Linux", 0.192)
			},
			region:          "us-east-1",
			instanceType:    "m5.xlarge",
			operatingSystem: "Linux",
			expectError:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockPricing := NewMockPricingClient()
			if tt.setupPrices != nil {
				tt.setupPrices(mockPricing)
			}

			ctx := context.Background()
			result, err := mockPricing.GetOnDemandPrice(ctx, tt.region, tt.instanceType, tt.operatingSystem)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if result.PricePerHour != tt.expectedPrice {
				t.Errorf("expected price %f, got %f", tt.expectedPrice, result.PricePerHour)
			}
		})
	}
}

func TestMockPricingClient_GetOnDemandPrices(t *testing.T) {
	mockPricing := NewMockPricingClient()
	mockPricing.SetOnDemandPrice("us-west-2", "m5.xlarge", "Linux", 0.192)
	mockPricing.SetOnDemandPrice("us-west-2", "c5.2xlarge", "Linux", 0.340)
	mockPricing.SetOnDemandPrice("us-west-2", "r5.large", "Linux", 0.126)

	tests := []struct {
		name            string
		region          string
		instanceTypes   []string
		operatingSystem string
		expectedCount   int
	}{
		{
			name:            "multiple prices found",
			region:          "us-west-2",
			instanceTypes:   []string{"m5.xlarge", "c5.2xlarge", "r5.large"},
			operatingSystem: "Linux",
			expectedCount:   3,
		},
		{
			name:            "partial matches",
			region:          "us-west-2",
			instanceTypes:   []string{"m5.xlarge", "i-dont-exist"},
			operatingSystem: "Linux",
			expectedCount:   1,
		},
		{
			name:            "no matches",
			region:          "us-east-1",
			instanceTypes:   []string{"m5.xlarge"},
			operatingSystem: "Linux",
			expectedCount:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			result, err := mockPricing.GetOnDemandPrices(ctx, tt.region, tt.instanceTypes, tt.operatingSystem)

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if len(result) != tt.expectedCount {
				t.Errorf("expected %d prices, got %d", tt.expectedCount, len(result))
			}
		})
	}
}

// TestMockEC2Client_DescribeReservedInstances_ErrorInjection tests error injection.
func TestMockEC2Client_DescribeReservedInstances_ErrorInjection(t *testing.T) {
	mockEC2 := NewMockEC2Client()
	mockEC2.DescribeReservedInstancesError = errors.New("mock error")

	ctx := context.Background()
	result, err := mockEC2.DescribeReservedInstances(ctx, []string{"us-west-2"})

	if err == nil {
		t.Error("expected error, got nil")
	}
	if result != nil {
		t.Errorf("expected nil result, got %v", result)
	}
}

// TestMockSavingsPlansClient_DescribeSavingsPlans_ErrorInjection tests error injection.
func TestMockSavingsPlansClient_DescribeSavingsPlans_ErrorInjection(t *testing.T) {
	mockSP := NewMockSavingsPlansClient()
	mockSP.DescribeSavingsPlansError = errors.New("mock error")

	ctx := context.Background()
	result, err := mockSP.DescribeSavingsPlans(ctx)

	if err == nil {
		t.Error("expected error, got nil")
	}
	if result != nil {
		t.Errorf("expected nil result, got %v", result)
	}
}
