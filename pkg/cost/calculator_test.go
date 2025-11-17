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

// testBaseTime returns a fixed time for test determinism
func testBaseTime() time.Time {
	return time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
}

// TestCalculatorBasicFlow tests the basic happy path: on-demand instances
// with no RIs or Savings Plans.
func TestCalculatorBasicFlow(t *testing.T) {
	calc := NewCalculator()
	baseTime := testBaseTime()

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
				LaunchTime:       baseTime.Add(1 * time.Hour),
			},
			{
				InstanceID:       "i-002",
				InstanceType:     "c5.2xlarge",
				Region:           "us-west-2",
				AccountID:        "123456789012",
				AvailabilityZone: "us-west-2b",
				State:            "running",
				Lifecycle:        "on-demand",
				LaunchTime:       baseTime.Add(2 * time.Hour),
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

	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	input := CalculationInput{
		Instances: []aws.Instance{
			{
				InstanceID:       "i-001",
				InstanceType:     "m5.xlarge",
				Region:           "us-west-2",
				AccountID:        "123456789012",
				AvailabilityZone: "us-west-2a",
				State:            "running",
				LaunchTime:       baseTime.Add(1 * time.Hour),
			},
			{
				InstanceID:       "i-002",
				InstanceType:     "m5.xlarge",
				Region:           "us-west-2",
				AccountID:        "123456789012",
				AvailabilityZone: "us-west-2a",
				State:            "running",
				LaunchTime:       baseTime.Add(2 * time.Hour),
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

	// The older instance (i-001) should be RI-covered, newer instance (i-002) stays on-demand
	assert.Equal(t, CoverageReservedInstance, result.InstanceCosts["i-001"].CoverageType,
		"Older instance should get RI coverage")
	assert.Equal(t, 0.192, result.InstanceCosts["i-001"].RICoverage)
	assert.Equal(t, 0.0, result.InstanceCosts["i-001"].EffectiveCost)

	assert.Equal(t, CoverageOnDemand, result.InstanceCosts["i-002"].CoverageType,
		"Newer instance should stay on-demand when RI is exhausted")
	assert.Equal(t, 0.192, result.InstanceCosts["i-002"].EffectiveCost)

	// Check aggregates
	assert.Equal(t, 0.384, result.TotalShelfPrice)    // 2 * 0.192
	assert.Equal(t, 0.192, result.TotalEstimatedCost) // Only 1 on-demand
	assert.Equal(t, 0.192, result.TotalSavings)       // 1 RI saves $0.192/hour
}

// TestCalculatorWithEC2InstanceSavingsPlan tests EC2 Instance SP application.
func TestCalculatorWithEC2InstanceSavingsPlan(t *testing.T) {
	calc := NewCalculator()
	baseTime := testBaseTime()

	input := CalculationInput{
		Instances: []aws.Instance{
			{
				InstanceID:       "i-001",
				InstanceType:     "m5.xlarge",
				Region:           "us-west-2",
				AccountID:        "123456789012",
				AvailabilityZone: "us-west-2a",
				State:            "running",
				LaunchTime:       baseTime.Add(1 * time.Hour),
			},
			{
				InstanceID:       "i-002",
				InstanceType:     "m5.2xlarge",
				Region:           "us-west-2",
				AccountID:        "123456789012",
				AvailabilityZone: "us-west-2b",
				State:            "running",
				LaunchTime:       baseTime.Add(2 * time.Hour),
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
	baseTime := testBaseTime()

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
				LaunchTime:       baseTime.Add(1 * time.Hour),
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

	// Use fixed launch times to ensure deterministic ordering
	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

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
				LaunchTime:       baseTime.Add(1 * time.Hour),
			},
			// Instance 2: Will be covered by EC2 Instance SP
			{
				InstanceID:       "i-ec2sp",
				InstanceType:     "m5.2xlarge",
				Region:           "us-west-2",
				AccountID:        "123456789012",
				AvailabilityZone: "us-west-2b",
				State:            "running",
				LaunchTime:       baseTime.Add(2 * time.Hour),
			},
			// Instance 3: Will be covered by Compute SP (launched before i-ondemand)
			{
				InstanceID:       "i-compsp",
				InstanceType:     "c5.xlarge",
				Region:           "us-west-2",
				AccountID:        "123456789012",
				AvailabilityZone: "us-west-2a",
				State:            "running",
				LaunchTime:       baseTime.Add(3 * time.Hour),
			},
			// Instance 4: Will be on-demand (launched after i-compsp, gets no coverage)
			{
				InstanceID:       "i-ondemand",
				InstanceType:     "r5.xlarge",
				Region:           "us-west-2",
				AccountID:        "123456789012",
				AvailabilityZone: "us-west-2a",
				State:            "running",
				LaunchTime:       baseTime.Add(4 * time.Hour),
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
				Commitment:      0.1122, // Exactly enough to cover c5.xlarge, nothing left for r5.xlarge
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

	// Debug output to understand what's happening in CI
	t.Logf("i-compsp: coverage=%s, shelf=%.6f, effective=%.6f, sp_coverage=%.6f",
		result.InstanceCosts["i-compsp"].CoverageType,
		result.InstanceCosts["i-compsp"].ShelfPrice,
		result.InstanceCosts["i-compsp"].EffectiveCost,
		result.InstanceCosts["i-compsp"].SavingsPlanCoverage)
	t.Logf("i-ondemand: coverage=%s, shelf=%.6f, effective=%.6f, sp_coverage=%.6f",
		result.InstanceCosts["i-ondemand"].CoverageType,
		result.InstanceCosts["i-ondemand"].ShelfPrice,
		result.InstanceCosts["i-ondemand"].EffectiveCost,
		result.InstanceCosts["i-ondemand"].SavingsPlanCoverage)

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
	baseTime := testBaseTime()

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
				LaunchTime:       baseTime.Add(1 * time.Hour),
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

// TestCalculatorLaunchTimeStability tests that older instances get SP coverage first,
// providing stable discount assignment when instances have identical savings characteristics.
func TestCalculatorLaunchTimeStability(t *testing.T) {
	calc := NewCalculator()

	// Create a scenario where two m5.xlarge instances have identical:
	// - Instance type (same savings %, same SP rate)
	// - Account, region, AZ
	// The only difference is launch time. The older instance should get coverage.

	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	olderInstance := baseTime.Add(1 * time.Hour)  // Launched first
	newerInstance := baseTime.Add(10 * time.Hour) // Launched later

	input := CalculationInput{
		Instances: []aws.Instance{
			{
				InstanceID:       "i-newer",
				InstanceType:     "m5.xlarge",
				Region:           "us-west-2",
				AccountID:        "123456789012",
				AvailabilityZone: "us-west-2a",
				State:            "running",
				LaunchTime:       newerInstance,
			},
			{
				InstanceID:       "i-older",
				InstanceType:     "m5.xlarge",
				Region:           "us-west-2",
				AccountID:        "123456789012",
				AvailabilityZone: "us-west-2a",
				State:            "running",
				LaunchTime:       olderInstance,
			},
		},
		ReservedInstances: []aws.ReservedInstance{},
		SavingsPlans: []aws.SavingsPlan{
			{
				SavingsPlanARN:  "arn:aws:savingsplans::123456789012:savingsplan/sp-001",
				SavingsPlanType: "EC2Instance",
				Region:          "us-west-2",
				InstanceFamily:  "m5",
				Commitment:      0.138, // Only enough to cover ONE m5.xlarge
				AccountID:       "123456789012",
			},
		},
		OnDemandPrices: map[string]float64{
			"m5.xlarge:us-west-2": 0.192,
		},
	}

	result := calc.Calculate(input)

	// The older instance should get SP coverage, newer instance stays on-demand
	assert.Equal(t, CoverageEC2InstanceSavingsPlan, result.InstanceCosts["i-older"].CoverageType,
		"Older instance should get SP coverage")
	assert.Equal(t, CoverageOnDemand, result.InstanceCosts["i-newer"].CoverageType,
		"Newer instance should stay on-demand when SP is exhausted")

	// Verify the older instance got full coverage
	assert.Greater(t, result.InstanceCosts["i-older"].SavingsPlanCoverage, 0.0)

	// Verify the newer instance got no coverage
	assert.Equal(t, 0.0, result.InstanceCosts["i-newer"].SavingsPlanCoverage)
}

// TestCalculatorMultipleAccounts tests cost calculation across multiple AWS accounts.
func TestCalculatorMultipleAccounts(t *testing.T) {
	calc := NewCalculator()
	baseTime := testBaseTime()

	input := CalculationInput{
		Instances: []aws.Instance{
			{
				InstanceID:       "i-account1",
				InstanceType:     "m5.xlarge",
				Region:           "us-west-2",
				AccountID:        "111111111111",
				AvailabilityZone: "us-west-2a",
				State:            "running",
				LaunchTime:       baseTime.Add(1 * time.Hour),
			},
			{
				InstanceID:       "i-account2",
				InstanceType:     "m5.xlarge",
				Region:           "us-west-2",
				AccountID:        "222222222222",
				AvailabilityZone: "us-west-2a",
				State:            "running",
				LaunchTime:       baseTime.Add(2 * time.Hour),
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
