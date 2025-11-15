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
	"fmt"
	"net/http"

	"github.com/nextdoor/lumina/pkg/config"
)

// HealthChecker provides health check functionality for AWS account access.
// It implements the controller-runtime healthz.Checker interface and can be
// used as a readiness probe to ensure the controller doesn't mark itself as
// ready until all configured AWS accounts are accessible.
type HealthChecker struct {
	validator Validator
	accounts  []config.AWSAccount
}

// NewHealthChecker creates a new health checker that validates access to
// the provided AWS accounts using the given validator.
func NewHealthChecker(validator Validator, accounts []config.AWSAccount) *HealthChecker {
	return &HealthChecker{
		validator: validator,
		accounts:  accounts,
	}
}

// Name returns the name of this health checker for logging purposes.
func (h *HealthChecker) Name() string {
	return "aws-account-access"
}

// Check validates that all configured AWS accounts are accessible.
// This method is called by controller-runtime's health probe server.
//
// It returns nil if all accounts are accessible, or an error if any account
// fails validation. The error message includes details about which accounts
// failed and why.
//
// This check is designed to be used as a readiness probe, not a liveness probe.
// Temporary AWS API failures should not cause the pod to be killed, but they
// should prevent the pod from receiving traffic until AWS access is restored.
func (h *HealthChecker) Check(req *http.Request) error {
	ctx := req.Context()

	// If no accounts are configured, consider this healthy (controller can still run,
	// it just won't collect any data). This allows the controller to start even if
	// configuration is incomplete, which is useful for debugging and gradual rollout.
	if len(h.accounts) == 0 {
		return nil
	}

	// Validate each account
	var failedAccounts []string
	for _, account := range h.accounts {
		accountConfig := AccountConfig{
			AccountID:     account.AccountID,
			AssumeRoleARN: account.AssumeRoleARN,
			Region:        account.Region,
		}

		if err := h.validator.ValidateAccountAccess(ctx, accountConfig); err != nil {
			// Don't fail immediately; collect all failures to provide comprehensive feedback
			failedAccounts = append(failedAccounts, fmt.Sprintf("%s (%s): %v",
				account.Name, account.AccountID, err))
		}
	}

	// If any accounts failed, return an error with all failure details
	if len(failedAccounts) > 0 {
		return fmt.Errorf("failed to validate access to %d/%d AWS accounts: %v",
			len(failedAccounts), len(h.accounts), failedAccounts)
	}

	return nil
}
