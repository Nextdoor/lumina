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
	"testing"
	"time"

	"github.com/nextdoor/lumina/pkg/aws"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCalculatorComprehensiveScenarios tests the cost calculator with easy-to-understand
// pricing numbers. This uses simple dollar amounts ($1, $2, etc.) to make the test
// scenarios clear and easy to reason about.
//
// Pricing scheme used across all tests:
//   - m5.2xlarge: $2.00/hr OD, $0.68/hr Compute SP (66% discount)
//   - m5.xlarge:  $1.00/hr OD, $0.34/hr Compute SP (66% discount), $0.50/hr Spot
//   - c5.xlarge:  $1.00/hr OD, $0.34/hr Compute SP (66% discount), $0.40/hr Spot
//   - t3.medium:  $0.50/hr OD, $0.17/hr Compute SP (66% discount), $0.20/hr Spot
//
// Note: Compute Savings Plans provide 66% discount (see pkg/cost/savings_plans.go:602)
// EC2 Instance Savings Plans provide 72% discount
func TestCalculatorComprehensiveScenarios(t *testing.T) {
	baseTime := testBaseTime()

	tests := []struct {
		name        string
		description string

		// Input: instances to run
		instances []aws.Instance

		// Input: Reserved Instances
		reservedInstances []aws.ReservedInstance

		// Input: Savings Plans
		savingsPlans []aws.SavingsPlan

		// Expected: per-instance results
		expectedCosts map[string]expectedInstanceCost

		// Expected: aggregate totals
		expectedTotalShelfPrice    float64
		expectedTotalEstimatedCost float64
		expectedTotalSavings       float64
		expectedTotalRICoverage    float64
		expectedTotalSPCoverage    float64
	}{
		{
			name: "Scenario 1: RI coverage only",
			description: "5 RIs for m5.2xlarge, 15 m5.2xlarge instances running. " +
				"5 should be RI-covered, 10 should be on-demand.",
			//nolint:dupl // Test data duplication is acceptable for clarity
			instances: []aws.Instance{
				// 15 m5.2xlarge instances, all on-demand lifecycle
				newTestInstance("i-001", "m5.2xlarge", "us-west-2a", "on-demand", baseTime.Add(1*time.Hour)),
				newTestInstance("i-002", "m5.2xlarge", "us-west-2a", "on-demand", baseTime.Add(2*time.Hour)),
				newTestInstance("i-003", "m5.2xlarge", "us-west-2a", "on-demand", baseTime.Add(3*time.Hour)),
				newTestInstance("i-004", "m5.2xlarge", "us-west-2a", "on-demand", baseTime.Add(4*time.Hour)),
				newTestInstance("i-005", "m5.2xlarge", "us-west-2a", "on-demand", baseTime.Add(5*time.Hour)),
				newTestInstance("i-006", "m5.2xlarge", "us-west-2a", "on-demand", baseTime.Add(6*time.Hour)),
				newTestInstance("i-007", "m5.2xlarge", "us-west-2a", "on-demand", baseTime.Add(7*time.Hour)),
				newTestInstance("i-008", "m5.2xlarge", "us-west-2a", "on-demand", baseTime.Add(8*time.Hour)),
				newTestInstance("i-009", "m5.2xlarge", "us-west-2a", "on-demand", baseTime.Add(9*time.Hour)),
				newTestInstance("i-010", "m5.2xlarge", "us-west-2a", "on-demand", baseTime.Add(10*time.Hour)),
				newTestInstance("i-011", "m5.2xlarge", "us-west-2a", "on-demand", baseTime.Add(11*time.Hour)),
				newTestInstance("i-012", "m5.2xlarge", "us-west-2a", "on-demand", baseTime.Add(12*time.Hour)),
				newTestInstance("i-013", "m5.2xlarge", "us-west-2a", "on-demand", baseTime.Add(13*time.Hour)),
				newTestInstance("i-014", "m5.2xlarge", "us-west-2a", "on-demand", baseTime.Add(14*time.Hour)),
				newTestInstance("i-015", "m5.2xlarge", "us-west-2a", "on-demand", baseTime.Add(15*time.Hour)),
			},
			reservedInstances: []aws.ReservedInstance{
				// 5 RIs for m5.2xlarge in us-west-2a
				newTestRI("m5.2xlarge", "us-west-2a", 5),
			},
			savingsPlans: []aws.SavingsPlan{},
			//nolint:dupl // Test expectations duplication is acceptable for clarity
			expectedCosts: map[string]expectedInstanceCost{
				// First 5 instances (oldest) should be RI-covered
				"i-001": {ShelfPrice: 2.00, EffectiveCost: 0.00, CoverageType: CoverageReservedInstance, RICoverage: 2.00},
				"i-002": {ShelfPrice: 2.00, EffectiveCost: 0.00, CoverageType: CoverageReservedInstance, RICoverage: 2.00},
				"i-003": {ShelfPrice: 2.00, EffectiveCost: 0.00, CoverageType: CoverageReservedInstance, RICoverage: 2.00},
				"i-004": {ShelfPrice: 2.00, EffectiveCost: 0.00, CoverageType: CoverageReservedInstance, RICoverage: 2.00},
				"i-005": {ShelfPrice: 2.00, EffectiveCost: 0.00, CoverageType: CoverageReservedInstance, RICoverage: 2.00},
				// Remaining 10 instances should be on-demand
				"i-006": {ShelfPrice: 2.00, EffectiveCost: 2.00, CoverageType: CoverageOnDemand, OnDemandCost: 2.00},
				"i-007": {ShelfPrice: 2.00, EffectiveCost: 2.00, CoverageType: CoverageOnDemand, OnDemandCost: 2.00},
				"i-008": {ShelfPrice: 2.00, EffectiveCost: 2.00, CoverageType: CoverageOnDemand, OnDemandCost: 2.00},
				"i-009": {ShelfPrice: 2.00, EffectiveCost: 2.00, CoverageType: CoverageOnDemand, OnDemandCost: 2.00},
				"i-010": {ShelfPrice: 2.00, EffectiveCost: 2.00, CoverageType: CoverageOnDemand, OnDemandCost: 2.00},
				"i-011": {ShelfPrice: 2.00, EffectiveCost: 2.00, CoverageType: CoverageOnDemand, OnDemandCost: 2.00},
				"i-012": {ShelfPrice: 2.00, EffectiveCost: 2.00, CoverageType: CoverageOnDemand, OnDemandCost: 2.00},
				"i-013": {ShelfPrice: 2.00, EffectiveCost: 2.00, CoverageType: CoverageOnDemand, OnDemandCost: 2.00},
				"i-014": {ShelfPrice: 2.00, EffectiveCost: 2.00, CoverageType: CoverageOnDemand, OnDemandCost: 2.00},
				"i-015": {ShelfPrice: 2.00, EffectiveCost: 2.00, CoverageType: CoverageOnDemand, OnDemandCost: 2.00},
			},
			expectedTotalShelfPrice:    30.00, // 15 instances * $2.00
			expectedTotalEstimatedCost: 20.00, // 10 on-demand * $2.00 (5 RIs are $0)
			expectedTotalSavings:       10.00, // Savings from 5 RIs * $2.00
			expectedTotalRICoverage:    10.00, // 5 instances * $2.00
			expectedTotalSPCoverage:    0.00,
		},
		{
			name: "Scenario 2: RI + SP coverage",
			description: "5 RIs, 1 SP with $3.00 commitment, 15 total instances. " +
				"5 RI-covered, ~2.3 SP-covered (partial), rest on-demand. ",
			//nolint:dupl // Test data duplication is acceptable for clarity
			instances: []aws.Instance{
				// 15 m5.2xlarge instances
				newTestInstance("i-001", "m5.2xlarge", "us-west-2a", "on-demand", baseTime.Add(1*time.Hour)),
				newTestInstance("i-002", "m5.2xlarge", "us-west-2a", "on-demand", baseTime.Add(2*time.Hour)),
				newTestInstance("i-003", "m5.2xlarge", "us-west-2a", "on-demand", baseTime.Add(3*time.Hour)),
				newTestInstance("i-004", "m5.2xlarge", "us-west-2a", "on-demand", baseTime.Add(4*time.Hour)),
				newTestInstance("i-005", "m5.2xlarge", "us-west-2a", "on-demand", baseTime.Add(5*time.Hour)),
				newTestInstance("i-006", "m5.2xlarge", "us-west-2a", "on-demand", baseTime.Add(6*time.Hour)),
				newTestInstance("i-007", "m5.2xlarge", "us-west-2a", "on-demand", baseTime.Add(7*time.Hour)),
				newTestInstance("i-008", "m5.2xlarge", "us-west-2a", "on-demand", baseTime.Add(8*time.Hour)),
				newTestInstance("i-009", "m5.2xlarge", "us-west-2a", "on-demand", baseTime.Add(9*time.Hour)),
				newTestInstance("i-010", "m5.2xlarge", "us-west-2a", "on-demand", baseTime.Add(10*time.Hour)),
				newTestInstance("i-011", "m5.2xlarge", "us-west-2a", "on-demand", baseTime.Add(11*time.Hour)),
				newTestInstance("i-012", "m5.2xlarge", "us-west-2a", "on-demand", baseTime.Add(12*time.Hour)),
				newTestInstance("i-013", "m5.2xlarge", "us-west-2a", "on-demand", baseTime.Add(13*time.Hour)),
				newTestInstance("i-014", "m5.2xlarge", "us-west-2a", "on-demand", baseTime.Add(14*time.Hour)),
				newTestInstance("i-015", "m5.2xlarge", "us-west-2a", "on-demand", baseTime.Add(15*time.Hour)),
			},
			reservedInstances: []aws.ReservedInstance{
				// 5 RIs
				newTestRI("m5.2xlarge", "us-west-2a", 5),
			},
			savingsPlans: []aws.SavingsPlan{
				// SP commitment is the DISCOUNT BUDGET (not payment amount)
				// With 66% Compute SP discount:
				//   - SP rate: $2.00 * 0.34 = $0.68/hr
				//   - Coverage per instance: $2.00 - $0.68 = $1.32/hr
				//   - $3.00 commitment covers: 2 full instances + 0.27 of 3rd instance
				newTestComputeSP("sp-001", 3.00),
			},
			//nolint:dupl // Test expectations duplication is acceptable for clarity
			expectedCosts: map[string]expectedInstanceCost{
				// First 5 instances (oldest) should be RI-covered
				"i-001": {ShelfPrice: 2.00, EffectiveCost: 0.00, CoverageType: CoverageReservedInstance, RICoverage: 2.00},
				"i-002": {ShelfPrice: 2.00, EffectiveCost: 0.00, CoverageType: CoverageReservedInstance, RICoverage: 2.00},
				"i-003": {ShelfPrice: 2.00, EffectiveCost: 0.00, CoverageType: CoverageReservedInstance, RICoverage: 2.00},
				"i-004": {ShelfPrice: 2.00, EffectiveCost: 0.00, CoverageType: CoverageReservedInstance, RICoverage: 2.00},
				"i-005": {ShelfPrice: 2.00, EffectiveCost: 0.00, CoverageType: CoverageReservedInstance, RICoverage: 2.00},
				// Next 2 instances get full SP coverage (66% discount)
				"i-006": {ShelfPrice: 2.00, EffectiveCost: 0.68, CoverageType: CoverageComputeSavingsPlan, SPCoverage: 1.32},
				"i-007": {ShelfPrice: 2.00, EffectiveCost: 0.68, CoverageType: CoverageComputeSavingsPlan, SPCoverage: 1.32},
				// i-008 gets partial SP coverage (commitment exhausted)
				"i-008": {ShelfPrice: 2.00, EffectiveCost: 1.64, CoverageType: CoverageComputeSavingsPlan, SPCoverage: 0.36},
				// Remaining 7 instances are on-demand (no SP budget left)
				"i-009": {ShelfPrice: 2.00, EffectiveCost: 2.00, CoverageType: CoverageOnDemand, OnDemandCost: 2.00},
				"i-010": {ShelfPrice: 2.00, EffectiveCost: 2.00, CoverageType: CoverageOnDemand, OnDemandCost: 2.00},
				"i-011": {ShelfPrice: 2.00, EffectiveCost: 2.00, CoverageType: CoverageOnDemand, OnDemandCost: 2.00},
				"i-012": {ShelfPrice: 2.00, EffectiveCost: 2.00, CoverageType: CoverageOnDemand, OnDemandCost: 2.00},
				"i-013": {ShelfPrice: 2.00, EffectiveCost: 2.00, CoverageType: CoverageOnDemand, OnDemandCost: 2.00},
				"i-014": {ShelfPrice: 2.00, EffectiveCost: 2.00, CoverageType: CoverageOnDemand, OnDemandCost: 2.00},
				"i-015": {ShelfPrice: 2.00, EffectiveCost: 2.00, CoverageType: CoverageOnDemand, OnDemandCost: 2.00},
			},
			expectedTotalShelfPrice: 30.00, // 15 * $2.00
			// Total cost: (5 RI * $0) + (2 SP * $0.68) + (1 partial * $1.64) + (7 OD * $2.00)
			//           = 0 + 1.36 + 1.64 + 14 = 17
			expectedTotalEstimatedCost: 17.00,
			expectedTotalSavings:       13.00, // (5 RI * $2.00) + (2.27 SP * ~$1.32) = 10 + 3 = 13
			expectedTotalRICoverage:    10.00, // 5 * $2.00
			expectedTotalSPCoverage:    3.00,  // $1.32 + $1.32 + $0.36 (discount amounts)
		},
		{
			name: "Scenario 3: RI coverage with spot instances (spot should NOT get RI coverage)",
			description: "5 RIs for m5.2xlarge, 10 m5.2xlarge on-demand, 10 m5.xlarge spot. " +
				"Spot instances should NOT get RI/SP coverage.",
			instances: []aws.Instance{
				// 10 m5.2xlarge on-demand instances
				newTestInstance("i-od-001", "m5.2xlarge", "us-west-2a", "on-demand", baseTime.Add(1*time.Hour)),
				newTestInstance("i-od-002", "m5.2xlarge", "us-west-2a", "on-demand", baseTime.Add(2*time.Hour)),
				newTestInstance("i-od-003", "m5.2xlarge", "us-west-2a", "on-demand", baseTime.Add(3*time.Hour)),
				newTestInstance("i-od-004", "m5.2xlarge", "us-west-2a", "on-demand", baseTime.Add(4*time.Hour)),
				newTestInstance("i-od-005", "m5.2xlarge", "us-west-2a", "on-demand", baseTime.Add(5*time.Hour)),
				newTestInstance("i-od-006", "m5.2xlarge", "us-west-2a", "on-demand", baseTime.Add(6*time.Hour)),
				newTestInstance("i-od-007", "m5.2xlarge", "us-west-2a", "on-demand", baseTime.Add(7*time.Hour)),
				newTestInstance("i-od-008", "m5.2xlarge", "us-west-2a", "on-demand", baseTime.Add(8*time.Hour)),
				newTestInstance("i-od-009", "m5.2xlarge", "us-west-2a", "on-demand", baseTime.Add(9*time.Hour)),
				newTestInstance("i-od-010", "m5.2xlarge", "us-west-2a", "on-demand", baseTime.Add(10*time.Hour)),
				// 10 m5.xlarge spot instances (should NOT get RI coverage even though same family)
				newTestInstance("i-spot-001", "m5.xlarge", "us-west-2a", "spot", baseTime.Add(11*time.Hour)),
				newTestInstance("i-spot-002", "m5.xlarge", "us-west-2a", "spot", baseTime.Add(12*time.Hour)),
				newTestInstance("i-spot-003", "m5.xlarge", "us-west-2a", "spot", baseTime.Add(13*time.Hour)),
				newTestInstance("i-spot-004", "m5.xlarge", "us-west-2a", "spot", baseTime.Add(14*time.Hour)),
				newTestInstance("i-spot-005", "m5.xlarge", "us-west-2a", "spot", baseTime.Add(15*time.Hour)),
				newTestInstance("i-spot-006", "m5.xlarge", "us-west-2a", "spot", baseTime.Add(16*time.Hour)),
				newTestInstance("i-spot-007", "m5.xlarge", "us-west-2a", "spot", baseTime.Add(17*time.Hour)),
				newTestInstance("i-spot-008", "m5.xlarge", "us-west-2a", "spot", baseTime.Add(18*time.Hour)),
				newTestInstance("i-spot-009", "m5.xlarge", "us-west-2a", "spot", baseTime.Add(19*time.Hour)),
				newTestInstance("i-spot-010", "m5.xlarge", "us-west-2a", "spot", baseTime.Add(20*time.Hour)),
			},
			reservedInstances: []aws.ReservedInstance{
				// 5 RIs for m5.2xlarge (should only match m5.2xlarge, not m5.xlarge)
				newTestRI("m5.2xlarge", "us-west-2a", 5),
			},
			savingsPlans: []aws.SavingsPlan{},
			expectedCosts: map[string]expectedInstanceCost{
				// First 5 m5.2xlarge instances should be RI-covered
				"i-od-001": {ShelfPrice: 2.00, EffectiveCost: 0.00, CoverageType: CoverageReservedInstance, RICoverage: 2.00},
				"i-od-002": {ShelfPrice: 2.00, EffectiveCost: 0.00, CoverageType: CoverageReservedInstance, RICoverage: 2.00},
				"i-od-003": {ShelfPrice: 2.00, EffectiveCost: 0.00, CoverageType: CoverageReservedInstance, RICoverage: 2.00},
				"i-od-004": {ShelfPrice: 2.00, EffectiveCost: 0.00, CoverageType: CoverageReservedInstance, RICoverage: 2.00},
				"i-od-005": {ShelfPrice: 2.00, EffectiveCost: 0.00, CoverageType: CoverageReservedInstance, RICoverage: 2.00},
				// Remaining 5 m5.2xlarge instances should be on-demand
				"i-od-006": {ShelfPrice: 2.00, EffectiveCost: 2.00, CoverageType: CoverageOnDemand, OnDemandCost: 2.00},
				"i-od-007": {ShelfPrice: 2.00, EffectiveCost: 2.00, CoverageType: CoverageOnDemand, OnDemandCost: 2.00},
				"i-od-008": {ShelfPrice: 2.00, EffectiveCost: 2.00, CoverageType: CoverageOnDemand, OnDemandCost: 2.00},
				"i-od-009": {ShelfPrice: 2.00, EffectiveCost: 2.00, CoverageType: CoverageOnDemand, OnDemandCost: 2.00},
				"i-od-010": {ShelfPrice: 2.00, EffectiveCost: 2.00, CoverageType: CoverageOnDemand, OnDemandCost: 2.00},
				// All 10 m5.xlarge spot instances should be spot-priced (not RI/SP covered)
				"i-spot-001": {ShelfPrice: 1.00, EffectiveCost: 0.50, CoverageType: CoverageSpot, SpotPrice: 0.50, IsSpot: true},
				"i-spot-002": {ShelfPrice: 1.00, EffectiveCost: 0.50, CoverageType: CoverageSpot, SpotPrice: 0.50, IsSpot: true},
				"i-spot-003": {ShelfPrice: 1.00, EffectiveCost: 0.50, CoverageType: CoverageSpot, SpotPrice: 0.50, IsSpot: true},
				"i-spot-004": {ShelfPrice: 1.00, EffectiveCost: 0.50, CoverageType: CoverageSpot, SpotPrice: 0.50, IsSpot: true},
				"i-spot-005": {ShelfPrice: 1.00, EffectiveCost: 0.50, CoverageType: CoverageSpot, SpotPrice: 0.50, IsSpot: true},
				"i-spot-006": {ShelfPrice: 1.00, EffectiveCost: 0.50, CoverageType: CoverageSpot, SpotPrice: 0.50, IsSpot: true},
				"i-spot-007": {ShelfPrice: 1.00, EffectiveCost: 0.50, CoverageType: CoverageSpot, SpotPrice: 0.50, IsSpot: true},
				"i-spot-008": {ShelfPrice: 1.00, EffectiveCost: 0.50, CoverageType: CoverageSpot, SpotPrice: 0.50, IsSpot: true},
				"i-spot-009": {ShelfPrice: 1.00, EffectiveCost: 0.50, CoverageType: CoverageSpot, SpotPrice: 0.50, IsSpot: true},
				"i-spot-010": {ShelfPrice: 1.00, EffectiveCost: 0.50, CoverageType: CoverageSpot, SpotPrice: 0.50, IsSpot: true},
			},
			expectedTotalShelfPrice:    30.00, // (10 * $2.00) + (10 * $1.00)
			expectedTotalEstimatedCost: 15.00, // (5 OD * $2.00) + (10 spot * $0.50)
			expectedTotalSavings:       15.00, // (5 RI * $2.00) + (10 spot * $0.50 savings)
			expectedTotalRICoverage:    10.00, // 5 * $2.00
			expectedTotalSPCoverage:    0.00,
		},
		{
			name:        "Scenario 4: Spot instances with SP available (spot should NOT use SP)",
			description: "10 m5.xlarge spot instances, SP commitment available. Spot should pay spot price, NOT use SP.",
			instances: []aws.Instance{
				// 10 m5.xlarge spot instances
				newTestInstance("i-spot-001", "m5.xlarge", "us-west-2a", "spot", baseTime.Add(1*time.Hour)),
				newTestInstance("i-spot-002", "m5.xlarge", "us-west-2a", "spot", baseTime.Add(2*time.Hour)),
				newTestInstance("i-spot-003", "m5.xlarge", "us-west-2a", "spot", baseTime.Add(3*time.Hour)),
				newTestInstance("i-spot-004", "m5.xlarge", "us-west-2a", "spot", baseTime.Add(4*time.Hour)),
				newTestInstance("i-spot-005", "m5.xlarge", "us-west-2a", "spot", baseTime.Add(5*time.Hour)),
				newTestInstance("i-spot-006", "m5.xlarge", "us-west-2a", "spot", baseTime.Add(6*time.Hour)),
				newTestInstance("i-spot-007", "m5.xlarge", "us-west-2a", "spot", baseTime.Add(7*time.Hour)),
				newTestInstance("i-spot-008", "m5.xlarge", "us-west-2a", "spot", baseTime.Add(8*time.Hour)),
				newTestInstance("i-spot-009", "m5.xlarge", "us-west-2a", "spot", baseTime.Add(9*time.Hour)),
				newTestInstance("i-spot-010", "m5.xlarge", "us-west-2a", "spot", baseTime.Add(10*time.Hour)),
			},
			reservedInstances: []aws.ReservedInstance{},
			savingsPlans: []aws.SavingsPlan{
				// Large SP commitment that COULD cover all instances, but shouldn't apply to spot
				newTestComputeSP("sp-001", 10.00), // $10/hr commitment
			},
			expectedCosts: map[string]expectedInstanceCost{
				// All spot instances should pay spot price, NOT use SP
				"i-spot-001": {ShelfPrice: 1.00, EffectiveCost: 0.50, CoverageType: CoverageSpot, SpotPrice: 0.50, IsSpot: true},
				"i-spot-002": {ShelfPrice: 1.00, EffectiveCost: 0.50, CoverageType: CoverageSpot, SpotPrice: 0.50, IsSpot: true},
				"i-spot-003": {ShelfPrice: 1.00, EffectiveCost: 0.50, CoverageType: CoverageSpot, SpotPrice: 0.50, IsSpot: true},
				"i-spot-004": {ShelfPrice: 1.00, EffectiveCost: 0.50, CoverageType: CoverageSpot, SpotPrice: 0.50, IsSpot: true},
				"i-spot-005": {ShelfPrice: 1.00, EffectiveCost: 0.50, CoverageType: CoverageSpot, SpotPrice: 0.50, IsSpot: true},
				"i-spot-006": {ShelfPrice: 1.00, EffectiveCost: 0.50, CoverageType: CoverageSpot, SpotPrice: 0.50, IsSpot: true},
				"i-spot-007": {ShelfPrice: 1.00, EffectiveCost: 0.50, CoverageType: CoverageSpot, SpotPrice: 0.50, IsSpot: true},
				"i-spot-008": {ShelfPrice: 1.00, EffectiveCost: 0.50, CoverageType: CoverageSpot, SpotPrice: 0.50, IsSpot: true},
				"i-spot-009": {ShelfPrice: 1.00, EffectiveCost: 0.50, CoverageType: CoverageSpot, SpotPrice: 0.50, IsSpot: true},
				"i-spot-010": {ShelfPrice: 1.00, EffectiveCost: 0.50, CoverageType: CoverageSpot, SpotPrice: 0.50, IsSpot: true},
			},
			expectedTotalShelfPrice:    10.00, // 10 * $1.00
			expectedTotalEstimatedCost: 5.00,  // 10 * $0.50
			expectedTotalSavings:       5.00,  // 10 * $0.50
			expectedTotalRICoverage:    0.00,
			expectedTotalSPCoverage:    0.00, // SP should NOT be used
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calc := NewCalculator()

			// Build pricing maps with simple pricing
			onDemandPrices := map[string]float64{
				"m5.2xlarge:us-west-2": 2.00,
				"m5.xlarge:us-west-2":  1.00,
				"c5.xlarge:us-west-2":  1.00,
				"t3.medium:us-west-2":  0.50,
			}

			spotPrices := map[string]float64{
				"m5.xlarge:us-west-2a": 0.50,
				"m5.xlarge:us-west-2b": 0.50,
				"c5.xlarge:us-west-2a": 0.40,
				"c5.xlarge:us-west-2b": 0.40,
				"t3.medium:us-west-2a": 0.20,
				"t3.medium:us-west-2b": 0.20,
			}

			input := CalculationInput{
				Instances:         tt.instances,
				ReservedInstances: tt.reservedInstances,
				SavingsPlans:      tt.savingsPlans,
				OnDemandPrices:    onDemandPrices,
				SpotPrices:        spotPrices,
			}

			result := calc.Calculate(input)

			// Verify per-instance costs
			require.Len(t, result.InstanceCosts, len(tt.expectedCosts), "Number of instance costs should match")

			for instanceID, expected := range tt.expectedCosts {
				actual, exists := result.InstanceCosts[instanceID]
				require.True(t, exists, "Instance %s should exist in results", instanceID)

				// Use InDelta for float comparisons (allow 0.01 delta)
				assert.InDelta(t, expected.ShelfPrice, actual.ShelfPrice, 0.01,
					"Instance %s: ShelfPrice mismatch", instanceID)
				assert.InDelta(t, expected.EffectiveCost, actual.EffectiveCost, 0.01,
					"Instance %s: EffectiveCost mismatch", instanceID)
				assert.Equal(t, expected.CoverageType, actual.CoverageType,
					"Instance %s: CoverageType mismatch", instanceID)

				if expected.RICoverage > 0 {
					assert.InDelta(t, expected.RICoverage, actual.RICoverage, 0.01,
						"Instance %s: RICoverage mismatch", instanceID)
				}
				if expected.SPCoverage > 0 {
					assert.InDelta(t, expected.SPCoverage, actual.SavingsPlanCoverage, 0.01,
						"Instance %s: SavingsPlanCoverage mismatch", instanceID)
				}
				if expected.OnDemandCost > 0 {
					assert.InDelta(t, expected.OnDemandCost, actual.OnDemandCost, 0.01,
						"Instance %s: OnDemandCost mismatch", instanceID)
				}
				if expected.SpotPrice > 0 {
					assert.InDelta(t, expected.SpotPrice, actual.SpotPrice, 0.01,
						"Instance %s: SpotPrice mismatch", instanceID)
				}
				if expected.IsSpot {
					assert.True(t, actual.IsSpot,
						"Instance %s: should be marked as spot", instanceID)
				}
			}

			// Verify aggregate totals
			assert.InDelta(t, tt.expectedTotalShelfPrice, result.TotalShelfPrice, 0.01,
				"TotalShelfPrice mismatch")
			assert.InDelta(t, tt.expectedTotalEstimatedCost, result.TotalEstimatedCost, 0.01,
				"TotalEstimatedCost mismatch")
			assert.InDelta(t, tt.expectedTotalSavings, result.TotalSavings, 0.01,
				"TotalSavings mismatch")

			// Calculate and verify additional totals from instance costs
			var actualTotalRICoverage, actualTotalSPCoverage float64
			for _, cost := range result.InstanceCosts {
				actualTotalRICoverage += cost.RICoverage
				actualTotalSPCoverage += cost.SavingsPlanCoverage
			}
			assert.InDelta(t, tt.expectedTotalRICoverage, actualTotalRICoverage, 0.01,
				"TotalRICoverage mismatch")
			assert.InDelta(t, tt.expectedTotalSPCoverage, actualTotalSPCoverage, 0.01,
				"TotalSPCoverage mismatch")
		})
	}
}

// expectedInstanceCost defines the expected cost values for a single instance in tests
type expectedInstanceCost struct {
	ShelfPrice    float64
	EffectiveCost float64
	CoverageType  CoverageType
	RICoverage    float64
	SPCoverage    float64
	OnDemandCost  float64
	SpotPrice     float64
	IsSpot        bool
}

// Helper functions for creating test data with simple, understandable values

//nolint:unparam // az parameter varies in different test scenarios
func newTestInstance(id, instanceType, az, lifecycle string, launchTime time.Time) aws.Instance {
	region := "us-west-2" // Extract region from AZ
	return aws.Instance{
		InstanceID:       id,
		InstanceType:     instanceType,
		Region:           region,
		AccountID:        "111111111111",
		AvailabilityZone: az,
		State:            "running",
		Lifecycle:        lifecycle,
		LaunchTime:       launchTime,
	}
}

func newTestRI(instanceType, az string, count int) aws.ReservedInstance {
	region := "us-west-2"
	return aws.ReservedInstance{
		ReservedInstanceID: "ri-" + instanceType + "-" + az,
		InstanceType:       instanceType,
		AvailabilityZone:   az,
		Region:             region,
		AccountID:          "111111111111",
		InstanceCount:      int32(count),
		State:              "active",
	}
}

func newTestComputeSP(arn string, commitment float64) aws.SavingsPlan {
	return aws.SavingsPlan{
		SavingsPlanARN:  "arn:aws:savingsplans::111111111111:savingsplan/" + arn,
		SavingsPlanType: "Compute",
		Region:          "us-west-2",
		InstanceFamily:  "", // Compute SPs don't restrict by family
		Commitment:      commitment,
		AccountID:       "111111111111",
	}
}
