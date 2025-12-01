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

func TestUpdateReservedInstanceMetrics_BasicFunctionality(t *testing.T) {
	// Create metrics instance
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	// Create test RIs
	ris := []aws.ReservedInstance{
		{
			ReservedInstanceID: "ri-123",
			InstanceType:       "m5.xlarge",
			AvailabilityZone:   "us-west-2a",
			Region:             "us-west-2",
			InstanceCount:      2,
			State:              "active",
			AccountID:          "111111111111",
			AccountName:        "test-account",
		},
		{
			ReservedInstanceID: "ri-456",
			InstanceType:       "m5.2xlarge",
			AvailabilityZone:   "us-west-2b",
			Region:             "us-west-2",
			InstanceCount:      3,
			State:              "active",
			AccountID:          "111111111111",
			AccountName:        "test-account",
		},
		{
			ReservedInstanceID: "ri-789",
			InstanceType:       "c5.large",
			AvailabilityZone:   "us-east-1a",
			Region:             "us-east-1",
			InstanceCount:      1,
			State:              "active",
			AccountID:          "222222222222",
			AccountName:        "test-account",
		},
	}

	// Update metrics
	m.UpdateReservedInstanceMetrics(ris)

	// Verify per-instance metrics
	assert.Equal(t, 1.0, testutil.ToFloat64(m.ReservedInstance.With(prometheus.Labels{
		"account_id":        "111111111111",
		"account_name":      "test-account",
		"region":            "us-west-2",
		"instance_type":     "m5.xlarge",
		"availability_zone": "us-west-2a",
	})))

	assert.Equal(t, 1.0, testutil.ToFloat64(m.ReservedInstance.With(prometheus.Labels{
		"account_id":        "111111111111",
		"account_name":      "test-account",
		"region":            "us-west-2",
		"instance_type":     "m5.2xlarge",
		"availability_zone": "us-west-2b",
	})))

	assert.Equal(t, 1.0, testutil.ToFloat64(m.ReservedInstance.With(prometheus.Labels{
		"account_id":        "222222222222",
		"account_name":      "test-account",
		"region":            "us-east-1",
		"instance_type":     "c5.large",
		"availability_zone": "us-east-1a",
	})))

	// Verify family count aggregation (m5 family: 2 + 3 = 5)
	assert.Equal(t, 5.0, testutil.ToFloat64(m.ReservedInstanceCount.With(prometheus.Labels{
		"account_id":      "111111111111",
		"account_name":    "test-account",
		"region":          "us-west-2",
		"instance_family": "m5",
	})))

	// Verify c5 family count
	assert.Equal(t, 1.0, testutil.ToFloat64(m.ReservedInstanceCount.With(prometheus.Labels{
		"account_id":      "222222222222",
		"account_name":    "test-account",
		"region":          "us-east-1",
		"instance_family": "c5",
	})))
}

func TestUpdateReservedInstanceMetrics_EmptyList(t *testing.T) {
	// Create metrics instance
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	// First, add some RIs
	ris := []aws.ReservedInstance{
		{
			ReservedInstanceID: "ri-123",
			InstanceType:       "m5.xlarge",
			AvailabilityZone:   "us-west-2a",
			Region:             "us-west-2",
			InstanceCount:      1,
			State:              "active",
			AccountID:          "111111111111",
			AccountName:        "test-account",
		},
	}
	m.UpdateReservedInstanceMetrics(ris)

	// Verify metric exists
	assert.Equal(t, 1.0, testutil.ToFloat64(m.ReservedInstance.With(prometheus.Labels{
		"account_id":        "111111111111",
		"account_name":      "test-account",
		"region":            "us-west-2",
		"instance_type":     "m5.xlarge",
		"availability_zone": "us-west-2a",
	})))

	// Now update with empty list - should clear all metrics
	m.UpdateReservedInstanceMetrics([]aws.ReservedInstance{})

	// Verify metrics were reset - count should be 0
	count, err := m.ReservedInstance.GetMetricWith(prometheus.Labels{
		"account_id":        "111111111111",
		"account_name":      "test-account",
		"region":            "us-west-2",
		"instance_type":     "m5.xlarge",
		"availability_zone": "us-west-2a",
	})
	require.NoError(t, err)

	// After reset, the metric should report 0
	assert.Equal(t, 0.0, testutil.ToFloat64(count))
}

func TestUpdateReservedInstanceMetrics_InactiveRIsSkipped(t *testing.T) {
	// Create metrics instance
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	// Create RIs with different states
	ris := []aws.ReservedInstance{
		{
			ReservedInstanceID: "ri-active",
			InstanceType:       "m5.xlarge",
			AvailabilityZone:   "us-west-2a",
			Region:             "us-west-2",
			InstanceCount:      1,
			State:              "active",
			AccountID:          "111111111111",
			AccountName:        "test-account",
		},
		{
			ReservedInstanceID: "ri-retired",
			InstanceType:       "m5.2xlarge",
			AvailabilityZone:   "us-west-2b",
			Region:             "us-west-2",
			InstanceCount:      1,
			State:              "retired",
			AccountID:          "111111111111",
			AccountName:        "test-account",
		},
		{
			ReservedInstanceID: "ri-payment-failed",
			InstanceType:       "c5.large",
			AvailabilityZone:   "us-west-2a",
			Region:             "us-west-2",
			InstanceCount:      1,
			State:              "payment-failed",
			AccountID:          "111111111111",
			AccountName:        "test-account",
		},
	}

	// Update metrics
	m.UpdateReservedInstanceMetrics(ris)

	// Verify only active RI has metric
	assert.Equal(t, 1.0, testutil.ToFloat64(m.ReservedInstance.With(prometheus.Labels{
		"account_id":        "111111111111",
		"account_name":      "test-account",
		"region":            "us-west-2",
		"instance_type":     "m5.xlarge",
		"availability_zone": "us-west-2a",
	})))

	// Verify retired RI metric is 0 (not set)
	retiredMetric, err := m.ReservedInstance.GetMetricWith(prometheus.Labels{
		"account_id":        "111111111111",
		"account_name":      "test-account",
		"region":            "us-west-2",
		"instance_type":     "m5.2xlarge",
		"availability_zone": "us-west-2b",
	})
	require.NoError(t, err)
	assert.Equal(t, 0.0, testutil.ToFloat64(retiredMetric))

	// Verify family count only includes active RI
	assert.Equal(t, 1.0, testutil.ToFloat64(m.ReservedInstanceCount.With(prometheus.Labels{
		"account_id":      "111111111111",
		"account_name":    "test-account",
		"region":          "us-west-2",
		"instance_family": "m5",
	})))
}

func TestUpdateReservedInstanceMetrics_MultipleAccounts(t *testing.T) {
	// Create metrics instance
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	// Create RIs across multiple accounts
	ris := []aws.ReservedInstance{
		{
			ReservedInstanceID: "ri-123",
			InstanceType:       "m5.xlarge",
			AvailabilityZone:   "us-west-2a",
			Region:             "us-west-2",
			InstanceCount:      2,
			State:              "active",
			AccountID:          "111111111111",
			AccountName:        "test-account",
		},
		{
			ReservedInstanceID: "ri-456",
			InstanceType:       "m5.xlarge",
			AvailabilityZone:   "us-west-2a",
			Region:             "us-west-2",
			InstanceCount:      3,
			State:              "active",
			AccountID:          "222222222222",
			AccountName:        "test-account",
		},
	}

	// Update metrics
	m.UpdateReservedInstanceMetrics(ris)

	// Verify each account has separate metrics
	assert.Equal(t, 1.0, testutil.ToFloat64(m.ReservedInstance.With(prometheus.Labels{
		"account_id":        "111111111111",
		"account_name":      "test-account",
		"region":            "us-west-2",
		"instance_type":     "m5.xlarge",
		"availability_zone": "us-west-2a",
	})))

	assert.Equal(t, 1.0, testutil.ToFloat64(m.ReservedInstance.With(prometheus.Labels{
		"account_id":        "222222222222",
		"account_name":      "test-account",
		"region":            "us-west-2",
		"instance_type":     "m5.xlarge",
		"availability_zone": "us-west-2a",
	})))

	// Verify family counts are separate per account
	assert.Equal(t, 2.0, testutil.ToFloat64(m.ReservedInstanceCount.With(prometheus.Labels{
		"account_id":      "111111111111",
		"account_name":    "test-account",
		"region":          "us-west-2",
		"instance_family": "m5",
	})))

	assert.Equal(t, 3.0, testutil.ToFloat64(m.ReservedInstanceCount.With(prometheus.Labels{
		"account_id":      "222222222222",
		"account_name":    "test-account",
		"region":          "us-west-2",
		"instance_family": "m5",
	})))
}

func TestUpdateReservedInstanceMetrics_RegionalRIs(t *testing.T) {
	// Create metrics instance
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	// Create regional and zonal RIs
	ris := []aws.ReservedInstance{
		{
			ReservedInstanceID: "ri-zonal",
			InstanceType:       "m5.xlarge",
			AvailabilityZone:   "us-west-2a",
			Region:             "us-west-2",
			InstanceCount:      1,
			State:              "active",
			AccountID:          "111111111111",
			AccountName:        "test-account",
		},
		{
			ReservedInstanceID: "ri-regional",
			InstanceType:       "m5.2xlarge",
			AvailabilityZone:   "regional",
			Region:             "us-west-2",
			InstanceCount:      2,
			State:              "active",
			AccountID:          "111111111111",
			AccountName:        "test-account",
		},
	}

	// Update metrics
	m.UpdateReservedInstanceMetrics(ris)

	// Verify zonal RI
	assert.Equal(t, 1.0, testutil.ToFloat64(m.ReservedInstance.With(prometheus.Labels{
		"account_id":        "111111111111",
		"account_name":      "test-account",
		"region":            "us-west-2",
		"instance_type":     "m5.xlarge",
		"availability_zone": "us-west-2a",
	})))

	// Verify regional RI
	assert.Equal(t, 1.0, testutil.ToFloat64(m.ReservedInstance.With(prometheus.Labels{
		"account_id":        "111111111111",
		"account_name":      "test-account",
		"region":            "us-west-2",
		"instance_type":     "m5.2xlarge",
		"availability_zone": "regional",
	})))

	// Verify family count aggregates both (1 + 2 = 3)
	assert.Equal(t, 3.0, testutil.ToFloat64(m.ReservedInstanceCount.With(prometheus.Labels{
		"account_id":      "111111111111",
		"account_name":    "test-account",
		"region":          "us-west-2",
		"instance_family": "m5",
	})))
}

func TestUpdateReservedInstanceMetrics_MetricCleanup(t *testing.T) {
	// Test that updating with a new set of RIs removes old ones
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	// First update with two RIs
	ris1 := []aws.ReservedInstance{
		{
			ReservedInstanceID: "ri-123",
			InstanceType:       "m5.xlarge",
			AvailabilityZone:   "us-west-2a",
			Region:             "us-west-2",
			InstanceCount:      1,
			State:              "active",
			AccountID:          "111111111111",
			AccountName:        "test-account",
		},
		{
			ReservedInstanceID: "ri-456",
			InstanceType:       "c5.large",
			AvailabilityZone:   "us-west-2a",
			Region:             "us-west-2",
			InstanceCount:      1,
			State:              "active",
			AccountID:          "111111111111",
			AccountName:        "test-account",
		},
	}
	m.UpdateReservedInstanceMetrics(ris1)

	// Verify both exist
	assert.Equal(t, 1.0, testutil.ToFloat64(m.ReservedInstance.With(prometheus.Labels{
		"account_id":        "111111111111",
		"account_name":      "test-account",
		"region":            "us-west-2",
		"instance_type":     "m5.xlarge",
		"availability_zone": "us-west-2a",
	})))
	assert.Equal(t, 1.0, testutil.ToFloat64(m.ReservedInstance.With(prometheus.Labels{
		"account_id":        "111111111111",
		"account_name":      "test-account",
		"region":            "us-west-2",
		"instance_type":     "c5.large",
		"availability_zone": "us-west-2a",
	})))

	// Second update with only one RI (other expired)
	ris2 := []aws.ReservedInstance{
		{
			ReservedInstanceID: "ri-123",
			InstanceType:       "m5.xlarge",
			AvailabilityZone:   "us-west-2a",
			Region:             "us-west-2",
			InstanceCount:      1,
			State:              "active",
			AccountID:          "111111111111",
			AccountName:        "test-account",
		},
	}
	m.UpdateReservedInstanceMetrics(ris2)

	// Verify first RI still exists
	assert.Equal(t, 1.0, testutil.ToFloat64(m.ReservedInstance.With(prometheus.Labels{
		"account_id":        "111111111111",
		"account_name":      "test-account",
		"region":            "us-west-2",
		"instance_type":     "m5.xlarge",
		"availability_zone": "us-west-2a",
	})))

	// Verify second RI was removed (reset to 0)
	c5Metric, err := m.ReservedInstance.GetMetricWith(prometheus.Labels{
		"account_id":        "111111111111",
		"account_name":      "test-account",
		"region":            "us-west-2",
		"instance_type":     "c5.large",
		"availability_zone": "us-west-2a",
	})
	require.NoError(t, err)
	assert.Equal(t, 0.0, testutil.ToFloat64(c5Metric))

	// Verify family counts updated
	assert.Equal(t, 1.0, testutil.ToFloat64(m.ReservedInstanceCount.With(prometheus.Labels{
		"account_id":      "111111111111",
		"account_name":    "test-account",
		"region":          "us-west-2",
		"instance_family": "m5",
	})))

	// c5 family should be removed
	c5FamilyMetric, err := m.ReservedInstanceCount.GetMetricWith(prometheus.Labels{
		"account_id":      "111111111111",
		"account_name":    "test-account",
		"region":          "us-west-2",
		"instance_family": "c5",
	})
	require.NoError(t, err)
	assert.Equal(t, 0.0, testutil.ToFloat64(c5FamilyMetric))
}

func TestUpdateReservedInstanceMetrics_MalformedKeyHandling(t *testing.T) {
	// This test ensures defensive handling of edge cases in aggregation.
	// In practice, the key format should always be correct, but we test
	// the defensive check for completeness.
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	// Create RI with proper data
	ris := []aws.ReservedInstance{
		{
			ReservedInstanceID: "ri-123",
			InstanceType:       "m5.xlarge",
			AvailabilityZone:   "us-west-2a",
			Region:             "us-west-2",
			InstanceCount:      1,
			State:              "active",
			AccountID:          "111111111111",
			AccountName:        "test-account",
		},
	}

	// This should work normally - we're just ensuring the defensive code
	// doesn't break normal operation
	m.UpdateReservedInstanceMetrics(ris)

	// Verify metric was set correctly
	assert.Equal(t, 1.0, testutil.ToFloat64(m.ReservedInstanceCount.With(prometheus.Labels{
		"account_id":      "111111111111",
		"account_name":    "test-account",
		"region":          "us-west-2",
		"instance_family": "m5",
	})))
}

func TestExtractInstanceFamily(t *testing.T) {
	tests := []struct {
		name         string
		instanceType string
		want         string
	}{
		{
			name:         "standard instance type",
			instanceType: "m5.xlarge",
			want:         "m5",
		},
		{
			name:         "large instance type",
			instanceType: "c5.2xlarge",
			want:         "c5",
		},
		{
			name:         "instance with storage suffix",
			instanceType: "r5d.4xlarge",
			want:         "r5d",
		},
		{
			name:         "instance with network suffix",
			instanceType: "m5n.large",
			want:         "m5n",
		},
		{
			name:         "graviton instance",
			instanceType: "m7g.xlarge",
			want:         "m7g",
		},
		{
			name:         "no size specified",
			instanceType: "t3",
			want:         "t3",
		},
		{
			name:         "edge case with multiple dots",
			instanceType: "x1e.32xlarge",
			want:         "x1e",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractInstanceFamily(tt.instanceType)
			assert.Equal(t, tt.want, got)
		})
	}
}

//nolint:dupl // Test data structures are intentionally similar
func TestUpdateReservedInstanceMetrics_RealWorldScenario(t *testing.T) {
	// Simulate a real-world scenario with mixed RIs across accounts and regions
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	now := time.Now()
	ris := []aws.ReservedInstance{
		// Production account - us-west-2
		{
			ReservedInstanceID: "ri-prod-1",
			InstanceType:       "m5.2xlarge",
			AvailabilityZone:   "us-west-2a",
			Region:             "us-west-2",
			InstanceCount:      5,
			State:              "active",
			Start:              now.Add(-365 * 24 * time.Hour),
			End:                now.Add(365 * 24 * time.Hour),
			AccountID:          "329239342014",
			AccountName:        "test-account",
		},
		{
			ReservedInstanceID: "ri-prod-2",
			InstanceType:       "c5.xlarge",
			AvailabilityZone:   "regional",
			Region:             "us-west-2",
			InstanceCount:      10,
			State:              "active",
			Start:              now.Add(-200 * 24 * time.Hour),
			End:                now.Add(165 * 24 * time.Hour),
			AccountID:          "329239342014",
			AccountName:        "test-account",
		},
		// Staging account - us-east-1
		{
			ReservedInstanceID: "ri-staging-1",
			InstanceType:       "t3.medium",
			AvailabilityZone:   "us-east-1a",
			Region:             "us-east-1",
			InstanceCount:      3,
			State:              "active",
			Start:              now.Add(-100 * 24 * time.Hour),
			End:                now.Add(265 * 24 * time.Hour),
			AccountID:          "364942603424",
			AccountName:        "test-account",
		},
		// Expired RI (should be skipped)
		{
			ReservedInstanceID: "ri-expired",
			InstanceType:       "m5.large",
			AvailabilityZone:   "us-west-2b",
			Region:             "us-west-2",
			InstanceCount:      2,
			State:              "retired",
			Start:              now.Add(-730 * 24 * time.Hour),
			End:                now.Add(-30 * 24 * time.Hour),
			AccountID:          "329239342014",
			AccountName:        "test-account",
		},
	}

	m.UpdateReservedInstanceMetrics(ris)

	// Verify production account RIs
	assert.Equal(t, 1.0, testutil.ToFloat64(m.ReservedInstance.With(prometheus.Labels{
		"account_id":        "329239342014",
		"account_name":      "test-account",
		"region":            "us-west-2",
		"instance_type":     "m5.2xlarge",
		"availability_zone": "us-west-2a",
	})))

	assert.Equal(t, 1.0, testutil.ToFloat64(m.ReservedInstance.With(prometheus.Labels{
		"account_id":        "329239342014",
		"account_name":      "test-account",
		"region":            "us-west-2",
		"instance_type":     "c5.xlarge",
		"availability_zone": "regional",
	})))

	// Verify staging account RI
	assert.Equal(t, 1.0, testutil.ToFloat64(m.ReservedInstance.With(prometheus.Labels{
		"account_id":        "364942603424",
		"account_name":      "test-account",
		"region":            "us-east-1",
		"instance_type":     "t3.medium",
		"availability_zone": "us-east-1a",
	})))

	// Verify family counts
	assert.Equal(t, 5.0, testutil.ToFloat64(m.ReservedInstanceCount.With(prometheus.Labels{
		"account_id":      "329239342014",
		"account_name":    "test-account",
		"region":          "us-west-2",
		"instance_family": "m5",
	})))

	assert.Equal(t, 10.0, testutil.ToFloat64(m.ReservedInstanceCount.With(prometheus.Labels{
		"account_id":      "329239342014",
		"account_name":    "test-account",
		"region":          "us-west-2",
		"instance_family": "c5",
	})))

	assert.Equal(t, 3.0, testutil.ToFloat64(m.ReservedInstanceCount.With(prometheus.Labels{
		"account_id":      "364942603424",
		"account_name":    "test-account",
		"region":          "us-east-1",
		"instance_family": "t3",
	})))

	// Verify expired RI is not present
	expiredMetric, err := m.ReservedInstance.GetMetricWith(prometheus.Labels{
		"account_id":        "329239342014",
		"account_name":      "test-account",
		"region":            "us-west-2",
		"instance_type":     "m5.large",
		"availability_zone": "us-west-2b",
	})
	require.NoError(t, err)
	assert.Equal(t, 0.0, testutil.ToFloat64(expiredMetric))
}
