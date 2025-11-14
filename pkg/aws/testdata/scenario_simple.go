// Copyright 2025 Lumina Contributors
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

package testdata

import (
	"time"

	"github.com/nextdoor/lumina/pkg/aws"
)

// SimpleScenario represents a basic single-account, single-region setup
// with a mix of on-demand instances, RIs, and Savings Plans.
//
// Environment:
// - 1 AWS account (Production)
// - 1 region (us-west-2)
// - 10 instances total:
//   - 5 covered by Reserved Instances
//   - 3 covered by Compute Savings Plan
//   - 2 running on-demand
//
// Expected Outcomes:
// - Monthly cost should reflect RI discounts (~40% off on-demand)
// - SP should show ~66% utilization (2 hours used out of 3 committed)
// - RI should show 100% utilization (all RIs actively used)
var SimpleScenario = Scenario{
	Name:        "simple",
	Description: "Single account with basic RI and SP coverage",
	Accounts: []Account{
		{
			ID:     "111111111111",
			Name:   "production",
			Region: "us-west-2",
			Instances: []aws.Instance{
				// Reserved Instance covered (5 instances)
				{
					InstanceID:   "i-ri-001",
					InstanceType: "m5.xlarge",
					Region:       "us-west-2",
					AvailabilityZone: "us-west-2a",
					State:        "running",
					LaunchTime:   time.Now().Add(-30 * 24 * time.Hour),
					Platform:     "Linux/UNIX",
					Tags: map[string]string{
						"Name":        "web-server-1",
						"Environment": "production",
						"Team":        "platform",
					},
				},
				{
					InstanceID:   "i-ri-002",
					InstanceType: "m5.xlarge",
					Region:       "us-west-2",
					AvailabilityZone: "us-west-2b",
					State:        "running",
					LaunchTime:   time.Now().Add(-30 * 24 * time.Hour),
					Platform:     "Linux/UNIX",
					Tags: map[string]string{
						"Name":        "web-server-2",
						"Environment": "production",
						"Team":        "platform",
					},
				},
				{
					InstanceID:   "i-ri-003",
					InstanceType: "m5.xlarge",
					Region:       "us-west-2",
					AvailabilityZone: "us-west-2c",
					State:        "running",
					LaunchTime:   time.Now().Add(-30 * 24 * time.Hour),
					Platform:     "Linux/UNIX",
					Tags: map[string]string{
						"Name":        "web-server-3",
						"Environment": "production",
						"Team":        "platform",
					},
				},
				{
					InstanceID:   "i-ri-004",
					InstanceType: "c5.2xlarge",
					Region:       "us-west-2",
					AvailabilityZone: "us-west-2a",
					State:        "running",
					LaunchTime:   time.Now().Add(-15 * 24 * time.Hour),
					Platform:     "Linux/UNIX",
					Tags: map[string]string{
						"Name":        "app-server-1",
						"Environment": "production",
						"Team":        "backend",
					},
				},
				{
					InstanceID:   "i-ri-005",
					InstanceType: "c5.2xlarge",
					Region:       "us-west-2",
					AvailabilityZone: "us-west-2b",
					State:        "running",
					LaunchTime:   time.Now().Add(-15 * 24 * time.Hour),
					Platform:     "Linux/UNIX",
					Tags: map[string]string{
						"Name":        "app-server-2",
						"Environment": "production",
						"Team":        "backend",
					},
				},

				// Savings Plan covered (3 instances)
				{
					InstanceID:   "i-sp-001",
					InstanceType: "m5.2xlarge",
					Region:       "us-west-2",
					AvailabilityZone: "us-west-2a",
					State:        "running",
					LaunchTime:   time.Now().Add(-10 * 24 * time.Hour),
					Platform:     "Linux/UNIX",
					Tags: map[string]string{
						"Name":        "data-processor-1",
						"Environment": "production",
						"Team":        "data",
					},
				},
				{
					InstanceID:   "i-sp-002",
					InstanceType: "m5.2xlarge",
					Region:       "us-west-2",
					AvailabilityZone: "us-west-2b",
					State:        "running",
					LaunchTime:   time.Now().Add(-10 * 24 * time.Hour),
					Platform:     "Linux/UNIX",
					Tags: map[string]string{
						"Name":        "data-processor-2",
						"Environment": "production",
						"Team":        "data",
					},
				},
				{
					InstanceID:   "i-sp-003",
					InstanceType: "r5.xlarge",
					Region:       "us-west-2",
					AvailabilityZone: "us-west-2c",
					State:        "running",
					LaunchTime:   time.Now().Add(-5 * 24 * time.Hour),
					Platform:     "Linux/UNIX",
					Tags: map[string]string{
						"Name":        "cache-server-1",
						"Environment": "production",
						"Team":        "infrastructure",
					},
				},

				// On-demand (2 instances - recent launches, not covered yet)
				{
					InstanceID:   "i-od-001",
					InstanceType: "t3.large",
					Region:       "us-west-2",
					AvailabilityZone: "us-west-2a",
					State:        "running",
					LaunchTime:   time.Now().Add(-2 * 24 * time.Hour),
					Platform:     "Linux/UNIX",
					Tags: map[string]string{
						"Name":        "temp-builder-1",
						"Environment": "ci",
						"Team":        "devops",
					},
				},
				{
					InstanceID:   "i-od-002",
					InstanceType: "t3.large",
					Region:       "us-west-2",
					AvailabilityZone: "us-west-2b",
					State:        "running",
					LaunchTime:   time.Now().Add(-1 * 24 * time.Hour),
					Platform:     "Linux/UNIX",
					Tags: map[string]string{
						"Name":        "temp-builder-2",
						"Environment": "ci",
						"Team":        "devops",
					},
				},
			},

			// Reserved Instances covering 5 instances
			ReservedInstances: []aws.ReservedInstance{
				{
					ReservedInstanceID: "ri-001",
					InstanceType:       "m5.xlarge",
					InstanceCount:      3, // Covers i-ri-001, i-ri-002, i-ri-003
					Region:             "us-west-2",
					AvailabilityZone:   "", // Regional RI
					State:              "active",
					Start:              time.Now().Add(-365 * 24 * time.Hour),
					End:                time.Now().Add(2 * 365 * 24 * time.Hour),
					OfferingClass:      "standard",
					OfferingType:       "All Upfront",
					Platform:           "Linux/UNIX",
				},
				{
					ReservedInstanceID: "ri-002",
					InstanceType:       "c5.2xlarge",
					InstanceCount:      2, // Covers i-ri-004, i-ri-005
					Region:             "us-west-2",
					AvailabilityZone:   "", // Regional RI
					State:              "active",
					Start:              time.Now().Add(-180 * 24 * time.Hour),
					End:                time.Now().Add(2*365*24*time.Hour - 180*24*time.Hour),
					OfferingClass:      "standard",
					OfferingType:       "Partial Upfront",
					Platform:           "Linux/UNIX",
				},
			},

			// Compute Savings Plan
			SavingsPlans: []aws.SavingsPlan{
				{
					SavingsPlanARN:  "arn:aws:savingsplans::111111111111:savingsplan/sp-001",
					SavingsPlanType: "ComputeSavingsPlans",
					State:           "active",
					Start:           time.Now().Add(-90 * 24 * time.Hour),
					End:             time.Now().Add(2*365*24*time.Hour - 90*24*time.Hour),
					Commitment:      2.0, // $2/hour commitment
					
					Region:          "", // Compute SP is region-flexible
					InstanceFamily: "", // Compute SP is instance-family flexible
				},
			},

			// Spot prices (for reference, not used in this scenario)
			SpotPrices: []aws.SpotPrice{
				{
					InstanceType:     "m5.xlarge",
					AvailabilityZone: "us-west-2a",
					SpotPrice:        0.0576, // ~30% discount from on-demand
					Timestamp:        time.Now(),
				},
				{
					InstanceType:     "c5.2xlarge",
					AvailabilityZone: "us-west-2a",
					SpotPrice:        0.102, // ~30% discount from on-demand
					Timestamp:        time.Now(),
				},
			},
		},
	},

	Expected: ExpectedOutcomes{
		// Rough calculations:
		// - 3x m5.xlarge RI: $0.192/hr * 3 * 0.6 (40% discount) * 730hr = ~$251
		// - 2x c5.2xlarge RI: $0.34/hr * 2 * 0.6 (40% discount) * 730hr = ~$297
		// - 2x m5.2xlarge SP: $0.384/hr * 2 * 0.67 (33% discount) * 730hr = ~$376
		// - 1x r5.xlarge SP: $0.252/hr * 0.67 * 730hr = ~$123
		// - 2x t3.large OD: $0.0832/hr * 2 * 730hr = ~$121
		// Total: ~$1168/month
		TotalMonthlyCost: 1168.0,

		CostByAccount: map[string]float64{
			"111111111111": 1168.0,
		},

		CostByRegion: map[string]float64{
			"us-west-2": 1168.0,
		},

		// 3 instances using $2/hr commitment, but commitment could cover more
		// Actual usage: 3 instances * average cost = ~$0.384 + $0.384 + $0.252 = ~$1.02/hr
		// Commitment: $2/hr
		// Utilization: ~51%
		SavingsPlanUtilization: 51.0,

		// All 5 RIs actively used
		ReservedInstanceUtilization: 100.0,
	},
}
