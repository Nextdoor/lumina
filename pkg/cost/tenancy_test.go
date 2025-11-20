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

	"github.com/nextdoor/lumina/internal/cache"
	"github.com/nextdoor/lumina/pkg/aws"
	"github.com/stretchr/testify/assert"
)

// mockTenancyPricingCache implements PricingCacheReader for testing tenancy handling.
type mockTenancyPricingCache struct {
	// spRates maps "spArn:instanceType:region:tenancy:os" to rate
	spRates map[string]float64
}

// GetSPRate returns the SP rate for the given SP, instance type, region, tenancy, and OS.
func (m *mockTenancyPricingCache) GetSPRate(
	spArn, instanceType, region, tenancy, operatingSystem string,
) (float64, bool) {
	// Normalize OS to match behavior of real PricingCache
	// Empty string → "linux", "windows" → "windows"
	normalizedOS := operatingSystem
	if normalizedOS == "" {
		normalizedOS = cache.OSLinux
	}

	key := spArn + ":" + instanceType + ":" + region + ":" + tenancy + ":" + normalizedOS
	rate, exists := m.spRates[key]
	return rate, exists
}

// TestCalculatorTenancyHandling tests that the calculator correctly uses tenancy
// when looking up Savings Plan rates.
//
// This test verifies the fix for the production bug where 223 m7g.2xlarge instances
// were showing "estimated" pricing instead of "accurate" because we weren't filtering
// by tenancy when querying SP rates.
func TestCalculatorTenancyHandling(t *testing.T) {
	baseTime := testBaseTime()

	// Create a mock pricing cache with different rates for different tenancies
	mockCache := &mockTenancyPricingCache{
		spRates: map[string]float64{
			// Default (shared) tenancy rates - 28% discount
			"arn:aws:savingsplans::123456789012:savingsplan/sp-001:m5.xlarge:us-west-2:default:linux":  0.072,
			"arn:aws:savingsplans::123456789012:savingsplan/sp-001:m5.2xlarge:us-west-2:default:linux": 0.144,

			// Dedicated tenancy rates - higher rates (less discount) due to dedicated hardware
			"arn:aws:savingsplans::123456789012:savingsplan/sp-001:m5.xlarge:us-west-2:dedicated:linux":  0.1708,
			"arn:aws:savingsplans::123456789012:savingsplan/sp-001:m5.2xlarge:us-west-2:dedicated:linux": 0.3416,
		},
	}

	calc := NewCalculator(mockCache, nil)

	input := CalculationInput{
		Instances: []aws.Instance{
			{
				InstanceID:       "i-shared",
				InstanceType:     "m5.xlarge",
				Region:           "us-west-2",
				AccountID:        "123456789012",
				AvailabilityZone: "us-west-2a",
				State:            "running",
				Tenancy:          "default", // Shared tenancy
				LaunchTime:       baseTime.Add(1 * time.Hour),
			},
			{
				InstanceID:       "i-dedicated",
				InstanceType:     "m5.xlarge",
				Region:           "us-west-2",
				AccountID:        "123456789012",
				AvailabilityZone: "us-west-2b",
				State:            "running",
				Tenancy:          "dedicated", // Dedicated tenancy
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
				Commitment:      0.50, // Enough to cover both instances
				AccountID:       "123456789012",
			},
		},
		OnDemandPrices: map[string]float64{
			"m5.xlarge:us-west-2": 0.192, // Same on-demand price for both tenancies
		},
	}

	result := calc.Calculate(input)

	// Verify both instances exist
	assert.Len(t, result.InstanceCosts, 2)

	// Check shared tenancy instance - should get accurate pricing with lower rate
	sharedCost := result.InstanceCosts["i-shared"]
	assert.Equal(t, "i-shared", sharedCost.InstanceID)
	assert.Equal(t, 0.192, sharedCost.ShelfPrice)
	assert.InDelta(t, 0.072, sharedCost.EffectiveCost, 0.001) // Pays SP rate for shared tenancy
	assert.InDelta(t, 0.072, sharedCost.SavingsPlanCoverage, 0.001)
	assert.Equal(t, CoverageEC2InstanceSavingsPlan, sharedCost.CoverageType)
	assert.Equal(t, PricingAccurate, sharedCost.PricingAccuracy, "Should use accurate pricing from cache")

	// Check dedicated tenancy instance - should get accurate pricing with higher rate
	dedicatedCost := result.InstanceCosts["i-dedicated"]
	assert.Equal(t, "i-dedicated", dedicatedCost.InstanceID)
	assert.Equal(t, 0.192, dedicatedCost.ShelfPrice)
	assert.InDelta(t, 0.1708, dedicatedCost.EffectiveCost, 0.001) // Pays SP rate for dedicated tenancy
	assert.InDelta(t, 0.1708, dedicatedCost.SavingsPlanCoverage, 0.001)
	assert.Equal(t, CoverageEC2InstanceSavingsPlan, dedicatedCost.CoverageType)
	assert.Equal(t, PricingAccurate, dedicatedCost.PricingAccuracy, "Should use accurate pricing from cache")

	// Verify SP utilization is correctly calculated
	spUtil := result.SavingsPlanUtilization["arn:aws:savingsplans::123456789012:savingsplan/sp-001"]
	assert.Equal(t, 0.50, spUtil.HourlyCommitment)
	// Total used: $0.072 (shared) + $0.1708 (dedicated) = $0.2428
	assert.InDelta(t, 0.2428, spUtil.CurrentUtilizationRate, 0.001)
	assert.InDelta(t, 0.2572, spUtil.RemainingCapacity, 0.001) // $0.50 - $0.2428
	assert.InDelta(t, 48.56, spUtil.UtilizationPercent, 1.0)   // ~48.56%
}

// TestCalculatorTenancyMismatch tests that instances with the wrong tenancy
// don't get accurate pricing and fall back to estimated pricing.
func TestCalculatorTenancyMismatch(t *testing.T) {
	baseTime := testBaseTime()

	// Create a mock pricing cache with only default tenancy rates
	mockCache := &mockTenancyPricingCache{
		spRates: map[string]float64{
			// Only default tenancy rates available
			"arn:aws:savingsplans::123456789012:savingsplan/sp-001:m5.xlarge:us-west-2:default:linux": 0.072,
		},
	}

	calc := NewCalculator(mockCache, nil)

	input := CalculationInput{
		Instances: []aws.Instance{
			{
				InstanceID:       "i-dedicated",
				InstanceType:     "m5.xlarge",
				Region:           "us-west-2",
				AccountID:        "123456789012",
				AvailabilityZone: "us-west-2a",
				State:            "running",
				Tenancy:          "dedicated", // Dedicated tenancy, but no dedicated rate in cache
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
				Commitment:      0.50,
				AccountID:       "123456789012",
			},
		},
		OnDemandPrices: map[string]float64{
			"m5.xlarge:us-west-2": 0.192,
		},
	}

	result := calc.Calculate(input)

	// Verify instance exists
	assert.Len(t, result.InstanceCosts, 1)

	// Check dedicated tenancy instance - should fall back to estimated pricing
	dedicatedCost := result.InstanceCosts["i-dedicated"]
	assert.Equal(t, "i-dedicated", dedicatedCost.InstanceID)
	assert.Equal(t, 0.192, dedicatedCost.ShelfPrice)

	// Should use estimated pricing (28% discount = 0.72 multiplier)
	// $0.192 * 0.72 = $0.13824
	assert.InDelta(t, 0.13824, dedicatedCost.EffectiveCost, 0.001)
	assert.InDelta(t, 0.13824, dedicatedCost.SavingsPlanCoverage, 0.001)
	assert.Equal(t, CoverageEC2InstanceSavingsPlan, dedicatedCost.CoverageType)
	assert.Equal(t, PricingEstimated, dedicatedCost.PricingAccuracy,
		"Should use estimated pricing when accurate rate not found for tenancy")
}

// TestCalculatorTenancyWithComputeSP tests that Compute Savings Plans also
// correctly handle tenancy filtering.
func TestCalculatorTenancyWithComputeSP(t *testing.T) {
	baseTime := testBaseTime()

	// Create a mock pricing cache with rates for both EC2 Instance and Compute SPs
	mockCache := &mockTenancyPricingCache{
		spRates: map[string]float64{
			// Compute SP rates (different from EC2 Instance SP rates)
			"arn:aws:savingsplans::123456789012:savingsplan/sp-compute:m5.xlarge:us-west-2:default:linux":   0.08,
			"arn:aws:savingsplans::123456789012:savingsplan/sp-compute:m5.xlarge:us-west-2:dedicated:linux": 0.18,
		},
	}

	calc := NewCalculator(mockCache, nil)

	input := CalculationInput{
		Instances: []aws.Instance{
			{
				InstanceID:       "i-shared",
				InstanceType:     "m5.xlarge",
				Region:           "us-west-2",
				AccountID:        "123456789012",
				AvailabilityZone: "us-west-2a",
				State:            "running",
				Tenancy:          "default",
				LaunchTime:       baseTime.Add(1 * time.Hour),
			},
			{
				InstanceID:       "i-dedicated",
				InstanceType:     "m5.xlarge",
				Region:           "us-west-2",
				AccountID:        "123456789012",
				AvailabilityZone: "us-west-2b",
				State:            "running",
				Tenancy:          "dedicated",
				LaunchTime:       baseTime.Add(2 * time.Hour),
			},
		},
		ReservedInstances: []aws.ReservedInstance{},
		SavingsPlans: []aws.SavingsPlan{
			{
				SavingsPlanARN:  "arn:aws:savingsplans::123456789012:savingsplan/sp-compute",
				SavingsPlanType: "Compute",
				Region:          "all", // Compute SPs apply to all regions
				Commitment:      0.50,
				AccountID:       "123456789012",
			},
		},
		OnDemandPrices: map[string]float64{
			"m5.xlarge:us-west-2": 0.192,
		},
	}

	result := calc.Calculate(input)

	// Verify both instances exist
	assert.Len(t, result.InstanceCosts, 2)

	// Check shared tenancy instance
	sharedCost := result.InstanceCosts["i-shared"]
	assert.Equal(t, PricingAccurate, sharedCost.PricingAccuracy)
	assert.InDelta(t, 0.08, sharedCost.EffectiveCost, 0.001)
	assert.Equal(t, CoverageComputeSavingsPlan, sharedCost.CoverageType)

	// Check dedicated tenancy instance
	dedicatedCost := result.InstanceCosts["i-dedicated"]
	assert.Equal(t, PricingAccurate, dedicatedCost.PricingAccuracy)
	assert.InDelta(t, 0.18, dedicatedCost.EffectiveCost, 0.001)
	assert.Equal(t, CoverageComputeSavingsPlan, dedicatedCost.CoverageType)
}
