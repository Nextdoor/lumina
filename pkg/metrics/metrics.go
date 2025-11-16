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

// Package metrics provides Prometheus metrics for the Lumina controller.
// It exposes controller health, AWS account validation status, and data
// freshness metrics to enable operational visibility and alerting.
package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Metrics holds all Prometheus metrics for the Lumina controller.
// These metrics provide observability into controller health, AWS account
// validation status, and data collection freshness.
type Metrics struct {
	// ControllerRunning indicates whether the controller is running.
	// This is a simple gauge set to 1 on startup. If the metric disappears
	// from the metrics endpoint, it indicates the controller has crashed.
	ControllerRunning prometheus.Gauge

	// AccountValidationStatus tracks the validation status for each configured
	// AWS account. A value of 1 indicates successful validation (AssumeRole
	// succeeded), while 0 indicates validation failure.
	// Labels: account_id, account_name
	AccountValidationStatus *prometheus.GaugeVec

	// AccountValidationLastSuccess records the Unix timestamp of the last
	// successful validation for each AWS account. This enables alerting on
	// stale validations (e.g., no successful validation in 10+ minutes).
	// Labels: account_id, account_name
	AccountValidationLastSuccess *prometheus.GaugeVec

	// AccountValidationDuration measures the time taken to validate account
	// access via AssumeRole. This helps identify slow or timing-out accounts.
	// Labels: account_id, account_name
	AccountValidationDuration *prometheus.HistogramVec

	// DataFreshness tracks how long it's been since the last successful data
	// collection for each data type and region. This metric will be populated
	// in Phase 2+ when data collection is implemented.
	// Labels: account_id, region, data_type
	DataFreshness *prometheus.GaugeVec

	// DataLastSuccess indicates whether the last data collection attempt
	// succeeded (1) or failed (0). This metric will be populated in Phase 2+.
	// Labels: account_id, region, data_type
	DataLastSuccess *prometheus.GaugeVec

	// ReservedInstance indicates the presence of a Reserved Instance.
	// Value is always 1 when the RI exists. When the RI expires or is removed,
	// the metric is deleted entirely (not set to 0).
	// Labels: account_id, region, instance_type, availability_zone
	ReservedInstance *prometheus.GaugeVec

	// ReservedInstanceCount tracks the count of RIs by instance family.
	// This provides a higher-level view of RI inventory without per-type granularity.
	// Labels: account_id, region, instance_family
	ReservedInstanceCount *prometheus.GaugeVec

	// SavingsPlanCommitment tracks the hourly commitment amount ($/hour) for each Savings Plan.
	// Value is the fixed hourly commitment. When the SP expires or is removed, the metric
	// is deleted entirely.
	// Labels: savings_plan_arn, account_id, type, region, instance_family
	SavingsPlanCommitment *prometheus.GaugeVec

	// SavingsPlanRemainingHours tracks the number of hours remaining until the SP expires.
	// This enables alerting on upcoming expirations for renewal planning.
	// Labels: savings_plan_arn, account_id, type
	SavingsPlanRemainingHours *prometheus.GaugeVec
}

// NewMetrics creates and registers all Prometheus metrics with the provided
// registry. The registry is typically the controller-runtime metrics registry
// (ctrlmetrics.Registry) which exposes metrics via the /metrics endpoint.
//
// Example usage:
//
//	import ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
//	metrics := metrics.NewMetrics(ctrlmetrics.Registry)
//	metrics.ControllerRunning.Set(1)
func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		ControllerRunning: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "lumina_controller_running",
			Help: "Indicates whether the Lumina controller is running (1 = running)",
		}),

		AccountValidationStatus: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "lumina_account_validation_status",
			Help: "AWS account validation status (1 = success, 0 = failed)",
		}, []string{"account_id", "account_name"}),

		AccountValidationLastSuccess: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "lumina_account_validation_last_success_timestamp",
			Help: "Unix timestamp of last successful validation",
		}, []string{"account_id", "account_name"}),

		AccountValidationDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "lumina_account_validation_duration_seconds",
			Help: "Time taken to validate account access",
			// Buckets cover 100ms to 10 seconds, reasonable for AssumeRole calls
			Buckets: []float64{0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		}, []string{"account_id", "account_name"}),

		DataFreshness: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "lumina_data_freshness_seconds",
			Help: "Seconds since last successful data collection (Phase 2+)",
		}, []string{"account_id", "region", "data_type"}),

		DataLastSuccess: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "lumina_data_last_success",
			Help: "Indicator of whether last data collection succeeded (1 = success, 0 = failed, Phase 2+)",
		}, []string{"account_id", "region", "data_type"}),

		ReservedInstance: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "ec2_reserved_instance",
			Help: "Indicates presence of a Reserved Instance (1 = exists, metric absent = does not exist)",
		}, []string{"account_id", "region", "instance_type", "availability_zone"}),

		ReservedInstanceCount: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "ec2_reserved_instance_count",
			Help: "Count of Reserved Instances by instance family",
		}, []string{"account_id", "region", "instance_family"}),

		SavingsPlanCommitment: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "savings_plan_hourly_commitment",
			Help: "Hourly commitment amount ($/hour) for a Savings Plan",
		}, []string{"savings_plan_arn", "account_id", "type", "region", "instance_family"}),

		SavingsPlanRemainingHours: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "savings_plan_remaining_hours",
			Help: "Number of hours remaining until Savings Plan expires",
		}, []string{"savings_plan_arn", "account_id", "type"}),
	}

	// Register all metrics with the provided registry
	reg.MustRegister(
		m.ControllerRunning,
		m.AccountValidationStatus,
		m.AccountValidationLastSuccess,
		m.AccountValidationDuration,
		m.DataFreshness,
		m.DataLastSuccess,
		m.ReservedInstance,
		m.ReservedInstanceCount,
		m.SavingsPlanCommitment,
		m.SavingsPlanRemainingHours,
	)

	return m
}

// RecordAccountValidation records the result of an AWS account validation
// attempt. This should be called by the account validation reconciler after
// each validation attempt.
//
// Parameters:
//   - accountID: The AWS account ID (e.g., "329239342014")
//   - accountName: The human-readable account name (e.g., "Production")
//   - success: Whether the validation succeeded (AssumeRole worked)
//   - duration: How long the validation took
//
// Example usage:
//
//	start := time.Now()
//	err := validator.ValidateAccount(ctx, accountID, roleARN)
//	duration := time.Since(start)
//	metrics.RecordAccountValidation(accountID, accountName, err == nil, duration)
func (m *Metrics) RecordAccountValidation(accountID, accountName string, success bool, duration time.Duration) {
	labels := prometheus.Labels{
		"account_id":   accountID,
		"account_name": accountName,
	}

	// Record validation duration regardless of success/failure
	m.AccountValidationDuration.With(labels).Observe(duration.Seconds())

	// Update validation status (1 for success, 0 for failure)
	if success {
		m.AccountValidationStatus.With(labels).Set(1)
		// Only update last success timestamp on successful validations
		m.AccountValidationLastSuccess.With(labels).Set(float64(time.Now().Unix()))
	} else {
		m.AccountValidationStatus.With(labels).Set(0)
		// Don't update last success timestamp on failures - we want to track
		// how long it's been since the LAST successful validation
	}
}

// DeleteAccountMetrics removes all metric labels associated with a specific
// AWS account. This should be called when an account is removed from the
// configuration to prevent stale metrics.
//
// Example usage:
//
//	metrics.DeleteAccountMetrics("329239342014", "Production")
func (m *Metrics) DeleteAccountMetrics(accountID, accountName string) {
	labels := prometheus.Labels{
		"account_id":   accountID,
		"account_name": accountName,
	}

	// Delete all account-specific metrics
	m.AccountValidationStatus.Delete(labels)
	m.AccountValidationLastSuccess.Delete(labels)
	m.AccountValidationDuration.Delete(labels)
}
