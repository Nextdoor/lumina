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
	"fmt"
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
			"m5.xlarge:us-west-2":  1.00,
			"c5.2xlarge:us-west-2": 2.00,
		},
	}

	result := calc.Calculate(input)

	// Verify instance costs
	assert.Len(t, result.InstanceCosts, 2)

	// Check first instance
	cost1 := result.InstanceCosts["i-001"]
	assert.Equal(t, "i-001", cost1.InstanceID)
	assert.Equal(t, 1.00, cost1.ShelfPrice)
	assert.Equal(t, 1.00, cost1.EffectiveCost)
	assert.Equal(t, CoverageOnDemand, cost1.CoverageType)
	assert.Equal(t, 0.0, cost1.RICoverage)
	assert.Equal(t, 0.0, cost1.SavingsPlanCoverage)

	// Check second instance
	cost2 := result.InstanceCosts["i-002"]
	assert.Equal(t, "i-002", cost2.InstanceID)
	assert.Equal(t, 2.00, cost2.ShelfPrice)
	assert.Equal(t, 2.00, cost2.EffectiveCost)

	// Check aggregates
	assert.Equal(t, 3.00, result.TotalShelfPrice)
	assert.Equal(t, 3.00, result.TotalEstimatedCost)
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
			"m5.xlarge:us-west-2": 1.00,
		},
	}

	result := calc.Calculate(input)

	// The older instance (i-001) should be RI-covered, newer instance (i-002) stays on-demand
	assert.Equal(t, CoverageReservedInstance, result.InstanceCosts["i-001"].CoverageType,
		"Older instance should get RI coverage")
	assert.Equal(t, 1.00, result.InstanceCosts["i-001"].RICoverage)
	assert.Equal(t, 0.0, result.InstanceCosts["i-001"].EffectiveCost)

	assert.Equal(t, CoverageOnDemand, result.InstanceCosts["i-002"].CoverageType,
		"Newer instance should stay on-demand when RI is exhausted")
	assert.Equal(t, 1.00, result.InstanceCosts["i-002"].EffectiveCost)

	// Check aggregates
	assert.Equal(t, 2.00, result.TotalShelfPrice)    // 2 * $1.00
	assert.Equal(t, 1.00, result.TotalEstimatedCost) // Only 1 on-demand
	assert.Equal(t, 1.00, result.TotalSavings)       // 1 RI saves $1.00/hour
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
				Commitment:      1.00, // Enough to cover both instances (SP rates: $0.28 + $0.56 = $0.84)
				AccountID:       "123456789012",
			},
		},
		OnDemandPrices: map[string]float64{
			"m5.xlarge:us-west-2":  1.00,
			"m5.2xlarge:us-west-2": 2.00,
		},
	}

	result := calc.Calculate(input)

	// Verify both instances get full EC2 Instance SP coverage
	// With 72% EC2 Instance SP discount:
	//   - m5.xlarge: $1.00 OD → $0.28 SP rate
	//   - m5.2xlarge: $2.00 OD → $0.56 SP rate
	// Total commitment used: $0.28 + $0.56 = $0.84 (out of $1.00 available)

	// Check first instance (m5.xlarge)
	cost1 := result.InstanceCosts["i-001"]
	assert.Equal(t, "i-001", cost1.InstanceID)
	assert.Equal(t, 1.00, cost1.ShelfPrice)
	assert.InDelta(t, 0.28, cost1.EffectiveCost, 0.01)       // Pays SP rate
	assert.InDelta(t, 0.28, cost1.SavingsPlanCoverage, 0.01) // SP commitment consumed
	assert.Equal(t, CoverageEC2InstanceSavingsPlan, cost1.CoverageType)
	assert.Equal(t, "arn:aws:savingsplans::123456789012:savingsplan/sp-001", cost1.SavingsPlanARN)

	// Check second instance (m5.2xlarge)
	cost2 := result.InstanceCosts["i-002"]
	assert.Equal(t, "i-002", cost2.InstanceID)
	assert.Equal(t, 2.00, cost2.ShelfPrice)
	assert.InDelta(t, 0.56, cost2.EffectiveCost, 0.01)       // Pays SP rate
	assert.InDelta(t, 0.56, cost2.SavingsPlanCoverage, 0.01) // SP commitment consumed
	assert.Equal(t, CoverageEC2InstanceSavingsPlan, cost2.CoverageType)
	assert.Equal(t, "arn:aws:savingsplans::123456789012:savingsplan/sp-001", cost2.SavingsPlanARN)

	// Check SP utilization metrics
	spUtil := result.SavingsPlanUtilization["arn:aws:savingsplans::123456789012:savingsplan/sp-001"]
	assert.Equal(t, 1.00, spUtil.HourlyCommitment)
	assert.InDelta(t, 0.84, spUtil.CurrentUtilizationRate, 0.01) // $0.28 + $0.56
	assert.InDelta(t, 0.16, spUtil.RemainingCapacity, 0.01)      // $1.00 - $0.84
	assert.InDelta(t, 84.0, spUtil.UtilizationPercent, 1.0)      // 84%

	// Check aggregate totals
	assert.InDelta(t, 3.00, result.TotalShelfPrice, 0.01)    // $1.00 + $2.00
	assert.InDelta(t, 0.84, result.TotalEstimatedCost, 0.01) // $0.28 + $0.56
	assert.InDelta(t, 2.16, result.TotalSavings, 0.01)       // $3.00 - $0.84
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
			"m5.xlarge:us-west-2a": 0.30, // Spot price is much lower than on-demand
		},
		OnDemandPrices: map[string]float64{
			"m5.xlarge:us-west-2": 1.00,
		},
	}

	result := calc.Calculate(input)

	cost := result.InstanceCosts["i-spot-001"]
	assert.Equal(t, 1.00, cost.ShelfPrice)    // On-demand shelf price
	assert.Equal(t, 0.30, cost.EffectiveCost) // Actual spot price
	assert.Equal(t, CoverageSpot, cost.CoverageType)
	assert.Equal(t, 0.30, cost.SpotPrice)
	assert.True(t, cost.IsSpot)

	// Savings should reflect spot discount
	assert.InDelta(t, 0.70, result.TotalSavings, 0.001)
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
				Commitment:      0.56, // Enough to cover m5.2xlarge SP rate ($2.00 * 0.28 = $0.56)
				AccountID:       "123456789012",
			},
			{
				SavingsPlanARN:  "arn:aws:savingsplans::123456789012:savingsplan/sp-compute",
				SavingsPlanType: "Compute",
				Region:          "all",
				InstanceFamily:  "all",
				Commitment:      0.3399, // Just under c5.xlarge SP rate ($1.00 * 0.34), nothing left for r5.xlarge
				AccountID:       "123456789012",
			},
		},
		OnDemandPrices: map[string]float64{
			"m5.xlarge:us-west-2":  1.00,
			"m5.2xlarge:us-west-2": 2.00,
			"c5.xlarge:us-west-2":  1.00,
			"r5.xlarge:us-west-2":  1.50,
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
				Commitment:      0.50, // Commitment to cover the instance (SP rate $0.28)
				AccountID:       "123456789012",
				End:             endTime,
			},
		},
		OnDemandPrices: map[string]float64{
			"m5.xlarge:us-west-2": 1.00,
		},
	}

	result := calc.Calculate(input)

	// Verify instance got SP coverage
	// With 72% EC2 Instance SP discount:
	//   - m5.xlarge: $1.00 OD → $0.28 SP rate
	//   - Commitment: $0.50 (enough to fully cover the instance)
	cost := result.InstanceCosts["i-001"]
	assert.Equal(t, 1.00, cost.ShelfPrice)
	assert.InDelta(t, 0.28, cost.EffectiveCost, 0.01)
	assert.InDelta(t, 0.28, cost.SavingsPlanCoverage, 0.01) // SP commitment consumed
	assert.Equal(t, CoverageEC2InstanceSavingsPlan, cost.CoverageType)

	// Verify SP utilization metrics
	spUtil := result.SavingsPlanUtilization["arn:aws:savingsplans::123456789012:savingsplan/sp-001"]
	assert.Equal(t, 0.50, spUtil.HourlyCommitment)
	assert.InDelta(t, 0.28, spUtil.CurrentUtilizationRate, 0.01) // Instance uses $0.28 of commitment
	assert.InDelta(t, 0.22, spUtil.RemainingCapacity, 0.01)      // $0.50 - $0.28 remaining
	assert.InDelta(t, 56.0, spUtil.UtilizationPercent, 1.0)      // 56% utilized
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
				Commitment:      0.28, // Exactly ONE m5.xlarge SP rate ($1.00 * 0.28)
				AccountID:       "123456789012",
			},
		},
		OnDemandPrices: map[string]float64{
			"m5.xlarge:us-west-2": 1.00,
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
			"m5.xlarge:us-west-2": 1.00,
		},
	}

	result := calc.Calculate(input)

	// Account 1 instance should be RI-covered
	assert.Equal(t, CoverageReservedInstance, result.InstanceCosts["i-account1"].CoverageType)

	// Account 2 instance should be on-demand (RI doesn't cross accounts)
	assert.Equal(t, CoverageOnDemand, result.InstanceCosts["i-account2"].CoverageType)
}

// TestCalculatorRIAndSPInteraction tests the critical bug fix: instances already
// covered by RIs should not go negative when Savings Plans are applied.
//
// BUG SCENARIO (before fix):
//  1. Instance fully covered by RI (EffectiveCost = $0)
//  2. Compute SP calculates coverage based on shelf price ($0.192)
//  3. SP tries to subtract $0.13 from $0 → NEGATIVE $-0.13/hr ❌
//
// EXPECTED BEHAVIOR (after fix):
//  1. Instance fully covered by RI (EffectiveCost = $0)
//  2. Compute SP calculates coverage would be $0.13
//  3. Coverage limited to remaining EffectiveCost ($0)
//  4. Final EffectiveCost remains $0 ✅
func TestCalculatorRIAndSPInteraction(t *testing.T) {
	calc := NewCalculator()
	baseTime := testBaseTime()

	input := CalculationInput{
		Instances: []aws.Instance{
			// Instance fully covered by RI - should NOT go negative when SP tries to apply
			{
				InstanceID:       "i-ri-covered",
				InstanceType:     "m5.xlarge",
				Region:           "us-west-2",
				AccountID:        "123456789012",
				AvailabilityZone: "us-west-2a",
				State:            "running",
				LaunchTime:       baseTime.Add(1 * time.Hour),
			},
			// Instance NOT covered by RI - should get SP coverage
			{
				InstanceID:       "i-sp-covered",
				InstanceType:     "c5.xlarge",
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
		SavingsPlans: []aws.SavingsPlan{
			{
				SavingsPlanARN:  "arn:aws:savingsplans::123456789012:savingsplan/sp-compute",
				SavingsPlanType: "Compute",
				Region:          "all",
				InstanceFamily:  "all",
				Commitment:      2.00, // Large commitment - enough to try covering both instances
				AccountID:       "123456789012",
			},
		},
		OnDemandPrices: map[string]float64{
			"m5.xlarge:us-west-2": 1.00,
			"c5.xlarge:us-west-2": 1.00,
		},
	}

	result := calc.Calculate(input)

	// CRITICAL: RI-covered instance must have non-negative cost
	riCoveredCost := result.InstanceCosts["i-ri-covered"]
	assert.Equal(t, CoverageReservedInstance, riCoveredCost.CoverageType,
		"Instance should remain RI-covered, not SP-covered")
	assert.Equal(t, 0.0, riCoveredCost.EffectiveCost,
		"RI-covered instance EffectiveCost must be exactly $0")
	assert.GreaterOrEqual(t, riCoveredCost.EffectiveCost, 0.0,
		"CRITICAL BUG: RI-covered instance went NEGATIVE after SP allocation!")
	assert.Equal(t, 0.0, riCoveredCost.SavingsPlanCoverage,
		"RI-covered instance should not get SP coverage")
	assert.Equal(t, 1.00, riCoveredCost.RICoverage,
		"RI coverage amount should be full shelf price")

	// The SP should cover the other instance instead
	spCoveredCost := result.InstanceCosts["i-sp-covered"]
	assert.Equal(t, CoverageComputeSavingsPlan, spCoveredCost.CoverageType)
	assert.Greater(t, spCoveredCost.SavingsPlanCoverage, 0.0,
		"Non-RI instance should get SP coverage")
	assert.GreaterOrEqual(t, spCoveredCost.EffectiveCost, 0.0,
		"EffectiveCost must never be negative")
}

// TestCalculatorPartialRIAndSPOverlap tests that when an RI partially covers
// an instance, a Savings Plan should NOT cause negative costs.
//
// SCENARIO:
//   - Instance costs $0.20/hr on-demand
//   - RI provides $0.10/hr coverage (partial) → EffectiveCost = $0.10/hr
//   - EC2 Instance SP calculates $0.14/hr coverage (based on shelf price)
//   - SP coverage should be LIMITED to remaining $0.10/hr, not exceed it
//
// This tests the coverage limiting logic for BOTH EC2 Instance SPs and Compute SPs.
func TestCalculatorPartialRIAndSPOverlap(t *testing.T) {
	calc := NewCalculator()
	baseTime := testBaseTime()

	input := CalculationInput{
		Instances: []aws.Instance{
			{
				InstanceID:       "i-partial-ri",
				InstanceType:     "m5.xlarge",
				Region:           "us-west-2",
				AccountID:        "123456789012",
				AvailabilityZone: "us-west-2a",
				State:            "running",
				LaunchTime:       baseTime.Add(1 * time.Hour),
			},
		},
		// No RIs - we'll simulate partial RI coverage by using a small EC2 Instance SP first,
		// then a larger Compute SP that would normally exceed the remaining cost
		ReservedInstances: []aws.ReservedInstance{},
		SavingsPlans: []aws.SavingsPlan{
			{
				SavingsPlanARN:  "arn:aws:savingsplans::123456789012:savingsplan/sp-ec2-small",
				SavingsPlanType: "EC2Instance",
				Region:          "us-west-2",
				InstanceFamily:  "m5",
				Commitment:      0.10, // Small commitment - only partially covers the instance
				AccountID:       "123456789012",
			},
			{
				SavingsPlanARN:  "arn:aws:savingsplans::123456789012:savingsplan/sp-compute-large",
				SavingsPlanType: "Compute",
				Region:          "all",
				InstanceFamily:  "all",
				Commitment:      2.00, // Large commitment - tries to over-cover remaining cost
				AccountID:       "123456789012",
			},
		},
		OnDemandPrices: map[string]float64{
			"m5.xlarge:us-west-2": 1.00,
		},
	}

	result := calc.Calculate(input)

	cost := result.InstanceCosts["i-partial-ri"]

	// CRITICAL: Cost must never go negative
	assert.GreaterOrEqual(t, cost.EffectiveCost, 0.0,
		"CRITICAL BUG: Instance cost went NEGATIVE when multiple SPs overlapped!")

	// Verify SP coverage was applied but limited
	assert.Greater(t, cost.SavingsPlanCoverage, 0.0,
		"Instance should have some SP coverage")

	// Total coverage (SP) should not exceed shelf price
	assert.LessOrEqual(t, cost.SavingsPlanCoverage, cost.ShelfPrice,
		"SP coverage exceeded shelf price - should be capped")

	// Calculate expected values based on SIMPLIFIED model (only ONE SP applies)
	// 1. EC2 Instance SP (72% discount): rate = $1.00 * 0.28 = $0.28
	//    Commitment $0.10 < rate $0.28, so PARTIAL coverage
	//    Contributes $0.10, EffectiveCost = $1.00 - $0.10 = $0.90
	//    SavingsPlanCoverage = $0.10 (SP commitment consumed)
	//
	// 2. Compute SP DOES NOT APPLY (instance already has SP coverage)
	//    Our simplified model prevents multiple SPs from applying to same instance
	//
	// Final state:
	// - EffectiveCost: $0.90 (partially covered, $0.80 spillover at on-demand)
	// - SavingsPlanCoverage: $0.10 (what EC2 SP contributed)

	expectedSPCoverage := 0.10    // EC2 Instance SP contributed its full commitment
	expectedEffectiveCost := 0.90 // $1.00 - $0.10 = $0.90

	assert.InDelta(t, expectedEffectiveCost, cost.EffectiveCost, 0.001,
		"EffectiveCost should be $0.90 (partial EC2 SP coverage)")

	assert.InDelta(t, expectedSPCoverage, cost.SavingsPlanCoverage, 0.001,
		"SP coverage should be $0.10 (only EC2 SP applies, Compute SP blocked)")
}

// TestCalculatorNegativeCostPrevention is a comprehensive test ensuring no combination
// of RIs and SPs can cause negative costs.
func TestCalculatorNegativeCostPrevention(t *testing.T) {
	calc := NewCalculator()
	baseTime := testBaseTime()

	// Test multiple scenarios that previously could cause negative costs
	testCases := []struct {
		name          string
		instance      aws.Instance
		ris           []aws.ReservedInstance
		sps           []aws.SavingsPlan
		onDemandPrice float64
		description   string
	}{
		{
			name: "RI-covered + EC2 Instance SP",
			instance: aws.Instance{
				InstanceID:       "i-test-1",
				InstanceType:     "m5.xlarge",
				Region:           "us-west-2",
				AccountID:        "123456789012",
				AvailabilityZone: "us-west-2a",
				State:            "running",
				LaunchTime:       baseTime,
			},
			ris: []aws.ReservedInstance{
				{
					ReservedInstanceID: "ri-001",
					InstanceType:       "m5.xlarge",
					InstanceCount:      1,
					AvailabilityZone:   "us-west-2a",
					Region:             "us-west-2",
					AccountID:          "123456789012",
				},
			},
			sps: []aws.SavingsPlan{
				{
					SavingsPlanARN:  "arn:aws:savingsplans::123456789012:savingsplan/sp-ec2",
					SavingsPlanType: "EC2Instance",
					Region:          "us-west-2",
					InstanceFamily:  "m5",
					Commitment:      1.0,
					AccountID:       "123456789012",
				},
			},
			onDemandPrice: 1.00,
			description:   "Instance fully covered by RI, EC2 Instance SP tries to apply",
		},
		{
			name: "RI-covered + Compute SP",
			instance: aws.Instance{
				InstanceID:       "i-test-2",
				InstanceType:     "c5.xlarge",
				Region:           "us-west-2",
				AccountID:        "123456789012",
				AvailabilityZone: "us-west-2a",
				State:            "running",
				LaunchTime:       baseTime,
			},
			ris: []aws.ReservedInstance{
				{
					ReservedInstanceID: "ri-002",
					InstanceType:       "c5.xlarge",
					InstanceCount:      1,
					AvailabilityZone:   "us-west-2a",
					Region:             "us-west-2",
					AccountID:          "123456789012",
				},
			},
			sps: []aws.SavingsPlan{
				{
					SavingsPlanARN:  "arn:aws:savingsplans::123456789012:savingsplan/sp-compute",
					SavingsPlanType: "Compute",
					Region:          "all",
					InstanceFamily:  "all",
					Commitment:      2.00,
					AccountID:       "123456789012",
				},
			},
			onDemandPrice: 1.00,
			description:   "Instance fully covered by RI, Compute SP tries to apply",
		},
		{
			name: "EC2 Instance SP + Compute SP",
			instance: aws.Instance{
				InstanceID:       "i-test-3",
				InstanceType:     "m5.xlarge",
				Region:           "us-west-2",
				AccountID:        "123456789012",
				AvailabilityZone: "us-west-2a",
				State:            "running",
				LaunchTime:       baseTime,
			},
			ris: []aws.ReservedInstance{},
			sps: []aws.SavingsPlan{
				{
					SavingsPlanARN:  "arn:aws:savingsplans::123456789012:savingsplan/sp-ec2",
					SavingsPlanType: "EC2Instance",
					Region:          "us-west-2",
					InstanceFamily:  "m5",
					Commitment:      2.00,
					AccountID:       "123456789012",
				},
				{
					SavingsPlanARN:  "arn:aws:savingsplans::123456789012:savingsplan/sp-compute",
					SavingsPlanType: "Compute",
					Region:          "all",
					InstanceFamily:  "all",
					Commitment:      2.00,
					AccountID:       "123456789012",
				},
			},
			onDemandPrice: 1.00,
			description:   "Instance covered by EC2 Instance SP, Compute SP tries to apply after",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			input := CalculationInput{
				Instances:         []aws.Instance{tc.instance},
				ReservedInstances: tc.ris,
				SavingsPlans:      tc.sps,
				OnDemandPrices: map[string]float64{
					tc.instance.InstanceType + ":" + tc.instance.Region: tc.onDemandPrice,
				},
			}

			result := calc.Calculate(input)
			cost := result.InstanceCosts[tc.instance.InstanceID]

			// CRITICAL: No matter what combination of RIs and SPs, cost must NEVER be negative
			assert.GreaterOrEqual(t, cost.EffectiveCost, 0.0,
				"NEGATIVE COST BUG: %s - EffectiveCost went negative: $%.6f/hr",
				tc.description, cost.EffectiveCost)

			// Additional validation
			assert.GreaterOrEqual(t, cost.RICoverage, 0.0, "RI coverage cannot be negative")
			assert.GreaterOrEqual(t, cost.SavingsPlanCoverage, 0.0, "SP coverage cannot be negative")

			// Total coverage should not exceed shelf price
			totalCoverage := cost.RICoverage + cost.SavingsPlanCoverage
			assert.LessOrEqual(t, totalCoverage, cost.ShelfPrice+0.001, // Small epsilon for float precision
				"Total coverage exceeded shelf price")
		})
	}
}

// TestCalculatorInvariantValidation tests that the runtime invariant validation
// catches calculation bugs before they reach metrics.
func TestCalculatorInvariantValidation(t *testing.T) {
	calc := NewCalculator()
	baseTime := testBaseTime()

	// SCENARIO 1: Valid calculation - should NOT panic
	t.Run("Valid Calculation", func(t *testing.T) {
		input := CalculationInput{
			Instances: []aws.Instance{
				{
					InstanceID:       "i-valid",
					InstanceType:     "m5.xlarge",
					Region:           "us-west-2",
					AccountID:        "123456789012",
					AvailabilityZone: "us-west-2a",
					State:            "running",
					LaunchTime:       baseTime,
				},
			},
			ReservedInstances: []aws.ReservedInstance{},
			SavingsPlans: []aws.SavingsPlan{
				{
					SavingsPlanARN:  "arn:aws:savingsplans::123456789012:savingsplan/sp-valid",
					SavingsPlanType: "EC2Instance",
					Region:          "us-west-2",
					InstanceFamily:  "m5",
					Commitment:      0.50,
					AccountID:       "123456789012",
				},
			},
			OnDemandPrices: map[string]float64{
				"m5.xlarge:us-west-2": 1.00,
			},
		}

		// This should NOT panic - the calculation is valid
		assert.NotPanics(t, func() {
			calc.Calculate(input)
		}, "Valid calculation should not trigger invariant violation")
	})

	// SCENARIO 2: All existing tests should pass invariant validation
	// This verifies that the validation doesn't reject valid calculations
	t.Run("Existing Tests Compatibility", func(t *testing.T) {
		// Run a subset of existing test scenarios through the validation
		testCases := []struct {
			name  string
			input CalculationInput
		}{
			{
				name: "On-Demand Only",
				input: CalculationInput{
					Instances: []aws.Instance{
						{
							InstanceID:       "i-ondemand",
							InstanceType:     "m5.xlarge",
							Region:           "us-west-2",
							AccountID:        "123456789012",
							AvailabilityZone: "us-west-2a",
							State:            "running",
							LaunchTime:       baseTime,
						},
					},
					ReservedInstances: []aws.ReservedInstance{},
					SavingsPlans:      []aws.SavingsPlan{},
					OnDemandPrices: map[string]float64{
						"m5.xlarge:us-west-2": 1.00,
					},
				},
			},
			{
				name: "RI Coverage",
				input: CalculationInput{
					Instances: []aws.Instance{
						{
							InstanceID:       "i-ri",
							InstanceType:     "m5.xlarge",
							Region:           "us-west-2",
							AccountID:        "123456789012",
							AvailabilityZone: "us-west-2a",
							State:            "running",
							LaunchTime:       baseTime,
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
						"m5.xlarge:us-west-2": 1.00,
					},
				},
			},
			{
				name: "RI + SP (should not panic after our fix)",
				input: CalculationInput{
					Instances: []aws.Instance{
						{
							InstanceID:       "i-ri-sp",
							InstanceType:     "m5.xlarge",
							Region:           "us-west-2",
							AccountID:        "123456789012",
							AvailabilityZone: "us-west-2a",
							State:            "running",
							LaunchTime:       baseTime,
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
							SavingsPlanARN:  "arn:aws:savingsplans::123456789012:savingsplan/sp-compute",
							SavingsPlanType: "Compute",
							Region:          "all",
							InstanceFamily:  "all",
							Commitment:      2.00,
							AccountID:       "123456789012",
						},
					},
					OnDemandPrices: map[string]float64{
						"m5.xlarge:us-west-2": 1.00,
					},
				},
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				assert.NotPanics(t, func() {
					calc.Calculate(tc.input)
				}, "Scenario '%s' should not trigger invariant violation", tc.name)
			})
		}
	})
}

// TestCalculatorInvariantViolationDetection tests that the validation DOES catch bugs.
// This is critical - if the validation doesn't catch bugs, it's useless.
//
// NOTE: These tests verify the validation logic itself, not the cost calculation.
// We're testing that validateSavingsPlansInvariants() correctly detects violations.
func TestCalculatorInvariantViolationDetection(t *testing.T) {
	calc := NewCalculator()

	// We can't easily trigger violations through Calculate() because the algorithm
	// is correct. Instead, we'll test the validation function directly with
	// deliberately broken state to ensure it catches violations.

	t.Run("Negative Cost Detection", func(t *testing.T) {
		// Create a costs map with a negative effective cost
		costs := map[string]*InstanceCost{
			"i-negative": {
				InstanceID:          "i-negative",
				InstanceType:        "m5.xlarge",
				Region:              "us-west-2",
				AccountID:           "123456789012",
				AvailabilityZone:    "us-west-2a",
				ShelfPrice:          1.00,
				EffectiveCost:       -0.20, // BUG: Negative cost!
				RICoverage:          1.00,
				SavingsPlanCoverage: 0.20,
				CoverageType:        CoverageReservedInstance,
			},
		}

		utilization := map[string]*SavingsPlanUtilization{}

		// The validation should panic on negative cost
		assert.Panics(t, func() {
			calc.validateSavingsPlansInvariants([]aws.SavingsPlan{}, costs, utilization)
		}, "Should panic on negative effective cost")
	})

	t.Run("Coverage Exceeds Shelf Price", func(t *testing.T) {
		// Create a costs map where coverage exceeds shelf price
		costs := map[string]*InstanceCost{
			"i-overcovered": {
				InstanceID:          "i-overcovered",
				InstanceType:        "m5.xlarge",
				Region:              "us-west-2",
				AccountID:           "123456789012",
				AvailabilityZone:    "us-west-2a",
				ShelfPrice:          1.00,
				EffectiveCost:       0.00,
				RICoverage:          0.70, // RI coverage
				SavingsPlanCoverage: 0.50, // SP coverage
				CoverageType:        CoverageReservedInstance,
				// Total: 0.70 + 0.50 = 1.20 > 1.00 ← BUG!
			},
		}

		utilization := map[string]*SavingsPlanUtilization{}

		// The validation should panic on over-coverage
		assert.Panics(t, func() {
			calc.validateSavingsPlansInvariants([]aws.SavingsPlan{}, costs, utilization)
		}, "Should panic when total coverage exceeds shelf price")
	})

	t.Run("SP Commitment Balance Violation", func(t *testing.T) {
		// Create an SP with utilization that doesn't match commitment
		sp := aws.SavingsPlan{
			SavingsPlanARN:  "arn:aws:savingsplans::123456789012:savingsplan/sp-broken",
			SavingsPlanType: "Compute",
			Region:          "all",
			InstanceFamily:  "all",
			Commitment:      1.00, // Commitment is $1.00/hour
			AccountID:       "123456789012",
		}

		costs := map[string]*InstanceCost{}

		utilization := map[string]*SavingsPlanUtilization{
			sp.SavingsPlanARN: {
				SavingsPlanARN:         sp.SavingsPlanARN,
				HourlyCommitment:       sp.Commitment,
				CurrentUtilizationRate: 0.80, // Used $0.80/hour
				RemainingCapacity:      0.30, // Remaining $0.30/hour
				// Total: 0.80 + 0.30 = 1.10 ≠ 1.00 ← BUG!
			},
		}

		// The validation should panic on commitment imbalance
		assert.Panics(t, func() {
			calc.validateSavingsPlansInvariants([]aws.SavingsPlan{sp}, costs, utilization)
		}, "Should panic when SP utilization + remaining ≠ commitment")
	})

	t.Run("Valid State Does Not Panic", func(t *testing.T) {
		// Create a valid costs map - should NOT panic
		costs := map[string]*InstanceCost{
			"i-valid": {
				InstanceID:          "i-valid",
				InstanceType:        "m5.xlarge",
				Region:              "us-west-2",
				AccountID:           "123456789012",
				AvailabilityZone:    "us-west-2a",
				ShelfPrice:          1.00,
				EffectiveCost:       0.28, // Positive, reasonable cost (EC2 Instance SP rate)
				RICoverage:          0.0,
				SavingsPlanCoverage: 0.28, // Coverage < shelf price ✓
				CoverageType:        CoverageEC2InstanceSavingsPlan,
			},
		}

		sp := aws.SavingsPlan{
			SavingsPlanARN:  "arn:aws:savingsplans::123456789012:savingsplan/sp-valid",
			SavingsPlanType: "EC2Instance",
			Region:          "us-west-2",
			InstanceFamily:  "m5",
			Commitment:      0.50,
			AccountID:       "123456789012",
		}

		utilization := map[string]*SavingsPlanUtilization{
			sp.SavingsPlanARN: {
				SavingsPlanARN:         sp.SavingsPlanARN,
				HourlyCommitment:       sp.Commitment,
				CurrentUtilizationRate: 0.28, // Used $0.28/hour
				RemainingCapacity:      0.22, // Remaining $0.22/hour
				// Total: 0.28 + 0.22 = 0.50 ✓
			},
		}

		// Valid state should NOT panic
		assert.NotPanics(t, func() {
			calc.validateSavingsPlansInvariants([]aws.SavingsPlan{sp}, costs, utilization)
		}, "Valid state should not trigger invariant violation")
	})
}

// TestCalculatorMultipleSavingsPlansOnSameInstance tests the scenario where
// multiple Savings Plans can apply to the same instance. This is the regression
// test for the production bug where SavingsPlanCoverage was accumulating SP rates
// instead of actual discounts, causing total coverage to exceed shelf price.
//
// Scenario: An instance with 5 Savings Plans that could all apply to it.
// Bug: Each SP adds its rate to SavingsPlanCoverage, causing it to exceed shelf price.
// Fix: SavingsPlanCoverage now tracks the actual cost reduction, not the SP rate.
func TestCalculatorMultipleSavingsPlansOnSameInstance(t *testing.T) {
	calc := NewCalculator()
	baseTime := testBaseTime()

	// Create an instance with a relatively low shelf price
	instance := aws.Instance{
		InstanceID:       "i-multi-sp",
		InstanceType:     "m5.large",
		Region:           "us-west-2",
		AccountID:        "123456789012",
		AvailabilityZone: "us-west-2a",
		State:            "running",
		Lifecycle:        "on-demand",
		LaunchTime:       baseTime,
	}

	// Create 5 EC2 Instance Savings Plans that all match the instance
	// Each has enough commitment to partially cover the instance
	savingsPlans := []aws.SavingsPlan{}
	for i := 1; i <= 5; i++ {
		savingsPlans = append(savingsPlans, aws.SavingsPlan{
			SavingsPlanARN:  "arn:aws:savingsplans::123456789012:savingsplan/sp-" + string(rune('0'+i)),
			SavingsPlanType: "EC2Instance",
			Region:          "us-west-2",
			InstanceFamily:  "m5",
			Commitment:      0.05, // Each SP has $0.05/hour commitment
			AccountID:       "123456789012",
			Start:           baseTime.Add(-24 * time.Hour),
			End:             baseTime.Add(365 * 24 * time.Hour),
		})
	}

	input := CalculationInput{
		Instances:         []aws.Instance{instance},
		ReservedInstances: []aws.ReservedInstance{},
		SavingsPlans:      savingsPlans,
		SpotPrices:        make(map[string]float64),
		OnDemandPrices: map[string]float64{
			"m5.large:us-west-2": 0.10, // $0.10/hour shelf price
		},
	}

	// This should NOT panic with the fix
	// Before the fix: SavingsPlanCoverage would accumulate to ~$0.14 (5 * $0.028)
	// After the fix: SavingsPlanCoverage should be the actual discount (~$0.072)
	result := calc.Calculate(input)

	// Verify the instance cost
	cost := result.InstanceCosts["i-multi-sp"]
	assert.Equal(t, "i-multi-sp", cost.InstanceID)
	assert.Equal(t, 0.10, cost.ShelfPrice)

	// The instance should be covered by multiple SPs
	assert.Greater(t, cost.SavingsPlanCoverage, 0.0, "Instance should have SP coverage")

	// CRITICAL: Total coverage must not exceed shelf price
	totalCoverage := cost.RICoverage + cost.SavingsPlanCoverage
	assert.LessOrEqual(t, totalCoverage, cost.ShelfPrice,
		"Total coverage (RI + SP) must not exceed shelf price")

	// The effective cost should be positive (one of the SPs is covering it)
	assert.Greater(t, cost.EffectiveCost, 0.0, "Effective cost should be positive")
	assert.Less(t, cost.EffectiveCost, cost.ShelfPrice, "Effective cost should be less than shelf price")

	// Verify SavingsPlanCoverage tracks SP commitment consumed (not discount):
	// For a fully covered instance: SavingsPlanCoverage == EffectiveCost (both = SP rate)
	// Our simplified model: only ONE SP applies, so this should hold
	assert.InDelta(t, cost.EffectiveCost, cost.SavingsPlanCoverage, 0.001,
		"SavingsPlanCoverage should equal SP commitment consumed (EffectiveCost for fully covered)")

	// Verify SP utilization is tracked correctly
	// At least one SP should have utilization (the first one that matched)
	hasUtilization := false
	for _, sp := range savingsPlans {
		util, exists := result.SavingsPlanUtilization[sp.SavingsPlanARN]
		assert.True(t, exists, "SP utilization should be tracked")
		if util.CurrentUtilizationRate > 0 {
			hasUtilization = true
		}
	}
	assert.True(t, hasUtilization, "At least one SP should have utilization")

	// Verify aggregates are sane
	assert.Equal(t, 0.10, result.TotalShelfPrice, "Total shelf price should match instance shelf price")
	assert.Greater(t, result.TotalSavings, 0.0, "Should have savings from SP coverage")
	assert.Less(t, result.TotalEstimatedCost, result.TotalShelfPrice, "Estimated cost should be less than shelf price")
}

// TestCalculatorMultipleComputeSPsMatchCommitment tests that when multiple Compute
// Savings Plans exist, only ONE applies to each instance, and the sum of instance
// EffectiveCosts equals the sum of SP commitment consumed.
//
// This is a regression test for the bug where multiple Compute SPs would all try to
// apply to the same instance, each reducing EffectiveCost, causing a mismatch between:
// - sum(ec2_instance_hourly_cost{cost_type="compute_savings_plan"}) [too low]
// - sum(savings_plan_current_utilization_rate) [correct]
//
// The fix prevents multiple SPs of the same type from applying to the same instance.
func TestCalculatorMultipleComputeSPsMatchCommitment(t *testing.T) {
	calc := NewCalculator()
	baseTime := testBaseTime()

	// Create 10 instances that can be covered by Compute SPs
	instances := []aws.Instance{}
	onDemandPrices := make(map[string]float64)

	for i := 1; i <= 10; i++ {
		instanceID := fmt.Sprintf("i-compute-sp-%d", i)
		instances = append(instances, aws.Instance{
			InstanceID:       instanceID,
			InstanceType:     "m5.xlarge",
			Region:           "us-west-2",
			AccountID:        "123456789012",
			AvailabilityZone: "us-west-2a",
			State:            "running",
			Lifecycle:        "on-demand",
			LaunchTime:       baseTime,
		})
	}
	onDemandPrices["m5.xlarge:us-west-2"] = 0.192 // $0.192/hour shelf price

	// Create 5 Compute Savings Plans (matching production scenario)
	// Total commitment: $258/hour (similar to production)
	savingsPlans := []aws.SavingsPlan{
		{
			SavingsPlanARN:  "arn:aws:savingsplans::123456789012:savingsplan/sp-compute-1",
			SavingsPlanType: "Compute",
			Region:          "all", // Compute SPs apply to any region
			InstanceFamily:  "all", // Compute SPs apply to any family
			Commitment:      60.0,  // $60/hour
			AccountID:       "123456789012",
			Start:           baseTime.Add(-24 * time.Hour),
			End:             baseTime.Add(365 * 24 * time.Hour),
		},
		{
			SavingsPlanARN:  "arn:aws:savingsplans::123456789012:savingsplan/sp-compute-2",
			SavingsPlanType: "Compute",
			Region:          "all",
			InstanceFamily:  "all",
			Commitment:      57.0, // $57/hour
			AccountID:       "123456789012",
			Start:           baseTime.Add(-24 * time.Hour),
			End:             baseTime.Add(365 * 24 * time.Hour),
		},
		{
			SavingsPlanARN:  "arn:aws:savingsplans::123456789012:savingsplan/sp-compute-3",
			SavingsPlanType: "Compute",
			Region:          "all",
			InstanceFamily:  "all",
			Commitment:      96.0, // $96/hour
			AccountID:       "123456789012",
			Start:           baseTime.Add(-24 * time.Hour),
			End:             baseTime.Add(365 * 24 * time.Hour),
		},
		{
			SavingsPlanARN:  "arn:aws:savingsplans::123456789012:savingsplan/sp-compute-4",
			SavingsPlanType: "Compute",
			Region:          "all",
			InstanceFamily:  "all",
			Commitment:      26.0, // $26/hour
			AccountID:       "123456789012",
			Start:           baseTime.Add(-24 * time.Hour),
			End:             baseTime.Add(365 * 24 * time.Hour),
		},
		{
			SavingsPlanARN:  "arn:aws:savingsplans::123456789012:savingsplan/sp-compute-5",
			SavingsPlanType: "Compute",
			Region:          "all",
			InstanceFamily:  "all",
			Commitment:      19.0, // $19/hour
			AccountID:       "123456789012",
			Start:           baseTime.Add(-24 * time.Hour),
			End:             baseTime.Add(365 * 24 * time.Hour),
		},
	}
	// Total SP commitment: $258/hour

	input := CalculationInput{
		Instances:         instances,
		ReservedInstances: []aws.ReservedInstance{},
		SavingsPlans:      savingsPlans,
		SpotPrices:        make(map[string]float64),
		OnDemandPrices:    onDemandPrices,
	}

	result := calc.Calculate(input)

	// Calculate sum of EffectiveCost for SP-covered instances
	totalSPInstanceCost := 0.0
	for _, cost := range result.InstanceCosts {
		if cost.CoverageType == CoverageComputeSavingsPlan {
			totalSPInstanceCost += cost.EffectiveCost
		}
	}

	// Calculate sum of SP commitment consumed
	totalSPUtilization := 0.0
	for _, util := range result.SavingsPlanUtilization {
		totalSPUtilization += util.CurrentUtilizationRate
	}

	// CRITICAL: These must match!
	// If multiple SPs incorrectly applied to the same instance, totalSPInstanceCost
	// would be artificially low (instances pay less than they should), while
	// totalSPUtilization would be correct (each SP consumes its commitment).
	assert.InDelta(t, totalSPUtilization, totalSPInstanceCost, 0.01,
		"Sum of EffectiveCost for SP-covered instances must equal sum of SP commitment consumed")

	// Verify each instance is covered by at most one Compute SP
	for _, cost := range result.InstanceCosts {
		if cost.CoverageType == CoverageComputeSavingsPlan {
			// Instance should have exactly one SP ARN
			assert.NotEmpty(t, cost.SavingsPlanARN, "SP-covered instance should have SP ARN")

			// EffectiveCost should equal the SP rate (what you pay)
			// This is approximately 72% discount for Compute SPs (varies by instance type)
			assert.Greater(t, cost.EffectiveCost, 0.0, "Effective cost should be positive")
			assert.Less(t, cost.EffectiveCost, cost.ShelfPrice, "Effective cost should be less than shelf price")
		}
	}

	// Verify SP utilization metrics are sane
	for _, sp := range savingsPlans {
		util, exists := result.SavingsPlanUtilization[sp.SavingsPlanARN]
		assert.True(t, exists, "SP utilization should be tracked")
		assert.GreaterOrEqual(t, util.CurrentUtilizationRate, 0.0, "Utilization rate should be non-negative")
		assert.LessOrEqual(t, util.CurrentUtilizationRate, sp.Commitment,
			"Utilization rate should not exceed commitment (no over-utilization in this test)")
	}
}
