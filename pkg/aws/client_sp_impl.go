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
	"strings"
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

// DescribeSavingsPlanRates returns the actual rates for a specific purchased Savings Plan.
// This API returns the PURCHASE-TIME rates that were locked in when the SP was bought,
// not current market offering rates.
// coverage:ignore - requires real AWS credentials, tested via E2E with LocalStack
func (c *RealSPClient) DescribeSavingsPlanRates(
	ctx context.Context,
	savingsPlanId string,
) ([]SavingsPlanRate, error) {
	var allRates []SavingsPlanRate

	// Build input - query for a specific Savings Plan ID
	input := &savingsplans.DescribeSavingsPlanRatesInput{
		SavingsPlanId: aws.String(savingsPlanId),
	}

	// Handle pagination
	for {
		output, err := c.client.DescribeSavingsPlanRates(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("failed to describe savings plan rates for %s: %w", savingsPlanId, err)
		}

		// Convert AWS SDK types to our types
		for _, rate := range output.SearchResults {
			convertedRate := convertSavingsPlanRate(rate, savingsPlanId)
			if convertedRate != nil {
				allRates = append(allRates, *convertedRate)
			}
		}

		// Check for more pages
		if output.NextToken == nil {
			break
		}
		input.NextToken = output.NextToken
	}

	return allRates, nil
}

// convertSavingsPlanRate converts an AWS SDK SavingsPlanRate to our type.
// Returns nil if the rate cannot be parsed.
func convertSavingsPlanRate(rate types.SavingsPlanRate, savingsPlanId string) *SavingsPlanRate {
	// Parse the rate value
	rateValue := 0.0
	if rate.Rate != nil {
		if parsed, err := strconv.ParseFloat(aws.ToString(rate.Rate), 64); err == nil {
			rateValue = parsed
		}
	}

	// Extract instance type and tenancy from properties
	instanceType := ""
	tenancy := "shared" // Default to shared

	for _, prop := range rate.Properties {
		name := string(prop.Name) // Name is SavingsPlanRatePropertyKey type (string)
		value := aws.ToString(prop.Value)

		switch name {
		case "instanceType":
			instanceType = value
		case "tenancy":
			tenancy = value
		}
	}

	// Skip rates without instance type
	if instanceType == "" {
		return nil
	}

	// Extract region from usageType
	// Format: "APN1-DedicatedUsage:c6i.large" or "USW2-BoxUsage:m5.xlarge"
	region := extractRegionFromUsageType(aws.ToString(rate.UsageType))

	return &SavingsPlanRate{
		SavingsPlanId:  savingsPlanId,
		SavingsPlanARN: "", // Will be populated by caller if needed
		InstanceType:   instanceType,
		Region:         region,
		Rate:           rateValue,
		Currency:       string(rate.Currency),
		Unit:           string(rate.Unit),
		ProductType:    string(rate.ProductType),
		ServiceCode:    string(rate.ServiceCode),
		UsageType:      aws.ToString(rate.UsageType),
		Operation:      aws.ToString(rate.Operation),
		Tenancy:        tenancy,
	}
}

// extractRegionFromUsageType extracts the AWS region from a usage type string.
// Usage type format: "{REGION_CODE}-{USAGE_CATEGORY}:{INSTANCE_TYPE}"
// Examples:
//   - "APN1-DedicatedUsage:c6i.large" -> "ap-northeast-1"
//   - "USW2-BoxUsage:m5.xlarge" -> "us-west-2"
//   - "USE1-BoxUsage:t3.micro" -> "us-east-1"
func extractRegionFromUsageType(usageType string) string {
	// Split on hyphen to get region code
	parts := strings.Split(usageType, "-")
	if len(parts) == 0 {
		return ""
	}

	regionCode := parts[0]

	// Map of AWS usage type region codes to actual region names
	// Based on: https://docs.aws.amazon.com/cur/latest/userguide/usagetype.html
	regionMap := map[string]string{
		"USE1": "us-east-1",
		"USE2": "us-east-2",
		"USW1": "us-west-1",
		"USW2": "us-west-2",
		"AFS1": "af-south-1",
		"APE1": "ap-east-1",
		"APS1": "ap-south-1",
		"APS2": "ap-south-2",
		"APN1": "ap-northeast-1",
		"APN2": "ap-northeast-2",
		"APN3": "ap-northeast-3",
		"APS3": "ap-southeast-1",
		"APS4": "ap-southeast-2",
		"APS5": "ap-southeast-3",
		"APS6": "ap-southeast-4",
		"CAN1": "ca-central-1",
		"EUC1": "eu-central-1",
		"EUC2": "eu-central-2",
		"EUW1": "eu-west-1",
		"EUW2": "eu-west-2",
		"EUW3": "eu-west-3",
		"EUS1": "eu-south-1",
		"EUS2": "eu-south-2",
		"EUN1": "eu-north-1",
		"MES1": "me-south-1",
		"MEC1": "me-central-1",
		"SAE1": "sa-east-1",
	}

	if region, ok := regionMap[regionCode]; ok {
		return region
	}

	// If not found, return the code as-is
	return strings.ToLower(regionCode)
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
