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
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/pricing"
	pricingtypes "github.com/aws/aws-sdk-go-v2/service/pricing/types"
)

// RealPricingClient is a production implementation of PricingClient that makes
// real API calls to AWS Pricing using the AWS SDK v2.
//
// AWS Pricing API characteristics:
//   - Pricing data is public (no account-specific credentials needed)
//   - Pricing API is only available in us-east-1 and ap-south-1 regions
//   - Pricing data changes infrequently (typically monthly)
//   - Results should be cached to minimize API calls
//
// Caching strategy:
//   - In-memory cache with 24-hour TTL (prices change monthly)
//   - Cache key format: "region:instanceType:os:tenancy"
//   - Thread-safe with read-write mutex
type RealPricingClient struct {
	client        *pricing.Client
	cache         map[string]*cachedPrice
	cacheMutex    sync.RWMutex
	cacheTTL      time.Duration
	endpointURL   string // Optional endpoint URL for testing
	pricingRegion string // Region for pricing API (us-east-1 or ap-south-1)
}

// cachedPrice represents a cached pricing entry with expiration.
type cachedPrice struct {
	price     *OnDemandPrice
	expiresAt time.Time
}

const (
	// PricingRegionUSEast1 is the primary AWS Pricing API region.
	PricingRegionUSEast1 = "us-east-1"
	// PricingRegionAPSouth1 is the secondary AWS Pricing API region.
	PricingRegionAPSouth1 = "ap-south-1"
)

// NewRealPricingClient creates a new Pricing client.
// The pricing API is only available in us-east-1 and ap-south-1.
// By default, uses us-east-1 for the pricing API region.
func NewRealPricingClient(ctx context.Context, endpointURL string) (*RealPricingClient, error) {
	return NewRealPricingClientWithRegion(ctx, PricingRegionUSEast1, endpointURL)
}

// NewRealPricingClientWithRegion creates a new Pricing client with a specific pricing region.
// The pricing API is only available in us-east-1 and ap-south-1.
func NewRealPricingClientWithRegion(
	ctx context.Context,
	pricingRegion string,
	endpointURL string,
) (*RealPricingClient, error) {
	// Validate pricing region
	if pricingRegion != PricingRegionUSEast1 && pricingRegion != PricingRegionAPSouth1 {
		return nil, fmt.Errorf(
			"pricing API region must be %s or %s, got: %s",
			PricingRegionUSEast1,
			PricingRegionAPSouth1,
			pricingRegion,
		)
	}

	// Load AWS configuration
	// Pricing API does not require account-specific credentials as pricing data is public
	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(pricingRegion),
	)
	if err != nil { // coverage:ignore - AWS SDK config loading errors are difficult to trigger in unit tests
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Create pricing client with optional endpoint override
	var client *pricing.Client
	if endpointURL != "" {
		// For testing with LocalStack
		client = pricing.NewFromConfig(cfg, func(o *pricing.Options) {
			o.BaseEndpoint = aws.String(endpointURL)
		})
	} else {
		client = pricing.NewFromConfig(cfg)
	}

	return &RealPricingClient{
		client:        client,
		cache:         make(map[string]*cachedPrice),
		cacheTTL:      24 * time.Hour, // Prices change monthly, 24h cache is safe
		endpointURL:   endpointURL,
		pricingRegion: pricingRegion,
	}, nil
}

// GetOnDemandPrice returns the on-demand price for an instance type in a region.
//
// AWS Pricing API returns results as JSON documents that need to be parsed.
// We filter by:
//   - ServiceCode: AmazonEC2
//   - Location: mapped from AWS region (e.g., us-west-2 → "US West (Oregon)")
//   - InstanceType: exact match (e.g., "m5.xlarge")
//   - OperatingSystem: typically "Linux" or "Windows"
//   - Tenancy: typically "Shared", "Dedicated", or "Host"
//   - CapacityStatus: "Used" (excludes capacity reservations)
//   - PreInstalledSw: "NA" (excludes pre-installed software costs)
func (c *RealPricingClient) GetOnDemandPrice(
	ctx context.Context,
	region string,
	instanceType string,
	operatingSystem string,
) (*OnDemandPrice, error) {
	// Check cache first
	cacheKey := fmt.Sprintf("%s:%s:%s:Shared", region, instanceType, operatingSystem)
	c.cacheMutex.RLock()
	if cached, exists := c.cache[cacheKey]; exists && time.Now().Before(cached.expiresAt) {
		c.cacheMutex.RUnlock()
		return cached.price, nil
	}
	c.cacheMutex.RUnlock()

	// Convert AWS region code to location name
	// AWS Pricing API uses location names, not region codes
	// Example: "us-west-2" → "US West (Oregon)"
	location, err := regionToLocation(region)
	if err != nil {
		return nil, fmt.Errorf("unsupported region %s: %w", region, err)
	}

	// Query AWS Pricing API
	// We use GetProducts API with filters to find the matching price
	input := &pricing.GetProductsInput{
		ServiceCode: aws.String("AmazonEC2"),
		Filters: []pricingtypes.Filter{
			{
				Type:  pricingtypes.FilterTypeTermMatch,
				Field: aws.String("location"),
				Value: aws.String(location),
			},
			{
				Type:  pricingtypes.FilterTypeTermMatch,
				Field: aws.String("instanceType"),
				Value: aws.String(instanceType),
			},
			{
				Type:  pricingtypes.FilterTypeTermMatch,
				Field: aws.String("operatingSystem"),
				Value: aws.String(operatingSystem),
			},
			{
				Type:  pricingtypes.FilterTypeTermMatch,
				Field: aws.String("tenancy"),
				Value: aws.String("Shared"),
			},
			{
				Type:  pricingtypes.FilterTypeTermMatch,
				Field: aws.String("capacitystatus"),
				Value: aws.String("Used"),
			},
			{
				Type:  pricingtypes.FilterTypeTermMatch,
				Field: aws.String("preInstalledSw"),
				Value: aws.String("NA"),
			},
		},
		MaxResults: aws.Int32(1), // We only need one result
	}

	output, err := c.client.GetProducts(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to query AWS pricing API: %w", err)
	}

	// Parse the first result
	// AWS returns pricing data as JSON strings that need to be parsed
	if len(output.PriceList) == 0 {
		return nil, fmt.Errorf("no pricing data found for %s in %s (%s)", instanceType, region, operatingSystem)
	}

	// Parse the pricing JSON document
	price, err := parsePricingDocument(output.PriceList[0], region, instanceType, operatingSystem)
	if err != nil {
		return nil, fmt.Errorf("failed to parse pricing data: %w", err)
	}

	// Cache the result
	c.cacheMutex.Lock()
	c.cache[cacheKey] = &cachedPrice{
		price:     price,
		expiresAt: time.Now().Add(c.cacheTTL),
	}
	c.cacheMutex.Unlock()

	return price, nil
}

// GetOnDemandPrices returns on-demand prices for multiple instance types.
// This is more efficient than calling GetOnDemandPrice multiple times
// as it reduces the number of cache lookups.
func (c *RealPricingClient) GetOnDemandPrices(
	ctx context.Context,
	region string,
	instanceTypes []string,
	operatingSystem string,
) ([]OnDemandPrice, error) {
	prices := make([]OnDemandPrice, 0, len(instanceTypes))

	for _, instanceType := range instanceTypes {
		price, err := c.GetOnDemandPrice(ctx, region, instanceType, operatingSystem)
		if err != nil {
			// Skip instances we can't find pricing for
			// This is expected for new instance types not yet in the pricing API
			continue
		}
		prices = append(prices, *price)
	}

	return prices, nil
}

// LoadAllPricing bulk-loads pricing data for all instance types in the specified
// regions and operating systems. This is the most efficient way to populate pricing
// data at startup.
//
// The function makes paginated API calls to fetch all EC2 pricing data, which takes
// approximately 3-10 seconds for 5 regions depending on network latency and AWS API
// response times.
//
// Results are cached internally with 24-hour TTL and also returned as a map for
// callers who want to maintain their own cache.
//
// Rate Limiting: AWS Pricing API has strict rate limits (~10 req/sec). We use a
// semaphore to limit concurrent API calls to avoid throttling errors.
//
// Example usage:
//
//	prices, err := client.LoadAllPricing(ctx, []string{"us-west-2", "us-east-1"}, []string{"Linux"})
//	// Returns ~1200 prices (600 instance types × 2 regions × 1 OS)
func (c *RealPricingClient) LoadAllPricing(
	ctx context.Context,
	regions []string,
	operatingSystems []string,
) (map[string]float64, error) {
	// Parallel loading: Each region+OS combination is independent and can be queried concurrently
	// However, we must rate-limit to avoid AWS API throttling (10 req/sec limit)
	allPrices := make(map[string]float64)
	var mu sync.Mutex // Protect allPrices map
	var wg sync.WaitGroup
	errors := make(chan error, len(regions)*len(operatingSystems))

	// Rate limiting: AWS Pricing API allows ~10 requests/second
	// We'll be conservative and allow max 3 concurrent region+OS workers
	// Each worker makes multiple paginated requests (~6-8 pages per region/OS)
	// This gives us ~3-5 req/sec burst rate, well under the 10 req/sec limit
	semaphore := make(chan struct{}, 3)

	// Iterate through each region and OS combination in parallel
	for _, region := range regions {
		for _, os := range operatingSystems {
			wg.Add(1)
			go func(reg, operatingSystem string) {
				defer wg.Done()

				// Acquire semaphore slot (blocks if 3 workers already running)
				semaphore <- struct{}{}
				defer func() { <-semaphore }() // Release slot when done

				// Convert region code to location name for AWS Pricing API
				location, err := regionToLocation(reg)
				if err != nil {
					// Skip unsupported regions rather than failing entirely
					return
				}

				// Query AWS Pricing API for all EC2 instance pricing in this region/OS
				// We use pagination to fetch all results (typically 100 prices per page)
				var nextToken *string
				pageCount := 0

				for {
					pageCount++

					// Build the GetProducts request
					input := &pricing.GetProductsInput{
						ServiceCode: aws.String("AmazonEC2"),
						Filters: []pricingtypes.Filter{
							{
								Type:  pricingtypes.FilterTypeTermMatch,
								Field: aws.String("location"),
								Value: aws.String(location),
							},
							{
								Type:  pricingtypes.FilterTypeTermMatch,
								Field: aws.String("operatingSystem"),
								Value: aws.String(operatingSystem),
							},
							{
								Type:  pricingtypes.FilterTypeTermMatch,
								Field: aws.String("tenancy"),
								Value: aws.String("Shared"),
							},
							{
								Type:  pricingtypes.FilterTypeTermMatch,
								Field: aws.String("capacitystatus"),
								Value: aws.String("Used"),
							},
							{
								Type:  pricingtypes.FilterTypeTermMatch,
								Field: aws.String("preInstalledSw"),
								Value: aws.String("NA"),
							},
						},
						MaxResults: aws.Int32(100), // Maximum allowed per page
						NextToken:  nextToken,
					}

					// Make the API call
					output, err := c.client.GetProducts(ctx, input)
					if err != nil {
						errors <- fmt.Errorf("failed to query pricing API for %s/%s (page %d): %w",
							reg, operatingSystem, pageCount, err)
						return
					}

					// Parse each pricing document in this page
					for _, priceDoc := range output.PriceList {
						// Extract instance type from the document
						// We need to parse enough of the JSON to get the instance type
						var productInfo struct {
							Product struct {
								Attributes struct {
									InstanceType string `json:"instanceType"`
								} `json:"attributes"`
							} `json:"product"`
						}

						if err := json.Unmarshal([]byte(priceDoc), &productInfo); err != nil {
							// Skip malformed documents
							continue
						}

						instanceType := productInfo.Product.Attributes.InstanceType
						if instanceType == "" {
							// Skip if no instance type found
							continue
						}

						// Parse the full pricing document to get the hourly rate
						price, err := parsePricingDocument(priceDoc, reg, instanceType, operatingSystem)
						if err != nil {
							// Skip pricing entries we can't parse
							continue
						}

						// Store in results map (thread-safe)
						key := fmt.Sprintf("%s:%s:%s", reg, instanceType, operatingSystem)
						mu.Lock()
						allPrices[key] = price.PricePerHour
						mu.Unlock()

						// Also store in internal cache (thread-safe)
						cacheKey := fmt.Sprintf("%s:%s:%s:Shared", reg, instanceType, operatingSystem)
						c.cacheMutex.Lock()
						c.cache[cacheKey] = &cachedPrice{
							price:     price,
							expiresAt: time.Now().Add(c.cacheTTL),
						}
						c.cacheMutex.Unlock()
					}

					// Check if there are more pages
					if output.NextToken == nil {
						break // No more pages
					}
					nextToken = output.NextToken
				}
			}(region, os)
		}
	}

	// Wait for all goroutines to complete
	wg.Wait()
	close(errors)

	// Check for errors
	if len(errors) > 0 {
		// Return the first error encountered
		return nil, <-errors
	}

	return allPrices, nil
}

// parsePricingDocument parses an AWS Pricing API JSON document into an OnDemandPrice.
//
// The AWS Pricing API returns complex nested JSON documents with the following structure:
//
//	{
//	  "product": {
//	    "attributes": { ... },
//	    "sku": "ABC123..."
//	  },
//	  "terms": {
//	    "OnDemand": {
//	      "ABC123.JRTCKXETXF": {
//	        "priceDimensions": {
//	          "ABC123.JRTCKXETXF.6YS6EN2CT7": {
//	            "pricePerUnit": {
//	              "USD": "0.0520000000"
//	            },
//	            "unit": "Hrs",
//	            ...
//	          }
//	        },
//	        ...
//	      }
//	    }
//	  }
//	}
//
// We need to:
// 1. Parse the JSON document
// 2. Navigate to terms.OnDemand (first key)
// 3. Navigate to priceDimensions (first key)
// 4. Extract pricePerUnit.USD
func parsePricingDocument(
	doc string,
	region string,
	instanceType string,
	operatingSystem string,
) (*OnDemandPrice, error) {
	// Parse the JSON document
	var pricingDoc struct {
		Terms struct {
			OnDemand map[string]struct {
				PriceDimensions map[string]struct {
					PricePerUnit struct {
						USD string `json:"USD"`
					} `json:"pricePerUnit"`
					Unit string `json:"unit"`
				} `json:"priceDimensions"`
			} `json:"OnDemand"`
		} `json:"terms"`
	}

	if err := json.Unmarshal([]byte(doc), &pricingDoc); err != nil {
		return nil, fmt.Errorf("failed to parse pricing JSON: %w", err)
	}

	// Navigate to OnDemand terms (there should be exactly one)
	var priceStr string
	for _, onDemandTerm := range pricingDoc.Terms.OnDemand {
		// Navigate to price dimensions (there should be exactly one for compute hours)
		for _, dimension := range onDemandTerm.PriceDimensions {
			if dimension.Unit == "Hrs" {
				priceStr = dimension.PricePerUnit.USD
				break
			}
		}
		if priceStr != "" {
			break
		}
	}

	if priceStr == "" {
		return nil, fmt.Errorf("no hourly price found in pricing document")
	}

	// Parse the price string to float64
	var pricePerHour float64
	if _, err := fmt.Sscanf(priceStr, "%f", &pricePerHour); err != nil {
		return nil, fmt.Errorf("failed to parse price %q: %w", priceStr, err)
	}

	return &OnDemandPrice{
		InstanceType:    instanceType,
		Region:          region,
		PricePerHour:    pricePerHour,
		OperatingSystem: operatingSystem,
		Tenancy:         "Shared",
	}, nil
}

// regionToLocation converts an AWS region code to a location name used by the Pricing API.
//
// AWS Pricing API uses human-readable location names instead of region codes.
// For example: "us-west-2" → "US West (Oregon)"
//
// This mapping is required for querying the Pricing API correctly.
func regionToLocation(region string) (string, error) {
	// AWS region to location name mapping
	// Source: https://docs.aws.amazon.com/general/latest/gr/rande.html
	locations := map[string]string{
		// US regions
		"us-east-1":     "US East (N. Virginia)",
		"us-east-2":     "US East (Ohio)",
		"us-west-1":     "US West (N. California)",
		"us-west-2":     "US West (Oregon)",
		"us-gov-east-1": "AWS GovCloud (US-East)",
		"us-gov-west-1": "AWS GovCloud (US-West)",

		// Canada
		"ca-central-1": "Canada (Central)",
		"ca-west-1":    "Canada West (Calgary)",

		// Europe
		"eu-central-1": "EU (Frankfurt)",
		"eu-central-2": "EU (Zurich)",
		"eu-west-1":    "EU (Ireland)",
		"eu-west-2":    "EU (London)",
		"eu-west-3":    "EU (Paris)",
		"eu-north-1":   "EU (Stockholm)",
		"eu-south-1":   "EU (Milan)",
		"eu-south-2":   "EU (Spain)",

		// Asia Pacific
		"ap-east-1":      "Asia Pacific (Hong Kong)",
		"ap-south-1":     "Asia Pacific (Mumbai)",
		"ap-south-2":     "Asia Pacific (Hyderabad)",
		"ap-southeast-1": "Asia Pacific (Singapore)",
		"ap-southeast-2": "Asia Pacific (Sydney)",
		"ap-southeast-3": "Asia Pacific (Jakarta)",
		"ap-southeast-4": "Asia Pacific (Melbourne)",
		"ap-northeast-1": "Asia Pacific (Tokyo)",
		"ap-northeast-2": "Asia Pacific (Seoul)",
		"ap-northeast-3": "Asia Pacific (Osaka)",

		// Middle East
		"me-south-1":   "Middle East (Bahrain)",
		"me-central-1": "Middle East (UAE)",

		// South America
		"sa-east-1": "South America (Sao Paulo)",

		// Africa
		"af-south-1": "Africa (Cape Town)",

		// Israel
		"il-central-1": "Israel (Tel Aviv)",
	}

	location, exists := locations[region]
	if !exists {
		return "", fmt.Errorf("unknown AWS region: %s", region)
	}

	return location, nil
}
