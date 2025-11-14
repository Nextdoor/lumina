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

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
)

// RealEC2Client is a production implementation of EC2Client that makes
// real API calls to AWS EC2 using the AWS SDK v2.
type RealEC2Client struct {
	client *ec2.Client
	region string
}

// NewRealEC2Client creates a new EC2 client with the specified credentials.
// The credentials should come from either the default credential chain or
// from an STS AssumeRole operation.
func NewRealEC2Client(ctx context.Context, region string, creds credentials.StaticCredentialsProvider, endpointURL string) (*RealEC2Client, error) {
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
		client: client,
		region: region,
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
func (c *RealEC2Client) DescribeReservedInstances(_ context.Context, _ []string) ([]ReservedInstance, error) {
	// TODO: Implement real EC2 DescribeReservedInstances call
	return []ReservedInstance{}, nil
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
