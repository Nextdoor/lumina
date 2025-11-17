// Copyright 2025 Lumina Contributors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package aws

import (
	"net/http"
)

// HealthChecker provides health check functionality for AWS account access.
// It implements the controller-runtime healthz.Checker interface and can be
// used as a readiness probe to ensure the controller doesn't mark itself as
// ready until all configured AWS accounts are accessible.
//
// The health checker now uses a CredentialMonitor for efficient background
// credential validation, reducing AWS API traffic from ~42 calls/min to
// ~0.7 calls/min (for 7 accounts with default settings) while still detecting
// credential issues within the configured monitor interval.
type HealthChecker struct {
	monitor *CredentialMonitor
}

// NewHealthChecker creates a new health checker that uses the provided
// credential monitor for health status checks.
//
// The monitor should be started separately via monitor.Start() before
// the health checker is registered with the controller-runtime manager.
func NewHealthChecker(monitor *CredentialMonitor) *HealthChecker {
	return &HealthChecker{
		monitor: monitor,
	}
}

// Name returns the name of this health checker for logging purposes.
func (h *HealthChecker) Name() string {
	return "aws-account-access"
}

// Check validates that all configured AWS accounts are accessible by
// reading cached status from the credential monitor.
//
// This method is called by controller-runtime's health probe server
// (typically every 10 seconds). Instead of making AWS API calls on every
// check, it reads cached status from the credential monitor's in-memory state.
// This provides:
//   - Sub-millisecond response times (memory read vs network call)
//   - 98% reduction in AWS API traffic (from ~42 calls/min to ~0.7 calls/min)
//   - Graceful degradation (fails only if ALL accounts are unhealthy)
//   - Still detects credential issues within the monitor's check interval
//
// The method returns nil if all accounts are healthy (or if some are degraded
// but not all failed). It returns an error only if ALL accounts are unhealthy,
// implementing graceful degradation to allow the controller to continue operating
// when individual accounts have issues.
//
// This check is designed to be used as a readiness probe, not a liveness probe.
// Temporary AWS API failures should not cause the pod to be killed, but they
// should prevent the pod from receiving traffic until AWS access is restored.
func (h *HealthChecker) Check(req *http.Request) error {
	// Read cached status from monitor (no AWS API calls)
	return h.monitor.GetStatus()
}
