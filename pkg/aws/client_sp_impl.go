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
	"strconv"
	"time"

	aws "github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/savingsplans"
	"github.com/aws/aws-sdk-go-v2/service/savingsplans/types"
)

const (
	// RegionAll represents region scope for Compute Savings Plans that apply organization-wide
	RegionAll = "all"
)

// RealSPClient is a production implementation of SavingsPlansClient that makes
// real API calls to AWS Savings Plans using the AWS SDK v2.
type RealSPClient struct {
	client    *savingsplans.Client
	region    string
	accountID string
}

// NewRealSPClient creates a new Savings Plans client with the specified credential provider.
// The credential provider should come from either the default credential chain or
// from an AssumeRoleProvider that automatically refreshes credentials.
func NewRealSPClient(
	ctx context.Context,
	accountID string,
	region string,
	credsProvider aws.CredentialsProvider,
	endpointURL string,
) (*RealSPClient, error) {
	// Load AWS configuration with the provided credential provider.
	// The provider handles credential refresh automatically, preventing
	// expiration issues that occurred with static credentials.
	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(region),
		awsconfig.WithCredentialsProvider(credsProvider),
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
		client:    client,
		region:    region,
		accountID: accountID,
	}, nil
}

// DescribeSavingsPlans returns all active Savings Plans for the account.
// This API is not region-specific - it returns all Savings Plans for the account.
// The method handles pagination automatically to retrieve all Savings Plans.
// coverage:ignore - requires real AWS credentials, tested via E2E with LocalStack
func (c *RealSPClient) DescribeSavingsPlans(ctx context.Context) ([]SavingsPlan, error) {
	var allSPs []SavingsPlan

	// Query for active Savings Plans only
	// States: payment-pending, payment-failed, active, retired, queued, queued-deleted
	states := []types.SavingsPlanState{
		types.SavingsPlanStateActive,
	}

	input := &savingsplans.DescribeSavingsPlansInput{
		States: states,
	}

	// Handle pagination
	for {
		output, err := c.client.DescribeSavingsPlans(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("failed to describe savings plans: %w", err)
		}

		// Convert AWS SDK types to our types
		for _, sp := range output.SavingsPlans {
			allSPs = append(allSPs, convertSavingsPlan(sp, c.accountID))
		}

		// Check for more pages
		if output.NextToken == nil {
			break
		}
		input.NextToken = output.NextToken
	}

	return allSPs, nil
}

// GetSavingsPlanByARN returns a specific Savings Plan by ARN.
// coverage:ignore - requires real AWS credentials, tested via E2E with LocalStack
func (c *RealSPClient) GetSavingsPlanByARN(ctx context.Context, arn string) (*SavingsPlan, error) {
	input := &savingsplans.DescribeSavingsPlansInput{
		SavingsPlanArns: []string{arn},
	}

	output, err := c.client.DescribeSavingsPlans(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to get savings plan %s: %w", arn, err)
	}

	if len(output.SavingsPlans) == 0 {
		return nil, nil // Not found
	}

	sp := convertSavingsPlan(output.SavingsPlans[0], c.accountID)
	return &sp, nil
}

// convertSavingsPlan converts an AWS SDK SavingsPlan to our type.
func convertSavingsPlan(sp types.SavingsPlan, accountID string) SavingsPlan {
	// Parse start and end times (strings in RFC3339 format)
	var start, end time.Time
	if sp.Start != nil {
		if t, err := time.Parse(time.RFC3339, aws.ToString(sp.Start)); err == nil {
			start = t
		}
	}
	if sp.End != nil {
		if t, err := time.Parse(time.RFC3339, aws.ToString(sp.End)); err == nil {
			end = t
		}
	}

	// Parse commitment (hourly rate in USD)
	commitment := 0.0
	if sp.Commitment != nil {
		// The Commitment field contains the hourly commitment amount as a string
		if parsed, err := strconv.ParseFloat(aws.ToString(sp.Commitment), 64); err == nil {
			commitment = parsed
		}
	}

	// Determine region - Compute Savings Plans apply organization-wide
	region := aws.ToString(sp.Region)
	savingsPlanType := string(sp.SavingsPlanType)
	if savingsPlanType == "Compute" || savingsPlanType == "ComputeSP" {
		region = RegionAll // Compute SPs apply across all regions
	}

	// Extract instance family for EC2 Instance Savings Plans
	instanceFamily := ""
	if savingsPlanType == "EC2Instance" || savingsPlanType == "EC2InstanceSP" {
		// For EC2 Instance SPs, the instance family is in the product types
		// e.g., ["EC2"], and the EC2InstanceFamily property has the family
		if sp.Ec2InstanceFamily != nil {
			instanceFamily = aws.ToString(sp.Ec2InstanceFamily)
		}
	}

	return SavingsPlan{
		SavingsPlanARN:    aws.ToString(sp.SavingsPlanArn),
		SavingsPlanID:     aws.ToString(sp.SavingsPlanId),
		SavingsPlanType:   savingsPlanType,
		State:             string(sp.State),
		Commitment:        commitment,
		Region:            region,
		InstanceFamily:    instanceFamily,
		Start:             start,
		End:               end,
		AccountID:         accountID,
		EC2InstanceFamily: instanceFamily, // Legacy field for compatibility
	}
}
