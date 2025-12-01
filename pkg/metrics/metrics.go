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
	"sync"
	"time"

	"github.com/nextdoor/lumina/pkg/config"
	"github.com/prometheus/client_golang/prometheus"
)

// Metrics holds all Prometheus metrics for the Lumina controller.
// These metrics provide observability into controller health, AWS account
// validation status, and data collection freshness.
type Metrics struct {
	// config holds the Lumina configuration for accessing label names and settings
	config *config.Config

	// lastUpdateTimes tracks when each data type was last updated.
	// Key format: "account_id:account_name:region:data_type" (e.g., "123456789012:Production:us-west-2:ec2_instances")
	// This is used by the background goroutine to calculate age for DataFreshness metrics.
	lastUpdateTimes map[string]time.Time
	lastUpdateMu    sync.RWMutex

	// stopCh signals the background goroutine to stop when the controller shuts down
	stopCh chan struct{}
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

	// DataFreshness stores the age (in seconds) of cached data since the last
	// successful update. A value of 60 means the data is 60 seconds old.
	// This metric is automatically updated every second by a background goroutine.
	// This enables direct alerting on stale data (e.g., lumina_data_freshness_seconds > 600).
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

	// EC2Instance indicates the presence of an EC2 instance.
	// Value is always 1 when the instance exists and is running. When the instance
	// is stopped or terminated, the metric is deleted entirely (not set to 0).
	// Labels: account_id, region, instance_type, availability_zone, instance_id, tenancy, platform
	EC2Instance *prometheus.GaugeVec

	// EC2InstanceCount tracks the count of running instances by instance family.
	// This provides a higher-level view of EC2 inventory without per-instance granularity.
	// Labels: account_id, region, instance_family
	EC2InstanceCount *prometheus.GaugeVec

	// EC2InstanceHourlyCost tracks the effective hourly cost for each EC2 instance after
	// applying all discounts (Reserved Instances, Savings Plans, spot pricing).
	// This enables per-instance cost tracking and chargeback. Value is in USD/hour.
	// Labels: instance_id, account_id, region, instance_type, cost_type, availability_zone, lifecycle, pricing_accuracy
	EC2InstanceHourlyCost *prometheus.GaugeVec

	// SavingsPlanCurrentUtilizationRate tracks the current hourly rate being consumed by
	// instances covered by this Savings Plan. This is a snapshot of current usage ($/hour).
	// Labels: savings_plan_arn, account_id, type
	SavingsPlanCurrentUtilizationRate *prometheus.GaugeVec

	// SavingsPlanRemainingCapacity tracks the unused capacity in $/hour for a Savings Plan.
	// Calculated as: HourlyCommitment - CurrentUtilizationRate
	// Can be negative if over-utilized (spillover to on-demand rates).
	// Labels: savings_plan_arn, account_id, type
	SavingsPlanRemainingCapacity *prometheus.GaugeVec

	// SavingsPlanUtilizationPercent tracks the utilization percentage of a Savings Plan.
	// Calculated as: (CurrentUtilizationRate / HourlyCommitment) * 100
	// Can exceed 100% if the SP is over-utilized.
	// Labels: savings_plan_arn, account_id, type
	SavingsPlanUtilizationPercent *prometheus.GaugeVec
}

// NewMetrics creates and registers all Prometheus metrics with the provided
// registry. The registry is typically the controller-runtime metrics registry
// (ctrlmetrics.Registry) which exposes metrics via the /metrics endpoint.
//
// The config parameter is used to determine:
//   - Custom metric label names (e.g., cluster_name, account_name)
//   - Whether instance metrics should be disabled
//
// Example usage:
//
//	import ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
//	metrics := metrics.NewMetrics(ctrlmetrics.Registry, cfg)
//	metrics.ControllerRunning.Set(1)
func NewMetrics(reg prometheus.Registerer, cfg *config.Config) *Metrics {
	m := &Metrics{
		config:          cfg,
		lastUpdateTimes: make(map[string]time.Time),
		stopCh:          make(chan struct{}),

		ControllerRunning: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "lumina_controller_running",
			Help: "Indicates whether the Lumina controller is running (1 = running)",
		}),

		AccountValidationStatus: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "lumina_account_validation_status",
			Help: "AWS account validation status (1 = success, 0 = failed)",
		}, []string{cfg.GetAccountIDLabel(), cfg.GetAccountNameLabel()}),

		AccountValidationLastSuccess: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "lumina_account_validation_last_success_timestamp",
			Help: "Unix timestamp of last successful validation",
		}, []string{cfg.GetAccountIDLabel(), cfg.GetAccountNameLabel()}),

		AccountValidationDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "lumina_account_validation_duration_seconds",
			Help: "Time taken to validate account access",
			// Buckets cover 100ms to 10 seconds, reasonable for AssumeRole calls
			Buckets: []float64{0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		}, []string{cfg.GetAccountIDLabel(), cfg.GetAccountNameLabel()}),

		DataFreshness: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "lumina_data_freshness_seconds",
			Help: "Age of cached data in seconds since last successful update (updated every second)",
		}, []string{cfg.GetAccountIDLabel(), cfg.GetAccountNameLabel(), cfg.GetRegionLabel(), LabelDataType}),

		DataLastSuccess: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "lumina_data_last_success",
			Help: "Indicator of whether last data collection succeeded (1 = success, 0 = failed, Phase 2+)",
		}, []string{cfg.GetAccountIDLabel(), cfg.GetAccountNameLabel(), cfg.GetRegionLabel(), LabelDataType}),

		ReservedInstance: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "ec2_reserved_instance",
			Help: "Indicates presence of a Reserved Instance (1 = exists, metric absent = does not exist)",
		}, []string{
			cfg.GetAccountIDLabel(),
			cfg.GetAccountNameLabel(),
			cfg.GetRegionLabel(),
			LabelInstanceType,
			LabelAvailabilityZone,
		}),

		ReservedInstanceCount: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "ec2_reserved_instance_count",
			Help: "Count of Reserved Instances by instance family",
		}, []string{cfg.GetAccountIDLabel(), cfg.GetAccountNameLabel(), cfg.GetRegionLabel(), LabelInstanceFamily}),

		SavingsPlanCommitment: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "savings_plan_hourly_commitment",
			Help: "Hourly commitment amount ($/hour) for a Savings Plan",
		}, []string{
			LabelSavingsPlanARN,
			cfg.GetAccountIDLabel(),
			cfg.GetAccountNameLabel(),
			LabelType,
			cfg.GetRegionLabel(),
			LabelInstanceFamily,
		}),

		SavingsPlanRemainingHours: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "savings_plan_remaining_hours",
			Help: "Number of hours remaining until Savings Plan expires",
		}, []string{LabelSavingsPlanARN, cfg.GetAccountIDLabel(), cfg.GetAccountNameLabel(), LabelType}),

		EC2Instance: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "ec2_instance",
			Help: "Indicates presence of a running EC2 instance (1 = exists, metric absent = stopped or terminated)",
		}, []string{
			cfg.GetAccountIDLabel(),
			cfg.GetAccountNameLabel(),
			cfg.GetRegionLabel(),
			LabelInstanceType,
			LabelAvailabilityZone,
			LabelInstanceID,
			LabelTenancy,
			LabelPlatform,
		}),

		EC2InstanceCount: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "ec2_instance_count",
			Help: "Count of running EC2 instances by instance family",
		}, []string{cfg.GetAccountIDLabel(), cfg.GetAccountNameLabel(), cfg.GetRegionLabel(), LabelInstanceFamily}),

		EC2InstanceHourlyCost: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "ec2_instance_hourly_cost",
			Help: "Effective hourly cost for an EC2 instance after applying all discounts (USD/hour)",
		}, []string{
			LabelInstanceID,
			cfg.GetAccountIDLabel(),
			cfg.GetAccountNameLabel(),
			cfg.GetRegionLabel(),
			LabelInstanceType,
			LabelCostType,
			LabelAvailabilityZone,
			LabelLifecycle,
			LabelPricingAccuracy,
			cfg.GetNodeNameLabel(),
			cfg.GetClusterNameLabel(),
			cfg.GetHostNameLabel(),
		}),

		SavingsPlanCurrentUtilizationRate: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "savings_plan_current_utilization_rate",
			Help: "Current hourly rate being consumed by instances covered by this Savings Plan (USD/hour)",
		}, []string{LabelSavingsPlanARN, cfg.GetAccountIDLabel(), cfg.GetAccountNameLabel(), LabelType}),

		SavingsPlanRemainingCapacity: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "savings_plan_remaining_capacity",
			Help: "Unused capacity in USD/hour for a Savings Plan (negative if over-utilized)",
		}, []string{LabelSavingsPlanARN, cfg.GetAccountIDLabel(), cfg.GetAccountNameLabel(), LabelType}),

		SavingsPlanUtilizationPercent: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "savings_plan_utilization_percent",
			Help: "Utilization percentage of a Savings Plan (can exceed 100% if over-utilized)",
		}, []string{LabelSavingsPlanARN, cfg.GetAccountIDLabel(), cfg.GetAccountNameLabel(), LabelType}),
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
		m.EC2Instance,
		m.EC2InstanceCount,
		m.EC2InstanceHourlyCost,
		m.SavingsPlanCurrentUtilizationRate,
		m.SavingsPlanRemainingCapacity,
		m.SavingsPlanUtilizationPercent,
	)

	// Start background goroutine to update data freshness metrics every second
	go m.updateDataFreshnessLoop()

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
		m.config.GetAccountIDLabel():   accountID,
		m.config.GetAccountNameLabel(): accountName,
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

// MarkDataUpdated marks that data of a specific type has been successfully updated.
// This records the current timestamp, which is used by the background goroutine to
// calculate the age for the lumina_data_freshness_seconds metric.
//
// Parameters:
//   - accountID: AWS account ID (can be empty string for global data like pricing)
//   - accountName: AWS account name (can be empty string for global data)
//   - region: AWS region (can be empty string for global data)
//   - dataType: Type of data (e.g., "ec2_instances", "pricing", "spot-pricing")
//
// Example usage:
//
//	metrics.MarkDataUpdated("329239342014", "Production", "us-west-2", "ec2_instances")
//	metrics.MarkDataUpdated("", "", "", "pricing")  // Global pricing data
func (m *Metrics) MarkDataUpdated(accountID, accountName, region, dataType string) {
	key := accountID + ":" + accountName + ":" + region + ":" + dataType
	m.lastUpdateMu.Lock()
	m.lastUpdateTimes[key] = time.Now()
	m.lastUpdateMu.Unlock()
}

// updateDataFreshnessLoop runs in a background goroutine and updates all
// lumina_data_freshness_seconds metrics every second. This calculates the age
// of each data type by comparing the current time with the last update time.
//
// The loop continues until m.stopCh is closed (controller shutdown).
func (m *Metrics) updateDataFreshnessLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.updateAllDataFreshnessMetrics()
		case <-m.stopCh:
			return
		}
	}
}

// updateAllDataFreshnessMetrics updates all data freshness gauges with current ages.
// This is called every second by the background goroutine.
func (m *Metrics) updateAllDataFreshnessMetrics() {
	now := time.Now()

	m.lastUpdateMu.RLock()
	defer m.lastUpdateMu.RUnlock()

	for key, lastUpdate := range m.lastUpdateTimes {
		// Parse key back into labels (format: "account_id:account_name:region:data_type")
		parts := splitKey(key)
		if len(parts) != 4 {
			continue // Invalid key format, skip
		}

		accountID, accountName, region, dataType := parts[0], parts[1], parts[2], parts[3]
		age := now.Sub(lastUpdate).Seconds()

		m.DataFreshness.With(prometheus.Labels{
			m.config.GetAccountIDLabel():   accountID,
			m.config.GetAccountNameLabel(): accountName,
			m.config.GetRegionLabel():      region,
			LabelDataType:                  dataType,
		}).Set(age)
	}
}

// splitKey splits a key in "account_id:account_name:region:data_type" format into parts.
// Handles empty account_id, account_name, or region (they become empty strings in the parts).
func splitKey(key string) []string {
	// Simple split - empty values become empty strings
	parts := make([]string, 0, 4)
	start := 0
	for i := 0; i < len(key); i++ {
		if key[i] == ':' {
			parts = append(parts, key[start:i])
			start = i + 1
		}
	}
	// Add the last part (after the last colon)
	if start < len(key) {
		parts = append(parts, key[start:])
	} else {
		parts = append(parts, "")
	}
	return parts
}

// Stop signals the background goroutine to stop updating metrics.
// This should be called when the controller is shutting down.
func (m *Metrics) Stop() {
	close(m.stopCh)
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
		m.config.GetAccountIDLabel():   accountID,
		m.config.GetAccountNameLabel(): accountName,
	}

	// Delete all account-specific metrics
	m.AccountValidationStatus.Delete(labels)
	m.AccountValidationLastSuccess.Delete(labels)
	m.AccountValidationDuration.Delete(labels)
}
