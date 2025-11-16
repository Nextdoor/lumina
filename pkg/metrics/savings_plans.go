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

// UpdateSavingsPlansMetrics updates Savings Plans inventory metrics from the
// provided list of Savings Plans. This implements Phase 3 of the RFC, which
// covers SP inventory metrics only (not utilization metrics - those come in Phase 6).
//
// This function implements proper metric lifecycle management:
//  1. Resets all existing SP metrics (clean slate approach)
//  2. Sets new values for all currently active SPs
//  3. Deleted/expired SPs are automatically removed by the reset
//
// The function emits two types of metrics per RFC Phase 3 (lines 1774-1778):
//   - savings_plan_hourly_commitment: Fixed hourly commitment amount ($/hour)
//   - savings_plan_remaining_hours: Hours until SP expires
//
// Note: SP utilization metrics (current_utilization_rate, remaining_capacity,
// utilization_percent) are NOT emitted here. Per RFC lines 1779-1784, these
// are "added after Phase 6 when cost calculation works."
//
// This should be called by the RISP reconciler after successfully updating
// the Savings Plans cache (typically hourly).
//
// Example usage:
//
//	sps := rispCache.GetAllSavingsPlans()
//	metrics.UpdateSavingsPlansMetrics(sps)
func (m *Metrics) UpdateSavingsPlansMetrics(sps []aws.SavingsPlan) {
	// Reset all existing SP metrics to ensure deleted/expired SPs are removed.
	// This is more reliable than trying to track which specific SPs were deleted.
	m.SavingsPlanHourlyCommitment.Reset()
	m.SavingsPlanRemainingHours.Reset()

	// Process each Savings Plan
	for _, sp := range sps {
		// Skip inactive Savings Plans (expired, queued, etc.)
		// Only emit metrics for active SPs
		if sp.State != "active" {
			continue
		}

		// Determine region label value
		// EC2 Instance SPs are regional, Compute SPs apply to all regions
		region := sp.Region
		if region == "" {
			region = "all"
		}

		// Determine instance family label value
		// EC2 Instance SPs are specific to a family, Compute SPs apply to all
		instanceFamily := sp.InstanceFamily
		if instanceFamily == "" {
			instanceFamily = "all"
		}

		// Set hourly commitment metric
		// This is the fixed $/hour amount the customer committed to spend
		m.SavingsPlanHourlyCommitment.With(prometheus.Labels{
			"savings_plan_arn": sp.SavingsPlanARN,
			"type":             sp.SavingsPlanType,
			"account_id":       sp.AccountID,
			"region":           region,
			"instance_family":  instanceFamily,
		}).Set(sp.Commitment)

		// Calculate remaining hours until SP expires
		remainingHours := time.Until(sp.End).Hours()
		if remainingHours < 0 {
			// SP has expired (shouldn't happen if State is "active", but defensive)
			remainingHours = 0
		}

		// Set remaining hours metric
		m.SavingsPlanRemainingHours.With(prometheus.Labels{
			"savings_plan_arn": sp.SavingsPlanARN,
			"account_id":       sp.AccountID,
		}).Set(remainingHours)
	}
}
