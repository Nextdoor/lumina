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
	"github.com/nextdoor/lumina/pkg/cost"
	"github.com/prometheus/client_golang/prometheus"
)

// UpdateInstanceCostMetrics updates all cost-related metrics based on calculation results.
// This function is called by the CostReconciler after each cost calculation cycle (every 5 minutes).
//
// The function implements proper metric lifecycle management:
//  1. Resets all existing cost metrics (clean slate approach)
//  2. Sets new values for all currently running instances
//  3. Terminated instances are automatically removed by the reset
//
// The function handles four types of metrics:
//   - ec2_instance_hourly_cost: Per-instance effective hourly cost ($/hour)
//   - savings_plan_current_utilization_rate: Current SP consumption ($/hour)
//   - savings_plan_remaining_capacity: Unused SP capacity ($/hour)
//   - savings_plan_utilization_percent: SP utilization percentage (0-100+)
//
// These metrics enable:
//   - Per-instance cost tracking and chargeback
//   - Real-time Savings Plans utilization monitoring
//   - Alerting on under/over-utilized SPs
//   - Cost optimization opportunities identification
//
// Example usage:
//
//	result := calculator.Calculate(input)
//	metrics.UpdateInstanceCostMetrics(result)
func (m *Metrics) UpdateInstanceCostMetrics(result cost.CalculationResult) {
	// Reset all existing cost metrics to ensure terminated instances and expired SPs are removed.
	// This is more reliable than trying to track which specific resources changed.
	m.EC2InstanceHourlyCost.Reset()
	m.SavingsPlanCurrentUtilizationRate.Reset()
	m.SavingsPlanRemainingCapacity.Reset()
	m.SavingsPlanUtilizationPercent.Reset()

	// Set instance cost metrics
	for _, ic := range result.InstanceCosts {
		// Normalize coverage type for metrics label
		// Convert CoverageType constants to string representation
		costType := string(ic.CoverageType)

		// Always export EffectiveCost - this represents what the instance actually pays.
		//
		// For SP-covered instances with partial coverage:
		//   EffectiveCost = SP contribution + on-demand spillover
		//
		// This means:
		//   sum(ec2_instance_hourly_cost{cost_type="compute_savings_plan"}) >=
		//   sum(savings_plan_current_utilization_rate)
		//
		// The difference represents on-demand spillover from partially covered instances
		// and is real cost that should be visible in metrics.
		m.EC2InstanceHourlyCost.With(prometheus.Labels{
			"instance_id":       ic.InstanceID,
			"account_id":        ic.AccountID,
			"region":            ic.Region,
			"instance_type":     ic.InstanceType,
			"cost_type":         costType,
			"availability_zone": ic.AvailabilityZone,
			"lifecycle":         ic.Lifecycle,
		}).Set(ic.EffectiveCost)
	}

	// Set Savings Plan utilization metrics
	for _, sp := range result.SavingsPlanUtilization {
		// Normalize SP type for metrics consistency
		// The calculator uses AWS API types ("EC2Instance", "Compute")
		// We normalize to snake_case for consistency with other metrics
		spType := normalizeSPType(sp.Type)

		// Set current utilization rate ($/hour being consumed right now)
		m.SavingsPlanCurrentUtilizationRate.With(prometheus.Labels{
			"savings_plan_arn": sp.SavingsPlanARN,
			"account_id":       sp.AccountID,
			"type":             spType,
		}).Set(sp.CurrentUtilizationRate)

		// Set remaining capacity ($/hour still available)
		// Can be negative if SP is over-utilized (spillover to on-demand)
		m.SavingsPlanRemainingCapacity.With(prometheus.Labels{
			"savings_plan_arn": sp.SavingsPlanARN,
			"account_id":       sp.AccountID,
			"type":             spType,
		}).Set(sp.RemainingCapacity)

		// Set utilization percentage (0-100+)
		// Values >100 indicate over-utilization (some instances paying on-demand rates)
		m.SavingsPlanUtilizationPercent.With(prometheus.Labels{
			"savings_plan_arn": sp.SavingsPlanARN,
			"account_id":       sp.AccountID,
			"type":             spType,
		}).Set(sp.UtilizationPercent)
	}
}
