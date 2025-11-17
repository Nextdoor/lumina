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
	// Find eligible instances (matching family + region, not RI-covered)
	eligible := make([]instanceWithSavings, 0, len(instances))
	for idx := range instances {
		inst := &instances[idx]
		cost := costs[inst.InstanceID]

		// Skip if already RI-covered
		if cost.RICoverage > 0 {
			continue
		}

		// Check if instance matches SP criteria
		if !matchesEC2InstanceSP(inst, sp) {
			continue
		}

		// Calculate savings percentage and SP rate for this instance
		odRate := cost.ShelfPrice
		spRate := getSavingsPlanRate(sp, inst.InstanceType, odRate)

		if odRate <= 0 || spRate <= 0 {
			continue // Can't calculate savings
		}

		savingsPct := (odRate - spRate) / odRate

		eligible = append(eligible, instanceWithSavings{
			Instance:       inst,
			SavingsPercent: savingsPct,
			SPRate:         spRate,
			ODRate:         odRate,
		})
	}

	// Sort by savings percentage (descending), then by SP rate (ascending)
	// This follows AWS's documented prioritization algorithm
	sort.Slice(eligible, func(i, j int) bool {
		// Primary sort: highest savings percentage first
		if eligible[i].SavingsPercent != eligible[j].SavingsPercent {
			return eligible[i].SavingsPercent > eligible[j].SavingsPercent
		}
		// Tie-breaker: lowest SP rate first
		return eligible[i].SPRate < eligible[j].SPRate
	})

	// Apply SP coverage until commitment exhausted
	remainingCommitment := sp.Commitment

	for _, item := range eligible {
		if remainingCommitment <= 0 {
			break // SP commitment fully utilized
		}

		inst := item.Instance
		cost := costs[inst.InstanceID]

		// Calculate how much of this instance's cost can be covered by this SP
		// The SP covers the difference between on-demand rate and SP rate
		coverage := item.ODRate - item.SPRate
		if coverage > remainingCommitment {
			// Partial coverage: SP commitment runs out partway through this instance
			coverage = remainingCommitment
		}

		// Apply coverage to this instance
		cost.SavingsPlanCoverage += coverage
		cost.SavingsPlanARN = sp.SavingsPlanARN
		cost.EffectiveCost -= coverage

		// Remaining cost is charged at on-demand rate
		cost.OnDemandCost = cost.EffectiveCost

		if cost.SavingsPlanCoverage > 0 {
			cost.CoverageType = CoverageEC2InstanceSavingsPlan
		}

		// Consume SP commitment
		remainingCommitment -= coverage
	}

	// Track SP utilization
	util := utilization[sp.SavingsPlanARN]
	util.CurrentUtilizationRate = sp.Commitment - remainingCommitment
	util.RemainingCapacity = remainingCommitment
	if sp.Commitment > 0 {
		util.UtilizationPercent = (util.CurrentUtilizationRate / sp.Commitment) * 100
	}
}

// applyComputeSavingsPlan applies a single Compute Savings Plan to eligible instances.
//
// Compute SPs match based on:
//   - ANY instance family (m5, c5, r5, etc.)
//   - ANY region (us-west-2, us-east-1, etc.)
//   - Instance must NOT already be RI-covered or EC2 Instance SP-covered
//
// Algorithm is the same as EC2 Instance SPs, but with broader eligibility.
func applyComputeSavingsPlan(
	sp *aws.SavingsPlan,
	instances []aws.Instance,
	costs map[string]*InstanceCost,
	utilization map[string]*SavingsPlanUtilization,
) {
	// Find eligible instances (any family/region, not already covered)
	eligible := make([]instanceWithSavings, 0, len(instances))
	for idx := range instances {
		inst := &instances[idx]
		cost := costs[inst.InstanceID]

		// Skip if already RI-covered
		if cost.RICoverage > 0 {
			continue
		}

		// Skip if already fully covered by EC2 Instance SP
		// (Compute SPs apply after EC2 Instance SPs)
		if cost.SavingsPlanCoverage >= cost.ShelfPrice {
			continue
		}

		// Calculate savings percentage and SP rate for this instance
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

	// Sort by savings percentage (descending), then by SP rate (ascending)
	sort.Slice(eligible, func(i, j int) bool {
		if eligible[i].SavingsPercent != eligible[j].SavingsPercent {
			return eligible[i].SavingsPercent > eligible[j].SavingsPercent
		}
		return eligible[i].SPRate < eligible[j].SPRate
	})

	// Apply SP coverage until commitment exhausted
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
			coverage = remainingCommitment
		}

		// Apply coverage
		cost.SavingsPlanCoverage += coverage
		cost.SavingsPlanARN = sp.SavingsPlanARN
		cost.EffectiveCost -= coverage
		cost.OnDemandCost = cost.EffectiveCost

		if cost.SavingsPlanCoverage > 0 && cost.CoverageType == CoverageOnDemand {
			cost.CoverageType = CoverageComputeSavingsPlan
		}

		// Consume SP commitment
		remainingCommitment -= coverage
	}

	// Track SP utilization
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
