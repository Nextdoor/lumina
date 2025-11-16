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

//go:build integration
// +build integration

package seed

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// getLocalStackConfig creates an AWS config pointing to LocalStack.
// This assumes LocalStack is running at http://localhost:4566.
func getLocalStackConfig(t *testing.T) aws.Config {
	t.Helper()

	ctx := context.Background()
	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion("us-west-2"),
		awsconfig.WithEndpointResolverWithOptions(
			aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
				return aws.Endpoint{
					URL:               "http://localhost:4566",
					HostnameImmutable: true,
					SigningRegion:     region,
				}, nil
			}),
		),
		awsconfig.WithCredentialsProvider(aws.CredentialsProviderFunc(func(ctx context.Context) (aws.Credentials, error) {
			return aws.Credentials{
				AccessKeyID:     "test",
				SecretAccessKey: "test",
			}, nil
		})),
	)
	require.NoError(t, err, "Failed to create LocalStack config")
	return cfg
}

// TestSeedIAMIntegration tests IAM seeding against LocalStack.
func TestSeedIAMIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	cfg := getLocalStackConfig(t)
	ctx := context.Background()

	// Seed IAM resources
	err := SeedIAM(ctx, cfg)
	require.NoError(t, err, "Failed to seed IAM")

	// Verify roles were created
	iamClient := iam.NewFromConfig(cfg)

	// Check LuminaTestRole exists
	testRoleOutput, err := iamClient.GetRole(ctx, &iam.GetRoleInput{
		RoleName: aws.String("LuminaTestRole"),
	})
	require.NoError(t, err, "Failed to get LuminaTestRole")
	assert.Equal(t, "LuminaTestRole", *testRoleOutput.Role.RoleName, "Role name should match")
	assert.Equal(t, "/lumina/", *testRoleOutput.Role.Path, "Role path should match")

	// Check LuminaStagingRole exists
	stagingRoleOutput, err := iamClient.GetRole(ctx, &iam.GetRoleInput{
		RoleName: aws.String("LuminaStagingRole"),
	})
	require.NoError(t, err, "Failed to get LuminaStagingRole")
	assert.Equal(t, "LuminaStagingRole", *stagingRoleOutput.Role.RoleName, "Role name should match")

	// Check policy exists
	policyARN := "arn:aws:iam::000000000000:policy/LuminaReadOnlyPolicy"
	policyOutput, err := iamClient.GetPolicy(ctx, &iam.GetPolicyInput{
		PolicyArn: aws.String(policyARN),
	})
	require.NoError(t, err, "Failed to get LuminaReadOnlyPolicy")
	assert.Equal(t, "LuminaReadOnlyPolicy", *policyOutput.Policy.PolicyName, "Policy name should match")

	// Test idempotency - seeding again should not fail
	err = SeedIAM(ctx, cfg)
	assert.NoError(t, err, "Seeding IAM again should be idempotent")
}

// TestSeedEC2Integration tests EC2 seeding against LocalStack.
func TestSeedEC2Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	cfg := getLocalStackConfig(t)
	ctx := context.Background()

	// Seed EC2 resources
	err := SeedEC2(ctx, cfg)
	require.NoError(t, err, "Failed to seed EC2")

	// Verify security groups were created
	ec2Client := ec2.NewFromConfig(cfg)

	sgOutput, err := ec2Client.DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{
		GroupNames: []string{"lumina-test-sg"},
	})
	require.NoError(t, err, "Failed to describe security groups")
	require.Len(t, sgOutput.SecurityGroups, 1, "Should have exactly one security group")
	assert.Equal(t, "lumina-test-sg", *sgOutput.SecurityGroups[0].GroupName, "Security group name should match")

	// Verify instances were created
	instancesOutput, err := ec2Client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{})
	require.NoError(t, err, "Failed to describe instances")

	// Count instances by type
	instanceTypeCounts := make(map[string]int)
	for _, reservation := range instancesOutput.Reservations {
		for _, instance := range reservation.Instances {
			instanceType := string(instance.InstanceType)
			instanceTypeCounts[instanceType]++
		}
	}

	// Verify we have the expected number of each instance type
	assert.GreaterOrEqual(t, instanceTypeCounts["m5.xlarge"], 2, "Should have at least 2 m5.xlarge instances")
	assert.GreaterOrEqual(t, instanceTypeCounts["c5.large"], 1, "Should have at least 1 c5.large instance")
}

// TestSeedAllIntegration tests the complete seeding process.
func TestSeedAllIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	cfg := getLocalStackConfig(t)
	ctx := context.Background()

	// Seed all resources
	err := SeedAll(ctx, cfg)
	require.NoError(t, err, "Failed to seed all resources")

	// Verify IAM resources exist
	iamClient := iam.NewFromConfig(cfg)
	_, err = iamClient.GetRole(ctx, &iam.GetRoleInput{
		RoleName: aws.String("LuminaTestRole"),
	})
	assert.NoError(t, err, "LuminaTestRole should exist")

	// Verify EC2 resources exist
	ec2Client := ec2.NewFromConfig(cfg)
	sgOutput, err := ec2Client.DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{
		GroupNames: []string{"lumina-test-sg"},
	})
	assert.NoError(t, err, "Security group query should succeed")
	assert.NotEmpty(t, sgOutput.SecurityGroups, "Security group should exist")

	// Test idempotency of SeedAll
	err = SeedAll(ctx, cfg)
	assert.NoError(t, err, "SeedAll should be idempotent for IAM resources")
}
