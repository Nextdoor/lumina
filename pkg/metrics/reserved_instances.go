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

	"github.com/nextdoor/lumina/pkg/aws"
	"github.com/prometheus/client_golang/prometheus"
)

// UpdateReservedInstanceMetrics updates RI metrics from the provided list of Reserved Instances.
// This function implements proper metric lifecycle management:
//  1. Resets all existing RI metrics (clean slate approach)
//  2. Sets new values for all currently active RIs
//  3. Deleted/expired RIs are automatically removed by the reset
//
// The function handles two types of metrics:
//   - ec2_reserved_instance: Presence indicator (always 1 when RI exists)
//   - ec2_reserved_instance_count: Aggregated count by instance family
//
// This should be called by the RISP reconciler after successfully updating
// the RI cache (typically hourly).
//
// Example usage:
//
//	ris := rispCache.GetAllReservedInstances()
//	metrics.UpdateReservedInstanceMetrics(ris)
func (m *Metrics) UpdateReservedInstanceMetrics(ris []aws.ReservedInstance) {
	// Reset all existing RI metrics to ensure deleted/expired RIs are removed.
	// This is more reliable than trying to track which specific RIs were deleted.
	m.ReservedInstance.Reset()
	m.ReservedInstanceCount.Reset()

	// Track instance family counts for aggregation
	// Key format: "accountID:accountName:region:family"
	familyCounts := make(map[string]int32)

	// Process each Reserved Instance
	for _, ri := range ris {
		// Skip inactive RIs (expired, retired, etc.)
		if ri.State != "active" {
			continue
		}

		// Set per-instance metric (always 1 when RI exists)
		m.ReservedInstance.With(prometheus.Labels{
			m.config.GetAccountIDLabel():   ri.AccountID,
			m.config.GetAccountNameLabel(): ri.AccountName,
			m.config.GetRegionLabel():      ri.Region,
			LabelInstanceType:              ri.InstanceType,
			LabelAvailabilityZone:          ri.AvailabilityZone,
		}).Set(1)

		// Extract instance family from instance type
		// e.g., "m5.xlarge" -> "m5", "c5.2xlarge" -> "c5"
		family := extractInstanceFamily(ri.InstanceType)

		// Aggregate counts by family
		key := ri.AccountID + ":" + ri.AccountName + ":" + ri.Region + ":" + family
		familyCounts[key] += ri.InstanceCount
	}

	// Set aggregated family counts
	for key, count := range familyCounts {
		parts := strings.Split(key, ":")
		if len(parts) != 4 {
			continue // Skip malformed keys
		}
		m.ReservedInstanceCount.With(prometheus.Labels{
			m.config.GetAccountIDLabel():   parts[0],
			m.config.GetAccountNameLabel(): parts[1],
			m.config.GetRegionLabel():      parts[2],
			LabelInstanceFamily:            parts[3],
		}).Set(float64(count))
	}
}

// extractInstanceFamily extracts the instance family from an instance type.
// Examples:
//   - "m5.xlarge" -> "m5"
//   - "c5.2xlarge" -> "c5"
//   - "r5d.4xlarge" -> "r5d"
//   - "t3" -> "t3" (handles edge cases)
func extractInstanceFamily(instanceType string) string {
	// Split on the first dot
	parts := strings.SplitN(instanceType, ".", 2)
	return parts[0]
}
