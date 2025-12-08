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

	"github.com/prometheus/client_golang/prometheus"
)

// TestMetricNameConstants verifies that all exported metric name constants
// match the actual metric names used in the Metrics struct. This ensures
// that external consumers using these constants will query the correct metrics.
func TestMetricNameConstants(t *testing.T) {
	// Create a test registry and metrics instance
	reg := prometheus.NewRegistry()
	cfg := newTestConfig()
	m := NewMetrics(reg, cfg)

	// Test each metric name constant against the actual metric name
	tests := []struct {
		name         string
		constant     string
		actualMetric prometheus.Collector
	}{
		// Controller health metrics
		{
			name:         "ControllerRunning",
			constant:     MetricLuminaControllerRunning,
			actualMetric: m.ControllerRunning,
		},
		{
			name:         "DataFreshnessSeconds",
			constant:     MetricLuminaDataFreshnessSeconds,
			actualMetric: m.DataFreshness,
		},
		{
			name:         "DataLastSuccess",
			constant:     MetricLuminaDataLastSuccess,
			actualMetric: m.DataLastSuccess,
		},
		// Account validation metrics
		{
			name:         "AccountValidationStatus",
			constant:     MetricLuminaAccountValidationStatus,
			actualMetric: m.AccountValidationStatus,
		},
		{
			name:         "AccountValidationLastSuccess",
			constant:     MetricLuminaAccountValidationLastSuccess,
			actualMetric: m.AccountValidationLastSuccess,
		},
		{
			name:         "AccountValidationDurationSeconds",
			constant:     MetricLuminaAccountValidationDurationSeconds,
			actualMetric: m.AccountValidationDuration,
		},
		// Savings Plans metrics
		{
			name:         "SavingsPlanHourlyCommitment",
			constant:     MetricSavingsPlanHourlyCommitment,
			actualMetric: m.SavingsPlanCommitment,
		},
		{
			name:         "SavingsPlanRemainingHours",
			constant:     MetricSavingsPlanRemainingHours,
			actualMetric: m.SavingsPlanRemainingHours,
		},
		{
			name:         "SavingsPlanCurrentUtilizationRate",
			constant:     MetricSavingsPlanCurrentUtilizationRate,
			actualMetric: m.SavingsPlanCurrentUtilizationRate,
		},
		{
			name:         "SavingsPlanRemainingCapacity",
			constant:     MetricSavingsPlanRemainingCapacity,
			actualMetric: m.SavingsPlanRemainingCapacity,
		},
		{
			name:         "SavingsPlanUtilizationPercent",
			constant:     MetricSavingsPlanUtilizationPercent,
			actualMetric: m.SavingsPlanUtilizationPercent,
		},
		// Reserved Instances metrics
		{
			name:         "EC2ReservedInstance",
			constant:     MetricEC2ReservedInstance,
			actualMetric: m.ReservedInstance,
		},
		{
			name:         "EC2ReservedInstanceCount",
			constant:     MetricEC2ReservedInstanceCount,
			actualMetric: m.ReservedInstanceCount,
		},
		// EC2 Instance metrics
		{
			name:         "EC2Instance",
			constant:     MetricEC2Instance,
			actualMetric: m.EC2Instance,
		},
		{
			name:         "EC2InstanceCount",
			constant:     MetricEC2InstanceCount,
			actualMetric: m.EC2InstanceCount,
		},
		{
			name:         "EC2InstanceHourlyCost",
			constant:     MetricEC2InstanceHourlyCost,
			actualMetric: m.EC2InstanceHourlyCost,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Get the actual metric name from the prometheus.Desc
			desc := getMetricDesc(tt.actualMetric)
			if desc == nil {
				t.Fatalf("could not get metric description for %s", tt.name)
			}

			actualName := getMetricName(desc)
			if actualName != tt.constant {
				t.Errorf("metric name mismatch for %s: constant=%q, actual=%q",
					tt.name, tt.constant, actualName)
			}
		})
	}
}

// TestMetricNameConstantsAreUnique verifies that all exported metric name
// constants are unique (no duplicates).
func TestMetricNameConstantsAreUnique(t *testing.T) {
	constants := []string{
		MetricLuminaControllerRunning,
		MetricLuminaDataFreshnessSeconds,
		MetricLuminaDataLastSuccess,
		MetricLuminaAccountValidationStatus,
		MetricLuminaAccountValidationLastSuccess,
		MetricLuminaAccountValidationDurationSeconds,
		MetricSavingsPlanHourlyCommitment,
		MetricSavingsPlanRemainingHours,
		MetricSavingsPlanCurrentUtilizationRate,
		MetricSavingsPlanRemainingCapacity,
		MetricSavingsPlanUtilizationPercent,
		MetricEC2ReservedInstance,
		MetricEC2ReservedInstanceCount,
		MetricEC2Instance,
		MetricEC2InstanceCount,
		MetricEC2InstanceHourlyCost,
	}

	seen := make(map[string]bool)
	for _, constant := range constants {
		if seen[constant] {
			t.Errorf("duplicate metric name constant: %q", constant)
		}
		seen[constant] = true
	}
}

// TestMetricNameConstantsFormat verifies that all metric name constants
// follow Prometheus naming conventions (lowercase with underscores).
func TestMetricNameConstantsFormat(t *testing.T) {
	constants := map[string]string{
		"MetricLuminaControllerRunning":                MetricLuminaControllerRunning,
		"MetricLuminaDataFreshnessSeconds":             MetricLuminaDataFreshnessSeconds,
		"MetricLuminaDataLastSuccess":                  MetricLuminaDataLastSuccess,
		"MetricLuminaAccountValidationStatus":          MetricLuminaAccountValidationStatus,
		"MetricLuminaAccountValidationLastSuccess":     MetricLuminaAccountValidationLastSuccess,
		"MetricLuminaAccountValidationDurationSeconds": MetricLuminaAccountValidationDurationSeconds,
		"MetricSavingsPlanHourlyCommitment":            MetricSavingsPlanHourlyCommitment,
		"MetricSavingsPlanRemainingHours":              MetricSavingsPlanRemainingHours,
		"MetricSavingsPlanCurrentUtilizationRate":      MetricSavingsPlanCurrentUtilizationRate,
		"MetricSavingsPlanRemainingCapacity":           MetricSavingsPlanRemainingCapacity,
		"MetricSavingsPlanUtilizationPercent":          MetricSavingsPlanUtilizationPercent,
		"MetricEC2ReservedInstance":                    MetricEC2ReservedInstance,
		"MetricEC2ReservedInstanceCount":               MetricEC2ReservedInstanceCount,
		"MetricEC2Instance":                            MetricEC2Instance,
		"MetricEC2InstanceCount":                       MetricEC2InstanceCount,
		"MetricEC2InstanceHourlyCost":                  MetricEC2InstanceHourlyCost,
	}

	for name, value := range constants {
		// Check that the value doesn't contain uppercase letters
		for _, char := range value {
			if char >= 'A' && char <= 'Z' {
				t.Errorf("%s contains uppercase letter: %q", name, value)
				break
			}
		}

		// Check that the value doesn't start with a number
		if len(value) > 0 && value[0] >= '0' && value[0] <= '9' {
			t.Errorf("%s starts with a number: %q", name, value)
		}

		// Check that the value only contains alphanumeric and underscores
		for _, char := range value {
			isLowercase := char >= 'a' && char <= 'z'
			isDigit := char >= '0' && char <= '9'
			isUnderscore := char == '_'
			if !isLowercase && !isDigit && !isUnderscore {
				t.Errorf("%s contains invalid character: %q", name, value)
				break
			}
		}
	}
}

// getMetricDesc extracts the prometheus.Desc from a metric collector.
// This is a helper function needed because Prometheus doesn't expose
// the metric name directly on the collector.
func getMetricDesc(collector prometheus.Collector) *prometheus.Desc {
	// Create a channel to receive the metric description
	descChan := make(chan *prometheus.Desc, 1)

	// Prometheus collectors implement Describe() which sends their Desc to a channel
	go func() {
		collector.Describe(descChan)
		close(descChan)
	}()

	// Read the first (and typically only) description
	return <-descChan
}

// getMetricName extracts the metric name from a prometheus.Desc.
// We need to use String() and parse it because Prometheus doesn't
// expose the name directly.
func getMetricName(desc *prometheus.Desc) string {
	// The String() output looks like: Desc{fqName: "metric_name", help: "...", ...}
	// We need to extract "metric_name" from this string
	str := desc.String()

	// Find the start of the fqName value (after "fqName: \"")
	start := 0
	prefix := "fqName: \""
	for i := 0; i < len(str)-len(prefix); i++ {
		if str[i:i+len(prefix)] == prefix {
			start = i + len(prefix)
			break
		}
	}

	if start == 0 {
		return ""
	}

	// Find the end of the fqName value (the closing quote)
	end := start
	for end < len(str) && str[end] != '"' {
		end++
	}

	if end >= len(str) {
		return ""
	}

	return str[start:end]
}
