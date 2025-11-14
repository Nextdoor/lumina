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
	"fmt"
	"time"

	"github.com/nextdoor/lumina/pkg/aws"
)

// ComplexScenario represents a realistic multi-account, multi-region enterprise setup
// with organization-wide Savings Plans, per-account RIs, spot instances, and on-demand.
//
// Environment:
// - 3 AWS accounts (Production, Staging, Development)
// - 3 regions (us-west-2, us-east-1, eu-west-1)
// - 50+ instances total with various coverage:
//   - Organization-level Compute Savings Plan
//   - Account-specific EC2 Instance Savings Plans
//   - Regional and zonal Reserved Instances
//   - Spot instances for batch workloads
//   - On-demand for burst capacity
//
// This scenario tests cross-account SP allocation, regional RI usage,
// and complex cost attribution scenarios.
var ComplexScenario = Scenario{
	Name:        "complex",
	Description: "Multi-account enterprise with organization-wide Savings Plans",
	Accounts: []Account{
		// Production Account - Primary workloads
		{
			ID:     "111111111111",
			Name:   "production",
			Region: "us-west-2",
			Instances: append(
				// US-West-2: Primary production region
				productionUSWest2Instances(),
				// US-East-1: DR and latency-sensitive workloads
				productionUSEast1Instances()...,
			),
			ReservedInstances: append(
				productionUSWest2RIs(),
				productionUSEast1RIs()...,
			),
			SavingsPlans: productionSavingsPlans(),
			SpotPrices:   commonSpotPrices(),
		},

		// Staging Account - Pre-production testing
		{
			ID:     "222222222222",
			Name:   "staging",
			Region: "us-west-2",
			Instances: append(
				stagingUSWest2Instances(),
				stagingEUWest1Instances()...,
			),
			ReservedInstances: stagingRIs(),
			SavingsPlans:      []aws.SavingsPlan{}, // Uses org-level SP
			SpotPrices:        commonSpotPrices(),
		},

		// Development Account - Developer workloads
		{
			ID:     "333333333333",
			Name:   "development",
			Region: "us-west-2",
			Instances: append(
				developmentUSWest2Instances(),
				// Dev uses spot for cost savings
				developmentSpotInstances()...,
			),
			ReservedInstances: []aws.ReservedInstance{}, // Mostly spot and on-demand
			SavingsPlans:      []aws.SavingsPlan{},      // Uses org-level SP
			SpotPrices:        commonSpotPrices(),
		},
	},

	Expected: ExpectedOutcomes{
		// Total monthly cost across all accounts and regions
		// This will be calculated once cost calculation logic is implemented
		TotalMonthlyCost: 15000.0, // Placeholder

		CostByAccount: map[string]float64{
			"111111111111": 10000.0, // Production: 66% of total
			"222222222222": 3000.0,  // Staging: 20% of total
			"333333333333": 2000.0,  // Development: 14% of total
		},

		CostByRegion: map[string]float64{
			"us-west-2": 8000.0,  // 53% - Primary region
			"us-east-1": 5000.0,  // 33% - DR region
			"eu-west-1": 2000.0,  // 14% - International
		},

		// Org-wide Compute SP should show high utilization
		SavingsPlanUtilization: 85.0,

		// Account-specific EC2 Instance SP and RIs
		ReservedInstanceUtilization: 92.0,
	},
}

// Production US-West-2: Web tier, application tier, data tier
func productionUSWest2Instances() []aws.Instance {
	now := time.Now()
	instances := []aws.Instance{}

	// Web tier: 10x m5.xlarge behind load balancer
	for i := 1; i <= 10; i++ {
		instances = append(instances, aws.Instance{
			InstanceID:       fmt.Sprintf("i-prod-web-%02d", i),
			InstanceType:     "m5.xlarge",
			Region:           "us-west-2",
			AvailabilityZone: fmt.Sprintf("us-west-2%c", 'a'+((i-1)%3)),
			State:            "running",
			LaunchTime:       now.Add(-60 * 24 * time.Hour),
			Platform:         "Linux/UNIX",
			Tags: map[string]string{
				"Name":        fmt.Sprintf("web-server-%02d", i),
				"Environment": "production",
				"Tier":        "web",
				"Team":        "platform",
			},
		})
	}

	// Application tier: 8x c5.2xlarge (compute-intensive)
	for i := 1; i <= 8; i++ {
		instances = append(instances, aws.Instance{
			InstanceID:       fmt.Sprintf("i-prod-app-%02d", i),
			InstanceType:     "c5.2xlarge",
			Region:           "us-west-2",
			AvailabilityZone: fmt.Sprintf("us-west-2%c", 'a'+((i-1)%3)),
			State:            "running",
			LaunchTime:       now.Add(-45 * 24 * time.Hour),
			Platform:         "Linux/UNIX",
			Tags: map[string]string{
				"Name":        fmt.Sprintf("app-server-%02d", i),
				"Environment": "production",
				"Tier":        "application",
				"Team":        "backend",
			},
		})
	}

	// Data tier: 4x r5.2xlarge (memory-intensive)
	for i := 1; i <= 4; i++ {
		instances = append(instances, aws.Instance{
			InstanceID:       fmt.Sprintf("i-prod-data-%02d", i),
			InstanceType:     "r5.2xlarge",
			Region:           "us-west-2",
			AvailabilityZone: fmt.Sprintf("us-west-2%c", 'a'+((i-1)%3)),
			State:            "running",
			LaunchTime:       now.Add(-90 * 24 * time.Hour),
			Platform:         "Linux/UNIX",
			Tags: map[string]string{
				"Name":        fmt.Sprintf("database-%02d", i),
				"Environment": "production",
				"Tier":        "data",
				"Team":        "data-platform",
			},
		})
	}

	return instances
}

// Production US-East-1: DR failover capacity
func productionUSEast1Instances() []aws.Instance {
	now := time.Now()
	instances := []aws.Instance{}

	// DR web tier: 6x m5.xlarge (60% of primary)
	for i := 1; i <= 6; i++ {
		instances = append(instances, aws.Instance{
			InstanceID:       fmt.Sprintf("i-prod-dr-web-%02d", i),
			InstanceType:     "m5.xlarge",
			Region:           "us-east-1",
			AvailabilityZone: fmt.Sprintf("us-east-1%c", 'a'+((i-1)%3)),
			State:            "running",
			LaunchTime:       now.Add(-30 * 24 * time.Hour),
			Platform:         "Linux/UNIX",
			Tags: map[string]string{
				"Name":        fmt.Sprintf("dr-web-server-%02d", i),
				"Environment": "production",
				"Tier":        "web",
				"Team":        "platform",
				"Purpose":     "disaster-recovery",
			},
		})
	}

	// DR app tier: 5x c5.2xlarge
	for i := 1; i <= 5; i++ {
		instances = append(instances, aws.Instance{
			InstanceID:       fmt.Sprintf("i-prod-dr-app-%02d", i),
			InstanceType:     "c5.2xlarge",
			Region:           "us-east-1",
			AvailabilityZone: fmt.Sprintf("us-east-1%c", 'a'+((i-1)%3)),
			State:            "running",
			LaunchTime:       now.Add(-30 * 24 * time.Hour),
			Platform:         "Linux/UNIX",
			Tags: map[string]string{
				"Name":        fmt.Sprintf("dr-app-server-%02d", i),
				"Environment": "production",
				"Tier":        "application",
				"Team":        "backend",
				"Purpose":     "disaster-recovery",
			},
		})
	}

	return instances
}

// Reserved Instances for Production US-West-2
func productionUSWest2RIs() []aws.ReservedInstance {
	now := time.Now()
	return []aws.ReservedInstance{
		{
			ReservedInstanceID: "ri-prod-usw2-web",
			InstanceType:       "m5.xlarge",
			InstanceCount:      10, // Covers all web servers
			Region:             "us-west-2",
			AvailabilityZone:   "", // Regional RI for flexibility
			State:              "active",
			Start:              now.Add(-200 * 24 * time.Hour),
			End:                now.Add(365*24*time.Hour - 200*24*time.Hour),
			OfferingClass:      "standard",
			OfferingType:       "All Upfront",
			Platform:           "Linux/UNIX",
		},
		{
			ReservedInstanceID: "ri-prod-usw2-app",
			InstanceType:       "c5.2xlarge",
			InstanceCount:      8, // Covers all app servers
			Region:             "us-west-2",
			AvailabilityZone:   "",
			State:              "active",
			Start:              now.Add(-150 * 24 * time.Hour),
			End:                now.Add(3*365*24*time.Hour - 150*24*time.Hour),
			OfferingClass:      "standard",
			OfferingType:       "Partial Upfront",
			Platform:           "Linux/UNIX",
		},
	}
}

// Reserved Instances for Production US-East-1 (DR)
func productionUSEast1RIs() []aws.ReservedInstance {
	now := time.Now()
	return []aws.ReservedInstance{
		{
			ReservedInstanceID: "ri-prod-use1-dr",
			InstanceType:       "m5.xlarge",
			InstanceCount:      6, // Covers DR web servers
			Region:             "us-east-1",
			AvailabilityZone:   "",
			State:              "active",
			Start:              now.Add(-100 * 24 * time.Hour),
			End:                now.Add(365*24*time.Hour - 100*24*time.Hour),
			OfferingClass:      "standard",
			OfferingType:       "All Upfront",
			Platform:           "Linux/UNIX",
		},
	}
}

// Organization-level and account-specific Savings Plans for Production
func productionSavingsPlans() []aws.SavingsPlan {
	now := time.Now()
	return []aws.SavingsPlan{
		// Organization-wide Compute SP (applies across all accounts)
		{
			SavingsPlanARN:    "arn:aws:savingsplans::111111111111:savingsplan/org-compute-sp",
			SavingsPlanType:   "ComputeSavingsPlans",
			State:             "active",
			Start:             now.Add(-180 * 24 * time.Hour),
			End:               now.Add(2*365*24*time.Hour - 180*24*time.Hour),
			Commitment:        10.0, // $10/hour org-wide commitment
			
			Region:            "",  // Compute SP is region-flexible
			InstanceFamily: "",  // Compute SP is instance-family flexible
		},
		// EC2 Instance SP for memory workloads
		{
			SavingsPlanARN:    "arn:aws:savingsplans::111111111111:savingsplan/prod-ec2-instance-sp",
			SavingsPlanType:   "EC2InstanceSavingsPlans",
			State:             "active",
			Start:             now.Add(-90 * 24 * time.Hour),
			End:               now.Add(365*24*time.Hour - 90*24*time.Hour),
			Commitment:        2.5, // $2.50/hour for r5 family
			
			Region:            "us-west-2",
			InstanceFamily: "r5",
		},
	}
}

// Staging US-West-2 instances (smaller scale)
func stagingUSWest2Instances() []aws.Instance {
	now := time.Now()
	instances := []aws.Instance{}

	// 3x m5.large web servers
	for i := 1; i <= 3; i++ {
		instances = append(instances, aws.Instance{
			InstanceID:       fmt.Sprintf("i-stg-web-%02d", i),
			InstanceType:     "m5.large",
			Region:           "us-west-2",
			AvailabilityZone: fmt.Sprintf("us-west-2%c", 'a'+((i-1)%2)),
			State:            "running",
			LaunchTime:       now.Add(-20 * 24 * time.Hour),
			Platform:         "Linux/UNIX",
			Tags: map[string]string{
				"Name":        fmt.Sprintf("staging-web-%02d", i),
				"Environment": "staging",
				"Team":        "platform",
			},
		})
	}

	return instances
}

// Staging EU-West-1 instances (international testing)
func stagingEUWest1Instances() []aws.Instance {
	now := time.Now()
	return []aws.Instance{
		{
			InstanceID:       "i-stg-eu-web-01",
			InstanceType:     "m5.large",
			Region:           "eu-west-1",
			AvailabilityZone: "eu-west-1a",
			State:            "running",
			LaunchTime:       now.Add(-15 * 24 * time.Hour),
			Platform:         "Linux/UNIX",
			Tags: map[string]string{
				"Name":        "staging-eu-web-01",
				"Environment": "staging",
				"Region":      "europe",
				"Team":        "platform",
			},
		},
	}
}

// Staging RIs (minimal - mostly relies on org SP)
func stagingRIs() []aws.ReservedInstance {
	now := time.Now()
	return []aws.ReservedInstance{
		{
			ReservedInstanceID: "ri-stg-usw2",
			InstanceType:       "m5.large",
			InstanceCount:      3,
			Region:             "us-west-2",
			AvailabilityZone:   "",
			State:              "active",
			Start:              now.Add(-60 * 24 * time.Hour),
			End:                now.Add(365*24*time.Hour - 60*24*time.Hour),
			OfferingClass:      "standard",
			OfferingType:       "No Upfront",
			Platform:           "Linux/UNIX",
		},
	}
}

// Development US-West-2 instances (on-demand for flexibility)
func developmentUSWest2Instances() []aws.Instance {
	now := time.Now()
	instances := []aws.Instance{}

	// 5x t3.xlarge development workstations
	for i := 1; i <= 5; i++ {
		instances = append(instances, aws.Instance{
			InstanceID:       fmt.Sprintf("i-dev-workstation-%02d", i),
			InstanceType:     "t3.xlarge",
			Region:           "us-west-2",
			AvailabilityZone: "us-west-2a",
			State:            "running",
			LaunchTime:       now.Add(-7 * 24 * time.Hour),
			Platform:         "Linux/UNIX",
			Tags: map[string]string{
				"Name":        fmt.Sprintf("dev-workstation-%02d", i),
				"Environment": "development",
				"Owner":       fmt.Sprintf("developer-%d", i),
				"Team":        "engineering",
			},
		})
	}

	return instances
}

// Development spot instances for batch jobs
func developmentSpotInstances() []aws.Instance {
	now := time.Now()
	instances := []aws.Instance{}

	// 10x c5.xlarge spot instances for CI/CD builds
	for i := 1; i <= 10; i++ {
		instances = append(instances, aws.Instance{
			InstanceID:       fmt.Sprintf("i-dev-spot-ci-%02d", i),
			InstanceType:     "c5.xlarge",
			Region:           "us-west-2",
			AvailabilityZone: fmt.Sprintf("us-west-2%c", 'a'+((i-1)%3)),
			State:            "running",
			LaunchTime:       now.Add(-time.Duration(i) * 12 * time.Hour),
			Platform:         "Linux/UNIX",
			SpotInstanceRequestID: fmt.Sprintf("sir-spot-%02d", i),
			Tags: map[string]string{
				"Name":        fmt.Sprintf("ci-builder-%02d", i),
				"Environment": "development",
				"Purpose":     "ci-cd",
				"Team":        "devops",
				"Lifecycle":   "spot",
			},
		})
	}

	return instances
}

// Common spot price data across all accounts
func commonSpotPrices() []aws.SpotPrice {
	now := time.Now()
	return []aws.SpotPrice{
		// US-West-2
		{InstanceType: "m5.xlarge", AvailabilityZone: "us-west-2a", SpotPrice: 0.0576, Timestamp: now},
		{InstanceType: "m5.xlarge", AvailabilityZone: "us-west-2b", SpotPrice: 0.0580, Timestamp: now},
		{InstanceType: "m5.xlarge", AvailabilityZone: "us-west-2c", SpotPrice: 0.0570, Timestamp: now},
		{InstanceType: "c5.2xlarge", AvailabilityZone: "us-west-2a", SpotPrice: 0.102, Timestamp: now},
		{InstanceType: "c5.2xlarge", AvailabilityZone: "us-west-2b", SpotPrice: 0.105, Timestamp: now},
		{InstanceType: "c5.2xlarge", AvailabilityZone: "us-west-2c", SpotPrice: 0.100, Timestamp: now},
		{InstanceType: "c5.xlarge", AvailabilityZone: "us-west-2a", SpotPrice: 0.051, Timestamp: now},
		{InstanceType: "c5.xlarge", AvailabilityZone: "us-west-2b", SpotPrice: 0.052, Timestamp: now},

		// US-East-1
		{InstanceType: "m5.xlarge", AvailabilityZone: "us-east-1a", SpotPrice: 0.0580, Timestamp: now},
		{InstanceType: "m5.xlarge", AvailabilityZone: "us-east-1b", SpotPrice: 0.0575, Timestamp: now},
		{InstanceType: "c5.2xlarge", AvailabilityZone: "us-east-1a", SpotPrice: 0.103, Timestamp: now},
		{InstanceType: "c5.2xlarge", AvailabilityZone: "us-east-1b", SpotPrice: 0.104, Timestamp: now},

		// EU-West-1
		{InstanceType: "m5.large", AvailabilityZone: "eu-west-1a", SpotPrice: 0.029, Timestamp: now},
		{InstanceType: "m5.large", AvailabilityZone: "eu-west-1b", SpotPrice: 0.030, Timestamp: now},
	}
}
