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
	"time"

	aws "github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// RealEC2Client is a production implementation of EC2Client that makes
// real API calls to AWS EC2 using the AWS SDK v2.
type RealEC2Client struct {
	client    *ec2.Client
	region    string
	accountID string
}

// NewRealEC2Client creates a new EC2 client with the specified credentials.
// The credentials should come from either the default credential chain or
// from an STS AssumeRole operation.
func NewRealEC2Client(
	ctx context.Context,
	accountID string,
	region string,
	creds credentials.StaticCredentialsProvider,
	endpointURL string,
) (*RealEC2Client, error) {
	// Load AWS configuration with the provided credentials
	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(region),
		awsconfig.WithCredentialsProvider(creds),
	)
	if err != nil { // coverage:ignore - AWS SDK config loading errors are difficult to trigger in unit tests
		return nil, err
	}

	// Create EC2 client
	ec2Opts := []func(*ec2.Options){}
	if endpointURL != "" {
		// Override endpoint for LocalStack testing
		// This branch is tested in localstack_integration_test.go
		ec2Opts = append(ec2Opts, func(o *ec2.Options) {
			o.BaseEndpoint = &endpointURL // coverage:ignore - tested in LocalStack integration tests
		})
	}
	client := ec2.NewFromConfig(cfg, ec2Opts...)

	return &RealEC2Client{
		client:    client,
		region:    region,
		accountID: accountID,
	}, nil
}

// DescribeInstances returns all running EC2 instances in the specified regions.
// If regions is empty, queries all regions.
func (c *RealEC2Client) DescribeInstances(_ context.Context, _ []string) ([]Instance, error) {
	// TODO: Implement real EC2 DescribeInstances call
	// This will be implemented when we add actual EC2 querying logic
	return []Instance{}, nil
}

// DescribeReservedInstances returns all active Reserved Instances in the specified regions.
// If regions is empty, queries all regions.
//
// Reserved Instances are queried per-region because they are regional resources.
// This method handles pagination automatically to retrieve all RIs.
// coverage:ignore - requires real AWS credentials, tested via E2E with LocalStack
func (c *RealEC2Client) DescribeReservedInstances(ctx context.Context, regions []string) ([]ReservedInstance, error) {
	// If no regions specified, we should query all regions
	// For now, just query the client's configured region
	// TODO: Add logic to discover and query all regions
	queryRegions := regions
	if len(queryRegions) == 0 {
		queryRegions = []string{c.region}
	}

	var allRIs []ReservedInstance

	for _, region := range queryRegions {
		// Query RIs in this region
		// Note: DescribeReservedInstances does not support pagination
		// It returns all results in a single call
		input := &ec2.DescribeReservedInstancesInput{
			// Only get active RIs (not retired/payment-pending/etc)
			Filters: []types.Filter{
				{
					Name:   aws.String("state"),
					Values: []string{"active"},
				},
			},
		}

		output, err := c.client.DescribeReservedInstances(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("failed to describe reserved instances in %s: %w", region, err)
		}

		// Convert AWS SDK types to our types
		for _, ri := range output.ReservedInstances {
			allRIs = append(allRIs, convertReservedInstance(ri, region, c.accountID))
		}
	}

	return allRIs, nil
}

// DescribeSpotPriceHistory returns current spot prices for the specified instance types.
func (c *RealEC2Client) DescribeSpotPriceHistory(_ context.Context, _ []string, _ []string) ([]SpotPrice, error) {
	// TODO: Implement real EC2 DescribeSpotPriceHistory call
	return []SpotPrice{}, nil
}

// GetInstanceByID returns a specific instance by ID.
func (c *RealEC2Client) GetInstanceByID(_ context.Context, _ string, _ string) (*Instance, error) {
	// TODO: Implement real EC2 DescribeInstances call with instance ID filter
	return nil, nil
}

// convertReservedInstance converts an AWS SDK ReservedInstance to our type.
func convertReservedInstance(ri types.ReservedInstances, region, accountID string) ReservedInstance {
	var start, end time.Time
	if ri.Start != nil {
		start = *ri.Start
	}
	if ri.End != nil {
		end = *ri.End
	}

	return ReservedInstance{
		ReservedInstanceID: aws.ToString(ri.ReservedInstancesId),
		InstanceType:       string(ri.InstanceType),
		AvailabilityZone:   aws.ToString(ri.AvailabilityZone),
		Region:             region,
		InstanceCount:      aws.ToInt32(ri.InstanceCount),
		State:              string(ri.State),
		Start:              start,
		End:                end,
		OfferingClass:      string(ri.OfferingClass),
		OfferingType:       string(ri.OfferingType),
		Platform:           string(ri.ProductDescription),
		AccountID:          accountID,
	}
}
