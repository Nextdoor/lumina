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

// This file exports metric name constants for use by external consumers
// (such as Karve or other monitoring tools) that need to query Lumina metrics
// programmatically. Using these constants provides compile-time safety,
// refactoring support, and IDE autocomplete for metric names.
//
// For metric label names, see the exported label constants in labels.go:
// LabelAccountID, LabelAccountName, LabelRegion, LabelInstanceType, etc.
//
// Example usage in external tools:
//
//	import "github.com/nextdoor/lumina/pkg/metrics"
//
//	// Type-safe metric name reference
//	query := fmt.Sprintf("sum(%s)", metrics.MetricEC2InstanceCount)
//
//	// Type-safe label references
//	query := fmt.Sprintf("%s{%s=\"us-west-2\"}",
//	    metrics.MetricEC2InstanceCount,
//	    metrics.LabelRegion)
//
// Controller Health Metrics
//
// These metrics provide visibility into the operational health of the
// Lumina controller itself, including whether it's running and how fresh
// the collected data is.

const (
	// MetricLuminaControllerRunning indicates whether the Lumina controller is running.
	// Value is always 1 when the controller is active. If this metric disappears
	// from the metrics endpoint, it indicates the controller has crashed or stopped.
	// Type: Gauge
	// Labels: none
	MetricLuminaControllerRunning = "lumina_controller_running"

	// MetricLuminaDataFreshnessSeconds measures the age of cached data in seconds
	// since the last successful update. This metric is automatically updated every
	// second by a background goroutine. A value of 60 means the data is 60 seconds old.
	// Use this for alerting on stale data (e.g., alert if > 600 seconds).
	// Type: Gauge
	// Labels: account_id, account_name, region, data_type
	MetricLuminaDataFreshnessSeconds = "lumina_data_freshness_seconds"

	// MetricLuminaDataLastSuccess indicates whether the last data collection attempt
	// succeeded (1) or failed (0). This metric is populated in Phase 2+ implementations.
	// Type: Gauge
	// Labels: account_id, account_name, region, data_type
	MetricLuminaDataLastSuccess = "lumina_data_last_success"
)

// AWS Account Validation Metrics
//
// These metrics track the status and performance of AWS account validation
// (AssumeRole operations) for configured accounts. They help identify
// authentication failures and slow account access.

const (
	// MetricLuminaAccountValidationStatus tracks the validation status for each
	// configured AWS account. A value of 1 indicates successful validation
	// (AssumeRole succeeded), while 0 indicates validation failure.
	// Type: Gauge
	// Labels: account_id, account_name
	MetricLuminaAccountValidationStatus = "lumina_account_validation_status"

	// MetricLuminaAccountValidationLastSuccess records the Unix timestamp of the last
	// successful validation for each AWS account. This enables alerting on stale
	// validations (e.g., no successful validation in 10+ minutes indicates a problem).
	// Type: Gauge
	// Labels: account_id, account_name
	MetricLuminaAccountValidationLastSuccess = "lumina_account_validation_last_success_timestamp"

	// MetricLuminaAccountValidationDurationSeconds measures the time taken to validate
	// account access via AssumeRole. This helps identify slow or timing-out accounts.
	// Histogram buckets cover 100ms to 10 seconds.
	// Type: Histogram
	// Labels: account_id, account_name
	MetricLuminaAccountValidationDurationSeconds = "lumina_account_validation_duration_seconds"
)

// Savings Plans Metrics
//
// These metrics provide detailed visibility into AWS Savings Plans including
// commitments, utilization, remaining capacity, and time until expiration.
// They enable monitoring of Savings Plans efficiency and planning for renewals.

const (
	// MetricSavingsPlanHourlyCommitment tracks the fixed hourly commitment amount
	// (USD/hour) for each Savings Plan. The metric is deleted entirely when the
	// SP expires or is removed (not set to 0).
	// Type: Gauge
	// Labels: savings_plan_arn, account_id, account_name, type, region, instance_family
	MetricSavingsPlanHourlyCommitment = "savings_plan_hourly_commitment"

	// MetricSavingsPlanRemainingHours tracks the number of hours remaining until
	// the Savings Plan expires. Use this for alerting on upcoming expirations to
	// enable renewal planning (e.g., alert when < 720 hours / 30 days remaining).
	// Type: Gauge
	// Labels: savings_plan_arn, account_id, account_name, type
	MetricSavingsPlanRemainingHours = "savings_plan_remaining_hours"

	// MetricSavingsPlanCurrentUtilizationRate tracks the current hourly rate
	// being consumed by instances covered by this Savings Plan. This is a snapshot
	// of current usage in USD/hour. Compare against MetricSavingsPlanHourlyCommitment
	// to determine utilization efficiency.
	// Type: Gauge
	// Labels: savings_plan_arn, account_id, account_name, type
	MetricSavingsPlanCurrentUtilizationRate = "savings_plan_current_utilization_rate"

	// MetricSavingsPlanRemainingCapacity tracks the unused capacity in USD/hour
	// for a Savings Plan. Calculated as: HourlyCommitment - CurrentUtilizationRate.
	// Can be negative if over-utilized (spillover to on-demand rates), indicating
	// you may benefit from purchasing additional Savings Plans.
	// Type: Gauge
	// Labels: savings_plan_arn, account_id, account_name, type
	MetricSavingsPlanRemainingCapacity = "savings_plan_remaining_capacity"

	// MetricSavingsPlanUtilizationPercent tracks the utilization percentage of a
	// Savings Plan. Calculated as: (CurrentUtilizationRate / HourlyCommitment) * 100.
	// Can exceed 100% if the SP is over-utilized. Target 95-100% for optimal
	// cost efficiency without over-commitment.
	// Type: Gauge
	// Labels: savings_plan_arn, account_id, account_name, type
	MetricSavingsPlanUtilizationPercent = "savings_plan_utilization_percent"
)

// Reserved Instances Metrics
//
// These metrics track AWS EC2 Reserved Instance inventory and provide both
// detailed per-RI metrics and aggregated counts by instance family.

const (
	// MetricEC2ReservedInstance indicates the presence of a Reserved Instance.
	// Value is always 1 when the RI exists. When the RI expires or is removed,
	// the metric is deleted entirely (not set to 0). This allows for precise
	// RI inventory tracking via metric presence/absence.
	// Type: Gauge
	// Labels: account_id, account_name, region, instance_type, availability_zone
	MetricEC2ReservedInstance = "ec2_reserved_instance"

	// MetricEC2ReservedInstanceCount tracks the count of Reserved Instances by
	// instance family (e.g., "m5", "c5"). This provides a higher-level view of
	// RI inventory without per-instance-type granularity, useful for capacity
	// planning and overview dashboards.
	// Type: Gauge
	// Labels: account_id, account_name, region, instance_family
	MetricEC2ReservedInstanceCount = "ec2_reserved_instance_count"
)

// EC2 Instance Metrics
//
// These metrics provide visibility into running EC2 instances including
// presence indicators, aggregated counts, and per-instance cost tracking
// with discount attribution.

const (
	// MetricEC2Instance indicates the presence of a running EC2 instance.
	// Value is always 1 when the instance exists and is running. When the instance
	// is stopped or terminated, the metric is deleted entirely (not set to 0).
	// Type: Gauge
	// Labels: account_id, account_name, region, instance_type, availability_zone, instance_id, tenancy, platform
	MetricEC2Instance = "ec2_instance"

	// MetricEC2InstanceCount tracks the count of running EC2 instances by
	// instance family (e.g., "m5", "c5"). This provides a higher-level view
	// of EC2 inventory without per-instance granularity, useful for capacity
	// monitoring and trend analysis.
	// Type: Gauge
	// Labels: account_id, account_name, region, instance_family
	MetricEC2InstanceCount = "ec2_instance_count"

	// MetricEC2InstanceHourlyCost tracks the effective hourly cost for each EC2
	// instance after applying all discounts (Reserved Instances, Savings Plans,
	// spot pricing). This enables accurate per-instance cost tracking and chargeback.
	// Value is in USD/hour.
	// Type: Gauge
	// Labels: instance_id, account_id, account_name, region, instance_type, cost_type,
	//         availability_zone, lifecycle, pricing_accuracy
	MetricEC2InstanceHourlyCost = "ec2_instance_hourly_cost"
)
