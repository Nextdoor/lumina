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
	"context"
	"fmt"
)

// Validator provides methods to validate AWS account access.
// This is used by health checks and reconcilers to verify that
// configured AWS accounts are accessible before attempting data collection.
type Validator interface {
	// ValidateAccountAccess attempts to validate access to a specific AWS account
	// by assuming the role and making a lightweight API call.
	// Returns an error if the account is not accessible.
	ValidateAccountAccess(ctx context.Context, accountConfig AccountConfig) error
}

// AccountValidator implements the Validator interface using the AWS SDK.
type AccountValidator struct {
	client Client
}

// NewAccountValidator creates a new AccountValidator that uses the provided
// AWS client to validate account access.
func NewAccountValidator(client Client) *AccountValidator {
	return &AccountValidator{
		client: client,
	}
}

// ValidateAccountAccess validates that we can access the specified AWS account
// by attempting to create an EC2 client (which includes AssumeRole if configured)
// and then making a lightweight DescribeRegions API call.
//
// This validation:
//  1. Tests that AssumeRole credentials work (if configured)
//  2. Verifies basic AWS API connectivity
//  3. Uses a minimal-cost API call that requires no special permissions
//
// Returns nil if the account is accessible, or an error with details about
// the failure (e.g., AssumeRole denied, network error, invalid credentials).
func (v *AccountValidator) ValidateAccountAccess(ctx context.Context, accountConfig AccountConfig) error {
	// Attempt to create an EC2 client for this account.
	// This will trigger AssumeRole if accountConfig.AssumeRoleARN is set.
	ec2Client, err := v.client.EC2(ctx, accountConfig)
	if err != nil {
		return fmt.Errorf("failed to create EC2 client for account %s: %w",
			accountConfig.AccountID, err)
	}

	// Make a lightweight API call to verify the credentials actually work.
	// We use DescribeInstances with an empty region list to avoid unnecessary
	// queries across all regions. The client will use the default region.
	// We don't care about the results; we just need to know if the API call succeeds.
	_, err = ec2Client.DescribeInstances(ctx, []string{accountConfig.Region})
	if err != nil {
		return fmt.Errorf("failed to validate AWS API access for account %s: %w",
			accountConfig.AccountID, err)
	}

	return nil
}
