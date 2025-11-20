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

	"github.com/nextdoor/lumina/pkg/aws"
)

const (
	// lifecycleSpot is the EC2 instance lifecycle value for spot instances
	lifecycleSpot = "spot"
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
	// PricingCache provides access to per-SP rates from DescribeSavingsPlanRates API
	// Used for tier-1 lookup in getSavingsPlanRate()
	// Optional: if nil, only config-based fallback rates will be used
	PricingCache PricingCacheReader

	// Config provides fallback discount multipliers when API rates aren't available
	// Used for tier-2 lookup in getSavingsPlanRate()
	// Optional: if nil, conservative defaults (0.72 multiplier) will be used
	Config ConfigReader
}

// PricingCacheReader interface for reading Savings Plan rates from cache.
// This allows for easier testing with mocks.
type PricingCacheReader interface {
	GetSPRate(spArn, instanceType, region, tenancy, operatingSystem string) (float64, bool)
}

// ConfigReader interface for reading configuration values.
// This allows for easier testing with mocks.
type ConfigReader interface {
	GetEC2InstanceDiscount() float64
	GetComputeDiscount() float64
}

// NewCalculator creates a new cost calculator instance.
// Both pricingCache and config are optional (can be nil).
// If nil, getSavingsPlanRate will use conservative default multipliers.
func NewCalculator(pricingCache PricingCacheReader, config ConfigReader) *Calculator {
	return &Calculator{
		PricingCache: pricingCache,
		Config:       config,
	}
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
	applySavingsPlans(c, input.Instances, input.SavingsPlans, costsPtrs, spUtilPtrs)

	// Step 4.5: Validate Savings Plans math invariants
	// This runtime check ensures the algorithm calculated costs correctly
	c.validateSavingsPlansInvariants(input.SavingsPlans, costsPtrs, spUtilPtrs)

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
			PricingAccuracy:     PricingAccurate,  // On-demand pricing from AWS Pricing API is accurate
			RICoverage:          0,
			SavingsPlanCoverage: 0,
			SavingsPlanARN:      "",
			OnDemandCost:        shelfPrice, // Will be reduced as coverage applied
			SpotPrice:           0,
			IsSpot:              inst.State == "running" && inst.Lifecycle == lifecycleSpot,
			Lifecycle:           inst.Lifecycle, // Capture lifecycle for metrics
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
		if inst.Lifecycle != lifecycleSpot {
			continue
		}

		cost, exists := costs[inst.InstanceID]
		if !exists {
			continue
		}

		// Look up current spot price for this instance type + AZ
		// Use the cache accessor method to avoid key format dependencies
		// Default to Linux/UNIX for product description (most common)
		spotPrice, _ := input.PricingCache.GetSpotPrice(inst.InstanceType, inst.AvailabilityZone, "Linux/UNIX")

		// For spot instances, ALWAYS use the spot price as the effective cost.
		// Spot instances cannot use Reserved Instances or Savings Plans per AWS billing rules.
		//
		// If spot price data is available, use it. Otherwise, mark as $0 until spot pricing
		// is implemented (tracked in Phase 8).
		//
		// Reset any RI/SP coverage that may have been incorrectly applied (defense-in-depth).
		// The eligibility filters in applyReservedInstances() and applySavingsPlans()
		// should prevent spot instances from getting coverage, but we reset here to be safe.
		cost.EffectiveCost = spotPrice
		cost.SpotPrice = spotPrice
		cost.CoverageType = CoverageSpot
		cost.OnDemandCost = 0        // Not paying on-demand rate
		cost.RICoverage = 0          // Reset any incorrectly applied RI coverage
		cost.SavingsPlanCoverage = 0 // Reset any incorrectly applied SP coverage

		// Set pricing accuracy based on whether we have actual spot pricing data
		if spotPrice > 0 {
			cost.PricingAccuracy = PricingAccurate // Actual spot price from AWS Spot Pricing API
		} else {
			cost.PricingAccuracy = PricingEstimated // Using fallback (currently $0, will be on-demand estimate in future)
		}
		cost.IsSpot = true // Mark as spot instance
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

// validateSavingsPlansInvariants performs runtime validation of critical math invariants
// in the Savings Plans allocation algorithm. This catches calculation bugs before they
// reach metrics/dashboards.
//
// Invariants validated:
//
//  1. SP Commitment Balance:
//     For each Savings Plan: commitment = currentUtilization + remainingCapacity
//     This ensures we didn't over-allocate or under-allocate SP budget.
//
//  2. Non-Negative Costs:
//     All instance costs must be >= 0. Negative costs indicate a bug in the
//     coverage limiting logic.
//
//  3. Coverage Bounds:
//     Total coverage (RI + SP) must not exceed shelf price for any instance.
//
// This function panics if any invariant is violated, since that indicates a critical
// bug in the cost calculation logic that should never happen in production.
//
// Design rationale:
//   - Fail fast: Better to panic than emit bad metrics to production dashboards
//   - Defensive programming: Validates assumptions that "should always be true"
//   - Debugging aid: Makes it obvious when algorithm changes break invariants
//   - No performance impact: Only runs once per reconciliation loop (~1 minute)
func (c *Calculator) validateSavingsPlansInvariants(
	savingsPlans []aws.SavingsPlan,
	costs map[string]*InstanceCost,
	utilization map[string]*SavingsPlanUtilization,
) {
	const epsilon = 1e-6 // Tolerance for floating-point comparison

	// INVARIANT 1: Validate SP commitment balance for each Savings Plan
	//
	// The math MUST always work out to:
	//   SP Commitment = Current Utilization Rate + Remaining Capacity
	//
	// Where:
	//   - Commitment: Fixed $/hour budget (e.g., $150/hour)
	//   - Current Utilization Rate: Sum of coverage amounts we applied to instances
	//   - Remaining Capacity: Unused SP budget this hour
	//
	// If this doesn't balance, we either:
	//   - Over-allocated coverage (utilization > commitment) → instances got too much discount
	//   - Under-counted utilization (utilization + remaining < commitment) → lost SP budget
	//
	// Both scenarios indicate a critical bug in the allocation algorithm.
	for _, sp := range savingsPlans {
		util, exists := utilization[sp.SavingsPlanARN]
		if !exists {
			// SP wasn't tracked - should never happen, but don't panic in production
			continue
		}

		// Calculate what the commitment balance should be
		calculatedCommitment := util.CurrentUtilizationRate + util.RemainingCapacity

		// Verify it matches the SP's actual commitment
		diff := calculatedCommitment - sp.Commitment
		if diff > epsilon || diff < -epsilon {
			// CRITICAL BUG: SP math doesn't add up!
			//
			// This should NEVER happen. If it does, it means:
			// 1. We over-allocated coverage (applied more discount than SP commitment allows), OR
			// 2. We lost track of remaining capacity, OR
			// 3. The coverage amounts we applied don't match what we recorded in utilization
			//
			// Example failure:
			//   SP Commitment: $150/hour
			//   Current Utilization: $152/hour (bug: over-allocated!)
			//   Remaining Capacity: $0
			//   calculatedCommitment = $152 + $0 = $152 ≠ $150
			panic(invariantViolation{
				description: "Savings Plan commitment balance violation",
				spARN:       sp.SavingsPlanARN,
				expected:    sp.Commitment,
				actual:      calculatedCommitment,
				details: map[string]interface{}{
					"utilization": util.CurrentUtilizationRate,
					"remaining":   util.RemainingCapacity,
				},
			})
		}
	}

	// INVARIANT 2: Validate non-negative costs
	//
	// No instance can have negative effective cost. This was the bug we fixed:
	// instances with RI coverage going negative when Compute SP tried to apply.
	//
	// If this happens, it means the coverage limiting logic failed.
	for instanceID, cost := range costs {
		if cost.EffectiveCost < -epsilon {
			// CRITICAL BUG: Instance has negative cost!
			//
			// This indicates the coverage limiting logic didn't work correctly.
			// The bug scenario:
			//   1. Instance covered by RI: EffectiveCost = $0
			//   2. SP tries to apply $0.13 coverage
			//   3. Coverage limiting fails: EffectiveCost = $0 - $0.13 = -$0.13
			//
			// This should be impossible after our fix, but if it happens, we want
			// to know immediately (hence the panic).
			panic(invariantViolation{
				description: "Instance has negative effective cost",
				spARN:       cost.SavingsPlanARN,
				expected:    0.0,
				actual:      cost.EffectiveCost,
				details: map[string]interface{}{
					"instance_id":           instanceID,
					"instance_type":         cost.InstanceType,
					"shelf_price":           cost.ShelfPrice,
					"ri_coverage":           cost.RICoverage,
					"savings_plan_coverage": cost.SavingsPlanCoverage,
					"coverage_type":         cost.CoverageType,
				},
			})
		}
	}

	// INVARIANT 3: Validate coverage bounds
	//
	// Total coverage (RI + SP) must not exceed shelf price for any instance.
	// If it does, we're applying more discount than the instance costs!
	//
	// Example violation:
	//   Shelf price: $0.192/hour
	//   RI coverage: $0.100/hour
	//   SP coverage: $0.150/hour
	//   Total: $0.250/hour > $0.192/hour ← BUG!
	for instanceID, cost := range costs {
		totalCoverage := cost.RICoverage + cost.SavingsPlanCoverage

		// Allow small floating-point error
		if totalCoverage > cost.ShelfPrice+epsilon {
			// CRITICAL BUG: Over-allocated coverage!
			//
			// This means we applied more discount than the instance actually costs.
			// This should be caught by the coverage limiting logic, but if it happens,
			// it indicates a bug in either:
			//   - RI allocation (applyReservedInstances)
			//   - SP allocation (applySavingsPlans)
			//   - Coverage limiting (the fix we added)
			panic(invariantViolation{
				description: "Total coverage exceeds shelf price",
				spARN:       cost.SavingsPlanARN,
				expected:    cost.ShelfPrice,
				actual:      totalCoverage,
				details: map[string]interface{}{
					"instance_id":           instanceID,
					"instance_type":         cost.InstanceType,
					"shelf_price":           cost.ShelfPrice,
					"ri_coverage":           cost.RICoverage,
					"savings_plan_coverage": cost.SavingsPlanCoverage,
					"effective_cost":        cost.EffectiveCost,
				},
			})
		}
	}
}

// invariantViolation represents a critical math invariant violation in the cost
// calculation algorithm. This should NEVER happen in production.
//
// When this occurs, it indicates a bug in the algorithm logic (not bad input data).
// The panic includes detailed context to help debug what went wrong.
type invariantViolation struct {
	description string                 // Human-readable description of what failed
	spARN       string                 // Savings Plan ARN (if relevant)
	expected    float64                // Expected value
	actual      float64                // Actual value
	details     map[string]interface{} // Additional debugging context
}

// Error implements the error interface for invariantViolation.
func (v invariantViolation) Error() string {
	return v.description
}
