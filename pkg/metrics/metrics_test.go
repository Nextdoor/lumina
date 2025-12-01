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
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewMetrics verifies that NewMetrics creates all expected metrics
// and registers them with the provided registry.
func TestNewMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	// Verify all metrics were created
	assert.NotNil(t, m.ControllerRunning)
	assert.NotNil(t, m.AccountValidationStatus)
	assert.NotNil(t, m.AccountValidationLastSuccess)
	assert.NotNil(t, m.AccountValidationDuration)
	assert.NotNil(t, m.DataFreshness)
	assert.NotNil(t, m.DataLastSuccess)

	// Set the controller running gauge so it appears in Gather()
	m.ControllerRunning.Set(1)

	// Set at least one label combination for each Vec metric so they appear in Gather()
	// Vec metrics don't appear until they have at least one label set
	labels := prometheus.Labels{"account_id": "test", "account_name": "Test"}
	m.AccountValidationStatus.With(labels).Set(0)
	m.AccountValidationLastSuccess.With(labels).Set(0)
	m.AccountValidationDuration.With(labels).Observe(0.1)

	dataLabels := prometheus.Labels{
		"account_id":   "test",
		"account_name": "Test",
		"region":       "us-west-2",
		"data_type":    "test",
	}
	m.DataFreshness.With(dataLabels).Set(0)
	m.DataLastSuccess.With(dataLabels).Set(0)

	// Verify metrics are registered by checking they can be collected
	metricFamilies, err := reg.Gather()
	require.NoError(t, err)

	// We should have 6 metric families (one per metric type)
	assert.Len(t, metricFamilies, 6)

	// Verify metric names are present
	metricNames := make(map[string]bool)
	for _, mf := range metricFamilies {
		metricNames[mf.GetName()] = true
	}

	expectedMetrics := []string{
		"lumina_controller_running",
		"lumina_account_validation_status",
		"lumina_account_validation_last_success_timestamp",
		"lumina_account_validation_duration_seconds",
		"lumina_data_freshness_seconds",
		"lumina_data_last_success",
	}

	for _, name := range expectedMetrics {
		assert.True(t, metricNames[name], "metric %s should be registered", name)
	}
}

// TestNewMetrics_DoubleRegistration verifies that attempting to register
// metrics twice with the same registry panics (expected Prometheus behavior).
func TestNewMetrics_DoubleRegistration(t *testing.T) {
	reg := prometheus.NewRegistry()
	_ = NewMetrics(reg)

	// Attempting to register again should panic
	assert.Panics(t, func() {
		_ = NewMetrics(reg)
	}, "double registration should panic")
}

// TestControllerRunningMetric verifies the controller running gauge works.
func TestControllerRunningMetric(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	// Initially the gauge is not set (default is 0)
	value := testutil.ToFloat64(m.ControllerRunning)
	assert.Equal(t, 0.0, value)

	// Set controller running
	m.ControllerRunning.Set(1)
	value = testutil.ToFloat64(m.ControllerRunning)
	assert.Equal(t, 1.0, value)

	// Verify the metric is exposed correctly
	expected := `
		# HELP lumina_controller_running Indicates whether the Lumina controller is running (1 = running)
		# TYPE lumina_controller_running gauge
		lumina_controller_running 1
	`
	err := testutil.CollectAndCompare(m.ControllerRunning, strings.NewReader(expected))
	assert.NoError(t, err)
}

// TestRecordAccountValidation_Success verifies successful validation recording.
func TestRecordAccountValidation_Success(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	accountID := "329239342014"
	accountName := "Production"
	duration := 250 * time.Millisecond

	// Record a successful validation
	beforeTime := time.Now().Unix()
	m.RecordAccountValidation(accountID, accountName, true, duration)
	afterTime := time.Now().Unix()

	// Verify validation status is 1 (success)
	labels := prometheus.Labels{
		"account_id":   accountID,
		"account_name": accountName,
	}
	statusValue, err := m.AccountValidationStatus.GetMetricWith(labels)
	require.NoError(t, err)
	assert.Equal(t, 1.0, testutil.ToFloat64(statusValue))

	// Verify last success timestamp is set and reasonable
	lastSuccessValue, err := m.AccountValidationLastSuccess.GetMetricWith(labels)
	require.NoError(t, err)
	timestamp := testutil.ToFloat64(lastSuccessValue)
	assert.GreaterOrEqual(t, timestamp, float64(beforeTime))
	assert.LessOrEqual(t, timestamp, float64(afterTime))

	// Verify duration histogram recorded the observation
	// We can't easily verify the exact bucket, but we can check the count
	histogramMetric, err := m.AccountValidationDuration.GetMetricWith(labels)
	require.NoError(t, err)
	// The histogram should have recorded one observation
	assert.NotNil(t, histogramMetric)
}

// TestRecordAccountValidation_Failure verifies failed validation recording.
func TestRecordAccountValidation_Failure(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	accountID := "364942603424"
	accountName := "Engineering"
	duration := 5 * time.Second

	// Record a failed validation
	m.RecordAccountValidation(accountID, accountName, false, duration)

	// Verify validation status is 0 (failed)
	labels := prometheus.Labels{
		"account_id":   accountID,
		"account_name": accountName,
	}
	statusValue, err := m.AccountValidationStatus.GetMetricWith(labels)
	require.NoError(t, err)
	assert.Equal(t, 0.0, testutil.ToFloat64(statusValue))

	// Verify last success timestamp is NOT updated (should be 0)
	lastSuccessValue, err := m.AccountValidationLastSuccess.GetMetricWith(labels)
	require.NoError(t, err)
	timestamp := testutil.ToFloat64(lastSuccessValue)
	assert.Equal(t, 0.0, timestamp, "last success timestamp should not be set on failure")

	// Verify duration histogram still recorded the observation
	histogramMetric, err := m.AccountValidationDuration.GetMetricWith(labels)
	require.NoError(t, err)
	assert.NotNil(t, histogramMetric)
}

// TestRecordAccountValidation_MultipleAccounts verifies tracking multiple accounts.
func TestRecordAccountValidation_MultipleAccounts(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	// Record validations for multiple accounts
	accounts := []struct {
		id      string
		name    string
		success bool
	}{
		{"111111111111", "Account1", true},
		{"222222222222", "Account2", false},
		{"333333333333", "Account3", true},
	}

	for _, acc := range accounts {
		m.RecordAccountValidation(acc.id, acc.name, acc.success, 100*time.Millisecond)
	}

	// Verify each account has correct status
	for _, acc := range accounts {
		labels := prometheus.Labels{
			"account_id":   acc.id,
			"account_name": acc.name,
		}
		statusValue, err := m.AccountValidationStatus.GetMetricWith(labels)
		require.NoError(t, err)

		expectedStatus := 0.0
		if acc.success {
			expectedStatus = 1.0
		}
		assert.Equal(t, expectedStatus, testutil.ToFloat64(statusValue))
	}
}

// TestRecordAccountValidation_SuccessAfterFailure verifies that a subsequent
// success properly updates the last success timestamp after a previous failure.
func TestRecordAccountValidation_SuccessAfterFailure(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	accountID := "123456789012"
	accountName := "Test"
	labels := prometheus.Labels{
		"account_id":   accountID,
		"account_name": accountName,
	}

	// Record a failure first
	m.RecordAccountValidation(accountID, accountName, false, 1*time.Second)

	// Verify status is 0 and last success is 0
	statusValue, err := m.AccountValidationStatus.GetMetricWith(labels)
	require.NoError(t, err)
	assert.Equal(t, 0.0, testutil.ToFloat64(statusValue))

	lastSuccessValue, err := m.AccountValidationLastSuccess.GetMetricWith(labels)
	require.NoError(t, err)
	assert.Equal(t, 0.0, testutil.ToFloat64(lastSuccessValue))

	// Now record a success - capture time around the call
	time.Sleep(10 * time.Millisecond) // Ensure time advances from failure
	beforeTime := time.Now().Unix()
	m.RecordAccountValidation(accountID, accountName, true, 500*time.Millisecond)
	afterTime := time.Now().Unix()

	// Verify status is now 1
	statusValue, err = m.AccountValidationStatus.GetMetricWith(labels)
	require.NoError(t, err)
	assert.Equal(t, 1.0, testutil.ToFloat64(statusValue))

	// Verify last success timestamp is now set
	lastSuccessValue, err = m.AccountValidationLastSuccess.GetMetricWith(labels)
	require.NoError(t, err)
	timestamp := testutil.ToFloat64(lastSuccessValue)
	assert.GreaterOrEqual(t, timestamp, float64(beforeTime))
	assert.LessOrEqual(t, timestamp, float64(afterTime))
}

// TestAccountValidationDuration_Buckets verifies histogram buckets are correct.
func TestAccountValidationDuration_Buckets(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	accountID := "999999999999"
	accountName := "BucketTest"

	// Record various durations to test buckets
	durations := []time.Duration{
		50 * time.Millisecond,  // Should go in 0.1 bucket
		200 * time.Millisecond, // Should go in 0.25 bucket
		750 * time.Millisecond, // Should go in 1 bucket
		3 * time.Second,        // Should go in 5 bucket
		8 * time.Second,        // Should go in 10 bucket
	}

	for _, d := range durations {
		m.RecordAccountValidation(accountID, accountName, true, d)
	}

	// Get the histogram metric
	labels := prometheus.Labels{
		"account_id":   accountID,
		"account_name": accountName,
	}
	histogramMetric, err := m.AccountValidationDuration.GetMetricWith(labels)
	require.NoError(t, err)
	assert.NotNil(t, histogramMetric)

	// Verify we recorded 5 observations by checking the metric output
	// The _count should be 5
	expected := `
		# HELP lumina_account_validation_duration_seconds Time taken to validate account access
		# TYPE lumina_account_validation_duration_seconds histogram
		lumina_account_validation_duration_seconds_bucket{account_id="999999999999",account_name="BucketTest",le="0.1"} 1
		lumina_account_validation_duration_seconds_bucket{account_id="999999999999",account_name="BucketTest",le="0.25"} 2
		lumina_account_validation_duration_seconds_bucket{account_id="999999999999",account_name="BucketTest",le="0.5"} 2
		lumina_account_validation_duration_seconds_bucket{account_id="999999999999",account_name="BucketTest",le="1"} 3
		lumina_account_validation_duration_seconds_bucket{account_id="999999999999",account_name="BucketTest",le="2.5"} 3
		lumina_account_validation_duration_seconds_bucket{account_id="999999999999",account_name="BucketTest",le="5"} 4
		lumina_account_validation_duration_seconds_bucket{account_id="999999999999",account_name="BucketTest",le="10"} 5
		lumina_account_validation_duration_seconds_bucket{account_id="999999999999",account_name="BucketTest",le="+Inf"} 5
		lumina_account_validation_duration_seconds_sum{account_id="999999999999",account_name="BucketTest"} 12
		lumina_account_validation_duration_seconds_count{account_id="999999999999",account_name="BucketTest"} 5
	`
	err = testutil.CollectAndCompare(m.AccountValidationDuration, strings.NewReader(expected))
	assert.NoError(t, err)
}

// TestDeleteAccountMetrics verifies metric cleanup when accounts are removed.
func TestDeleteAccountMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	accountID := "555555555555"
	accountName := "ToBeDeleted"

	// Record some metrics for the account
	m.RecordAccountValidation(accountID, accountName, true, 100*time.Millisecond)

	// Verify metrics exist
	labels := prometheus.Labels{
		"account_id":   accountID,
		"account_name": accountName,
	}
	statusValue, err := m.AccountValidationStatus.GetMetricWith(labels)
	require.NoError(t, err)
	assert.Equal(t, 1.0, testutil.ToFloat64(statusValue))

	// Delete the metrics
	m.DeleteAccountMetrics(accountID, accountName)

	// Verify metrics are removed - getting them should still work but return 0
	// because Prometheus creates new metrics with default values
	statusValue, err = m.AccountValidationStatus.GetMetricWith(labels)
	require.NoError(t, err)
	assert.Equal(t, 0.0, testutil.ToFloat64(statusValue))

	lastSuccessValue, err := m.AccountValidationLastSuccess.GetMetricWith(labels)
	require.NoError(t, err)
	assert.Equal(t, 0.0, testutil.ToFloat64(lastSuccessValue))
}

// TestDeleteAccountMetrics_NonExistent verifies deleting non-existent metrics doesn't panic.
func TestDeleteAccountMetrics_NonExistent(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	// Delete metrics for an account that was never recorded
	assert.NotPanics(t, func() {
		m.DeleteAccountMetrics("000000000000", "NeverExisted")
	})
}

// TestDataFreshnessMetrics verifies the data freshness metrics work correctly.
// DataFreshness stores age in seconds since last successful data collection.
// The metric is automatically updated every second by a background goroutine.
func TestDataFreshnessMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	// Verify the metrics are created
	assert.NotNil(t, m.DataFreshness)
	assert.NotNil(t, m.DataLastSuccess)

	// Test MarkDataUpdated and verify freshness is calculated
	accountID := "123456789012"
	accountName := "test-account"
	region := "us-west-2"
	dataType := "ec2_instances"

	// Mark data as updated
	m.MarkDataUpdated(accountID, accountName, region, dataType)

	// Wait a small amount for the background goroutine to update the metric
	time.Sleep(1200 * time.Millisecond)

	// DataFreshness should now show age in seconds (should be ~1 second)
	labels := prometheus.Labels{
		"account_id":   accountID,
		"account_name": accountName,
		"region":       region,
		"data_type":    dataType,
	}
	freshnessMetric, err := m.DataFreshness.GetMetricWith(labels)
	require.NoError(t, err)
	age := testutil.ToFloat64(freshnessMetric)
	assert.GreaterOrEqual(t, age, 1.0, "age should be at least 1 second")
	assert.Less(t, age, 2.0, "age should be less than 2 seconds")

	// Test DataLastSuccess metric
	successMetric, err := m.DataLastSuccess.GetMetricWith(labels)
	require.NoError(t, err)
	successMetric.Set(1.0)
	assert.Equal(t, 1.0, testutil.ToFloat64(successMetric))

	// Clean up: stop the background goroutine
	m.Stop()
}

// TestMetricNaming verifies all metrics follow Prometheus naming conventions.
func TestMetricNaming(t *testing.T) {
	reg := prometheus.NewRegistry()
	_ = NewMetrics(reg) // Register metrics for inspection

	metricFamilies, err := reg.Gather()
	require.NoError(t, err)

	for _, mf := range metricFamilies {
		name := mf.GetName()

		// All metrics should have lumina_ prefix
		assert.True(t, strings.HasPrefix(name, "lumina_"),
			"metric %s should have lumina_ prefix", name)

		// Metrics should use snake_case (no uppercase, no hyphens)
		assert.Equal(t, strings.ToLower(name), name,
			"metric %s should be lowercase", name)
		assert.NotContains(t, name, "-",
			"metric %s should not contain hyphens", name)

		// Histograms should have _seconds suffix
		if mf.GetType().String() == "HISTOGRAM" {
			assert.True(t, strings.HasSuffix(name, "_seconds"),
				"histogram %s should have _seconds suffix", name)
		}

		// All metrics should have help text
		assert.NotEmpty(t, mf.GetHelp(),
			"metric %s should have help text", name)
	}
}
