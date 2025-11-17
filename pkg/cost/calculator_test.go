/*
Copyright 2025 Lumina Contributors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cost

import (
	"testing"
	"time"

	"github.com/nextdoor/lumina/pkg/aws"
	"github.com/stretchr/testify/assert"
)

// TestCalculatorBasicFlow tests the basic happy path: on-demand instances
// with no RIs or Savings Plans.
func TestCalculatorBasicFlow(t *testing.T) {
	calc := NewCalculator()

	input := CalculationInput{
		Instances: []aws.Instance{
			{
				InstanceID:       "i-001",
				InstanceType:     "m5.xlarge",
				Region:           "us-west-2",
				AccountID:        "123456789012",
				AvailabilityZone: "us-west-2a",
				State:            "running",
				Lifecycle:        "on-demand",
			},
			{
				InstanceID:       "i-002",
				InstanceType:     "c5.2xlarge",
				Region:           "us-west-2",
				AccountID:        "123456789012",
				AvailabilityZone: "us-west-2b",
				State:            "running",
				Lifecycle:        "on-demand",
			},
		},
		ReservedInstances: []aws.ReservedInstance{},
		SavingsPlans:      []aws.SavingsPlan{},
		SpotPrices:        make(map[string]float64),
		OnDemandPrices: map[string]float64{
			"m5.xlarge:us-west-2":  0.192,
			"c5.2xlarge:us-west-2": 0.34,
		},
	}

	result := calc.Calculate(input)

	// Verify instance costs
	assert.Len(t, result.InstanceCosts, 2)

	// Check first instance
	cost1 := result.InstanceCosts["i-001"]
	assert.Equal(t, "i-001", cost1.InstanceID)
	assert.Equal(t, 0.192, cost1.ShelfPrice)
	assert.Equal(t, 0.192, cost1.EffectiveCost)
	assert.Equal(t, CoverageOnDemand, cost1.CoverageType)
	assert.Equal(t, 0.0, cost1.RICoverage)
	assert.Equal(t, 0.0, cost1.SavingsPlanCoverage)

	// Check second instance
	cost2 := result.InstanceCosts["i-002"]
	assert.Equal(t, "i-002", cost2.InstanceID)
	assert.Equal(t, 0.34, cost2.ShelfPrice)
	assert.Equal(t, 0.34, cost2.EffectiveCost)

	// Check aggregates
	assert.Equal(t, 0.532, result.TotalShelfPrice)
	assert.Equal(t, 0.532, result.TotalEstimatedCost)
	assert.Equal(t, 0.0, result.TotalSavings)
}

// TestCalculatorWithReservedInstances tests that RIs are applied correctly
// before any Savings Plans.
func TestCalculatorWithReservedInstances(t *testing.T) {
	calc := NewCalculator()

	input := CalculationInput{
		Instances: []aws.Instance{
			{
				InstanceID:       "i-001",
				InstanceType:     "m5.xlarge",
				Region:           "us-west-2",
				AccountID:        "123456789012",
				AvailabilityZone: "us-west-2a",
				State:            "running",
			},
			{
				InstanceID:       "i-002",
				InstanceType:     "m5.xlarge",
				Region:           "us-west-2",
				AccountID:        "123456789012",
				AvailabilityZone: "us-west-2a",
				State:            "running",
			},
		},
		ReservedInstances: []aws.ReservedInstance{
			{
				ReservedInstanceID: "ri-001",
				InstanceType:       "m5.xlarge",
				InstanceCount:      1,
				AvailabilityZone:   "us-west-2a",
				Region:             "us-west-2",
				AccountID:          "123456789012",
			},
		},
		SavingsPlans: []aws.SavingsPlan{},
		OnDemandPrices: map[string]float64{
			"m5.xlarge:us-west-2": 0.192,
		},
	}

	result := calc.Calculate(input)

	// One instance should be RI-covered
	riCoveredCount := 0
	onDemandCount := 0

	for _, cost := range result.InstanceCosts {
		switch cost.CoverageType {
		case CoverageReservedInstance:
			riCoveredCount++
			assert.Equal(t, 0.192, cost.RICoverage)
			assert.Equal(t, 0.0, cost.EffectiveCost)
		case CoverageOnDemand:
			onDemandCount++
			assert.Equal(t, 0.192, cost.EffectiveCost)
		}
	}

	assert.Equal(t, 1, riCoveredCount, "Expected 1 RI-covered instance")
	assert.Equal(t, 1, onDemandCount, "Expected 1 on-demand instance")

	// Check aggregates
	assert.Equal(t, 0.384, result.TotalShelfPrice)    // 2 * 0.192
	assert.Equal(t, 0.192, result.TotalEstimatedCost) // Only 1 on-demand
	assert.Equal(t, 0.192, result.TotalSavings)       // 1 RI saves $0.192/hour
}

// TestCalculatorWithEC2InstanceSavingsPlan tests EC2 Instance SP application.
func TestCalculatorWithEC2InstanceSavingsPlan(t *testing.T) {
	calc := NewCalculator()

	input := CalculationInput{
		Instances: []aws.Instance{
			{
				InstanceID:       "i-001",
				InstanceType:     "m5.xlarge",
				Region:           "us-west-2",
				AccountID:        "123456789012",
				AvailabilityZone: "us-west-2a",
				State:            "running",
			},
			{
				InstanceID:       "i-002",
				InstanceType:     "m5.2xlarge",
				Region:           "us-west-2",
				AccountID:        "123456789012",
				AvailabilityZone: "us-west-2b",
				State:            "running",
			},
		},
		ReservedInstances: []aws.ReservedInstance{},
		SavingsPlans: []aws.SavingsPlan{
			{
				SavingsPlanARN:  "arn:aws:savingsplans::123456789012:savingsplan/sp-001",
				SavingsPlanType: "EC2Instance",
				Region:          "us-west-2",
				InstanceFamily:  "m5",
				Commitment:      0.20, // Enough to cover both instances
				AccountID:       "123456789012",
			},
		},
		OnDemandPrices: map[string]float64{
			"m5.xlarge:us-west-2":  0.192,
			"m5.2xlarge:us-west-2": 0.384,
		},
	}

	result := calc.Calculate(input)

	// Both instances should have SP coverage
	for _, cost := range result.InstanceCosts {
		assert.Equal(t, CoverageEC2InstanceSavingsPlan, cost.CoverageType)
		assert.Greater(t, cost.SavingsPlanCoverage, 0.0)
		assert.Equal(t, "arn:aws:savingsplans::123456789012:savingsplan/sp-001", cost.SavingsPlanARN)
	}

	// Check SP utilization
	spUtil := result.SavingsPlanUtilization["arn:aws:savingsplans::123456789012:savingsplan/sp-001"]
	assert.Equal(t, 0.20, spUtil.HourlyCommitment)
	assert.Greater(t, spUtil.CurrentUtilizationRate, 0.0)
	assert.LessOrEqual(t, spUtil.CurrentUtilizationRate, 0.20)
}

// TestCalculatorSpotPricing tests that spot instances use spot market prices.
func TestCalculatorSpotPricing(t *testing.T) {
	calc := NewCalculator()

	input := CalculationInput{
		Instances: []aws.Instance{
			{
				InstanceID:       "i-spot-001",
				InstanceType:     "m5.xlarge",
				Region:           "us-west-2",
				AccountID:        "123456789012",
				AvailabilityZone: "us-west-2a",
				State:            "running",
				Lifecycle:        "spot",
			},
		},
		ReservedInstances: []aws.ReservedInstance{},
		SavingsPlans:      []aws.SavingsPlan{},
		SpotPrices: map[string]float64{
			"m5.xlarge:us-west-2a": 0.058, // Spot price is much lower
		},
		OnDemandPrices: map[string]float64{
			"m5.xlarge:us-west-2": 0.192,
		},
	}

	result := calc.Calculate(input)

	cost := result.InstanceCosts["i-spot-001"]
	assert.Equal(t, 0.192, cost.ShelfPrice)    // On-demand shelf price
	assert.Equal(t, 0.058, cost.EffectiveCost) // Actual spot price
	assert.Equal(t, CoverageSpot, cost.CoverageType)
	assert.Equal(t, 0.058, cost.SpotPrice)
	assert.True(t, cost.IsSpot)

	// Savings should reflect spot discount
	assert.InDelta(t, 0.134, result.TotalSavings, 0.001)
}

// TestCalculatorPriorityOrder tests that discounts are applied in correct priority:
// RIs → EC2 Instance SPs → Compute SPs → OnDemand
func TestCalculatorPriorityOrder(t *testing.T) {
	calc := NewCalculator()

	input := CalculationInput{
		Instances: []aws.Instance{
			// Instance 1: Will be covered by RI
			{
				InstanceID:       "i-ri",
				InstanceType:     "m5.xlarge",
				Region:           "us-west-2",
				AccountID:        "123456789012",
				AvailabilityZone: "us-west-2a",
				State:            "running",
			},
			// Instance 2: Will be covered by EC2 Instance SP
			{
				InstanceID:       "i-ec2sp",
				InstanceType:     "m5.2xlarge",
				Region:           "us-west-2",
				AccountID:        "123456789012",
				AvailabilityZone: "us-west-2b",
				State:            "running",
			},
			// Instance 3: Will be covered by Compute SP
			{
				InstanceID:       "i-compsp",
				InstanceType:     "c5.xlarge",
				Region:           "us-west-2",
				AccountID:        "123456789012",
				AvailabilityZone: "us-west-2a",
				State:            "running",
			},
			// Instance 4: Will be on-demand (no coverage)
			{
				InstanceID:       "i-ondemand",
				InstanceType:     "r5.xlarge",
				Region:           "us-west-2",
				AccountID:        "123456789012",
				AvailabilityZone: "us-west-2a",
				State:            "running",
			},
		},
		ReservedInstances: []aws.ReservedInstance{
			{
				ReservedInstanceID: "ri-001",
				InstanceType:       "m5.xlarge",
				InstanceCount:      1,
				AvailabilityZone:   "us-west-2a",
				Region:             "us-west-2",
				AccountID:          "123456789012",
			},
		},
		SavingsPlans: []aws.SavingsPlan{
			{
				SavingsPlanARN:  "arn:aws:savingsplans::123456789012:savingsplan/sp-ec2",
				SavingsPlanType: "EC2Instance",
				Region:          "us-west-2",
				InstanceFamily:  "m5",
				Commitment:      0.30, // Enough to cover m5.2xlarge (~$0.277 coverage needed)
				AccountID:       "123456789012",
			},
			{
				SavingsPlanARN:  "arn:aws:savingsplans::123456789012:savingsplan/sp-compute",
				SavingsPlanType: "Compute",
				Region:          "all",
				InstanceFamily:  "all",
				Commitment:      0.112, // Exactly enough to cover c5.xlarge, nothing left for r5.xlarge
				AccountID:       "123456789012",
			},
		},
		OnDemandPrices: map[string]float64{
			"m5.xlarge:us-west-2":  0.192,
			"m5.2xlarge:us-west-2": 0.384,
			"c5.xlarge:us-west-2":  0.17,
			"r5.xlarge:us-west-2":  0.252,
		},
	}

	result := calc.Calculate(input)

	// Verify priority order
	assert.Equal(t, CoverageReservedInstance, result.InstanceCosts["i-ri"].CoverageType)
	assert.Equal(t, CoverageEC2InstanceSavingsPlan, result.InstanceCosts["i-ec2sp"].CoverageType)
	assert.Equal(t, CoverageComputeSavingsPlan, result.InstanceCosts["i-compsp"].CoverageType)
	assert.Equal(t, CoverageOnDemand, result.InstanceCosts["i-ondemand"].CoverageType)
}

// TestCalculatorEmptyInput tests calculator behavior with no instances.
func TestCalculatorEmptyInput(t *testing.T) {
	calc := NewCalculator()

	input := CalculationInput{
		Instances:         []aws.Instance{},
		ReservedInstances: []aws.ReservedInstance{},
		SavingsPlans:      []aws.SavingsPlan{},
		SpotPrices:        make(map[string]float64),
		OnDemandPrices:    make(map[string]float64),
	}

	result := calc.Calculate(input)

	assert.Empty(t, result.InstanceCosts)
	assert.Empty(t, result.SavingsPlanUtilization)
	assert.Equal(t, 0.0, result.TotalEstimatedCost)
	assert.Equal(t, 0.0, result.TotalShelfPrice)
	assert.Equal(t, 0.0, result.TotalSavings)
}

// TestCalculatorSPUtilizationMetrics tests that SP utilization metrics are
// calculated correctly.
func TestCalculatorSPUtilizationMetrics(t *testing.T) {
	calc := NewCalculator()

	endTime := time.Now().Add(365 * 24 * time.Hour) // SP expires in 1 year

	input := CalculationInput{
		Instances: []aws.Instance{
			{
				InstanceID:       "i-001",
				InstanceType:     "m5.xlarge",
				Region:           "us-west-2",
				AccountID:        "123456789012",
				AvailabilityZone: "us-west-2a",
				State:            "running",
			},
		},
		ReservedInstances: []aws.ReservedInstance{},
		SavingsPlans: []aws.SavingsPlan{
			{
				SavingsPlanARN:  "arn:aws:savingsplans::123456789012:savingsplan/sp-001",
				SavingsPlanType: "EC2Instance",
				Region:          "us-west-2",
				InstanceFamily:  "m5",
				Commitment:      0.20,
				AccountID:       "123456789012",
				End:             endTime,
			},
		},
		OnDemandPrices: map[string]float64{
			"m5.xlarge:us-west-2": 0.192,
		},
	}

	result := calc.Calculate(input)

	spUtil := result.SavingsPlanUtilization["arn:aws:savingsplans::123456789012:savingsplan/sp-001"]
	assert.Equal(t, 0.20, spUtil.HourlyCommitment)
	assert.Greater(t, spUtil.CurrentUtilizationRate, 0.0)
	assert.LessOrEqual(t, spUtil.CurrentUtilizationRate, 0.20)
	assert.Greater(t, spUtil.RemainingHours, 0.0)
	assert.InDelta(t, 365*24, spUtil.RemainingHours, 24) // Within 24 hours of 1 year
	assert.Equal(t, endTime, spUtil.EndTime)
}

// TestCalculatorMultipleAccounts tests cost calculation across multiple AWS accounts.
func TestCalculatorMultipleAccounts(t *testing.T) {
	calc := NewCalculator()

	input := CalculationInput{
		Instances: []aws.Instance{
			{
				InstanceID:       "i-account1",
				InstanceType:     "m5.xlarge",
				Region:           "us-west-2",
				AccountID:        "111111111111",
				AvailabilityZone: "us-west-2a",
				State:            "running",
			},
			{
				InstanceID:       "i-account2",
				InstanceType:     "m5.xlarge",
				Region:           "us-west-2",
				AccountID:        "222222222222",
				AvailabilityZone: "us-west-2a",
				State:            "running",
			},
		},
		ReservedInstances: []aws.ReservedInstance{
			{
				ReservedInstanceID: "ri-001",
				InstanceType:       "m5.xlarge",
				InstanceCount:      1,
				AvailabilityZone:   "us-west-2a",
				Region:             "us-west-2",
				AccountID:          "111111111111", // Only covers account 1

			},
		},
		SavingsPlans: []aws.SavingsPlan{},
		OnDemandPrices: map[string]float64{
			"m5.xlarge:us-west-2": 0.192,
		},
	}

	result := calc.Calculate(input)

	// Account 1 instance should be RI-covered
	assert.Equal(t, CoverageReservedInstance, result.InstanceCosts["i-account1"].CoverageType)

	// Account 2 instance should be on-demand (RI doesn't cross accounts)
	assert.Equal(t, CoverageOnDemand, result.InstanceCosts["i-account2"].CoverageType)
}
