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

	"github.com/nextdoor/lumina/pkg/aws"
)

// applyReservedInstances applies Reserved Instances to matching EC2 instances.
// RIs are applied BEFORE any Savings Plans according to AWS billing rules.
//
// Reserved Instances match based on:
//   - Instance Type (exact match, e.g., "m5.xlarge")
//   - Availability Zone (for zonal RIs) OR Region (for regional RIs)
//   - Account ID (RIs apply within the same account, or shared in organizations)
//
// When an RI is applied, the instance's cost is set to $0 because RIs are pre-paid.
// The instance is marked as RI-covered so Savings Plans don't apply to it.
//
// Algorithm:
//  1. For each Reserved Instance:
//     a. Find all uncovered instances matching instance type + AZ/region + account
//     b. Sort eligible instances by launch time (oldest first) for stable assignment
//     c. Apply RI coverage to oldest instances first until RI capacity exhausted
//  2. Move to next RI
//
// Reference: AWS Savings Plans documentation
// https://docs.aws.amazon.com/savingsplans/latest/userguide/sp-applying.html
func applyReservedInstances(
	instances []aws.Instance,
	reservedInstances []aws.ReservedInstance,
	costs map[string]*InstanceCost,
) {
	// Track which RIs have been utilized (matched to an instance)
	// Key: RI unique identifier (account_id:instance_type:availability_zone)
	utilizedRIs := make(map[string]int)

	// Process each Reserved Instance
	for _, ri := range reservedInstances {
		// STEP 1: Find all eligible instances for this RI
		//
		// Build a list of instances that match this RI's criteria and aren't
		// already covered. We'll sort them to ensure stable, deterministic assignment.
		var eligible []*aws.Instance
		for idx := range instances {
			inst := &instances[idx]

			// Skip if instance doesn't match RI criteria
			if !matchesReservedInstance(inst, &ri) {
				continue
			}

			// Get the cost object for this instance
			cost, exists := costs[inst.InstanceID]
			if !exists {
				continue
			}

			// Skip if instance is already RI-covered
			if cost.RICoverage > 0 {
				continue
			}

			eligible = append(eligible, inst)
		}

		// STEP 2: Sort eligible instances by launch time (oldest first)
		//
		// This provides stable discount assignment:
		// - Older instances keep their RI coverage across reconciliation loops
		// - Newer instances only get coverage if there's unused RI capacity
		// - Prevents discounts from "jumping" between instances
		//
		// Tie-breaker: Instance ID (for complete determinism)
		sort.Slice(eligible, func(i, j int) bool {
			if !eligible[i].LaunchTime.Equal(eligible[j].LaunchTime) {
				return eligible[i].LaunchTime.Before(eligible[j].LaunchTime)
			}
			return eligible[i].InstanceID < eligible[j].InstanceID
		})

		// STEP 3: Apply RI coverage to eligible instances in order
		//
		// RIs can cover multiple instances (based on InstanceCount).
		// Apply coverage to the oldest instances first until RI capacity is exhausted.
		riCount := ri.InstanceCount
		appliedCount := 0

		for _, inst := range eligible {
			if appliedCount >= int(riCount) {
				break // RI capacity exhausted
			}

			cost := costs[inst.InstanceID]

			// Apply RI coverage
			cost.RICoverage = cost.ShelfPrice
			cost.EffectiveCost = 0 // RIs are pre-paid, so effective cost is $0
			cost.CoverageType = CoverageReservedInstance

			// Track utilization
			riKey := riIdentifier(&ri)
			utilizedRIs[riKey]++
			appliedCount++
		}
	}
}

// matchesReservedInstance checks if an EC2 instance matches the criteria for a
// Reserved Instance to apply.
//
// Matching rules:
//   - Instance type must match exactly
//   - Account ID must match (RIs apply within account)
//   - For zonal RIs: Availability Zone must match exactly
//   - For regional RIs: Region must match (any AZ within the region)
//
// TODO(#46): Regional RI Size Flexibility Not Implemented
// AWS Regional RIs support instance size flexibility within the same family using
// normalization factors. For example, a Regional RI for m5.4xlarge (16 units) can
// be split to cover 4x m5.xlarge (4 units each). This is NOT currently implemented.
// The current implementation only supports exact instance type matching.
//
// Limitations:
//   - A Regional RI for m5.4xlarge can ONLY match m5.4xlarge instances (not smaller m5 sizes)
//   - Zonal RIs do not support size flexibility (AWS behavior - correct as-is)
//   - This may result in underutilization of Regional RIs if exact instance types don't match
//
// Returns true if the RI can apply to this instance.
func matchesReservedInstance(instance *aws.Instance, ri *aws.ReservedInstance) bool {
	// Instance type must match exactly
	// TODO(#46): For regional RIs, this should check instance family and use
	// normalization factors to allow size flexibility
	if instance.InstanceType != ri.InstanceType {
		return false
	}

	// Account must match
	if instance.AccountID != ri.AccountID {
		return false
	}

	// Check availability zone / region matching
	// If RI availability zone is "regional" or empty, it's a regional RI
	// Otherwise it's a zonal RI that must match exact AZ
	if ri.AvailabilityZone != "regional" && ri.AvailabilityZone != "" {
		// Zonal RI: must match exact AZ
		if instance.AvailabilityZone != ri.AvailabilityZone {
			return false
		}
	} else {
		// Regional RI: must match region (any AZ in that region)
		if instance.Region != ri.Region {
			return false
		}
	}

	return true
}

// riIdentifier creates a unique identifier for a Reserved Instance.
// This is used to track which RIs have been utilized.
//
// Format: "account_id:instance_type:scope:availability_zone_or_region"
// Examples:
//   - Zonal RI:    "123456789012:m5.xlarge:zonal:us-west-2a"
//   - Regional RI: "123456789012:m5.xlarge:regional:us-west-2"
func riIdentifier(ri *aws.ReservedInstance) string {
	if ri.AvailabilityZone != "regional" && ri.AvailabilityZone != "" {
		// Zonal RI
		return ri.AccountID + ":" + ri.InstanceType + ":zonal:" + ri.AvailabilityZone
	}
	// Regional RI
	return ri.AccountID + ":" + ri.InstanceType + ":regional:" + ri.Region
}
