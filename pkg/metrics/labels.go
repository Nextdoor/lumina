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

// Metric label name constants.
// These are the default label names used in metrics. They can be customized
// via configuration (config.Metrics.Labels).
const (
	// Account labels
	LabelAccountID   = "account_id"
	LabelAccountName = "account_name"

	// Location labels
	LabelRegion           = "region"
	LabelAvailabilityZone = "availability_zone"

	// Instance labels
	LabelInstanceID     = "instance_id"
	LabelInstanceType   = "instance_type"
	LabelInstanceFamily = "instance_family"
	LabelTenancy        = "tenancy"
	LabelPlatform       = "platform"
	LabelLifecycle      = "lifecycle"

	// Kubernetes labels
	LabelNodeName    = "node_name"
	LabelClusterName = "cluster_name"
	LabelHostName    = "host_name"

	// Cost labels
	LabelCostType        = "cost_type"
	LabelPricingAccuracy = "pricing_accuracy"

	// Savings Plan / Reserved Instance labels
	LabelSavingsPlanARN = "savings_plan_arn"
	LabelType           = "type"

	// Data freshness labels
	LabelDataType = "data_type"
)
