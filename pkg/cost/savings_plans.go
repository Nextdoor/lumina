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
	"sort"
	"strings"

	"github.com/nextdoor/lumina/pkg/aws"
)

// applySavingsPlans applies Savings Plans to EC2 instances that aren't already
// covered by Reserved Instances. This follows AWS's documented allocation algorithm.
//
// AWS applies Savings Plans in priority order:
//  1. EC2 Instance Savings Plans (specific instance family + region)
//  2. Compute Savings Plans (any instance family, any region)
//
// Within each Savings Plan, AWS applies coverage to instances in order of:
//  1. Highest savings percentage first (maximize cost reduction)
//  2. Tie-breaker: Lowest Savings Plans rate (prefer cheaper SP rates)
//  3. Continue until SP hourly commitment is exhausted
//
// The function operates in a rate-based model:
//   - Each SP has a fixed $/hour commitment (e.g., $150/hour)
//   - We calculate instantaneous utilization based on currently running instances
//   - Remaining capacity = commitment - current utilization rate
//   - This is stateless (controller restart safe) but may not match AWS's cumulative
//     billing within an hour if instances scale up/down
//
// Reference: AWS Savings Plans documentation
// https://docs.aws.amazon.com/savingsplans/latest/userguide/sp-applying.html
func applySavingsPlans(
	calc *Calculator,
	instances []aws.Instance,
	savingsPlans []aws.SavingsPlan,
	costs map[string]*InstanceCost,
	utilization map[string]*SavingsPlanUtilization,
) {
	// Separate EC2 Instance SPs from Compute SPs
	// EC2 Instance SPs apply first (more specific, higher priority)
	var ec2InstanceSPs []aws.SavingsPlan
	var computeSPs []aws.SavingsPlan

	for _, sp := range savingsPlans {
		switch sp.SavingsPlanType {
		case "EC2Instance":
			ec2InstanceSPs = append(ec2InstanceSPs, sp)
		case "Compute":
			computeSPs = append(computeSPs, sp)
		}
	}

	// Step 2: Apply EC2 Instance Savings Plans
	// These apply to specific instance family + region combinations
	for _, sp := range ec2InstanceSPs {
		applyEC2InstanceSavingsPlan(calc, &sp, instances, costs, utilization)
	}

	// Step 3: Apply Compute Savings Plans
	// These apply to any instance family, any region (broader coverage)
	for _, sp := range computeSPs {
		applyComputeSavingsPlan(calc, &sp, instances, costs, utilization)
	}
}

// applyEC2InstanceSavingsPlan applies a single EC2 Instance Savings Plan to
// eligible instances.
//
// EC2 Instance SPs match based on:
//   - Instance Family (e.g., "m5" matches "m5.large", "m5.xlarge", "m5.2xlarge")
//   - Region (e.g., "us-west-2")
//   - Instance must NOT already be RI-covered
//
// Algorithm:
//  1. Filter instances: matching family + region, not RI-covered
//  2. Calculate savings percentage for each instance
//  3. Sort by savings % (descending), then by SP rate (ascending)
//  4. Apply SP coverage until commitment exhausted
func applyEC2InstanceSavingsPlan(
	calc *Calculator,
	sp *aws.SavingsPlan,
	instances []aws.Instance,
	costs map[string]*InstanceCost,
	utilization map[string]*SavingsPlanUtilization,
) {
	// STEP 1: Find all instances eligible for this specific EC2 Instance Savings Plan
	//
	// An instance is eligible if:
	// 1. It's not already covered by a Reserved Instance (RIs take priority)
	// 2. It matches the SP's instance family (e.g., SP for "m5" can cover m5.large, m5.xlarge, etc.)
	// 3. It's in the same region as the SP (e.g., SP for "us-west-2" only covers us-west-2 instances)
	//
	// We build a list of eligible instances with their savings calculations so we can
	// prioritize which instances get coverage first (step 2).
	eligible := make([]instanceWithSavings, 0, len(instances))
	for idx := range instances {
		inst := &instances[idx]
		cost, exists := costs[inst.InstanceID]
		if !exists {
			// Instance not in costs map (pricing data missing or cache race)
			continue
		}

		// Skip spot instances - Savings Plans don't apply to spot per AWS docs
		// Spot instances always pay the spot market rate, they cannot use RIs or SPs
		// Reference: https://docs.aws.amazon.com/savingsplans/latest/userguide/sp-applying.html
		if inst.Lifecycle == lifecycleSpot {
			continue
		}

		// Skip if already RI-covered
		// Reserved Instances have already been applied (higher priority than SPs).
		// If an instance is RI-covered, it won't benefit from SP coverage, so skip it.
		if cost.RICoverage > 0 {
			continue
		}

		// Skip if already covered by ANY Savings Plan
		//
		// SIMPLIFIED MODEL: Once an instance is covered by any Savings Plan (EC2 Instance
		// or Compute), no additional Savings Plans should apply. This prevents:
		// 1. SP commitment waste (paying twice for the same instance hour)
		// 2. EffectiveCost calculation errors
		// 3. Mismatch between sum(instance costs) and sum(SP utilization)
		//
		// This is a conservative model that prioritizes correctness over maximizing
		// SP utilization in edge cases (e.g., partial coverage fill-in).
		if cost.SavingsPlanCoverage > 0 {
			continue
		}

		// Check if instance matches SP criteria (family + region)
		// For example, an EC2 Instance SP for "m5" family in "us-west-2" will only match
		// m5.* instances (m5.large, m5.xlarge, etc.) running in us-west-2.
		if !matchesEC2InstanceSP(inst, sp) {
			continue
		}

		// Calculate the on-demand rate and SP rate for this instance type
		//
		// On-demand rate: The full price AWS charges without any discounts ($/hour)
		// SP rate: The discounted price when using this Savings Plan ($/hour)
		//
		// Example with 28% discount (1-year commitment):
		//   - m5.xlarge on-demand rate: $0.192/hour
		//   - m5.xlarge with SP: $0.192 * 0.72 = $0.138/hour (28% discount)
		odRate := cost.ShelfPrice
		spRate, isAccurate := getSavingsPlanRate(
			calc, sp, inst.InstanceType, inst.Region, inst.Tenancy, inst.Platform, odRate,
		)

		if odRate <= 0 || spRate <= 0 {
			continue // Can't calculate savings - skip this instance
		}

		// Calculate savings percentage: how much discount this SP provides
		// Formula: (on-demand rate - SP rate) / on-demand rate
		//
		// Example:
		//   - on-demand: $0.192/hour, SP rate: $0.054/hour
		//   - savings: ($0.192 - $0.054) / $0.192 = 0.72 = 72% discount
		//
		// We use this percentage to prioritize which instances get SP coverage first.
		// AWS applies SPs to instances with the highest savings percentage first to
		// maximize cost reduction across the organization.
		savingsPct := (odRate - spRate) / odRate

		eligible = append(eligible, instanceWithSavings{
			Instance:       inst,
			SavingsPercent: savingsPct,
			SPRate:         spRate,
			ODRate:         odRate,
			IsAccurate:     isAccurate,
		})
	}

	// STEP 2: Sort eligible instances to prioritize which ones get coverage first
	//
	// AWS's allocation algorithm prioritizes instances to maximize cost savings across
	// the organization. The sorting order is:
	//
	// 1. PRIMARY: Highest savings percentage first
	//    - This ensures we apply the SP to instances where it provides the most value
	//    - Example: If instance A saves 72% and instance B saves 60%, cover A first
	//
	// 2. TIE-BREAKER: Lowest SP rate first (when savings percentages are equal)
	//    - This is a secondary optimization to prefer covering cheaper instances
	//    - Helps the SP commitment stretch to cover more instances
	//
	// 3. STABILITY TIE-BREAKER: Oldest instances first (by launch time)
	//    - When instances have identical savings % and SP rates, prioritize older instances
	//    - This provides stable, predictable discount assignment across reconciliation loops
	//    - Prevents discounts from "jumping" between instances when they come and go
	//    - Older instances keep their discounts; new instances only get coverage if capacity remains
	//
	// 4. FINAL TIE-BREAKER: Instance ID (for complete determinism)
	//    - Handles edge case where instances launched at exactly the same time
	//
	// Example scenario:
	//   - Instance A: m5.2xlarge, on-demand $0.384/hr, SP rate $0.107/hr (72% savings)
	//   - Instance B: m5.xlarge, on-demand $0.192/hr, SP rate $0.054/hr (72% savings)
	//   - Instance C: m5.large, on-demand $0.096/hr, SP rate $0.058/hr (60% savings)
	//
	// Sorted order: A, B, C
	//   - A and B both have 72% savings (higher than C's 60%), so they come first
	//   - Between A and B (tie at 72%), B has lower SP rate ($0.054 < $0.107), so B comes first
	//   - Final order: B ($0.054, 72%), A ($0.107, 72%), C ($0.058, 60%)
	sort.Slice(eligible, func(i, j int) bool {
		// Use epsilon comparison for floating-point values to handle precision issues
		const epsilon = 1e-9

		// Primary sort: highest savings percentage first
		savingsDiff := eligible[i].SavingsPercent - eligible[j].SavingsPercent
		if savingsDiff > epsilon {
			return true // i has higher savings
		}
		if savingsDiff < -epsilon {
			return false // j has higher savings
		}

		// Tie-breaker: lowest SP rate first
		rateDiff := eligible[i].SPRate - eligible[j].SPRate
		if rateDiff < -epsilon {
			return true // i has lower rate
		}
		if rateDiff > epsilon {
			return false // j has lower rate
		}

		// Stability tie-breaker: older instances first (by launch time)
		if !eligible[i].Instance.LaunchTime.Equal(eligible[j].Instance.LaunchTime) {
			return eligible[i].Instance.LaunchTime.Before(eligible[j].Instance.LaunchTime)
		}

		// Final tie-breaker: instance ID (for deterministic sort)
		return eligible[i].Instance.InstanceID < eligible[j].Instance.InstanceID
	})

	// STEP 3: Apply SP coverage to instances in priority order until commitment exhausted
	//
	// Each Savings Plan has a fixed hourly commitment (e.g., $150/hour). This is what you
	// SPEND per hour on SP-covered instances. We consume this commitment budget by
	// applying the SP to instances in the sorted order from Step 2.
	//
	// The SP pays the SP rate for each covered instance. This is what gets subtracted from
	// the commitment budget.
	//
	// Example with $0.20/hour commitment:
	//   - Instance 1: on-demand $0.192/hr, SP rate $0.054/hr → SP pays $0.054/hr
	//   - Instance 2: on-demand $0.096/hr, SP rate $0.027/hr → SP pays $0.027/hr
	//
	// After covering instance 1: remaining commitment = $0.20 - $0.054 = $0.146/hr
	// After covering instance 2: remaining commitment = $0.146 - $0.027 = $0.119/hr
	//
	// Result:
	//   - Instance 1: fully covered by SP, pays $0.054/hr (the SP rate)
	//   - Instance 2: fully covered by SP, pays $0.027/hr (the SP rate)
	//   - Commitment used: $0.054 + $0.027 = $0.081/hr out of $0.20/hr available
	remainingCommitment := sp.Commitment

	for _, item := range eligible {
		if remainingCommitment <= 0 {
			break // SP commitment fully utilized, no more coverage available
		}

		inst := item.Instance
		cost := costs[inst.InstanceID]

		// Calculate how much the SP will pay for this instance
		//
		// The SP commitment represents what you SPEND per hour on SP-covered instances.
		// The SP rate is the discounted rate you pay when using the SP.
		//
		// Example:
		//   - on-demand rate: $0.192/hour
		//   - SP rate: $0.054/hour (with 72% discount)
		//   - SP will pay: $0.054/hour from the commitment
		//
		// This $0.054/hour is consumed from the SP's hourly commitment budget.
		spCost := item.SPRate

		// FULL vs PARTIAL COVERAGE
		//
		// Full coverage: SP has enough commitment to pay the full SP rate
		// Partial coverage: SP runs out of commitment, can only contribute what's left
		//
		// Example of partial coverage:
		//   - SP rate: $0.068/hr (what the instance would cost with full SP)
		//   - Remaining commitment: $0.028/hr (not enough!)
		//   - SP contributes: $0.028/hr (partial)
		//   - Instance pays: $0.192 - $0.028 = $0.164/hr (on-demand spillover)
		//
		// The instance gets partial benefit: pays less than on-demand but more than SP rate.
		spContribution := spCost
		if spContribution > remainingCommitment {
			// Partial coverage: SP can only contribute what's left in the commitment
			spContribution = remainingCommitment
		}

		// CRITICAL: Limit SP contribution to not exceed remaining effective cost
		// This prevents negative costs when an instance is already partially covered
		// by Reserved Instances. The SP contribution cannot reduce EffectiveCost below zero.
		if spContribution > cost.EffectiveCost {
			spContribution = cost.EffectiveCost
		}

		// Apply SP contribution to this instance
		//
		// SavingsPlanCoverage tracks the SP COMMITMENT consumed (what the SP pays),
		// which is spContribution. This is NOT the discount amount!
		//
		// For fully covered: spContribution = SP rate = $0.34
		// For partially covered: spContribution = remaining commitment = e.g. $0.12
		//
		// SavingsPlanARN links this instance to the specific SP providing coverage
		//
		// For EffectiveCost:
		//   - If fully covered (spContribution == spCost): you pay the SP rate
		//   - If partially covered: SP pays what it can, you pay the rest at on-demand
		cost.SavingsPlanARN = sp.SavingsPlanARN
		cost.SavingsPlanCoverage += spContribution

		if spContribution == spCost {
			// Fully covered: you pay the SP rate
			cost.EffectiveCost = spCost
		} else {
			// Partially covered: SP contributes, you pay the rest
			cost.EffectiveCost -= spContribution
		}

		// OnDemandCost should remain at shelf price, not be modified by SP coverage
		// (This field tracks what the instance would cost without any discounts)

		if cost.SavingsPlanCoverage > 0 {
			cost.CoverageType = CoverageEC2InstanceSavingsPlan
			// Set pricing accuracy based on whether we used actual API rates or fallback estimates
			if item.IsAccurate {
				cost.PricingAccuracy = PricingAccurate // Actual SP rate from DescribeSavingsPlanRates API
			} else {
				cost.PricingAccuracy = PricingEstimated // Estimated using configured discount multiplier
			}
		}

		// Consume SP commitment
		// This reduces the available budget for covering additional instances
		remainingCommitment -= spContribution
	}

	// STEP 4: Track SP utilization metrics for monitoring and alerting
	//
	// These metrics help answer questions like:
	// - "How much of my SP commitment am I using right now?"
	// - "Do I have unused SP capacity that I'm wasting?"
	// - "Am I over-utilizing my SP (causing spillover to on-demand)?"
	//
	// Metrics calculated:
	// - CurrentUtilizationRate: How much of the SP commitment is being used ($/hour)
	// - RemainingCapacity: How much unused commitment is left ($/hour)
	// - UtilizationPercent: Utilization as a percentage (0-100%, can exceed 100%)
	//
	// Example with $0.20/hour commitment:
	//   - Used $0.138/hr covering instances
	//   - CurrentUtilizationRate = $0.138/hr
	//   - RemainingCapacity = $0.062/hr
	//   - UtilizationPercent = 69%
	//
	// These are rate-based metrics (instantaneous snapshot), not cumulative over time.
	// They represent "if these instances keep running for the rest of the hour, this is
	// how much of the SP commitment will be used."
	util := utilization[sp.SavingsPlanARN]
	util.CurrentUtilizationRate = sp.Commitment - remainingCommitment
	util.RemainingCapacity = remainingCommitment
	if sp.Commitment > 0 {
		util.UtilizationPercent = (util.CurrentUtilizationRate / sp.Commitment) * 100
	}
}

// applyComputeSavingsPlan applies a single Compute Savings Plan to eligible instances.
//
// Compute SPs are more flexible than EC2 Instance SPs:
//   - Match ANY instance family (m5, c5, r5, t3, etc.) - not restricted to one family
//   - Match ANY region (us-west-2, us-east-1, eu-west-1, etc.) - apply globally
//   - Instance must NOT already be RI-covered or fully EC2 Instance SP-covered
//
// Priority: Compute SPs apply AFTER EC2 Instance SPs (lower priority)
// This means Compute SPs can only cover instances that:
// 1. Don't have Reserved Instance coverage, AND
// 2. Don't have full EC2 Instance Savings Plan coverage
//
// Algorithm is identical to EC2 Instance SPs (steps 1-4), but with broader eligibility.
func applyComputeSavingsPlan(
	calc *Calculator,
	sp *aws.SavingsPlan,
	instances []aws.Instance,
	costs map[string]*InstanceCost,
	utilization map[string]*SavingsPlanUtilization,
) {
	// STEP 1: Find all instances eligible for this Compute Savings Plan
	//
	// Compute SPs have broader eligibility than EC2 Instance SPs:
	// - No instance family restriction (can cover m5, c5, r5, anything)
	// - No region restriction (can cover any region)
	//
	// However, instances are only eligible if they're not already fully covered:
	// - Must not be RI-covered (RIs have highest priority)
	// - Must not be fully covered by an EC2 Instance SP (those apply first)
	//
	// The second check is important: if an EC2 Instance SP already fully covers
	// an instance's cost, there's no remaining cost for a Compute SP to discount.
	eligible := make([]instanceWithSavings, 0, len(instances))
	for idx := range instances {
		inst := &instances[idx]
		cost, exists := costs[inst.InstanceID]
		if !exists {
			// Instance not in costs map (pricing data missing or cache race)
			continue
		}

		// Skip spot instances - Savings Plans don't apply to spot per AWS docs
		// Spot instances always pay the spot market rate, they cannot use RIs or SPs
		// Reference: https://docs.aws.amazon.com/savingsplans/latest/userguide/sp-applying.html
		if inst.Lifecycle == lifecycleSpot {
			continue
		}

		// Skip if already RI-covered
		// Reserved Instances have already been applied (highest priority).
		if cost.RICoverage > 0 {
			continue
		}

		// Skip if already covered by ANY Savings Plan
		//
		// SIMPLIFIED MODEL: Once an instance is covered by any Savings Plan (EC2 Instance
		// or Compute), no additional Savings Plans should apply. This prevents:
		// 1. SP commitment waste (paying twice for the same instance hour)
		// 2. EffectiveCost calculation errors
		// 3. Mismatch between sum(instance costs) and sum(SP utilization)
		//
		// This is a conservative model that prioritizes correctness over maximizing
		// SP utilization in edge cases (e.g., partial coverage fill-in).
		if cost.SavingsPlanCoverage > 0 {
			continue
		}

		// Calculate savings percentage and SP rate for this instance
		// (Same logic as EC2 Instance SPs - see detailed comments there)
		odRate := cost.ShelfPrice
		spRate, isAccurate := getSavingsPlanRate(
			calc, sp, inst.InstanceType, inst.Region, inst.Tenancy, inst.Platform, odRate,
		)

		if odRate <= 0 || spRate <= 0 {
			continue
		}

		savingsPct := (odRate - spRate) / odRate

		eligible = append(eligible, instanceWithSavings{
			Instance:       inst,
			SavingsPercent: savingsPct,
			SPRate:         spRate,
			ODRate:         odRate,
			IsAccurate:     isAccurate,
		})
	}

	// STEP 2: Sort eligible instances by savings priority
	//
	// Uses the same prioritization algorithm as EC2 Instance SPs:
	// 1. Highest savings percentage first
	// 2. Tie-breaker: lowest SP rate first
	// 3. Stability tie-breaker: oldest instances first (by launch time)
	// 4. Final tie-breaker: instance ID (for deterministic sort)
	//
	// See detailed comments in applyEC2InstanceSavingsPlan() for the rationale.
	sort.Slice(eligible, func(i, j int) bool {
		// Use epsilon comparison for floating-point values to handle precision issues
		const epsilon = 1e-9

		// Primary sort: highest savings percentage first
		savingsDiff := eligible[i].SavingsPercent - eligible[j].SavingsPercent
		if savingsDiff > epsilon {
			return true // i has higher savings
		}
		if savingsDiff < -epsilon {
			return false // j has higher savings
		}

		// Tie-breaker: lowest SP rate first
		rateDiff := eligible[i].SPRate - eligible[j].SPRate
		if rateDiff < -epsilon {
			return true // i has lower rate
		}
		if rateDiff > epsilon {
			return false // j has lower rate
		}

		// Stability tie-breaker: older instances first (by launch time)
		if !eligible[i].Instance.LaunchTime.Equal(eligible[j].Instance.LaunchTime) {
			return eligible[i].Instance.LaunchTime.Before(eligible[j].Instance.LaunchTime)
		}

		// Final tie-breaker: instance ID (for deterministic sort)
		return eligible[i].Instance.InstanceID < eligible[j].Instance.InstanceID
	})

	// STEP 3: Apply SP coverage in priority order until commitment exhausted
	//
	// This is identical to the EC2 Instance SP algorithm. The key difference is
	// that Compute SPs can cover ANY instance type (not restricted by family/region),
	// so they typically cover a wider variety of instances.
	//
	// See detailed comments in applyEC2InstanceSavingsPlan() for how coverage works,
	// including partial coverage scenarios and commitment consumption.
	remainingCommitment := sp.Commitment

	for _, item := range eligible {
		if remainingCommitment <= 0 {
			break
		}

		inst := item.Instance
		cost := costs[inst.InstanceID]

		// Calculate how much the SP will pay for this instance
		// (Same logic as EC2 Instance SPs - see detailed comments there)
		spCost := item.SPRate

		// Handle full vs partial coverage
		spContribution := spCost
		if spContribution > remainingCommitment {
			// Partial coverage: SP can only contribute what's left in the commitment
			spContribution = remainingCommitment
		}

		// CRITICAL: Limit SP contribution to not exceed remaining effective cost
		// This prevents negative costs when an instance is already partially covered
		// by RIs or EC2 Instance SPs. The SP contribution cannot reduce EffectiveCost below zero.
		if spContribution > cost.EffectiveCost {
			spContribution = cost.EffectiveCost
		}

		// Apply SP contribution (same logic as EC2 Instance SPs)
		//
		// SavingsPlanCoverage tracks the SP COMMITMENT consumed (what the SP pays),
		// which is spContribution. This is NOT the discount amount!
		cost.SavingsPlanARN = sp.SavingsPlanARN
		cost.SavingsPlanCoverage += spContribution

		if spContribution == spCost {
			// Fully covered: you pay the SP rate
			cost.EffectiveCost = spCost
		} else {
			// Partially covered: SP contributes, you pay the rest
			cost.EffectiveCost -= spContribution
		}

		// OnDemandCost should remain at shelf price, not be modified by SP coverage
		// (This field tracks what the instance would cost without any discounts)

		// Set coverage type to Compute SP ONLY if this is the instance's first SP coverage
		// If an EC2 Instance SP already partially covered this instance, we want to keep
		// the CoverageType as EC2InstanceSavingsPlan (the more specific/higher priority type)
		if cost.SavingsPlanCoverage > 0 && cost.CoverageType == CoverageOnDemand {
			cost.CoverageType = CoverageComputeSavingsPlan
			// Set pricing accuracy based on whether we used actual API rates or fallback estimates
			if item.IsAccurate {
				cost.PricingAccuracy = PricingAccurate // Actual SP rate from DescribeSavingsPlanRates API
			} else {
				cost.PricingAccuracy = PricingEstimated // Estimated using configured discount multiplier
			}
		}

		// Consume SP commitment
		remainingCommitment -= spContribution
	}

	// STEP 4: Track SP utilization metrics
	//
	// Same metrics as EC2 Instance SPs. See detailed comments in
	// applyEC2InstanceSavingsPlan() for what these metrics mean and how to use them.
	util := utilization[sp.SavingsPlanARN]
	util.CurrentUtilizationRate = sp.Commitment - remainingCommitment
	util.RemainingCapacity = remainingCommitment
	if sp.Commitment > 0 {
		util.UtilizationPercent = (util.CurrentUtilizationRate / sp.Commitment) * 100
	}
}

// matchesEC2InstanceSP checks if an instance is eligible for an EC2 Instance Savings Plan.
// Returns true if the instance's family and region match the SP.
func matchesEC2InstanceSP(instance *aws.Instance, sp *aws.SavingsPlan) bool {
	// Extract instance family from instance type (e.g., "m5.xlarge" → "m5")
	family := extractInstanceFamily(instance.InstanceType)

	// SP must match this instance family
	if sp.InstanceFamily != family {
		return false
	}

	// SP must match this region
	if sp.Region != instance.Region {
		return false
	}

	return true
}

// getSavingsPlanRate returns the Savings Plan rate ($/hour) for a given instance type, region, and tenancy,
// along with a boolean indicating whether the rate is accurate (from API) or estimated (from config).
// This uses a two-tier lookup strategy:
//
//  1. Tier 1: Try to get actual rate from DescribeSavingsPlanRates API (via PricingCache)
//     - These are the exact rates that were locked in when the SP was purchased
//     - Provided by the SP Rates Reconciler which queries AWS periodically
//     - Returns (rate, true) to indicate accurate pricing
//
//  2. Tier 2: Fall back to configured discount multipliers (from Config)
//     - Conservative defaults: ~28% discount (0.72 multiplier) for 1-year commitments
//     - Can be configured for 3-year: ~50% discount (0.50 multiplier)
//     - Returns (rate, false) to indicate estimated pricing
//
// Formula: effectiveCost = onDemandRate * multiplier
// Example: $1.00 on-demand * 0.72 = $0.72 with 28% discount
func getSavingsPlanRate(
	calc *Calculator,
	sp *aws.SavingsPlan,
	instanceType string,
	region string,
	tenancy string,
	operatingSystem string,
	onDemandRate float64,
) (float64, bool) {
	// Tier 1: Try to get actual rate from DescribeSavingsPlanRates API
	if calc.PricingCache != nil {
		rate, found := calc.PricingCache.GetSPRate(
			sp.SavingsPlanARN, instanceType, region, tenancy, operatingSystem,
		)
		if found {
			return rate, true // Accurate: actual SP rate from API
		}
	}

	// Tier 2: Fall back to configured discount multipliers
	var rateMultiplier float64
	if calc.Config != nil {
		switch sp.SavingsPlanType {
		case "EC2Instance", "EC2InstanceSP":
			rateMultiplier = calc.Config.GetEC2InstanceDiscount()
		case "Compute", "ComputeSP":
			rateMultiplier = calc.Config.GetComputeDiscount()
		default:
			// Unknown SP type - use conservative default
			rateMultiplier = 0.72 // 28% discount
		}
	} else {
		// No config available - use conservative defaults
		rateMultiplier = 0.72 // 28% discount (typical 1-year commitment)
	}

	// Calculate the SP rate: onDemandRate * rateMultiplier
	// Example: $1.00 * 0.72 = $0.72 (you pay 72%, save 28%)
	return onDemandRate * rateMultiplier, false // Estimated: using fallback multiplier
}

// extractInstanceFamily extracts the instance family from an instance type.
// Examples:
//   - "m5.xlarge" → "m5"
//   - "c5.2xlarge" → "c5"
//   - "r5d.4xlarge" → "r5d"
//
// This is a shared helper function used by both RI and SP metrics.
func extractInstanceFamily(instanceType string) string {
	// Instance types are formatted as: family.size
	// Example: "m5.xlarge" where "m5" is the family
	parts := strings.Split(instanceType, ".")
	if len(parts) > 0 {
		return parts[0]
	}
	// Fallback: return the whole string if no period found
	return instanceType
}

// instanceWithSavings is a helper struct used for sorting instances by savings potential.
// This is used internally by the SP allocation algorithm.
type instanceWithSavings struct {
	Instance       *aws.Instance
	SavingsPercent float64 // (ODRate - SPRate) / ODRate
	SPRate         float64 // Savings Plan rate ($/hour)
	ODRate         float64 // On-Demand rate ($/hour)
	IsAccurate     bool    // Whether SPRate came from actual API data or estimated
}
