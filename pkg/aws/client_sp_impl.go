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
//
// coverage:ignore - requires real AWS credentials, tested via E2E with LocalStack
func (c *RealSPClient) DescribeSavingsPlanRates(
	ctx context.Context,
	savingsPlanId string,
	instanceTypes []string,
	regions []string,
	operatingSystems []string,
	tenancies []string,
) ([]SavingsPlanRate, error) {
	var allRates []SavingsPlanRate

	// Build input - query for a specific Savings Plan ID
	input := &savingsplans.DescribeSavingsPlanRatesInput{
		SavingsPlanId: aws.String(savingsPlanId),
	}

	// Add filters to dramatically reduce data transfer and memory usage
	// Without filters: AWS returns 20k-128k+ rates per SP (all instance types × all regions × all OS × tenancy)
	// With filters: Only get rates for what we actually use (~50-200 rates per SP)
	// This is a ~99.5% reduction in data transfer and memory usage
	var filters []types.SavingsPlanRateFilter

	if len(instanceTypes) > 0 {
		filters = append(filters, types.SavingsPlanRateFilter{
			Name:   types.SavingsPlanRateFilterNameInstanceType,
			Values: instanceTypes,
		})
	}

	if len(regions) > 0 {
		filters = append(filters, types.SavingsPlanRateFilter{
			Name:   types.SavingsPlanRateFilterNameRegion,
			Values: regions,
		})
	}

	if len(operatingSystems) > 0 {
		filters = append(filters, types.SavingsPlanRateFilter{
			Name:   types.SavingsPlanRateFilterNameProductDescription,
			Values: operatingSystems,
		})
	}

	if len(tenancies) > 0 {
		// Convert EC2 tenancy values ("default", "dedicated", "host") to SP API format ("Shared", "Dedicated", "Host")
		spTenancies := make([]string, len(tenancies))
		for i, t := range tenancies {
			spTenancies[i] = normalizeTenancyForSPFilter(t)
		}
		filters = append(filters, types.SavingsPlanRateFilter{
			Name:   "tenancy",
			Values: spTenancies,
		})
	}

	if len(filters) > 0 {
		input.Filters = filters
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
			if convertedRate == nil {
				continue
			}

			allRates = append(allRates, *convertedRate)
		}

		// Check for more pages
		if output.NextToken == nil {
			break
		}
		input.NextToken = output.NextToken
	}

	return allRates, nil
}

// normalizeTenancyForSPFilter converts EC2 instance tenancy values to the format expected
// by the Savings Plans DescribeSavingsPlanRates API filter.
//
// EC2 instances use (types.Tenancy enum):
//   - "default"   - Shared hardware
//   - "dedicated" - Dedicated instance
//   - "host"      - Dedicated host
//
// Savings Plans API filters expect lowercase:
//   - "shared"    - For default/shared tenancy
//   - "dedicated" - For dedicated instances
//   - "host"      - For dedicated hosts
func normalizeTenancyForSPFilter(ec2Tenancy string) string {
	switch ec2Tenancy {
	case "default":
		return "shared"
	case "dedicated", "host":
		// Already lowercase, return as-is
		return ec2Tenancy
	default:
		// Unknown tenancy - return lowercase
		return strings.ToLower(ec2Tenancy)
	}
}

// normalizeProductDescription normalizes OS/platform values to a consistent format.
// This function converts between EC2 and Savings Plans API formats to ensure cache keys match.
//
// EC2 DescribeInstances returns (Platform field):
//   - ""        - Linux instances (field is empty/omitted)
//   - "windows" - Windows instances (lowercase)
//
// Savings Plans DescribeSavingsPlanRates returns (productDescription property):
//   - "Linux/UNIX"           - Linux instances
//   - "Windows"              - Windows instances
//   - "Red Hat Enterprise Linux" - RHEL instances
//   - "SUSE Linux"           - SUSE instances
//
// This function normalizes all values to a simplified format for cache keys:
//   - "linux"   - All Linux variants (Linux/UNIX, RHEL, SUSE, or empty Platform)
//   - "windows" - Windows instances
func normalizeProductDescription(input string) string {
	normalized := strings.ToLower(strings.TrimSpace(input))

	// EC2 Platform values (already normalized)
	if normalized == "" || normalized == PlatformLinux {
		return PlatformLinux
	}
	if normalized == PlatformWindows {
		return PlatformWindows
	}

	// Savings Plans ProductDescription values
	if strings.Contains(normalized, "linux") || strings.Contains(normalized, "unix") {
		return PlatformLinux
	}
	if strings.Contains(normalized, "windows") {
		return PlatformWindows
	}

	// Default to linux for unknown values (most instances are Linux)
	return PlatformLinux
}

// normalizeTenancyForSPRate normalizes tenancy values from Savings Plan rate properties
// to match EC2 instance tenancy values.
//
// EC2 DescribeInstances returns (types.Tenancy enum):
//   - "default"   - Shared hardware (most common, ~99% of instances)
//   - "dedicated" - Dedicated instance
//   - "host"      - Dedicated host
//
// Savings Plans DescribeSavingsPlanRates returns in properties (lowercase):
//   - "shared"    - Shared/default tenancy
//   - "dedicated" - Dedicated instance
//   - "host"      - Dedicated host
//
// This function normalizes SP rate tenancy values to match EC2 instance values:
//   - "shared"    → "default"
//   - "dedicated" → "dedicated"
//   - "host"      → "host"
func normalizeTenancyForSPRate(spTenancy string) string {
	switch strings.ToLower(spTenancy) {
	case "", "shared":
		// AWS uses "shared", EC2 uses "default"
		return "default"
	case "dedicated":
		return "dedicated"
	case "host":
		return "host"
	case "default":
		// Already normalized (passthrough for safety)
		return "default"
	default:
		// Unknown tenancy value - default to "default"
		return "default"
	}
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

	// Extract instance type, region, tenancy, and OS from properties
	instanceType := ""
	region := ""
	rawTenancy := ""         // Will be normalized below
	productDescription := "" // OS type (e.g., "Linux/UNIX", "Windows")

	for _, prop := range rate.Properties {
		name := string(prop.Name) // Name is SavingsPlanRatePropertyKey type (string)
		value := aws.ToString(prop.Value)

		switch name {
		case "instanceType":
			instanceType = value
		case "region":
			region = value
		case "tenancy":
			rawTenancy = value
		case "productDescription":
			productDescription = value
		}
	}

	// Skip rates without instance type
	if instanceType == "" {
		return nil
	}

	// Skip rates without region - region is required for per-region instance pricing
	if region == "" {
		return nil
	}

	// Normalize tenancy value to match EC2 instance tenancy format
	// AWS omits the tenancy property for default/shared instances, only includes it for "Dedicated" or "Host"
	// This function converts AWS Savings Plans API tenancy values to match EC2 types.Tenancy enum values
	tenancy := normalizeTenancyForSPRate(rawTenancy)

	// Normalize product description (OS) to match EC2 platform format
	// Converts "Linux/UNIX" → "linux", "Windows" → "windows", etc.
	normalizedOS := normalizeProductDescription(productDescription)

	return &SavingsPlanRate{
		SavingsPlanId:      savingsPlanId,
		SavingsPlanARN:     "", // Will be populated by caller if needed
		InstanceType:       instanceType,
		Region:             region,
		Rate:               rateValue,
		Currency:           string(rate.Currency),
		Unit:               string(rate.Unit),
		ProductType:        string(rate.ProductType),
		ServiceCode:        string(rate.ServiceCode),
		UsageType:          aws.ToString(rate.UsageType),
		Operation:          aws.ToString(rate.Operation),
		ProductDescription: normalizedOS, // Normalized to "linux" or "windows"
		Tenancy:            tenancy,
	}
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
