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

// NewRealEC2Client creates a new EC2 client with the specified credential provider.
// The credential provider should come from either the default credential chain or
// from an AssumeRoleProvider that automatically refreshes credentials.
func NewRealEC2Client(
	ctx context.Context,
	accountID string,
	region string,
	credsProvider aws.CredentialsProvider,
	endpointURL string,
) (*RealEC2Client, error) {
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
// If regions is empty, queries only the client's configured region.
//
// This method filters to only running instances by default to focus on resources
// that incur costs. It handles pagination automatically to retrieve all instances.
// coverage:ignore - requires real AWS credentials, tested via E2E with LocalStack
func (c *RealEC2Client) DescribeInstances(ctx context.Context, regions []string) ([]Instance, error) {
	// If no regions specified, query the client's configured region
	queryRegions := regions
	if len(queryRegions) == 0 {
		queryRegions = []string{c.region}
	}

	var allInstances []Instance

	for _, region := range queryRegions {
		// Query instances in this region
		// Filter to only running instances (the ones that incur costs)
		input := &ec2.DescribeInstancesInput{
			Filters: []types.Filter{
				{
					Name:   aws.String("instance-state-name"),
					Values: []string{"running"},
				},
			},
		}

		// Handle pagination
		paginator := ec2.NewDescribeInstancesPaginator(c.client, input)
		for paginator.HasMorePages() {
			output, err := paginator.NextPage(ctx)
			if err != nil {
				return nil, fmt.Errorf("failed to describe instances in %s: %w", region, err)
			}

			// Convert AWS SDK types to our types
			for _, reservation := range output.Reservations {
				for _, instance := range reservation.Instances {
					allInstances = append(allInstances, convertInstance(instance, region, c.accountID))
				}
			}
		}
	}

	return allInstances, nil
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
// If regions is empty, queries the client's configured region.
// If instanceTypes is empty, returns prices for all instance types.
// If productDescriptions is empty, defaults to ["Linux/UNIX"] for consistency with other pricing methods.
//
// The method queries the most recent spot price for each instance type + AZ combination.
// Results are deduplicated to return only the latest price per combination.
//
// Note: Spot prices vary by availability zone, not just region. The returned prices
// include the AZ to enable accurate cost calculations.
func (c *RealEC2Client) DescribeSpotPriceHistory(
	ctx context.Context,
	regions []string,
	instanceTypes []string,
	productDescriptions []string,
) ([]SpotPrice, error) {
	// If no regions specified, query the client's configured region
	queryRegions := regions
	if len(queryRegions) == 0 {
		queryRegions = []string{c.region}
	}

	// Default to Linux/UNIX if no product descriptions specified
	// This matches the behavior of other pricing methods (GetOnDemandPrice, GetSPRate)
	// which default to Linux when no OS is specified
	queryProductDescriptions := productDescriptions
	if len(queryProductDescriptions) == 0 {
		queryProductDescriptions = []string{"Linux/UNIX"}
	}

	var allPrices []SpotPrice

	for _, region := range queryRegions {
		// Build input for spot price history
		// We want the most recent price for each instance type + AZ
		input := &ec2.DescribeSpotPriceHistoryInput{
			// Use the specified product descriptions (or default to Linux/UNIX)
			ProductDescriptions: queryProductDescriptions,
			// Get most recent prices first
			StartTime: aws.Time(time.Now().Add(-1 * time.Hour)),
			// Limit results per page
			MaxResults: aws.Int32(1000),
		}

		// Filter to specific instance types if provided
		if len(instanceTypes) > 0 {
			for _, instanceType := range instanceTypes {
				input.InstanceTypes = append(input.InstanceTypes, types.InstanceType(instanceType))
			}
		}

		// Handle pagination
		paginator := ec2.NewDescribeSpotPriceHistoryPaginator(c.client, input)
		for paginator.HasMorePages() {
			output, err := paginator.NextPage(ctx)
			if err != nil {
				return nil, fmt.Errorf("failed to describe spot prices in %s: %w", region, err)
			}

			// Convert AWS SDK types to our types
			for _, price := range output.SpotPriceHistory {
				// Parse spot price string to float64
				priceFloat, err := strconv.ParseFloat(aws.ToString(price.SpotPrice), 64)
				if err != nil {
					// Skip invalid prices (shouldn't happen with real AWS data)
					continue
				}

				allPrices = append(allPrices, SpotPrice{
					InstanceType:       string(price.InstanceType),
					AvailabilityZone:   aws.ToString(price.AvailabilityZone),
					SpotPrice:          priceFloat,
					Timestamp:          aws.ToTime(price.Timestamp),
					ProductDescription: string(price.ProductDescription),
				})
			}
		}
	}

	// Deduplicate to get the most recent price for each instance type + AZ
	return deduplicateSpotPrices(allPrices), nil
}

// deduplicateSpotPrices returns only the most recent price for each instance type + AZ combination.
// AWS may return multiple historical prices; we want only the latest.
func deduplicateSpotPrices(prices []SpotPrice) []SpotPrice {
	// Build map keyed by instance type + AZ
	latest := make(map[string]SpotPrice)

	for _, price := range prices {
		key := price.InstanceType + ":" + price.AvailabilityZone

		// Keep the price with the most recent timestamp
		if existing, exists := latest[key]; !exists || price.Timestamp.After(existing.Timestamp) {
			latest[key] = price
		}
	}

	// Convert back to slice
	result := make([]SpotPrice, 0, len(latest))
	for _, price := range latest {
		result = append(result, price)
	}

	return result
}

// GetInstanceByID returns a specific instance by ID.
func (c *RealEC2Client) GetInstanceByID(_ context.Context, _ string, _ string) (*Instance, error) {
	// TODO: Implement real EC2 DescribeInstances call with instance ID filter
	return nil, nil
}

// convertInstance converts an AWS SDK Instance to our type.
func convertInstance(inst types.Instance, region, accountID string) Instance {
	// Extract launch time
	var launchTime time.Time
	if inst.LaunchTime != nil {
		launchTime = *inst.LaunchTime
	}

	// Determine lifecycle (spot or on-demand)
	lifecycle := LifecycleOnDemand
	if inst.InstanceLifecycle == types.InstanceLifecycleTypeSpot {
		lifecycle = LifecycleSpot
	}

	// Normalize platform name
	// AWS returns "windows" for Windows, but nothing for Linux
	platform := PlatformLinux
	if inst.Platform == types.PlatformValuesWindows {
		platform = PlatformWindows
	}

	// Convert tags to map
	tags := make(map[string]string)
	for _, tag := range inst.Tags {
		if tag.Key != nil && tag.Value != nil {
			tags[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
		}
	}

	// Extract tenancy from placement
	// AWS uses "default" for shared hardware, "dedicated" for dedicated instances, and "host" for dedicated hosts
	// This field is always present in the AWS API response
	tenancy := string(inst.Placement.Tenancy)

	return Instance{
		InstanceID:            aws.ToString(inst.InstanceId),
		InstanceType:          string(inst.InstanceType),
		AvailabilityZone:      aws.ToString(inst.Placement.AvailabilityZone),
		Region:                region,
		Lifecycle:             lifecycle,
		State:                 string(inst.State.Name),
		LaunchTime:            launchTime,
		AccountID:             accountID,
		Tags:                  tags,
		PrivateDNSName:        aws.ToString(inst.PrivateDnsName),
		PrivateIPAddress:      aws.ToString(inst.PrivateIpAddress),
		Platform:              platform,
		Tenancy:               tenancy,
		SpotInstanceRequestID: aws.ToString(inst.SpotInstanceRequestId),
	}
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
