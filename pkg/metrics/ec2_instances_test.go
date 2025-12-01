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
)

// TestUpdateEC2InstanceMetrics_BasicFunctionality tests the basic behavior
// of updating EC2 instance metrics.
func TestUpdateEC2InstanceMetrics_BasicFunctionality(t *testing.T) {
	// Create test registry and metrics
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg, newTestConfig())

	// Create test instances
	instances := []aws.Instance{
		{
			InstanceID:       "i-001",
			InstanceType:     "m5.xlarge",
			AvailabilityZone: "us-west-2a",
			Region:           "us-west-2",
			AccountID:        "123456789012",
			AccountName:      "test-account",
			State:            "running",
			LaunchTime:       time.Now(),
			Tenancy:          "default",
			Platform:         "",
		},
		{
			InstanceID:       "i-002",
			InstanceType:     "m5.2xlarge",
			AvailabilityZone: "us-west-2b",
			Region:           "us-west-2",
			AccountID:        "123456789012",
			AccountName:      "test-account",
			State:            "running",
			LaunchTime:       time.Now(),
			Tenancy:          "default",
			Platform:         "",
		},
		{
			InstanceID:       "i-003",
			InstanceType:     "c5.large",
			AvailabilityZone: "us-west-2a",
			Region:           "us-west-2",
			AccountID:        "123456789012",
			AccountName:      "test-account",
			State:            "running",
			LaunchTime:       time.Now(),
			Tenancy:          "default",
			Platform:         "",
		},
	}

	// Update metrics
	m.UpdateEC2InstanceMetrics(instances)

	// Verify ec2_instance metric for each instance
	for _, inst := range instances {
		value := testutil.ToFloat64(m.EC2Instance.With(prometheus.Labels{
			"account_id":        inst.AccountID,
			"account_name":      inst.AccountName,
			"region":            inst.Region,
			"instance_type":     inst.InstanceType,
			"availability_zone": inst.AvailabilityZone,
			"instance_id":       inst.InstanceID,
			"tenancy":           "default",
			"platform":          "linux",
		}))
		assert.Equal(t, 1.0, value, "Expected ec2_instance metric to be 1 for instance %s", inst.InstanceID)
	}

	// Verify ec2_instance_count metric
	// m5 family should have 2 instances
	m5Count := testutil.ToFloat64(m.EC2InstanceCount.With(prometheus.Labels{
		"account_id":      "123456789012",
		"account_name":    "test-account",
		"region":          "us-west-2",
		"instance_family": "m5",
	}))
	assert.Equal(t, 2.0, m5Count, "Expected m5 family count to be 2")

	// c5 family should have 1 instance
	c5Count := testutil.ToFloat64(m.EC2InstanceCount.With(prometheus.Labels{
		"account_id":      "123456789012",
		"account_name":    "test-account",
		"region":          "us-west-2",
		"instance_family": "c5",
	}))
	assert.Equal(t, 1.0, c5Count, "Expected c5 family count to be 1")
}

// TestUpdateEC2InstanceMetrics_StateFiltering tests that only running instances
// are included in metrics.
func TestUpdateEC2InstanceMetrics_StateFiltering(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg, newTestConfig())

	instances := []aws.Instance{
		{
			InstanceID:       "i-running",
			InstanceType:     "m5.xlarge",
			AvailabilityZone: "us-west-2a",
			Region:           "us-west-2",
			AccountID:        "123456789012",
			AccountName:      "test-account",
			State:            "running",
			LaunchTime:       time.Now(),
			Tenancy:          "default",
			Platform:         "",
		},
		{
			InstanceID:       "i-stopped",
			InstanceType:     "m5.xlarge",
			AvailabilityZone: "us-west-2a",
			Region:           "us-west-2",
			AccountID:        "123456789012",
			AccountName:      "test-account",
			State:            "stopped",
			LaunchTime:       time.Now(),
			Tenancy:          "default",
			Platform:         "",
		},
		{
			InstanceID:       "i-terminated",
			InstanceType:     "m5.xlarge",
			AvailabilityZone: "us-west-2a",
			Region:           "us-west-2",
			AccountID:        "123456789012",
			AccountName:      "test-account",
			State:            "terminated",
			LaunchTime:       time.Now(),
			Tenancy:          "default",
			Platform:         "",
		},
	}

	m.UpdateEC2InstanceMetrics(instances)

	// Only running instance should have a metric
	runningValue := testutil.ToFloat64(m.EC2Instance.With(prometheus.Labels{
		"account_id":        "123456789012",
		"account_name":      "test-account",
		"region":            "us-west-2",
		"instance_type":     "m5.xlarge",
		"availability_zone": "us-west-2a",
		"instance_id":       "i-running",
		"tenancy":           "default",
		"platform":          "linux",
	}))
	assert.Equal(t, 1.0, runningValue, "Expected running instance to have metric value 1")

	// Verify counts only include running instance
	familyCount := testutil.ToFloat64(m.EC2InstanceCount.With(prometheus.Labels{
		"account_id":      "123456789012",
		"account_name":    "test-account",
		"region":          "us-west-2",
		"instance_family": "m5",
	}))
	assert.Equal(t, 1.0, familyCount, "Expected family count to only include running instance")
}

// TestUpdateEC2InstanceMetrics_ResetBehavior tests that metrics are properly
// reset on each update, removing old instances.
func TestUpdateEC2InstanceMetrics_ResetBehavior(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg, newTestConfig())

	// First update with 2 instances
	initialInstances := []aws.Instance{
		{
			InstanceID:       "i-001",
			InstanceType:     "m5.xlarge",
			AvailabilityZone: "us-west-2a",
			Region:           "us-west-2",
			AccountID:        "123456789012",
			AccountName:      "test-account",
			State:            "running",
			LaunchTime:       time.Now(),
			Tenancy:          "default",
			Platform:         "",
		},
		{
			InstanceID:       "i-002",
			InstanceType:     "m5.2xlarge",
			AvailabilityZone: "us-west-2b",
			Region:           "us-west-2",
			AccountID:        "123456789012",
			AccountName:      "test-account",
			State:            "running",
			LaunchTime:       time.Now(),
			Tenancy:          "default",
			Platform:         "",
		},
	}

	m.UpdateEC2InstanceMetrics(initialInstances)

	// Verify initial family count
	initialCount := testutil.ToFloat64(m.EC2InstanceCount.With(prometheus.Labels{
		"account_id":      "123456789012",
		"account_name":    "test-account",
		"region":          "us-west-2",
		"instance_family": "m5",
	}))
	assert.Equal(t, 2.0, initialCount, "Expected initial m5 family count to be 2")

	// Second update with only 1 instance (i-001 terminated)
	updatedInstances := []aws.Instance{
		{
			InstanceID:       "i-002",
			InstanceType:     "m5.2xlarge",
			AvailabilityZone: "us-west-2b",
			Region:           "us-west-2",
			AccountID:        "123456789012",
			AccountName:      "test-account",
			State:            "running",
			LaunchTime:       time.Now(),
			Tenancy:          "default",
			Platform:         "",
		},
	}

	m.UpdateEC2InstanceMetrics(updatedInstances)

	// Verify updated family count (should be 1, not 2)
	updatedCount := testutil.ToFloat64(m.EC2InstanceCount.With(prometheus.Labels{
		"account_id":      "123456789012",
		"account_name":    "test-account",
		"region":          "us-west-2",
		"instance_family": "m5",
	}))
	assert.Equal(t, 1.0, updatedCount, "Expected updated m5 family count to be 1")

	// Verify i-001 metric no longer exists (should be 0 after reset)
	i001Value := testutil.ToFloat64(m.EC2Instance.With(prometheus.Labels{
		"account_id":        "123456789012",
		"account_name":      "test-account",
		"region":            "us-west-2",
		"instance_type":     "m5.xlarge",
		"availability_zone": "us-west-2a",
		"instance_id":       "i-001",
		"tenancy":           "default",
		"platform":          "linux",
	}))
	assert.Equal(t, 0.0, i001Value, "Expected i-001 metric to be removed after reset")
}

// TestUpdateEC2InstanceMetrics_MultiAccountRegion tests metrics across
// multiple accounts and regions.
func TestUpdateEC2InstanceMetrics_MultiAccountRegion(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg, newTestConfig())

	instances := []aws.Instance{
		{
			InstanceID:       "i-001",
			InstanceType:     "m5.xlarge",
			AvailabilityZone: "us-west-2a",
			Region:           "us-west-2",
			AccountID:        "111111111111",
			AccountName:      "account-1",
			State:            "running",
			LaunchTime:       time.Now(),
			Tenancy:          "default",
			Platform:         "",
		},
		{
			InstanceID:       "i-002",
			InstanceType:     "m5.xlarge",
			AvailabilityZone: "us-east-1a",
			Region:           "us-east-1",
			AccountID:        "111111111111",
			AccountName:      "account-1",
			State:            "running",
			LaunchTime:       time.Now(),
			Tenancy:          "default",
			Platform:         "",
		},
		{
			InstanceID:       "i-003",
			InstanceType:     "c5.large",
			AvailabilityZone: "us-west-2a",
			Region:           "us-west-2",
			AccountID:        "222222222222",
			AccountName:      "account-2",
			State:            "running",
			LaunchTime:       time.Now(),
			Tenancy:          "default",
			Platform:         "",
		},
	}

	m.UpdateEC2InstanceMetrics(instances)

	// Verify family counts are separated by account+region
	m5Account1West := testutil.ToFloat64(m.EC2InstanceCount.With(prometheus.Labels{
		"account_id":      "111111111111",
		"account_name":    "account-1",
		"region":          "us-west-2",
		"instance_family": "m5",
	}))
	assert.Equal(t, 1.0, m5Account1West, "Expected m5 count for account 1 us-west-2 to be 1")

	m5Account1East := testutil.ToFloat64(m.EC2InstanceCount.With(prometheus.Labels{
		"account_id":      "111111111111",
		"account_name":    "account-1",
		"region":          "us-east-1",
		"instance_family": "m5",
	}))
	assert.Equal(t, 1.0, m5Account1East, "Expected m5 count for account 1 us-east-1 to be 1")
}

// TestUpdateEC2InstanceMetrics_EmptyInput tests behavior with empty instance list.
func TestUpdateEC2InstanceMetrics_EmptyInput(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg, newTestConfig())

	// First add some instances
	instances := []aws.Instance{
		{
			InstanceID:       "i-001",
			InstanceType:     "m5.xlarge",
			AvailabilityZone: "us-west-2a",
			Region:           "us-west-2",
			AccountID:        "123456789012",
			AccountName:      "test-account",
			State:            "running",
			LaunchTime:       time.Now(),
			Tenancy:          "default",
			Platform:         "",
		},
	}
	m.UpdateEC2InstanceMetrics(instances)

	// Verify instance exists
	initialValue := testutil.ToFloat64(m.EC2Instance.With(prometheus.Labels{
		"account_id":        "123456789012",
		"account_name":      "test-account",
		"region":            "us-west-2",
		"instance_type":     "m5.xlarge",
		"availability_zone": "us-west-2a",
		"instance_id":       "i-001",
		"tenancy":           "default",
		"platform":          "linux",
	}))
	assert.Equal(t, 1.0, initialValue, "Expected initial instance metric to be 1")

	// Update with empty list (all instances terminated)
	m.UpdateEC2InstanceMetrics([]aws.Instance{})

	// Verify all metrics are reset to 0
	emptyValue := testutil.ToFloat64(m.EC2Instance.With(prometheus.Labels{
		"account_id":        "123456789012",
		"account_name":      "test-account",
		"region":            "us-west-2",
		"instance_type":     "m5.xlarge",
		"availability_zone": "us-west-2a",
		"instance_id":       "i-001",
		"tenancy":           "default",
		"platform":          "linux",
	}))
	assert.Equal(t, 0.0, emptyValue, "Expected instance metric to be reset to 0")

	emptyCount := testutil.ToFloat64(m.EC2InstanceCount.With(prometheus.Labels{
		"account_id":      "123456789012",
		"account_name":    "test-account",
		"region":          "us-west-2",
		"instance_family": "m5",
	}))
	assert.Equal(t, 0.0, emptyCount, "Expected family count to be reset to 0")
}

// TestUpdateEC2InstanceMetrics_InstanceFamilyExtraction tests the instance
// family extraction logic with various instance type formats.
func TestUpdateEC2InstanceMetrics_InstanceFamilyExtraction(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg, newTestConfig())

	testCases := []struct {
		instanceType     string
		expectedFamily   string
		instanceID       string
		availabilityZone string
	}{
		{"m5.xlarge", "m5", "i-m5", "us-west-2a"},
		{"c5.2xlarge", "c5", "i-c5", "us-west-2a"},
		{"r5d.4xlarge", "r5d", "i-r5d", "us-west-2a"},
		{"t3.micro", "t3", "i-t3", "us-west-2a"},
		{"x1e.32xlarge", "x1e", "i-x1e", "us-west-2a"},
	}

	instances := make([]aws.Instance, 0, len(testCases))
	for _, tc := range testCases {
		instances = append(instances, aws.Instance{
			InstanceID:       tc.instanceID,
			InstanceType:     tc.instanceType,
			AvailabilityZone: tc.availabilityZone,
			Region:           "us-west-2",
			AccountID:        "123456789012",
			AccountName:      "test-account",
			State:            "running",
			LaunchTime:       time.Now(),
			Tenancy:          "default",
			Platform:         "",
		})
	}

	m.UpdateEC2InstanceMetrics(instances)

	// Verify each family has exactly 1 instance
	for _, tc := range testCases {
		familyCount := testutil.ToFloat64(m.EC2InstanceCount.With(prometheus.Labels{
			"account_id":      "123456789012",
			"account_name":    "test-account",
			"region":          "us-west-2",
			"instance_family": tc.expectedFamily,
		}))
		assert.Equal(t, 1.0, familyCount, "Expected family %s count to be 1", tc.expectedFamily)
	}
}

// TestUpdateEC2InstanceMetrics_MalformedInstanceType tests behavior with
// malformed instance types (edge case).
func TestUpdateEC2InstanceMetrics_MalformedInstanceType(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg, newTestConfig())

	instances := []aws.Instance{
		{
			InstanceID:       "i-malformed",
			InstanceType:     "malformed", // No period, so family should be "malformed"
			AvailabilityZone: "us-west-2a",
			Region:           "us-west-2",
			AccountID:        "123456789012",
			AccountName:      "test-account",
			State:            "running",
			LaunchTime:       time.Now(),
			Tenancy:          "default",
			Platform:         "",
		},
	}

	// Should not panic
	m.UpdateEC2InstanceMetrics(instances)

	// Verify metric exists with malformed family
	familyCount := testutil.ToFloat64(m.EC2InstanceCount.With(prometheus.Labels{
		"account_id":      "123456789012",
		"account_name":    "test-account",
		"region":          "us-west-2",
		"instance_family": "malformed",
	}))
	assert.Equal(t, 1.0, familyCount, "Expected malformed family count to be 1")
}

// Note: extractInstanceFamily is tested in reserved_instances_test.go
// since it's a shared helper function used by both RI and EC2 metrics.
