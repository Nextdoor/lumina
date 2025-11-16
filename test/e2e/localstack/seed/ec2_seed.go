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
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

//go:embed testdata/ec2.json
var ec2FixturesFS embed.FS

// SeedEC2 seeds EC2 resources (security groups and instances) into LocalStack
// from the embedded JSON fixtures.
//
// This function is idempotent for security groups but will create new instances
// on each run (LocalStack doesn't prevent duplicate instance creation).
//
// The function:
//  1. Loads EC2 fixtures from testdata/ec2.json
//  2. Creates security groups (if they don't exist)
//  3. Launches EC2 instances with the specified configuration
//
// Returns an error if any operation fails.
func SeedEC2(ctx context.Context, cfg aws.Config) error {
	// Load fixtures from embedded JSON
	fixtures, err := loadEC2Fixtures()
	if err != nil {
		return fmt.Errorf("failed to load EC2 fixtures: %w", err)
	}

	// Create EC2 client
	ec2Client := ec2.NewFromConfig(cfg)

	// Seed security groups first (instances may reference them)
	if err := seedSecurityGroups(ctx, ec2Client, fixtures.SecurityGroups); err != nil {
		return fmt.Errorf("failed to seed security groups: %w", err)
	}

	// Seed EC2 instances
	if err := seedEC2Instances(ctx, ec2Client, fixtures.Instances); err != nil {
		return fmt.Errorf("failed to seed EC2 instances: %w", err)
	}

	return nil
}

// loadEC2Fixtures loads EC2 fixtures from the embedded testdata/ec2.json file.
func loadEC2Fixtures() (*EC2Fixtures, error) {
	data, err := ec2FixturesFS.ReadFile("testdata/ec2.json")
	if err != nil {
		return nil, fmt.Errorf("failed to read ec2.json: %w", err)
	}

	var fixtures EC2Fixtures
	if err := json.Unmarshal(data, &fixtures); err != nil {
		return nil, fmt.Errorf("failed to parse ec2.json: %w", err)
	}

	return &fixtures, nil
}

// seedSecurityGroups creates security groups in LocalStack.
// This function is idempotent - it skips security groups that already exist.
func seedSecurityGroups(ctx context.Context, client *ec2.Client, securityGroups []SecurityGroup) error {
	for _, sg := range securityGroups {
		// Check if security group already exists
		describeOutput, err := client.DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{
			Filters: []types.Filter{
				{
					Name:   aws.String("group-name"),
					Values: []string{sg.GroupName},
				},
			},
		})
		if err != nil {
			return fmt.Errorf("failed to describe security groups: %w", err)
		}

		if len(describeOutput.SecurityGroups) > 0 {
			// Security group already exists, skip
			continue
		}

		// Create the security group
		_, err = client.CreateSecurityGroup(ctx, &ec2.CreateSecurityGroupInput{
			GroupName:   aws.String(sg.GroupName),
			Description: aws.String(sg.Description),
		})
		if err != nil {
			// LocalStack may return an error if the group already exists
			// We can safely continue in this case
			continue
		}
	}

	return nil
}

// seedEC2Instances launches EC2 instances in LocalStack.
// Note: This function is NOT idempotent - it will create new instances on each run.
// For E2E testing, this is typically fine as LocalStack is ephemeral.
func seedEC2Instances(ctx context.Context, client *ec2.Client, instances []EC2Instance) error {
	for _, instance := range instances {
		// Convert tags to EC2 tag specification format
		tagSpecs := []types.TagSpecification{}
		if len(instance.Tags) > 0 {
			ec2Tags := make([]types.Tag, len(instance.Tags))
			for i, tag := range instance.Tags {
				ec2Tags[i] = types.Tag{
					Key:   aws.String(tag.Key),
					Value: aws.String(tag.Value),
				}
			}
			tagSpecs = append(tagSpecs, types.TagSpecification{
				ResourceType: types.ResourceTypeInstance,
				Tags:         ec2Tags,
			})
		}

		// Launch instances
		// Note: LocalStack accepts any AMI ID, so we use the value from fixtures
		_, err := client.RunInstances(ctx, &ec2.RunInstancesInput{
			ImageId:           aws.String(instance.ImageID),
			InstanceType:      types.InstanceType(instance.InstanceType),
			MinCount:          aws.Int32(int32(instance.Count)),
			MaxCount:          aws.Int32(int32(instance.Count)),
			TagSpecifications: tagSpecs,
		})
		if err != nil {
			return fmt.Errorf("failed to launch instances (type: %s, count: %d): %w",
				instance.InstanceType, instance.Count, err)
		}
	}

	return nil
}
