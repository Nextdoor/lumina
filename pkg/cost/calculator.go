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
	"time"
)

// Calculator orchestrates the cost calculation process, implementing AWS's
// Savings Plans allocation algorithm. It takes a point-in-time snapshot of
// all compute resources and discount instruments, then calculates per-instance
// costs and Savings Plans utilization.
//
// The calculator is stateless and can be run at any time. It operates on a
// rate-based model ($/hour) rather than cumulative tracking, making it safe
// to restart the controller at any time.
//
// Thread-safety: Calculator methods are safe to call concurrently as they
// don't modify shared state. Each Calculate() call operates on its input
// parameters and returns new result objects.
type Calculator struct {
	// No state needed - calculator is stateless
}

// NewCalculator creates a new cost calculator instance.
func NewCalculator() *Calculator {
	return &Calculator{}
}

// Calculate runs the full cost calculation algorithm on the provided input.
// This implements the AWS Savings Plans allocation algorithm.
//
// Algorithm steps:
//  1. Initialize all instances with shelf prices (on-demand rates)
//  2. Apply Reserved Instances (RIs) - exact instance type + AZ matches
//  3. Apply EC2 Instance Savings Plans - specific family + region
//  4. Apply Compute Savings Plans - any family, any region
//  5. Calculate remaining on-demand costs
//  6. Calculate Savings Plans utilization metrics
//  7. Calculate aggregate costs and savings
//
// The function returns a complete CalculationResult with per-instance costs
// and SP utilization metrics.
//
// Reference: AWS Savings Plans documentation
// https://docs.aws.amazon.com/savingsplans/latest/userguide/sp-applying.html
func (c *Calculator) Calculate(input CalculationInput) CalculationResult {
	// Initialize result structure
	result := CalculationResult{
		InstanceCosts:          make(map[string]InstanceCost),
		SavingsPlanUtilization: make(map[string]SavingsPlanUtilization),
		CalculatedAt:           time.Now(),
	}

	// Step 1: Initialize cost objects for all instances with shelf prices
	// Convert map to use pointers for efficient updates
	costsPtrs := make(map[string]*InstanceCost)
	c.initializeInstanceCosts(input, costsPtrs)

	// Step 2: Initialize SP utilization tracking
	spUtilPtrs := make(map[string]*SavingsPlanUtilization)
	c.initializeSPUtilization(input, spUtilPtrs)

	// Step 3: Apply Reserved Instances (highest priority)
	// RIs apply before any Savings Plans
	applyReservedInstances(input.Instances, input.ReservedInstances, costsPtrs)

	// Step 4: Apply Savings Plans
	// This handles both EC2 Instance SPs and Compute SPs in priority order
	applySavingsPlans(input.Instances, input.SavingsPlans, costsPtrs, spUtilPtrs)

	// Step 5: Apply spot pricing for spot instances
	// Spot instances use current market rates, not on-demand rates
	c.applySpotPricing(input, costsPtrs)

	// Step 6: Convert pointer maps back to value maps for result
	for id, costPtr := range costsPtrs {
		result.InstanceCosts[id] = *costPtr
	}
	for arn, utilPtr := range spUtilPtrs {
		result.SavingsPlanUtilization[arn] = *utilPtr
	}

	// Step 7: Calculate aggregate metrics
	c.calculateAggregates(&result)

	return result
}

// initializeInstanceCosts creates initial cost objects for all instances,
// setting their shelf prices (on-demand rates) and initial effective costs.
//
// After initialization:
//   - ShelfPrice = on-demand rate for the instance type
//   - EffectiveCost = on-demand rate (will be reduced by RIs/SPs)
//   - CoverageType = "on_demand" (may change to RI/SP coverage)
//   - All coverage amounts = 0 (will be set by RI/SP application)
func (c *Calculator) initializeInstanceCosts(input CalculationInput, costs map[string]*InstanceCost) {
	for _, inst := range input.Instances {
		// Look up on-demand price for this instance type + region
		priceKey := inst.InstanceType + ":" + inst.Region
		shelfPrice := input.OnDemandPrices[priceKey]

		if shelfPrice <= 0 {
			// If we don't have pricing data, skip this instance
			// In production, this should log a warning
			continue
		}

		// Initialize cost object with shelf price
		// EffectiveCost starts at shelf price and will be reduced by discounts
		costs[inst.InstanceID] = &InstanceCost{
			InstanceID:          inst.InstanceID,
			InstanceType:        inst.InstanceType,
			Region:              inst.Region,
			AccountID:           inst.AccountID,
			AvailabilityZone:    inst.AvailabilityZone,
			ShelfPrice:          shelfPrice,
			EffectiveCost:       shelfPrice,       // Will be reduced by RIs/SPs
			CoverageType:        CoverageOnDemand, // May change to RI/SP
			RICoverage:          0,
			SavingsPlanCoverage: 0,
			SavingsPlanARN:      "",
			OnDemandCost:        shelfPrice, // Will be reduced as coverage applied
			SpotPrice:           0,
			IsSpot:              inst.State == "running" && inst.Lifecycle == "spot",
		}
	}
}

// initializeSPUtilization creates initial utilization tracking objects for all
// Savings Plans. These will be updated as the algorithm applies SP coverage to instances.
func (c *Calculator) initializeSPUtilization(input CalculationInput, utilization map[string]*SavingsPlanUtilization) {
	for _, sp := range input.SavingsPlans {
		// Calculate hours remaining until SP expires
		remainingHours := 0.0
		if !sp.End.IsZero() {
			remainingHours = time.Until(sp.End).Hours()
			if remainingHours < 0 {
				remainingHours = 0 // SP already expired
			}
		}

		// Initialize utilization tracking
		utilization[sp.SavingsPlanARN] = &SavingsPlanUtilization{
			SavingsPlanARN:         sp.SavingsPlanARN,
			AccountID:              sp.AccountID,
			Type:                   sp.SavingsPlanType,
			Region:                 sp.Region,
			InstanceFamily:         sp.InstanceFamily,
			HourlyCommitment:       sp.Commitment,
			CurrentUtilizationRate: 0, // Will be set as instances get coverage
			RemainingCapacity:      sp.Commitment,
			UtilizationPercent:     0,
			RemainingHours:         remainingHours,
			EndTime:                sp.End,
		}
	}
}

// applySpotPricing adjusts costs for spot instances to use current spot market prices
// instead of on-demand rates. Spot instances have different pricing that fluctuates
// based on market demand.
//
// Important: Spot prices are the CURRENT market rate, not the launch price.
// Instances pay the current spot price, which can change while running.
func (c *Calculator) applySpotPricing(input CalculationInput, costs map[string]*InstanceCost) {
	for _, inst := range input.Instances {
		// Skip non-spot instances
		if inst.Lifecycle != "spot" {
			continue
		}

		cost, exists := costs[inst.InstanceID]
		if !exists {
			continue
		}

		// Look up current spot price for this instance type + AZ
		spotKey := inst.InstanceType + ":" + inst.AvailabilityZone
		spotPrice := input.SpotPrices[spotKey]

		if spotPrice <= 0 {
			// No spot price data available, keep on-demand estimate
			continue
		}

		// For spot instances, the effective cost is the spot price
		// (unless covered by RI or SP, which would have already set EffectiveCost)
		//
		// If instance is NOT covered by RI/SP, use spot price
		if cost.RICoverage == 0 && cost.SavingsPlanCoverage == 0 {
			cost.EffectiveCost = spotPrice
			cost.SpotPrice = spotPrice
			cost.CoverageType = CoverageSpot
			cost.OnDemandCost = 0 // Not paying on-demand rate
		}
		// Note: cost is already a pointer, no need to reassign
	}
}

// calculateAggregates computes organization-wide aggregate metrics from individual
// instance costs. This includes total costs, total savings, and validates that
// all numbers add up correctly.
func (c *Calculator) calculateAggregates(result *CalculationResult) {
	totalEstimatedCost := 0.0
	totalShelfPrice := 0.0

	for _, cost := range result.InstanceCosts {
		totalEstimatedCost += cost.EffectiveCost
		totalShelfPrice += cost.ShelfPrice
	}

	result.TotalEstimatedCost = totalEstimatedCost
	result.TotalShelfPrice = totalShelfPrice
	result.TotalSavings = totalShelfPrice - totalEstimatedCost
}
