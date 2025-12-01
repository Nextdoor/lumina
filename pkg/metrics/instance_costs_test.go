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
	"testing"
	"time"

	"github.com/nextdoor/lumina/pkg/cost"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
)

func TestUpdateInstanceCostMetrics_BasicFunctionality(t *testing.T) {
	// Create metrics instance
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	// Create test calculation result with instance costs and SP utilization
	result := cost.CalculationResult{
		InstanceCosts: map[string]cost.InstanceCost{
			"i-abc123": {
				InstanceID:       "i-abc123",
				InstanceType:     "m5.xlarge",
				Region:           "us-west-2",
				AccountID:        "111111111111",
				AccountName:      "test-account",
				AvailabilityZone: "us-west-2a",
				EffectiveCost:    0.15, // Covered by RI
				CoverageType:     cost.CoverageReservedInstance,
				PricingAccuracy:  cost.PricingAccurate, Lifecycle: "on-demand",
			},
			"i-def456": {
				InstanceID:       "i-def456",
				InstanceType:     "c5.2xlarge",
				Region:           "us-east-1",
				AccountID:        "222222222222",
				AccountName:      "test-account",
				AvailabilityZone: "us-east-1b",
				EffectiveCost:    0.10, // Covered by SP
				CoverageType:     cost.CoverageComputeSavingsPlan,
				PricingAccuracy:  cost.PricingAccurate, SavingsPlanCoverage: 0.10, // SP commitment consumed
				Lifecycle: "on-demand",
			},
			"i-ghi789": {
				InstanceID:       "i-ghi789",
				InstanceType:     "t3.medium",
				Region:           "us-west-2",
				AccountID:        "111111111111",
				AccountName:      "test-account",
				AvailabilityZone: "us-west-2b",
				EffectiveCost:    0.0416, // On-demand
				CoverageType:     cost.CoverageOnDemand,
				PricingAccuracy:  cost.PricingAccurate, Lifecycle: "on-demand",
			},
		},
		SavingsPlanUtilization: map[string]cost.SavingsPlanUtilization{
			"arn:aws:savingsplans::111111111111:savingsplan/abc": {
				SavingsPlanARN:         "arn:aws:savingsplans::111111111111:savingsplan/abc",
				AccountID:              "111111111111",
				AccountName:            "test-account",
				Type:                   "EC2Instance",
				HourlyCommitment:       100.00,
				CurrentUtilizationRate: 75.00,
				RemainingCapacity:      25.00,
				UtilizationPercent:     75.0,
			},
			"arn:aws:savingsplans::222222222222:savingsplan/def": {
				SavingsPlanARN:         "arn:aws:savingsplans::222222222222:savingsplan/def",
				AccountID:              "222222222222",
				AccountName:            "test-account",
				Type:                   "Compute",
				HourlyCommitment:       200.00,
				CurrentUtilizationRate: 250.00, // Over-utilized!
				RemainingCapacity:      -50.00, // Negative = spillover to on-demand
				UtilizationPercent:     125.0,  // >100% indicates over-utilization
			},
		},
		CalculatedAt: time.Now(),
	}

	// Update metrics
	m.UpdateInstanceCostMetrics(result, nil)

	// Verify instance cost metrics
	assert.Equal(t, 0.15, testutil.ToFloat64(m.EC2InstanceHourlyCost.With(prometheus.Labels{
		"instance_id":       "i-abc123",
		"account_id":        "111111111111",
		"account_name":      "test-account",
		"region":            "us-west-2",
		"instance_type":     "m5.xlarge",
		"cost_type":         "reserved_instance",
		"availability_zone": "us-west-2a",
		"lifecycle":         "on-demand",
		"pricing_accuracy":  "accurate", "node_name": ""})))

	assert.Equal(t, 0.10, testutil.ToFloat64(m.EC2InstanceHourlyCost.With(prometheus.Labels{
		"instance_id":       "i-def456",
		"account_id":        "222222222222",
		"account_name":      "test-account",
		"region":            "us-east-1",
		"instance_type":     "c5.2xlarge",
		"cost_type":         "compute_savings_plan",
		"availability_zone": "us-east-1b",
		"lifecycle":         "on-demand",
		"pricing_accuracy":  "accurate", "node_name": ""})))

	assert.Equal(t, 0.0416, testutil.ToFloat64(m.EC2InstanceHourlyCost.With(prometheus.Labels{
		"instance_id":       "i-ghi789",
		"account_id":        "111111111111",
		"account_name":      "test-account",
		"region":            "us-west-2",
		"instance_type":     "t3.medium",
		"cost_type":         "on_demand",
		"availability_zone": "us-west-2b",
		"lifecycle":         "on-demand",
		"pricing_accuracy":  "accurate", "node_name": ""})))

	// Verify SP utilization metrics - EC2 Instance SP
	assert.Equal(t, 75.00, testutil.ToFloat64(m.SavingsPlanCurrentUtilizationRate.With(prometheus.Labels{
		"savings_plan_arn": "arn:aws:savingsplans::111111111111:savingsplan/abc",
		"account_id":       "111111111111",
		"account_name":     "test-account",
		"type":             "ec2_instance",
	})))

	assert.Equal(t, 25.00, testutil.ToFloat64(m.SavingsPlanRemainingCapacity.With(prometheus.Labels{
		"savings_plan_arn": "arn:aws:savingsplans::111111111111:savingsplan/abc",
		"account_id":       "111111111111",
		"account_name":     "test-account",
		"type":             "ec2_instance",
	})))

	assert.Equal(t, 75.0, testutil.ToFloat64(m.SavingsPlanUtilizationPercent.With(prometheus.Labels{
		"savings_plan_arn": "arn:aws:savingsplans::111111111111:savingsplan/abc",
		"account_id":       "111111111111",
		"account_name":     "test-account",
		"type":             "ec2_instance",
	})))

	// Verify SP utilization metrics - Compute SP (over-utilized)
	assert.Equal(t, 250.00, testutil.ToFloat64(m.SavingsPlanCurrentUtilizationRate.With(prometheus.Labels{
		"savings_plan_arn": "arn:aws:savingsplans::222222222222:savingsplan/def",
		"account_id":       "222222222222",
		"account_name":     "test-account",
		"type":             "compute",
	})))

	assert.Equal(t, -50.00, testutil.ToFloat64(m.SavingsPlanRemainingCapacity.With(prometheus.Labels{
		"savings_plan_arn": "arn:aws:savingsplans::222222222222:savingsplan/def",
		"account_id":       "222222222222",
		"account_name":     "test-account",
		"type":             "compute",
	})))

	assert.Equal(t, 125.0, testutil.ToFloat64(m.SavingsPlanUtilizationPercent.With(prometheus.Labels{
		"savings_plan_arn": "arn:aws:savingsplans::222222222222:savingsplan/def",
		"account_id":       "222222222222",
		"account_name":     "test-account",
		"type":             "compute",
	})))
}

func TestUpdateInstanceCostMetrics_EmptyResult(t *testing.T) {
	// Create metrics instance
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	// Create empty result
	result := cost.CalculationResult{
		InstanceCosts:          map[string]cost.InstanceCost{},
		SavingsPlanUtilization: map[string]cost.SavingsPlanUtilization{},
		CalculatedAt:           time.Now(),
	}

	// Update metrics - should not panic
	m.UpdateInstanceCostMetrics(result, nil)

	// Verify metrics are empty (no panics, no errors)
	// We can't directly verify metrics are "empty" but we can verify the function doesn't crash
	assert.NotNil(t, m.EC2InstanceHourlyCost)
	assert.NotNil(t, m.SavingsPlanCurrentUtilizationRate)
}

func TestUpdateInstanceCostMetrics_ResetsBetweenUpdates(t *testing.T) {
	// Create metrics instance
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	// First update with two instances
	result1 := cost.CalculationResult{
		InstanceCosts: map[string]cost.InstanceCost{
			"i-abc123": {
				InstanceID:       "i-abc123",
				InstanceType:     "m5.xlarge",
				Region:           "us-west-2",
				AccountID:        "111111111111",
				AccountName:      "test-account",
				AvailabilityZone: "us-west-2a",
				EffectiveCost:    0.15,
				CoverageType:     cost.CoverageOnDemand,
				PricingAccuracy:  cost.PricingAccurate, Lifecycle: "on-demand",
			},
			"i-def456": {
				InstanceID:       "i-def456",
				InstanceType:     "c5.2xlarge",
				Region:           "us-east-1",
				AccountID:        "222222222222",
				AccountName:      "test-account",
				AvailabilityZone: "us-east-1b",
				EffectiveCost:    0.34,
				CoverageType:     cost.CoverageOnDemand,
				PricingAccuracy:  cost.PricingAccurate, Lifecycle: "on-demand",
			},
		},
		SavingsPlanUtilization: map[string]cost.SavingsPlanUtilization{},
		CalculatedAt:           time.Now(),
	}

	m.UpdateInstanceCostMetrics(result1, nil)

	// Verify both instances exist
	assert.Equal(t, 0.15, testutil.ToFloat64(m.EC2InstanceHourlyCost.With(prometheus.Labels{
		"instance_id":       "i-abc123",
		"account_id":        "111111111111",
		"account_name":      "test-account",
		"region":            "us-west-2",
		"instance_type":     "m5.xlarge",
		"cost_type":         "on_demand",
		"availability_zone": "us-west-2a",
		"lifecycle":         "on-demand",
		"pricing_accuracy":  "accurate", "node_name": ""})))

	// Second update with only one instance (i-def456 terminated)
	result2 := cost.CalculationResult{
		InstanceCosts: map[string]cost.InstanceCost{
			"i-abc123": {
				InstanceID:       "i-abc123",
				InstanceType:     "m5.xlarge",
				Region:           "us-west-2",
				AccountID:        "111111111111",
				AccountName:      "test-account",
				AvailabilityZone: "us-west-2a",
				EffectiveCost:    0.15,
				CoverageType:     cost.CoverageOnDemand,
				PricingAccuracy:  cost.PricingAccurate, Lifecycle: "on-demand",
			},
		},
		SavingsPlanUtilization: map[string]cost.SavingsPlanUtilization{},
		CalculatedAt:           time.Now(),
	}

	m.UpdateInstanceCostMetrics(result2, nil)

	// Verify i-abc123 still exists
	assert.Equal(t, 0.15, testutil.ToFloat64(m.EC2InstanceHourlyCost.With(prometheus.Labels{
		"instance_id":       "i-abc123",
		"account_id":        "111111111111",
		"account_name":      "test-account",
		"region":            "us-west-2",
		"instance_type":     "m5.xlarge",
		"cost_type":         "on_demand",
		"availability_zone": "us-west-2a",
		"lifecycle":         "on-demand",
		"pricing_accuracy":  "accurate", "node_name": ""})))

	// Verify i-def456 was removed (metric should be 0 or not exist after reset)
	// After reset and not setting the metric, it should return 0
	assert.Equal(t, 0.0, testutil.ToFloat64(m.EC2InstanceHourlyCost.With(prometheus.Labels{
		"instance_id":       "i-def456",
		"account_id":        "222222222222",
		"account_name":      "test-account",
		"region":            "us-east-1",
		"instance_type":     "c5.2xlarge",
		"cost_type":         "on_demand",
		"availability_zone": "us-east-1b",
		"lifecycle":         "on-demand",
		"pricing_accuracy":  "accurate", "node_name": ""})))
}

func TestUpdateInstanceCostMetrics_AllCoverageTypes(t *testing.T) {
	// Create metrics instance
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	// Create result with all coverage types
	result := cost.CalculationResult{
		InstanceCosts: map[string]cost.InstanceCost{
			"i-ri": {
				InstanceID:       "i-ri",
				InstanceType:     "m5.xlarge",
				Region:           "us-west-2",
				AccountID:        "111111111111",
				AccountName:      "test-account",
				AvailabilityZone: "us-west-2a",
				EffectiveCost:    0.15,
				CoverageType:     cost.CoverageReservedInstance,
				PricingAccuracy:  cost.PricingAccurate, Lifecycle: "on-demand",
			},
			"i-ec2sp": {
				InstanceID:          "i-ec2sp",
				InstanceType:        "m5.2xlarge",
				Region:              "us-west-2",
				AccountID:           "111111111111",
				AccountName:         "test-account",
				AvailabilityZone:    "us-west-2a",
				EffectiveCost:       0.25,
				SavingsPlanCoverage: 0.25, // SP commitment consumed
				CoverageType:        cost.CoverageEC2InstanceSavingsPlan,
				PricingAccuracy:     cost.PricingAccurate, Lifecycle: "on-demand",
			},
			"i-computesp": {
				InstanceID:       "i-computesp",
				InstanceType:     "c5.2xlarge",
				Region:           "us-east-1",
				AccountID:        "222222222222",
				AccountName:      "test-account",
				AvailabilityZone: "us-east-1b",
				EffectiveCost:    0.34,
				CoverageType:     cost.CoverageComputeSavingsPlan,
				PricingAccuracy:  cost.PricingAccurate, SavingsPlanCoverage: 0.34, // SP commitment consumed
				Lifecycle: "on-demand",
			},
			"i-spot": {
				InstanceID:       "i-spot",
				InstanceType:     "c5.xlarge",
				Region:           "us-west-2",
				AccountID:        "111111111111",
				AccountName:      "test-account",
				AvailabilityZone: "us-west-2b",
				EffectiveCost:    0.05,
				CoverageType:     cost.CoverageSpot,
				PricingAccuracy:  cost.PricingAccurate, Lifecycle: "spot",
			},
			"i-od": {
				InstanceID:       "i-od",
				InstanceType:     "t3.medium",
				Region:           "us-west-2",
				AccountID:        "111111111111",
				AccountName:      "test-account",
				AvailabilityZone: "us-west-2c",
				EffectiveCost:    0.0416,
				CoverageType:     cost.CoverageOnDemand,
				PricingAccuracy:  cost.PricingAccurate, Lifecycle: "on-demand",
			},
		},
		SavingsPlanUtilization: map[string]cost.SavingsPlanUtilization{},
		CalculatedAt:           time.Now(),
	}

	// Update metrics
	m.UpdateInstanceCostMetrics(result, nil)

	// Verify all coverage types are properly represented
	assert.Equal(t, 0.15, testutil.ToFloat64(m.EC2InstanceHourlyCost.With(prometheus.Labels{
		"instance_id":       "i-ri",
		"account_id":        "111111111111",
		"account_name":      "test-account",
		"region":            "us-west-2",
		"instance_type":     "m5.xlarge",
		"cost_type":         "reserved_instance",
		"availability_zone": "us-west-2a",
		"lifecycle":         "on-demand",
		"pricing_accuracy":  "accurate", "node_name": ""})))

	assert.Equal(t, 0.25, testutil.ToFloat64(m.EC2InstanceHourlyCost.With(prometheus.Labels{
		"instance_id":       "i-ec2sp",
		"account_id":        "111111111111",
		"account_name":      "test-account",
		"region":            "us-west-2",
		"instance_type":     "m5.2xlarge",
		"cost_type":         "ec2_instance_savings_plan",
		"availability_zone": "us-west-2a",
		"lifecycle":         "on-demand",
		"pricing_accuracy":  "accurate", "node_name": ""})))

	assert.Equal(t, 0.34, testutil.ToFloat64(m.EC2InstanceHourlyCost.With(prometheus.Labels{
		"instance_id":       "i-computesp",
		"account_id":        "222222222222",
		"account_name":      "test-account",
		"region":            "us-east-1",
		"instance_type":     "c5.2xlarge",
		"cost_type":         "compute_savings_plan",
		"availability_zone": "us-east-1b",
		"lifecycle":         "on-demand",
		"pricing_accuracy":  "accurate", "node_name": ""})))

	assert.Equal(t, 0.05, testutil.ToFloat64(m.EC2InstanceHourlyCost.With(prometheus.Labels{
		"instance_id":       "i-spot",
		"account_id":        "111111111111",
		"account_name":      "test-account",
		"region":            "us-west-2",
		"instance_type":     "c5.xlarge",
		"cost_type":         "spot",
		"availability_zone": "us-west-2b",
		"lifecycle":         "spot",
		"pricing_accuracy":  "accurate", "node_name": ""})))

	assert.Equal(t, 0.0416, testutil.ToFloat64(m.EC2InstanceHourlyCost.With(prometheus.Labels{
		"instance_id":       "i-od",
		"account_id":        "111111111111",
		"account_name":      "test-account",
		"region":            "us-west-2",
		"instance_type":     "t3.medium",
		"cost_type":         "on_demand",
		"availability_zone": "us-west-2c",
		"lifecycle":         "on-demand",
		"pricing_accuracy":  "accurate", "node_name": ""})))
}

func TestUpdateInstanceCostMetrics_SPUnderAndOverUtilization(t *testing.T) {
	// Create metrics instance
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	// Create result with under-utilized, fully utilized, and over-utilized SPs
	result := cost.CalculationResult{
		InstanceCosts: map[string]cost.InstanceCost{},
		SavingsPlanUtilization: map[string]cost.SavingsPlanUtilization{
			"arn:aws:savingsplans::111:savingsplan/under": {
				SavingsPlanARN:         "arn:aws:savingsplans::111:savingsplan/under",
				AccountID:              "111111111111",
				AccountName:            "test-account",
				Type:                   "EC2Instance",
				HourlyCommitment:       100.00,
				CurrentUtilizationRate: 50.00, // 50% utilization
				RemainingCapacity:      50.00,
				UtilizationPercent:     50.0,
			},
			"arn:aws:savingsplans::222:savingsplan/full": {
				SavingsPlanARN:         "arn:aws:savingsplans::222:savingsplan/full",
				AccountID:              "222222222222",
				AccountName:            "test-account",
				Type:                   "Compute",
				HourlyCommitment:       200.00,
				CurrentUtilizationRate: 200.00, // 100% utilization
				RemainingCapacity:      0.00,
				UtilizationPercent:     100.0,
			},
			"arn:aws:savingsplans::333:savingsplan/over": {
				SavingsPlanARN:         "arn:aws:savingsplans::333:savingsplan/over",
				AccountID:              "333333333333",
				AccountName:            "test-account",
				Type:                   "Compute",
				HourlyCommitment:       150.00,
				CurrentUtilizationRate: 180.00, // 120% utilization (over-committed)
				RemainingCapacity:      -30.00, // Negative = spillover to on-demand
				UtilizationPercent:     120.0,
			},
		},
		CalculatedAt: time.Now(),
	}

	// Update metrics
	m.UpdateInstanceCostMetrics(result, nil)

	// Verify under-utilized SP (50%)
	assert.Equal(t, 50.00, testutil.ToFloat64(m.SavingsPlanCurrentUtilizationRate.With(prometheus.Labels{
		"savings_plan_arn": "arn:aws:savingsplans::111:savingsplan/under",
		"account_id":       "111111111111",
		"account_name":     "test-account",
		"type":             "ec2_instance",
	})))
	assert.Equal(t, 50.00, testutil.ToFloat64(m.SavingsPlanRemainingCapacity.With(prometheus.Labels{
		"savings_plan_arn": "arn:aws:savingsplans::111:savingsplan/under",
		"account_id":       "111111111111",
		"account_name":     "test-account",
		"type":             "ec2_instance",
	})))
	assert.Equal(t, 50.0, testutil.ToFloat64(m.SavingsPlanUtilizationPercent.With(prometheus.Labels{
		"savings_plan_arn": "arn:aws:savingsplans::111:savingsplan/under",
		"account_id":       "111111111111",
		"account_name":     "test-account",
		"type":             "ec2_instance",
	})))

	// Verify fully utilized SP (100%)
	assert.Equal(t, 200.00, testutil.ToFloat64(m.SavingsPlanCurrentUtilizationRate.With(prometheus.Labels{
		"savings_plan_arn": "arn:aws:savingsplans::222:savingsplan/full",
		"account_id":       "222222222222",
		"account_name":     "test-account",
		"type":             "compute",
	})))
	assert.Equal(t, 0.00, testutil.ToFloat64(m.SavingsPlanRemainingCapacity.With(prometheus.Labels{
		"savings_plan_arn": "arn:aws:savingsplans::222:savingsplan/full",
		"account_id":       "222222222222",
		"account_name":     "test-account",
		"type":             "compute",
	})))
	assert.Equal(t, 100.0, testutil.ToFloat64(m.SavingsPlanUtilizationPercent.With(prometheus.Labels{
		"savings_plan_arn": "arn:aws:savingsplans::222:savingsplan/full",
		"account_id":       "222222222222",
		"account_name":     "test-account",
		"type":             "compute",
	})))

	// Verify over-utilized SP (120%)
	assert.Equal(t, 180.00, testutil.ToFloat64(m.SavingsPlanCurrentUtilizationRate.With(prometheus.Labels{
		"savings_plan_arn": "arn:aws:savingsplans::333:savingsplan/over",
		"account_id":       "333333333333",
		"account_name":     "test-account",
		"type":             "compute",
	})))
	assert.Equal(t, -30.00, testutil.ToFloat64(m.SavingsPlanRemainingCapacity.With(prometheus.Labels{
		"savings_plan_arn": "arn:aws:savingsplans::333:savingsplan/over",
		"account_id":       "333333333333",
		"account_name":     "test-account",
		"type":             "compute",
	})))
	assert.Equal(t, 120.0, testutil.ToFloat64(m.SavingsPlanUtilizationPercent.With(prometheus.Labels{
		"savings_plan_arn": "arn:aws:savingsplans::333:savingsplan/over",
		"account_id":       "333333333333",
		"account_name":     "test-account",
		"type":             "compute",
	})))
}
