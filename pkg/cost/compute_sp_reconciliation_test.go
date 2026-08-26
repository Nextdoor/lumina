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

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestReconcileComputeSavingsPlanUtilization(t *testing.T) {
	tests := []struct {
		name     string
		observed *float64
		wantA    float64
		wantB    float64
	}{
		{name: "caps headroom", observed: float64Pointer(2), wantA: 8.0 / 7, wantB: 6.0 / 7},
		{name: "does not increase headroom", observed: float64Pointer(10), wantA: 4, wantB: 3},
		{name: "missing observation retains estimate", observed: nil, wantA: 4, wantB: 3},
		{name: "negative observation clamps to zero", observed: float64Pointer(-1), wantA: 0, wantB: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			utilization := map[string]*SavingsPlanUtilization{
				"b": {
					SavingsPlanARN: "b", AccountID: "account", Type: savingsPlanTypeCompute,
					HourlyCommitment: 5, RemainingCapacity: 3,
					CurrentUtilizationRate: 2, UtilizationPercent: 40,
				},
				"a": {
					SavingsPlanARN: "a", AccountID: "account", Type: savingsPlanTypeCompute,
					HourlyCommitment: 6, RemainingCapacity: 4,
					CurrentUtilizationRate: 2, UtilizationPercent: 100.0 / 3,
				},
				"ec2": {
					SavingsPlanARN: "ec2", AccountID: "account", Type: "EC2Instance",
					HourlyCommitment: 4, RemainingCapacity: 4,
				},
			}

			reconcileComputeSavingsPlanUtilization(CalculationInput{ComputeSPUnusedCommitment: tt.observed}, utilization)

			assert.InDelta(t, tt.wantA, utilization["a"].RemainingCapacity, 1e-9)
			assert.InDelta(t, tt.wantB, utilization["b"].RemainingCapacity, 1e-9)
			assert.Equal(t, 4.0, utilization["ec2"].RemainingCapacity)
			assert.InDelta(
				t, utilization["a"].HourlyCommitment-utilization["a"].RemainingCapacity,
				utilization["a"].CurrentUtilizationRate, 1e-9,
			)
		})
	}
}

func float64Pointer(value float64) *float64 {
	return &value
}
