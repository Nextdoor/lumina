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

// Package testdata provides realistic test data fixtures and scenarios for
// testing AWS cost calculations.
//
// This package contains extensive mock data representing multi-account,
// multi-region AWS environments with various pricing models (Reserved
// Instances, Savings Plans, Spot, On-Demand).
package testdata

import (
	"time"

	"github.com/nextdoor/lumina/pkg/aws"
)

// Scenario represents a complete AWS environment test scenario.
// Each scenario includes accounts, instances, pricing, and expected outcomes.
type Scenario struct {
	Name        string
	Description string

	// Accounts defines the AWS accounts in this scenario
	Accounts []Account

	// Expected outcomes for validation
	Expected ExpectedOutcomes
}

// Account represents an AWS account with its resources.
type Account struct {
	ID     string
	Name   string
	Region string

	// Resources in this account
	Instances         []aws.Instance
	ReservedInstances []aws.ReservedInstance
	SavingsPlans      []aws.SavingsPlan
	SpotPrices        []aws.SpotPrice
}

// ExpectedOutcomes defines expected cost calculation results for validation.
type ExpectedOutcomes struct {
	// TotalMonthlyCost is the expected total monthly cost across all resources
	TotalMonthlyCost float64

	// CostByAccount maps account ID to expected monthly cost
	CostByAccount map[string]float64

	// CostByRegion maps region to expected monthly cost
	CostByRegion map[string]float64

	// SavingsPlanUtilization is expected SP utilization percentage (0-100)
	SavingsPlanUtilization float64

	// ReservedInstanceUtilization is expected RI utilization percentage (0-100)
	ReservedInstanceUtilization float64
}

// LoadScenario populates a MockClient with data from a scenario.
// This allows running the same scenario through different test implementations.
func LoadScenario(scenario Scenario, client *aws.MockClient) {
	for _, account := range scenario.Accounts {
		accountConfig := aws.AccountConfig{
			AccountID: account.ID,
			Name:      account.Name,
			Region:    account.Region,
		}

		// Get or create EC2 client for this account
		ec2Client, _ := client.EC2(nil, accountConfig)
		mockEC2 := ec2Client.(*aws.MockEC2Client)

		// Load instances
		mockEC2.Instances = append(mockEC2.Instances, account.Instances...)
		mockEC2.ReservedInstances = append(mockEC2.ReservedInstances, account.ReservedInstances...)
		mockEC2.SpotPrices = append(mockEC2.SpotPrices, account.SpotPrices...)

		// Get or create Savings Plans client
		spClient, _ := client.SavingsPlans(nil, accountConfig)
		mockSP := spClient.(*aws.MockSavingsPlansClient)
		mockSP.SavingsPlans = append(mockSP.SavingsPlans, account.SavingsPlans...)
	}

	// Load pricing data (shared across accounts)
	pricingClient := client.Pricing(nil)
	mockPricing := pricingClient.(*aws.MockPricingClient)
	for _, account := range scenario.Accounts {
		for _, instance := range account.Instances {
			// Set default on-demand pricing if not already set
			mockPricing.SetOnDemandPrice(
				instance.Region,
				instance.InstanceType,
				instance.Platform,
				getDefaultOnDemandPrice(instance.InstanceType),
			)
		}
	}
}

// getDefaultOnDemandPrice returns a realistic on-demand hourly price for an instance type.
// These are approximate AWS prices as of 2025.
func getDefaultOnDemandPrice(instanceType string) float64 {
	prices := map[string]float64{
		// General Purpose
		"t3.micro":   0.0104,
		"t3.small":   0.0208,
		"t3.medium":  0.0416,
		"t3.large":   0.0832,
		"t3.xlarge":  0.1664,
		"t3.2xlarge": 0.3328,

		"m5.large":    0.096,
		"m5.xlarge":   0.192,
		"m5.2xlarge":  0.384,
		"m5.4xlarge":  0.768,
		"m5.8xlarge":  1.536,
		"m5.12xlarge": 2.304,
		"m5.16xlarge": 3.072,
		"m5.24xlarge": 4.608,

		// Compute Optimized
		"c5.large":    0.085,
		"c5.xlarge":   0.17,
		"c5.2xlarge":  0.34,
		"c5.4xlarge":  0.68,
		"c5.9xlarge":  1.53,
		"c5.12xlarge": 2.04,
		"c5.18xlarge": 3.06,
		"c5.24xlarge": 4.08,

		// Memory Optimized
		"r5.large":    0.126,
		"r5.xlarge":   0.252,
		"r5.2xlarge":  0.504,
		"r5.4xlarge":  1.008,
		"r5.8xlarge":  2.016,
		"r5.12xlarge": 3.024,
		"r5.16xlarge": 4.032,
		"r5.24xlarge": 6.048,
	}

	if price, ok := prices[instanceType]; ok {
		return price
	}

	// Default fallback for unknown types
	return 0.10
}

// Helper function to create a time.Time from a date string
func mustParseTime(layout, value string) time.Time {
	t, err := time.Parse(layout, value)
	if err != nil {
		panic(err)
	}
	return t
}
