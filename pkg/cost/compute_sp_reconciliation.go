// Copyright 2025 Nextdoor, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cost

const savingsPlanTypeCompute = "Compute"

// reconcileComputeSavingsPlanUtilization caps account-level Compute Savings
// Plans headroom at AWS's observed unused commitment. When multiple plans exist,
// the observed headroom is distributed proportionally without changing the EC2
// instance allocation calculated above.
func reconcileComputeSavingsPlanUtilization(
	input CalculationInput,
	utilization map[string]*SavingsPlanUtilization,
) {
	if input.ComputeSPUnusedCommitment == nil {
		return
	}
	observedUnused := *input.ComputeSPUnusedCommitment
	if observedUnused < 0 {
		observedUnused = 0
	}

	plans := make([]*SavingsPlanUtilization, 0)
	calculatedUnused := 0.0
	for _, util := range utilization {
		if util.Type == savingsPlanTypeCompute {
			plans = append(plans, util)
			calculatedUnused += util.RemainingCapacity
		}
	}
	if calculatedUnused <= 0 || observedUnused >= calculatedUnused {
		return
	}

	scale := observedUnused / calculatedUnused
	for _, plan := range plans {
		planUnused := plan.RemainingCapacity * scale
		plan.RemainingCapacity = planUnused
		plan.CurrentUtilizationRate = plan.HourlyCommitment - planUnused
		if plan.HourlyCommitment > 0 {
			plan.UtilizationPercent = plan.CurrentUtilizationRate / plan.HourlyCommitment * 100
		}
	}
}
