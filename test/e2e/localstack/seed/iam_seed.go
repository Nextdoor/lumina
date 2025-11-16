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

package seed

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/iam/types"
)

//go:embed testdata/iam.json
var iamFixturesFS embed.FS

// SeedIAM seeds IAM roles and policies into LocalStack from the embedded JSON fixtures.
// This function is idempotent - it's safe to run multiple times as it checks if resources
// already exist before attempting to create them.
//
// The function:
//  1. Loads IAM fixtures from testdata/iam.json
//  2. Creates IAM policies first (since roles depend on policies)
//  3. Creates IAM roles with the standard AssumeRole trust policy
//  4. Attaches policies to roles
//
// Returns an error if any operation fails.
func SeedIAM(ctx context.Context, cfg aws.Config) error {
	// Load fixtures from embedded JSON
	fixtures, err := loadIAMFixtures()
	if err != nil {
		return fmt.Errorf("failed to load IAM fixtures: %w", err)
	}

	// Create IAM client
	iamClient := iam.NewFromConfig(cfg)

	// Seed policies first (roles depend on policies)
	if err := seedIAMPolicies(ctx, iamClient, fixtures.Policies); err != nil {
		return fmt.Errorf("failed to seed IAM policies: %w", err)
	}

	// Seed roles
	if err := seedIAMRoles(ctx, iamClient, fixtures.Roles); err != nil {
		return fmt.Errorf("failed to seed IAM roles: %w", err)
	}

	return nil
}

// loadIAMFixtures loads IAM fixtures from the embedded testdata/iam.json file.
func loadIAMFixtures() (*IAMFixtures, error) {
	data, err := iamFixturesFS.ReadFile("testdata/iam.json")
	if err != nil {
		return nil, fmt.Errorf("failed to read iam.json: %w", err)
	}

	var fixtures IAMFixtures
	if err := json.Unmarshal(data, &fixtures); err != nil {
		return nil, fmt.Errorf("failed to parse iam.json: %w", err)
	}

	return &fixtures, nil
}

// seedIAMPolicies creates IAM policies in LocalStack.
// This function is idempotent - it skips policies that already exist.
func seedIAMPolicies(ctx context.Context, client *iam.Client, policies []IAMPolicy) error {
	for _, policy := range policies {
		// Check if policy already exists
		policyARN := fmt.Sprintf("arn:aws:iam::000000000000:policy/%s", policy.PolicyName)
		_, err := client.GetPolicy(ctx, &iam.GetPolicyInput{
			PolicyArn: aws.String(policyARN),
		})
		if err == nil {
			// Policy already exists, skip
			continue
		}

		// Convert policy document to JSON string
		policyDocJSON, err := json.Marshal(policy.PolicyDocument)
		if err != nil {
			return fmt.Errorf("failed to marshal policy document for %s: %w", policy.PolicyName, err)
		}

		// Create the policy
		_, err = client.CreatePolicy(ctx, &iam.CreatePolicyInput{
			PolicyName:     aws.String(policy.PolicyName),
			Description:    aws.String(policy.Description),
			PolicyDocument: aws.String(string(policyDocJSON)),
		})
		if err != nil {
			return fmt.Errorf("failed to create policy %s: %w", policy.PolicyName, err)
		}
	}

	return nil
}

// seedIAMRoles creates IAM roles in LocalStack and attaches policies to them.
// This function is idempotent - it skips roles that already exist.
func seedIAMRoles(ctx context.Context, client *iam.Client, roles []IAMRole) error {
	// Standard trust policy that allows any principal to assume the role.
	// LocalStack doesn't enforce strict IAM policies, making testing easier.
	trustPolicy := `{
		"Version": "2012-10-17",
		"Statement": [
			{
				"Effect": "Allow",
				"Principal": {
					"AWS": "*"
				},
				"Action": "sts:AssumeRole"
			}
		]
	}`

	for _, role := range roles {
		// Check if role already exists
		_, err := client.GetRole(ctx, &iam.GetRoleInput{
			RoleName: aws.String(role.RoleName),
		})
		if err == nil {
			// Role already exists, ensure policies are attached and skip creation
			if err := attachPoliciesToRole(ctx, client, role.RoleName, role.AttachedPolicies); err != nil {
				return err
			}
			continue
		}

		// Create the role
		_, err = client.CreateRole(ctx, &iam.CreateRoleInput{
			RoleName:                 aws.String(role.RoleName),
			Path:                     aws.String(role.Path),
			Description:              aws.String(role.Description),
			AssumeRolePolicyDocument: aws.String(trustPolicy),
		})
		if err != nil {
			return fmt.Errorf("failed to create role %s: %w", role.RoleName, err)
		}

		// Attach policies to the role
		if err := attachPoliciesToRole(ctx, client, role.RoleName, role.AttachedPolicies); err != nil {
			return err
		}
	}

	return nil
}

// attachPoliciesToRole attaches a list of policies to an IAM role.
// This function is idempotent - it handles cases where policies are already attached.
func attachPoliciesToRole(ctx context.Context, client *iam.Client, roleName string, policyARNs []string) error {
	for _, policyARN := range policyARNs {
		// Attach the policy (this operation is idempotent in LocalStack)
		_, err := client.AttachRolePolicy(ctx, &iam.AttachRolePolicyInput{
			RoleName:  aws.String(roleName),
			PolicyArn: aws.String(policyARN),
		})
		if err != nil {
			// Check if error is because policy is already attached
			// In LocalStack, this operation typically succeeds even if already attached
			var noSuchEntity *types.NoSuchEntityException
			if ok := err.(*types.NoSuchEntityException); ok == noSuchEntity {
				// Policy or role doesn't exist - this is a real error
				return fmt.Errorf("failed to attach policy %s to role %s: %w", policyARN, roleName, err)
			}
			// For other errors (like already attached), we can safely continue
		}
	}

	return nil
}
