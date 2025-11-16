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

// Package seed provides functionality to seed test data into LocalStack for E2E testing.
//
// This package replaces the brittle shell-based seeding approach with type-safe Go code
// that uses the AWS SDK v2. Test data is defined in JSON fixture files under testdata/
// and programmatically created in LocalStack.
//
// Example usage:
//
//	ctx := context.Background()
//	cfg, err := awsconfig.LoadDefaultConfig(ctx,
//	    awsconfig.WithRegion("us-west-2"),
//	)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	if err := seed.SeedAll(ctx, cfg); err != nil {
//	    log.Fatal(err)
//	}
package seed

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
)

// SeedAll orchestrates seeding of all test data into LocalStack.
// This function should be called during E2E test setup, before starting Lumina.
//
// The seeding order is important:
//  1. IAM resources (roles and policies) - required for authentication
//  2. EC2 resources (security groups and instances) - required for cost calculations
//
// This function is designed to be idempotent where possible. IAM resources and
// security groups won't be duplicated if they already exist. However, EC2 instances
// will be created on each run (which is acceptable for ephemeral LocalStack instances).
//
// Returns an error if any seeding operation fails.
func SeedAll(ctx context.Context, cfg aws.Config) error {
	// Add a timeout to prevent hanging if LocalStack is unresponsive
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	// Seed IAM resources first (roles and policies)
	// These are required for proper authentication in E2E tests
	if err := SeedIAM(ctx, cfg); err != nil {
		return fmt.Errorf("failed to seed IAM resources: %w", err)
	}

	// Seed EC2 resources (security groups and instances)
	// These provide the test data for cost calculations
	if err := SeedEC2(ctx, cfg); err != nil {
		return fmt.Errorf("failed to seed EC2 resources: %w", err)
	}

	// Future: Add seeding for Reserved Instances and Savings Plans
	// when LocalStack support is available or when using a different testing approach

	return nil
}
