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

package metrics

import (
	"time"

	"github.com/nextdoor/lumina/pkg/aws"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	// spLabelAll is used for Compute SPs which apply globally
	spLabelAll = "all"
)

// UpdateSavingsPlansInventoryMetrics updates SP inventory metrics from the provided list of Savings Plans.
// This function implements proper metric lifecycle management:
//  1. Resets all existing SP metrics (clean slate approach)
//  2. Sets new values for all currently active SPs
//  3. Deleted/expired SPs are automatically removed by the reset
//
// The function handles two types of metrics:
//   - savings_plan_hourly_commitment: Fixed hourly commitment amount ($/hour)
//   - savings_plan_remaining_hours: Hours until SP expires
//
// This should be called by the RISP reconciler after successfully updating
// the SP cache (typically hourly).
//
// Note: This function handles INVENTORY metrics only. Utilization metrics
// (current usage, remaining capacity, utilization %) will be added in Phase 6
// after cost calculation is implemented.
//
// Example usage:
//
//	sps := rispCache.GetAllSavingsPlans()
//	metrics.UpdateSavingsPlansInventoryMetrics(sps)
func (m *Metrics) UpdateSavingsPlansInventoryMetrics(sps []aws.SavingsPlan) {
	// Reset all existing SP metrics to ensure deleted/expired SPs are removed.
	// This is more reliable than trying to track which specific SPs were deleted.
	m.SavingsPlanCommitment.Reset()
	m.SavingsPlanRemainingHours.Reset()

	now := time.Now()

	// Process each Savings Plan
	for _, sp := range sps {
		// Skip inactive SPs (expired, retired, etc.)
		if sp.State != "active" {
			continue
		}

		// Determine type label: "ec2_instance" or "compute"
		// AWS uses PascalCase, we normalize to snake_case for consistency
		spType := normalizeSPType(sp.SavingsPlanType)

		// Determine region and instance_family labels based on SP type
		region := sp.Region
		instanceFamily := sp.InstanceFamily

		// For Compute SPs: apply globally to all regions and families
		if spType == "compute" {
			region = spLabelAll
			instanceFamily = spLabelAll
		}

		// For EC2 Instance SPs: if InstanceFamily is empty, use "all"
		// (defensive - should not happen in practice)
		if spType == "ec2_instance" && instanceFamily == "" {
			instanceFamily = spLabelAll
		}

		// Set commitment metric
		m.SavingsPlanCommitment.With(prometheus.Labels{
			"savings_plan_arn": sp.SavingsPlanARN,
			"account_id":       sp.AccountID,
			"type":             spType,
			"region":           region,
			"instance_family":  instanceFamily,
		}).Set(sp.Commitment)

		// Calculate remaining hours until expiration
		remainingHours := calculateRemainingHours(sp.End, now)

		// Set remaining hours metric
		m.SavingsPlanRemainingHours.With(prometheus.Labels{
			"savings_plan_arn": sp.SavingsPlanARN,
			"account_id":       sp.AccountID,
			"type":             spType,
		}).Set(remainingHours)
	}
}

// normalizeSPType converts AWS SavingsPlan type to normalized form.
// AWS API returns: "EC2Instance" or "Compute"
// We normalize to: "ec2_instance" or "compute"
func normalizeSPType(spType string) string {
	switch spType {
	case "EC2Instance":
		return "ec2_instance"
	case "Compute":
		return "compute"
	default:
		// Unknown type - return as-is (defensive programming)
		return spType
	}
}

// calculateRemainingHours calculates the number of hours remaining until expiration.
// Returns 0 if the SP has already expired.
func calculateRemainingHours(endTime time.Time, now time.Time) float64 {
	remaining := endTime.Sub(now)
	if remaining <= 0 {
		return 0
	}
	return remaining.Hours()
}
