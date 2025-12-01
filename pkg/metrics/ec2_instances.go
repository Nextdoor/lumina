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
	"github.com/nextdoor/lumina/pkg/aws"
	"github.com/prometheus/client_golang/prometheus"
)

// UpdateEC2InstanceMetrics updates all EC2 instance metrics based on cache data.
// This function is called by the EC2Reconciler after each cache refresh (every 5 minutes).
//
// The function implements proper metric lifecycle management:
//  1. Resets all existing EC2 metrics (clean slate approach)
//  2. Sets new values for all currently running instances
//  3. Terminated/stopped instances are automatically removed by the reset
//
// The function handles two types of metrics:
//   - ec2_instance: Per-instance presence indicator (always 1 when instance exists)
//   - ec2_instance_count: Aggregated count by instance family
//
// Only running instances are included in metrics. Stopped instances don't incur
// compute charges (only EBS charges), and terminated instances are being cleaned up.
//
// Example usage:
//
//	allInstances := ec2Cache.GetRunningInstances()
//	metrics.UpdateEC2InstanceMetrics(allInstances)
func (m *Metrics) UpdateEC2InstanceMetrics(instances []aws.Instance) {
	// Reset all existing metrics to ensure terminated/stopped instances are removed.
	// This is more reliable than trying to track which specific instances changed state.
	m.EC2Instance.Reset()
	m.EC2InstanceCount.Reset()

	// Track counts for aggregation
	// familyCounts: account -> region -> family -> count
	familyCounts := make(map[string]map[string]map[string]int)

	// Process each instance
	for _, inst := range instances {
		// Only count running instances in metrics.
		// Stopped instances don't consume compute costs (only EBS volume costs).
		// Terminated instances are in the process of being cleaned up.
		if inst.State != "running" {
			continue
		}

		// Set per-instance metric (always 1 when instance exists)
		// Platform (operating system) is normalized to "linux" or "windows" for consistency
		// Empty Platform field from AWS API is treated as "linux" (the default)
		platform := inst.Platform
		if platform == "" {
			platform = aws.PlatformLinux
		}

		m.EC2Instance.With(prometheus.Labels{
			"account_id":        inst.AccountID,
			"region":            inst.Region,
			"instance_type":     inst.InstanceType,
			"availability_zone": inst.AvailabilityZone,
			"instance_id":       inst.InstanceID,
			"tenancy":           inst.Tenancy,
			"platform":          platform,
		}).Set(1)

		// Extract instance family from instance type
		// e.g., "m5.xlarge" -> "m5", "c5.2xlarge" -> "c5"
		family := extractInstanceFamily(inst.InstanceType)

		// Initialize nested maps if needed
		if familyCounts[inst.AccountID] == nil {
			familyCounts[inst.AccountID] = make(map[string]map[string]int)
		}
		if familyCounts[inst.AccountID][inst.Region] == nil {
			familyCounts[inst.AccountID][inst.Region] = make(map[string]int)
		}
		familyCounts[inst.AccountID][inst.Region][family]++
	}

	// Set aggregated family counts
	for accountID, regions := range familyCounts {
		for region, families := range regions {
			for family, count := range families {
				m.EC2InstanceCount.With(prometheus.Labels{
					"account_id":      accountID,
					"region":          region,
					"instance_family": family,
				}).Set(float64(count))
			}
		}
	}
}
