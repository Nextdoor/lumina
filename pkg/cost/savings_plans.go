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
		applyEC2InstanceSavingsPlan(&sp, instances, costs, utilization)
	}

	// Step 3: Apply Compute Savings Plans
	// These apply to any instance family, any region (broader coverage)
	for _, sp := range computeSPs {
		applyComputeSavingsPlan(&sp, instances, costs, utilization)
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
		cost := costs[inst.InstanceID]

		// Skip if already RI-covered
		// Reserved Instances have already been applied (higher priority than SPs).
		// If an instance is RI-covered, it won't benefit from SP coverage, so skip it.
		if cost.RICoverage > 0 {
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
		// Example:
		//   - m5.xlarge on-demand rate: $0.192/hour
		//   - m5.xlarge with 72% EC2 Instance SP discount: $0.054/hour
		odRate := cost.ShelfPrice
		spRate := getSavingsPlanRate(sp, inst.InstanceType, odRate)

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
		// Primary sort: highest savings percentage first
		if eligible[i].SavingsPercent != eligible[j].SavingsPercent {
			return eligible[i].SavingsPercent > eligible[j].SavingsPercent
		}
		// Tie-breaker: lowest SP rate first
		if eligible[i].SPRate != eligible[j].SPRate {
			return eligible[i].SPRate < eligible[j].SPRate
		}
		// Final tie-breaker: instance ID (for deterministic sort)
		return eligible[i].Instance.InstanceID < eligible[j].Instance.InstanceID
	})

	// STEP 3: Apply SP coverage to instances in priority order until commitment exhausted
	//
	// Each Savings Plan has a fixed hourly commitment (e.g., $150/hour). This is the maximum
	// amount of discount the SP can provide per hour. We "spend" this commitment budget by
	// applying coverage to instances in the sorted order from Step 2.
	//
	// The "coverage amount" for an instance is the difference between its on-demand rate
	// and its SP rate. This is how much discount the SP provides for that instance.
	//
	// Example with $0.20/hour commitment:
	//   - Instance 1: on-demand $0.192/hr, SP rate $0.054/hr → coverage = $0.138/hr
	//   - Instance 2: on-demand $0.096/hr, SP rate $0.027/hr → coverage = $0.069/hr
	//
	// After covering instance 1: remaining commitment = $0.20 - $0.138 = $0.062/hr
	// Instance 2 needs $0.069/hr coverage, but only $0.062/hr remains → partial coverage
	//
	// Result:
	//   - Instance 1: fully covered by SP, effective cost = $0.054/hr (the SP rate)
	//   - Instance 2: partially covered, gets $0.062/hr discount, pays remaining at on-demand
	remainingCommitment := sp.Commitment

	for _, item := range eligible {
		if remainingCommitment <= 0 {
			break // SP commitment fully utilized, no more coverage available
		}

		inst := item.Instance
		cost := costs[inst.InstanceID]

		// Calculate how much coverage this instance needs
		//
		// The "coverage" is the discount amount ($/hour) that the SP provides.
		// It's the difference between what you'd pay on-demand vs. with the SP.
		//
		// Example:
		//   - on-demand rate: $0.192/hour
		//   - SP rate: $0.054/hour
		//   - coverage: $0.192 - $0.054 = $0.138/hour discount
		//
		// This $0.138/hour comes from the SP's hourly commitment budget.
		coverage := item.ODRate - item.SPRate

		if coverage > remainingCommitment {
			// PARTIAL COVERAGE SCENARIO
			//
			// The SP doesn't have enough commitment left to fully cover this instance.
			// This happens when the SP is running out of budget for the hour.
			//
			// Example:
			//   - Instance needs $0.138/hr coverage
			//   - SP only has $0.062/hr commitment remaining
			//   - Instance gets $0.062/hr coverage (partial)
			//   - Instance pays remaining $0.076/hr at on-demand rate
			//
			// After partial coverage:
			//   - Original on-demand cost: $0.192/hr
			//   - SP covers: $0.062/hr
			//   - Instance still pays: $0.130/hr (combination of SP rate + on-demand spillover)
			coverage = remainingCommitment
		}

		// Apply coverage to this instance
		//
		// SavingsPlanCoverage tracks how much discount this instance gets ($/hour)
		// EffectiveCost is reduced by the coverage amount
		// SavingsPlanARN links this instance to the specific SP providing coverage
		cost.SavingsPlanCoverage += coverage
		cost.SavingsPlanARN = sp.SavingsPlanARN
		cost.EffectiveCost -= coverage

		// The remaining cost after SP coverage is charged at on-demand rate
		// This handles both:
		// 1. Fully covered instances: OnDemandCost = 0 (EffectiveCost = SP rate)
		// 2. Partially covered instances: OnDemandCost > 0 (spillover to on-demand)
		cost.OnDemandCost = cost.EffectiveCost

		if cost.SavingsPlanCoverage > 0 {
			cost.CoverageType = CoverageEC2InstanceSavingsPlan
		}

		// Consume SP commitment
		// This reduces the available budget for covering additional instances
		remainingCommitment -= coverage
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
		cost := costs[inst.InstanceID]

		// Skip if already RI-covered
		// Reserved Instances have already been applied (highest priority).
		if cost.RICoverage > 0 {
			continue
		}

		// Skip if already fully covered by EC2 Instance SP
		//
		// EC2 Instance SPs apply before Compute SPs (higher priority).
		// If an EC2 Instance SP already covers the full cost, there's nothing
		// left for a Compute SP to discount.
		//
		// Example:
		//   - Instance on-demand cost: $0.192/hr
		//   - EC2 Instance SP already provided: $0.192/hr coverage
		//   - Remaining cost: $0 → skip, Compute SP can't help
		//
		// However, if EC2 Instance SP only partially covered the instance
		// (SP ran out of commitment), Compute SP can cover the remainder.
		if cost.SavingsPlanCoverage >= cost.ShelfPrice {
			continue
		}

		// Calculate savings percentage and SP rate for this instance
		// (Same logic as EC2 Instance SPs - see detailed comments there)
		odRate := cost.ShelfPrice
		spRate := getSavingsPlanRate(sp, inst.InstanceType, odRate)

		if odRate <= 0 || spRate <= 0 {
			continue
		}

		savingsPct := (odRate - spRate) / odRate

		eligible = append(eligible, instanceWithSavings{
			Instance:       inst,
			SavingsPercent: savingsPct,
			SPRate:         spRate,
			ODRate:         odRate,
		})
	}

	// STEP 2: Sort eligible instances by savings priority
	//
	// Uses the same prioritization algorithm as EC2 Instance SPs:
	// 1. Highest savings percentage first
	// 2. Tie-breaker: lowest SP rate first
	// 3. Final tie-breaker: instance ID (for deterministic sort)
	//
	// See detailed comments in applyEC2InstanceSavingsPlan() for the rationale.
	sort.Slice(eligible, func(i, j int) bool {
		if eligible[i].SavingsPercent != eligible[j].SavingsPercent {
			return eligible[i].SavingsPercent > eligible[j].SavingsPercent
		}
		if eligible[i].SPRate != eligible[j].SPRate {
			return eligible[i].SPRate < eligible[j].SPRate
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

		// Calculate coverage (difference between on-demand and SP rate)
		coverage := item.ODRate - item.SPRate
		if coverage > remainingCommitment {
			// Partial coverage scenario (see EC2 Instance SP comments for details)
			coverage = remainingCommitment
		}

		// Apply coverage
		cost.SavingsPlanCoverage += coverage
		cost.SavingsPlanARN = sp.SavingsPlanARN
		cost.EffectiveCost -= coverage
		cost.OnDemandCost = cost.EffectiveCost

		// Set coverage type to Compute SP ONLY if this is the instance's first SP coverage
		// If an EC2 Instance SP already partially covered this instance, we want to keep
		// the CoverageType as EC2InstanceSavingsPlan (the more specific/higher priority type)
		if cost.SavingsPlanCoverage > 0 && cost.CoverageType == CoverageOnDemand {
			cost.CoverageType = CoverageComputeSavingsPlan
		}

		// Consume SP commitment
		remainingCommitment -= coverage
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

// getSavingsPlanRate returns the Savings Plan rate ($/hour) for a given instance type.
// This is the discounted rate when the SP is applied.
//
// In a real implementation, this would:
//   - Look up the SP rate from AWS pricing data
//   - Use the SP's commitment percentage (e.g., 72% discount)
//   - Calculate: odRate * (1 - discountPercent)
//
// For MVP, we'll use a placeholder calculation with reasonable discount percentages.
// TODO(#48): Implement actual SP rate lookup from AWS pricing API or hardcoded tables.
// See https://github.com/Nextdoor/lumina/issues/48 for detailed implementation plan.
func getSavingsPlanRate(sp *aws.SavingsPlan, instanceType string, onDemandRate float64) float64 {
	// NOTE: instanceType parameter is currently unused but will be needed when implementing
	// actual SP rate lookup from AWS pricing tables. Different instance types within the
	// same family may have slightly different SP discount rates.
	_ = instanceType // Will be used in future implementation

	// Placeholder: Calculate SP rate based on typical discount percentages
	// In reality, this would look up the actual SP rate from pricing data
	//
	// EC2 Instance SPs typically provide 70-72% discount
	// Compute SPs typically provide 60-66% discount
	//
	// This is a simplification for MVP. Real implementation needs:
	// 1. AWS pricing API integration, OR
	// 2. Hardcoded SP rate tables per instance type
	//
	// The SP rate is NOT a percentage of on-demand; it's a fixed $/hour rate
	// that varies by instance type. For example:
	//   - m5.xlarge on-demand: $0.192/hour
	//   - m5.xlarge with EC2 Instance SP: ~$0.054/hour (72% discount)
	//
	// For now, use reasonable placeholder discount percentages
	var discountPercent float64
	switch sp.SavingsPlanType {
	case "EC2Instance":
		discountPercent = 0.72 // 72% discount
	case "Compute":
		discountPercent = 0.66 // 66% discount
	default:
		discountPercent = 0.50 // Conservative default
	}

	// Calculate the SP rate as: on-demand rate * (1 - discount percentage)
	spRate := onDemandRate * (1 - discountPercent)
	return spRate
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
}
