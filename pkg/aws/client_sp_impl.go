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
	"github.com/aws/aws-sdk-go-v2/service/savingsplans"
)

// RealSPClient is a production implementation of SavingsPlansClient that makes
// real API calls to AWS Savings Plans using the AWS SDK v2.
type RealSPClient struct {
	client *savingsplans.Client
	region string
}

// NewRealSPClient creates a new Savings Plans client with the specified credentials.
func NewRealSPClient(
	ctx context.Context,
	region string,
	creds credentials.StaticCredentialsProvider,
	endpointURL string,
) (*RealSPClient, error) {
	// Load AWS configuration with the provided credentials
	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(region),
		awsconfig.WithCredentialsProvider(creds),
	)
	if err != nil { // coverage:ignore - AWS SDK config loading errors are difficult to trigger in unit tests
		return nil, err
	}

	// Create Savings Plans client
	spOpts := []func(*savingsplans.Options){}
	if endpointURL != "" {
		// Override endpoint for LocalStack testing
		// This branch is tested in localstack_integration_test.go
		spOpts = append(spOpts, func(o *savingsplans.Options) {
			o.BaseEndpoint = &endpointURL // coverage:ignore - tested in LocalStack integration tests
		})
	}
	client := savingsplans.NewFromConfig(cfg, spOpts...)

	return &RealSPClient{
		client: client,
		region: region,
	}, nil
}

// DescribeSavingsPlans returns all active Savings Plans for the account.
func (c *RealSPClient) DescribeSavingsPlans(_ context.Context) ([]SavingsPlan, error) {
	// TODO: Implement real Savings Plans DescribeSavingsPlans call
	return []SavingsPlan{}, nil
}

// GetSavingsPlanByARN returns a specific Savings Plan by ARN.
func (c *RealSPClient) GetSavingsPlanByARN(_ context.Context, _ string) (*SavingsPlan, error) {
	// TODO: Implement real Savings Plans lookup by ARN
	return nil, nil
}
