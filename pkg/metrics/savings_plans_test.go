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

	"github.com/nextdoor/lumina/pkg/aws"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateSavingsPlansInventoryMetrics_BasicFunctionality(t *testing.T) {
	// Create metrics instance
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg, newTestConfig())

	now := time.Now()

	// Create test SPs
	sps := []aws.SavingsPlan{
		{
			SavingsPlanARN:  "arn:aws:savingsplans::111111111111:savingsplan/abc123",
			SavingsPlanType: "EC2Instance",
			State:           "active",
			Commitment:      150.00,
			Region:          "us-west-2",
			InstanceFamily:  "m5",
			Start:           now.Add(-365 * 24 * time.Hour),
			End:             now.Add(365 * 24 * time.Hour),
			AccountID:       "111111111111",
			AccountName:     "test-account",
		},
		{
			SavingsPlanARN:  "arn:aws:savingsplans::222222222222:savingsplan/def456",
			SavingsPlanType: "Compute",
			State:           "active",
			Commitment:      300.00,
			Region:          "all", // Compute SPs are global
			InstanceFamily:  "",    // Compute SPs apply to all families
			Start:           now.Add(-100 * 24 * time.Hour),
			End:             now.Add(265 * 24 * time.Hour),
			AccountID:       "222222222222",
			AccountName:     "test-account",
		},
	}

	// Update metrics
	m.UpdateSavingsPlansInventoryMetrics(sps)

	// Verify EC2 Instance SP commitment metric
	assert.Equal(t, 150.00, testutil.ToFloat64(m.SavingsPlanCommitment.With(prometheus.Labels{
		"savings_plan_arn": "arn:aws:savingsplans::111111111111:savingsplan/abc123",
		"account_id":       "111111111111",
		"account_name":     "test-account",
		"type":             "ec2_instance",
		"region":           "us-west-2",
		"instance_family":  "m5",
	})))

	// Verify Compute SP commitment metric
	assert.Equal(t, 300.00, testutil.ToFloat64(m.SavingsPlanCommitment.With(prometheus.Labels{
		"savings_plan_arn": "arn:aws:savingsplans::222222222222:savingsplan/def456",
		"account_id":       "222222222222",
		"account_name":     "test-account",
		"type":             "compute",
		"region":           "all",
		"instance_family":  "all",
	})))

	// Verify remaining hours metrics (should be approximately 365*24 and 265*24 hours)
	ec2Remaining := testutil.ToFloat64(m.SavingsPlanRemainingHours.With(prometheus.Labels{
		"savings_plan_arn": "arn:aws:savingsplans::111111111111:savingsplan/abc123",
		"account_id":       "111111111111",
		"account_name":     "test-account",
		"type":             "ec2_instance",
	}))
	assert.InDelta(t, 365*24, ec2Remaining, 1.0) // Within 1 hour tolerance

	computeRemaining := testutil.ToFloat64(m.SavingsPlanRemainingHours.With(prometheus.Labels{
		"savings_plan_arn": "arn:aws:savingsplans::222222222222:savingsplan/def456",
		"account_id":       "222222222222",
		"account_name":     "test-account",
		"type":             "compute",
	}))
	assert.InDelta(t, 265*24, computeRemaining, 1.0) // Within 1 hour tolerance
}

func TestUpdateSavingsPlansInventoryMetrics_EmptyList(t *testing.T) {
	// Create metrics instance
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg, newTestConfig())

	now := time.Now()

	// First, add some SPs
	sps := []aws.SavingsPlan{
		{
			SavingsPlanARN:  "arn:aws:savingsplans::111111111111:savingsplan/abc123",
			SavingsPlanType: "EC2Instance",
			State:           "active",
			Commitment:      150.00,
			Region:          "us-west-2",
			InstanceFamily:  "m5",
			Start:           now.Add(-365 * 24 * time.Hour),
			End:             now.Add(365 * 24 * time.Hour),
			AccountID:       "111111111111",
			AccountName:     "test-account",
		},
	}
	m.UpdateSavingsPlansInventoryMetrics(sps)

	// Verify metric exists
	assert.Equal(t, 150.00, testutil.ToFloat64(m.SavingsPlanCommitment.With(prometheus.Labels{
		"savings_plan_arn": "arn:aws:savingsplans::111111111111:savingsplan/abc123",
		"account_id":       "111111111111",
		"account_name":     "test-account",
		"type":             "ec2_instance",
		"region":           "us-west-2",
		"instance_family":  "m5",
	})))

	// Now update with empty list - should clear all metrics
	m.UpdateSavingsPlansInventoryMetrics([]aws.SavingsPlan{})

	// Verify metrics were reset - should be 0
	count, err := m.SavingsPlanCommitment.GetMetricWith(prometheus.Labels{
		"savings_plan_arn": "arn:aws:savingsplans::111111111111:savingsplan/abc123",
		"account_id":       "111111111111",
		"account_name":     "test-account",
		"type":             "ec2_instance",
		"region":           "us-west-2",
		"instance_family":  "m5",
	})
	require.NoError(t, err)
	assert.Equal(t, 0.0, testutil.ToFloat64(count))
}

func TestUpdateSavingsPlansInventoryMetrics_InactiveSPsSkipped(t *testing.T) {
	// Create metrics instance
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg, newTestConfig())

	now := time.Now()

	// Create SPs with different states
	sps := []aws.SavingsPlan{
		{
			SavingsPlanARN:  "arn:aws:savingsplans::111111111111:savingsplan/active",
			SavingsPlanType: "EC2Instance",
			State:           "active",
			Commitment:      150.00,
			Region:          "us-west-2",
			InstanceFamily:  "m5",
			Start:           now.Add(-365 * 24 * time.Hour),
			End:             now.Add(365 * 24 * time.Hour),
			AccountID:       "111111111111",
			AccountName:     "test-account",
		},
		{
			SavingsPlanARN:  "arn:aws:savingsplans::111111111111:savingsplan/retired",
			SavingsPlanType: "EC2Instance",
			State:           "retired",
			Commitment:      100.00,
			Region:          "us-west-2",
			InstanceFamily:  "c5",
			Start:           now.Add(-730 * 24 * time.Hour),
			End:             now.Add(-30 * 24 * time.Hour),
			AccountID:       "111111111111",
			AccountName:     "test-account",
		},
		{
			SavingsPlanARN:  "arn:aws:savingsplans::111111111111:savingsplan/payment-failed",
			SavingsPlanType: "Compute",
			State:           "payment-failed",
			Commitment:      200.00,
			Region:          "all",
			InstanceFamily:  "",
			Start:           now.Add(-100 * 24 * time.Hour),
			End:             now.Add(265 * 24 * time.Hour),
			AccountID:       "111111111111",
			AccountName:     "test-account",
		},
	}

	// Update metrics
	m.UpdateSavingsPlansInventoryMetrics(sps)

	// Verify only active SP has metric
	assert.Equal(t, 150.00, testutil.ToFloat64(m.SavingsPlanCommitment.With(prometheus.Labels{
		"savings_plan_arn": "arn:aws:savingsplans::111111111111:savingsplan/active",
		"account_id":       "111111111111",
		"account_name":     "test-account",
		"type":             "ec2_instance",
		"region":           "us-west-2",
		"instance_family":  "m5",
	})))

	// Verify retired SP metric is 0 (not set)
	retiredMetric, err := m.SavingsPlanCommitment.GetMetricWith(prometheus.Labels{
		"savings_plan_arn": "arn:aws:savingsplans::111111111111:savingsplan/retired",
		"account_id":       "111111111111",
		"account_name":     "test-account",
		"type":             "ec2_instance",
		"region":           "us-west-2",
		"instance_family":  "c5",
	})
	require.NoError(t, err)
	assert.Equal(t, 0.0, testutil.ToFloat64(retiredMetric))

	// Verify payment-failed SP metric is 0 (not set)
	failedMetric, err := m.SavingsPlanCommitment.GetMetricWith(prometheus.Labels{
		"savings_plan_arn": "arn:aws:savingsplans::111111111111:savingsplan/payment-failed",
		"account_id":       "111111111111",
		"account_name":     "test-account",
		"type":             "compute",
		"region":           "all",
		"instance_family":  "all",
	})
	require.NoError(t, err)
	assert.Equal(t, 0.0, testutil.ToFloat64(failedMetric))
}

//nolint:dupl // Test data structures are intentionally similar
func TestUpdateSavingsPlansInventoryMetrics_EC2InstanceVsCompute(t *testing.T) {
	// Test that EC2 Instance and Compute SPs have different label patterns
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg, newTestConfig())

	now := time.Now()

	sps := []aws.SavingsPlan{
		{
			SavingsPlanARN:  "arn:aws:savingsplans::111111111111:savingsplan/ec2",
			SavingsPlanType: "EC2Instance",
			State:           "active",
			Commitment:      150.00,
			Region:          "us-west-2",
			InstanceFamily:  "m5",
			Start:           now.Add(-365 * 24 * time.Hour),
			End:             now.Add(365 * 24 * time.Hour),
			AccountID:       "111111111111",
			AccountName:     "test-account",
		},
		{
			SavingsPlanARN:  "arn:aws:savingsplans::111111111111:savingsplan/compute",
			SavingsPlanType: "Compute",
			State:           "active",
			Commitment:      300.00,
			Region:          "all",
			InstanceFamily:  "",
			Start:           now.Add(-100 * 24 * time.Hour),
			End:             now.Add(265 * 24 * time.Hour),
			AccountID:       "111111111111",
			AccountName:     "test-account",
		},
	}

	m.UpdateSavingsPlansInventoryMetrics(sps)

	// Verify EC2 Instance SP has region and family
	assert.Equal(t, 150.00, testutil.ToFloat64(m.SavingsPlanCommitment.With(prometheus.Labels{
		"savings_plan_arn": "arn:aws:savingsplans::111111111111:savingsplan/ec2",
		"account_id":       "111111111111",
		"account_name":     "test-account",
		"type":             "ec2_instance",
		"region":           "us-west-2",
		"instance_family":  "m5",
	})))

	// Verify Compute SP has "all" for region and family
	assert.Equal(t, 300.00, testutil.ToFloat64(m.SavingsPlanCommitment.With(prometheus.Labels{
		"savings_plan_arn": "arn:aws:savingsplans::111111111111:savingsplan/compute",
		"account_id":       "111111111111",
		"account_name":     "test-account",
		"type":             "compute",
		"region":           "all",
		"instance_family":  "all",
	})))
}

//nolint:dupl // Test data structures are intentionally similar
func TestUpdateSavingsPlansInventoryMetrics_MultipleAccounts(t *testing.T) {
	// Test SPs across multiple accounts
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg, newTestConfig())

	now := time.Now()

	sps := []aws.SavingsPlan{
		{
			SavingsPlanARN:  "arn:aws:savingsplans::111111111111:savingsplan/sp1",
			SavingsPlanType: "EC2Instance",
			State:           "active",
			Commitment:      100.00,
			Region:          "us-west-2",
			InstanceFamily:  "m5",
			Start:           now.Add(-365 * 24 * time.Hour),
			End:             now.Add(365 * 24 * time.Hour),
			AccountID:       "111111111111",
			AccountName:     "test-account",
		},
		{
			SavingsPlanARN:  "arn:aws:savingsplans::222222222222:savingsplan/sp2",
			SavingsPlanType: "EC2Instance",
			State:           "active",
			Commitment:      200.00,
			Region:          "us-west-2",
			InstanceFamily:  "m5",
			Start:           now.Add(-200 * 24 * time.Hour),
			End:             now.Add(165 * 24 * time.Hour),
			AccountID:       "222222222222",
			AccountName:     "test-account",
		},
	}

	m.UpdateSavingsPlansInventoryMetrics(sps)

	// Verify each account has separate metrics
	assert.Equal(t, 100.00, testutil.ToFloat64(m.SavingsPlanCommitment.With(prometheus.Labels{
		"savings_plan_arn": "arn:aws:savingsplans::111111111111:savingsplan/sp1",
		"account_id":       "111111111111",
		"account_name":     "test-account",
		"type":             "ec2_instance",
		"region":           "us-west-2",
		"instance_family":  "m5",
	})))

	assert.Equal(t, 200.00, testutil.ToFloat64(m.SavingsPlanCommitment.With(prometheus.Labels{
		"savings_plan_arn": "arn:aws:savingsplans::222222222222:savingsplan/sp2",
		"account_id":       "222222222222",
		"account_name":     "test-account",
		"type":             "ec2_instance",
		"region":           "us-west-2",
		"instance_family":  "m5",
	})))
}

func TestUpdateSavingsPlansInventoryMetrics_MetricCleanup(t *testing.T) {
	// Test that updating with a new set of SPs removes old ones
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg, newTestConfig())

	now := time.Now()

	// First update with two SPs
	sps1 := []aws.SavingsPlan{
		{
			SavingsPlanARN:  "arn:aws:savingsplans::111111111111:savingsplan/sp1",
			SavingsPlanType: "EC2Instance",
			State:           "active",
			Commitment:      100.00,
			Region:          "us-west-2",
			InstanceFamily:  "m5",
			Start:           now.Add(-365 * 24 * time.Hour),
			End:             now.Add(365 * 24 * time.Hour),
			AccountID:       "111111111111",
			AccountName:     "test-account",
		},
		{
			SavingsPlanARN:  "arn:aws:savingsplans::111111111111:savingsplan/sp2",
			SavingsPlanType: "Compute",
			State:           "active",
			Commitment:      200.00,
			Region:          "all",
			InstanceFamily:  "",
			Start:           now.Add(-100 * 24 * time.Hour),
			End:             now.Add(265 * 24 * time.Hour),
			AccountID:       "111111111111",
			AccountName:     "test-account",
		},
	}
	m.UpdateSavingsPlansInventoryMetrics(sps1)

	// Verify both exist
	assert.Equal(t, 100.00, testutil.ToFloat64(m.SavingsPlanCommitment.With(prometheus.Labels{
		"savings_plan_arn": "arn:aws:savingsplans::111111111111:savingsplan/sp1",
		"account_id":       "111111111111",
		"account_name":     "test-account",
		"type":             "ec2_instance",
		"region":           "us-west-2",
		"instance_family":  "m5",
	})))
	assert.Equal(t, 200.00, testutil.ToFloat64(m.SavingsPlanCommitment.With(prometheus.Labels{
		"savings_plan_arn": "arn:aws:savingsplans::111111111111:savingsplan/sp2",
		"account_id":       "111111111111",
		"account_name":     "test-account",
		"type":             "compute",
		"region":           "all",
		"instance_family":  "all",
	})))

	// Second update with only one SP (other expired)
	sps2 := []aws.SavingsPlan{
		{
			SavingsPlanARN:  "arn:aws:savingsplans::111111111111:savingsplan/sp1",
			SavingsPlanType: "EC2Instance",
			State:           "active",
			Commitment:      100.00,
			Region:          "us-west-2",
			InstanceFamily:  "m5",
			Start:           now.Add(-365 * 24 * time.Hour),
			End:             now.Add(365 * 24 * time.Hour),
			AccountID:       "111111111111",
			AccountName:     "test-account",
		},
	}
	m.UpdateSavingsPlansInventoryMetrics(sps2)

	// Verify first SP still exists
	assert.Equal(t, 100.00, testutil.ToFloat64(m.SavingsPlanCommitment.With(prometheus.Labels{
		"savings_plan_arn": "arn:aws:savingsplans::111111111111:savingsplan/sp1",
		"account_id":       "111111111111",
		"account_name":     "test-account",
		"type":             "ec2_instance",
		"region":           "us-west-2",
		"instance_family":  "m5",
	})))

	// Verify second SP was removed (reset to 0)
	sp2Metric, err := m.SavingsPlanCommitment.GetMetricWith(prometheus.Labels{
		"savings_plan_arn": "arn:aws:savingsplans::111111111111:savingsplan/sp2",
		"account_id":       "111111111111",
		"account_name":     "test-account",
		"type":             "compute",
		"region":           "all",
		"instance_family":  "all",
	})
	require.NoError(t, err)
	assert.Equal(t, 0.0, testutil.ToFloat64(sp2Metric))
}

func TestUpdateSavingsPlansInventoryMetrics_RemainingHoursCalculation(t *testing.T) {
	// Test remaining hours calculation for various scenarios
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg, newTestConfig())

	now := time.Now()

	sps := []aws.SavingsPlan{
		{
			SavingsPlanARN:  "arn:aws:savingsplans::111111111111:savingsplan/expiring-soon",
			SavingsPlanType: "EC2Instance",
			State:           "active",
			Commitment:      100.00,
			Region:          "us-west-2",
			InstanceFamily:  "m5",
			Start:           now.Add(-365 * 24 * time.Hour),
			End:             now.Add(24 * time.Hour), // 1 day remaining
			AccountID:       "111111111111",
			AccountName:     "test-account",
		},
		{
			SavingsPlanARN:  "arn:aws:savingsplans::111111111111:savingsplan/long-term",
			SavingsPlanType: "Compute",
			State:           "active",
			Commitment:      200.00,
			Region:          "all",
			InstanceFamily:  "",
			Start:           now.Add(-30 * 24 * time.Hour),
			End:             now.Add(335 * 24 * time.Hour), // ~11 months remaining
			AccountID:       "111111111111",
			AccountName:     "test-account",
		},
	}

	m.UpdateSavingsPlansInventoryMetrics(sps)

	// Verify expiring soon SP (should be ~24 hours)
	expiringRemaining := testutil.ToFloat64(m.SavingsPlanRemainingHours.With(prometheus.Labels{
		"savings_plan_arn": "arn:aws:savingsplans::111111111111:savingsplan/expiring-soon",
		"account_id":       "111111111111",
		"account_name":     "test-account",
		"type":             "ec2_instance",
	}))
	assert.InDelta(t, 24.0, expiringRemaining, 0.1)

	// Verify long-term SP (should be ~335*24 hours)
	longTermRemaining := testutil.ToFloat64(m.SavingsPlanRemainingHours.With(prometheus.Labels{
		"savings_plan_arn": "arn:aws:savingsplans::111111111111:savingsplan/long-term",
		"account_id":       "111111111111",
		"account_name":     "test-account",
		"type":             "compute",
	}))
	assert.InDelta(t, 335*24.0, longTermRemaining, 1.0)
}

func TestNormalizeSPType(t *testing.T) {
	tests := []struct {
		name     string
		spType   string
		expected string
	}{
		{
			name:     "EC2Instance type",
			spType:   "EC2Instance",
			expected: "ec2_instance",
		},
		{
			name:     "Compute type",
			spType:   "Compute",
			expected: "compute",
		},
		{
			name:     "unknown type (defensive)",
			spType:   "Unknown",
			expected: "Unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeSPType(tt.spType)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestCalculateRemainingHours(t *testing.T) {
	tests := []struct {
		name     string
		endTime  time.Time
		now      time.Time
		expected float64
	}{
		{
			name:     "future expiration",
			endTime:  time.Now().Add(24 * time.Hour),
			now:      time.Now(),
			expected: 24.0,
		},
		{
			name:     "already expired",
			endTime:  time.Now().Add(-24 * time.Hour),
			now:      time.Now(),
			expected: 0.0,
		},
		{
			name:     "expiring in 1 year",
			endTime:  time.Now().Add(365 * 24 * time.Hour),
			now:      time.Now(),
			expected: 365 * 24.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateRemainingHours(tt.endTime, tt.now)
			assert.InDelta(t, tt.expected, got, 0.1)
		})
	}
}

//nolint:dupl // Test data structures are intentionally similar
func TestUpdateSavingsPlansInventoryMetrics_RealWorldScenario(t *testing.T) {
	// Simulate a real-world scenario with mixed SPs across accounts
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg, newTestConfig())

	now := time.Now()
	sps := []aws.SavingsPlan{
		// Production account - EC2 Instance SP for m5 family
		{
			SavingsPlanARN:  "arn:aws:savingsplans::329239342014:savingsplan/prod-ec2-m5",
			SavingsPlanType: "EC2Instance",
			State:           "active",
			Commitment:      150.50,
			Region:          "us-west-2",
			InstanceFamily:  "m5",
			Start:           now.Add(-365 * 24 * time.Hour),
			End:             now.Add(365 * 24 * time.Hour),
			AccountID:       "329239342014",
			AccountName:     "test-account",
		},
		// Production account - Compute SP (global)
		{
			SavingsPlanARN:  "arn:aws:savingsplans::329239342014:savingsplan/prod-compute",
			SavingsPlanType: "Compute",
			State:           "active",
			Commitment:      500.75,
			Region:          "all",
			InstanceFamily:  "",
			Start:           now.Add(-200 * 24 * time.Hour),
			End:             now.Add(165 * 24 * time.Hour),
			AccountID:       "329239342014",
			AccountName:     "test-account",
		},
		// Staging account - EC2 Instance SP for c5 family
		{
			SavingsPlanARN:  "arn:aws:savingsplans::364942603424:savingsplan/staging-ec2-c5",
			SavingsPlanType: "EC2Instance",
			State:           "active",
			Commitment:      75.25,
			Region:          "us-east-1",
			InstanceFamily:  "c5",
			Start:           now.Add(-100 * 24 * time.Hour),
			End:             now.Add(265 * 24 * time.Hour),
			AccountID:       "364942603424",
			AccountName:     "test-account",
		},
		// Expired SP (should be skipped)
		{
			SavingsPlanARN:  "arn:aws:savingsplans::329239342014:savingsplan/expired",
			SavingsPlanType: "EC2Instance",
			State:           "retired",
			Commitment:      100.00,
			Region:          "us-west-2",
			InstanceFamily:  "r5",
			Start:           now.Add(-730 * 24 * time.Hour),
			End:             now.Add(-30 * 24 * time.Hour),
			AccountID:       "329239342014",
			AccountName:     "test-account",
		},
	}

	m.UpdateSavingsPlansInventoryMetrics(sps)

	// Verify production EC2 Instance SP
	assert.Equal(t, 150.50, testutil.ToFloat64(m.SavingsPlanCommitment.With(prometheus.Labels{
		"savings_plan_arn": "arn:aws:savingsplans::329239342014:savingsplan/prod-ec2-m5",
		"account_id":       "329239342014",
		"account_name":     "test-account",
		"type":             "ec2_instance",
		"region":           "us-west-2",
		"instance_family":  "m5",
	})))

	// Verify production Compute SP
	assert.Equal(t, 500.75, testutil.ToFloat64(m.SavingsPlanCommitment.With(prometheus.Labels{
		"savings_plan_arn": "arn:aws:savingsplans::329239342014:savingsplan/prod-compute",
		"account_id":       "329239342014",
		"account_name":     "test-account",
		"type":             "compute",
		"region":           "all",
		"instance_family":  "all",
	})))

	// Verify staging SP
	assert.Equal(t, 75.25, testutil.ToFloat64(m.SavingsPlanCommitment.With(prometheus.Labels{
		"savings_plan_arn": "arn:aws:savingsplans::364942603424:savingsplan/staging-ec2-c5",
		"account_id":       "364942603424",
		"account_name":     "test-account",
		"type":             "ec2_instance",
		"region":           "us-east-1",
		"instance_family":  "c5",
	})))

	// Verify remaining hours
	prodRemaining := testutil.ToFloat64(m.SavingsPlanRemainingHours.With(prometheus.Labels{
		"savings_plan_arn": "arn:aws:savingsplans::329239342014:savingsplan/prod-ec2-m5",
		"account_id":       "329239342014",
		"account_name":     "test-account",
		"type":             "ec2_instance",
	}))
	assert.InDelta(t, 365*24, prodRemaining, 1.0)

	// Verify expired SP is not present
	expiredMetric, err := m.SavingsPlanCommitment.GetMetricWith(prometheus.Labels{
		"savings_plan_arn": "arn:aws:savingsplans::329239342014:savingsplan/expired",
		"account_id":       "329239342014",
		"account_name":     "test-account",
		"type":             "ec2_instance",
		"region":           "us-west-2",
		"instance_family":  "r5",
	})
	require.NoError(t, err)
	assert.Equal(t, 0.0, testutil.ToFloat64(expiredMetric))
}
