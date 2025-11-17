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
	"testing"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
)

// TestNewRealPricingClient tests that NewRealPricingClient creates a valid client.
func TestNewRealPricingClient(t *testing.T) {
	ctx := context.Background()

	// Load default credential provider for testing
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion("us-east-1"))
	if err != nil {
		t.Fatalf("failed to load AWS config: %v", err)
	}

	// Test successful creation with default region
	client, err := NewRealPricingClient(ctx, cfg.Credentials, "")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if client.pricingRegion != PricingRegionUSEast1 {
		t.Errorf("expected pricing region %s, got: %s", PricingRegionUSEast1, client.pricingRegion)
	}
	if client.cacheTTL != 24*time.Hour {
		t.Errorf("expected 24h cache TTL, got: %v", client.cacheTTL)
	}
}

// TestNewRealPricingClientWithRegion tests region validation.
func TestNewRealPricingClientWithRegion(t *testing.T) {
	ctx := context.Background()

	// Load default credential provider for testing
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion("us-east-1"))
	if err != nil {
		t.Fatalf("failed to load AWS config: %v", err)
	}

	tests := []struct {
		name           string
		region         string
		expectError    bool
		expectedRegion string
	}{
		{
			name:           "valid us-east-1",
			region:         PricingRegionUSEast1,
			expectError:    false,
			expectedRegion: PricingRegionUSEast1,
		},
		{
			name:           "valid ap-south-1",
			region:         PricingRegionAPSouth1,
			expectError:    false,
			expectedRegion: PricingRegionAPSouth1,
		},
		{
			name:        "invalid region",
			region:      "us-west-2",
			expectError: true,
		},
		{
			name:        "empty region",
			region:      "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewRealPricingClientWithRegion(ctx, tt.region, cfg.Credentials, "")

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got nil")
				}
				if client != nil {
					t.Errorf("expected nil client on error, got: %v", client)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if client == nil {
					t.Fatal("expected non-nil client")
				}
				if client.pricingRegion != tt.expectedRegion {
					t.Errorf("expected region %s, got: %s", tt.expectedRegion, client.pricingRegion)
				}
			}
		})
	}
}

// TestRegionToLocation tests the region to location name mapping.
func TestRegionToLocation(t *testing.T) {
	tests := []struct {
		region   string
		expected string
		wantErr  bool
	}{
		// US regions
		{"us-east-1", "US East (N. Virginia)", false},
		{"us-east-2", "US East (Ohio)", false},
		{"us-west-1", "US West (N. California)", false},
		{"us-west-2", "US West (Oregon)", false},
		{"us-gov-east-1", "AWS GovCloud (US-East)", false},
		{"us-gov-west-1", "AWS GovCloud (US-West)", false},

		// Canada
		{"ca-central-1", "Canada (Central)", false},
		{"ca-west-1", "Canada West (Calgary)", false},

		// Europe
		{"eu-central-1", "EU (Frankfurt)", false},
		{"eu-central-2", "EU (Zurich)", false},
		{"eu-west-1", "EU (Ireland)", false},
		{"eu-west-2", "EU (London)", false},
		{"eu-west-3", "EU (Paris)", false},
		{"eu-north-1", "EU (Stockholm)", false},
		{"eu-south-1", "EU (Milan)", false},
		{"eu-south-2", "EU (Spain)", false},

		// Asia Pacific
		{"ap-east-1", "Asia Pacific (Hong Kong)", false},
		{"ap-south-1", "Asia Pacific (Mumbai)", false},
		{"ap-south-2", "Asia Pacific (Hyderabad)", false},
		{"ap-southeast-1", "Asia Pacific (Singapore)", false},
		{"ap-southeast-2", "Asia Pacific (Sydney)", false},
		{"ap-southeast-3", "Asia Pacific (Jakarta)", false},
		{"ap-southeast-4", "Asia Pacific (Melbourne)", false},
		{"ap-northeast-1", "Asia Pacific (Tokyo)", false},
		{"ap-northeast-2", "Asia Pacific (Seoul)", false},
		{"ap-northeast-3", "Asia Pacific (Osaka)", false},

		// Middle East
		{"me-south-1", "Middle East (Bahrain)", false},
		{"me-central-1", "Middle East (UAE)", false},

		// South America
		{"sa-east-1", "South America (Sao Paulo)", false},

		// Africa
		{"af-south-1", "Africa (Cape Town)", false},

		// Israel
		{"il-central-1", "Israel (Tel Aviv)", false},

		// Unknown region
		{"xx-unknown-1", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.region, func(t *testing.T) {
			location, err := regionToLocation(tt.region)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error for region %s, got nil", tt.region)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error for region %s: %v", tt.region, err)
				}
				if location != tt.expected {
					t.Errorf("expected location %q for region %s, got %q", tt.expected, tt.region, location)
				}
			}
		})
	}
}

// TestParsePricingDocument tests parsing of AWS Pricing API JSON documents.
func TestParsePricingDocument(t *testing.T) {
	tests := []struct {
		name            string
		doc             string
		region          string
		instanceType    string
		operatingSystem string
		expectedPrice   float64
		expectError     bool
	}{
		{
			name: "valid pricing document",
			doc: `{
				"terms": {
					"OnDemand": {
						"ABC123.JRTCKXETXF": {
							"priceDimensions": {
								"ABC123.JRTCKXETXF.6YS6EN2CT7": {
									"pricePerUnit": {
										"USD": "0.0520000000"
									},
									"unit": "Hrs"
								}
							}
						}
					}
				}
			}`,
			region:          "us-west-2",
			instanceType:    "m5.xlarge",
			operatingSystem: "Linux",
			expectedPrice:   0.052,
			expectError:     false,
		},
		{
			name: "multiple price dimensions - picks hourly",
			doc: `{
				"terms": {
					"OnDemand": {
						"ABC123.JRTCKXETXF": {
							"priceDimensions": {
								"ABC123.JRTCKXETXF.6YS6EN2CT7": {
									"pricePerUnit": {
										"USD": "10.0000000000"
									},
									"unit": "GB-Mo"
								},
								"ABC123.JRTCKXETXF.ABCDEF": {
									"pricePerUnit": {
										"USD": "0.1920000000"
									},
									"unit": "Hrs"
								}
							}
						}
					}
				}
			}`,
			region:          "us-east-1",
			instanceType:    "c5.2xlarge",
			operatingSystem: "Linux",
			expectedPrice:   0.192,
			expectError:     false,
		},
		{
			name:            "invalid JSON",
			doc:             `{invalid json`,
			region:          "us-west-2",
			instanceType:    "m5.xlarge",
			operatingSystem: "Linux",
			expectError:     true,
		},
		{
			name: "missing OnDemand terms",
			doc: `{
				"terms": {
					"Reserved": {}
				}
			}`,
			region:          "us-west-2",
			instanceType:    "m5.xlarge",
			operatingSystem: "Linux",
			expectError:     true,
		},
		{
			name: "missing Hrs unit",
			doc: `{
				"terms": {
					"OnDemand": {
						"ABC123.JRTCKXETXF": {
							"priceDimensions": {
								"ABC123.JRTCKXETXF.6YS6EN2CT7": {
									"pricePerUnit": {
										"USD": "10.0000000000"
									},
									"unit": "GB-Mo"
								}
							}
						}
					}
				}
			}`,
			region:          "us-west-2",
			instanceType:    "m5.xlarge",
			operatingSystem: "Linux",
			expectError:     true,
		},
		{
			name: "invalid price format",
			doc: `{
				"terms": {
					"OnDemand": {
						"ABC123.JRTCKXETXF": {
							"priceDimensions": {
								"ABC123.JRTCKXETXF.6YS6EN2CT7": {
									"pricePerUnit": {
										"USD": "invalid"
									},
									"unit": "Hrs"
								}
							}
						}
					}
				}
			}`,
			region:          "us-west-2",
			instanceType:    "m5.xlarge",
			operatingSystem: "Linux",
			expectError:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			price, err := parsePricingDocument(tt.doc, tt.region, tt.instanceType, tt.operatingSystem)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got nil")
				}
				if price != nil {
					t.Errorf("expected nil price on error, got: %v", price)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if price == nil {
					t.Fatal("expected non-nil price")
				}
				if price.PricePerHour != tt.expectedPrice {
					t.Errorf("expected price %.4f, got %.4f", tt.expectedPrice, price.PricePerHour)
				}
				if price.Region != tt.region {
					t.Errorf("expected region %s, got %s", tt.region, price.Region)
				}
				if price.InstanceType != tt.instanceType {
					t.Errorf("expected instance type %s, got %s", tt.instanceType, price.InstanceType)
				}
				if price.OperatingSystem != tt.operatingSystem {
					t.Errorf("expected OS %s, got %s", tt.operatingSystem, price.OperatingSystem)
				}
				if price.Tenancy != "Shared" {
					t.Errorf("expected tenancy Shared, got %s", price.Tenancy)
				}
			}
		})
	}
}

// TestBrokenPricingClient tests the error-returning fallback client.
func TestBrokenPricingClient(t *testing.T) {
	ctx := context.Background()
	expectedErr := context.Canceled
	client := &BrokenPricingClient{err: expectedErr}

	// Test GetOnDemandPrice
	price, err := client.GetOnDemandPrice(ctx, "us-west-2", "m5.xlarge", "Linux")
	if err != expectedErr {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}
	if price != nil {
		t.Errorf("expected nil price, got %v", price)
	}

	// Test GetOnDemandPrices
	prices, err := client.GetOnDemandPrices(ctx, "us-west-2", []string{"m5.xlarge", "c5.2xlarge"}, "Linux")
	if err != expectedErr {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}
	if prices != nil {
		t.Errorf("expected nil prices, got %v", prices)
	}
}

// TestRealPricingClientCaching tests that pricing results are cached properly.
func TestRealPricingClientCaching(t *testing.T) {
	ctx := context.Background()

	// Load default credential provider for testing
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion("us-east-1"))
	if err != nil {
		t.Fatalf("failed to load AWS config: %v", err)
	}

	// Create a client with short TTL for testing
	client, err := NewRealPricingClient(ctx, cfg.Credentials, "")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	// Override TTL for testing
	client.cacheTTL = 100 * time.Millisecond

	// Manually populate cache
	cacheKey := "us-west-2:m5.xlarge:Linux:Shared"
	expectedPrice := &OnDemandPrice{
		InstanceType:    "m5.xlarge",
		Region:          "us-west-2",
		PricePerHour:    0.192,
		OperatingSystem: "Linux",
		Tenancy:         "Shared",
	}

	client.cacheMutex.Lock()
	client.cache[cacheKey] = &cachedPrice{
		price:     expectedPrice,
		expiresAt: time.Now().Add(client.cacheTTL),
	}
	client.cacheMutex.Unlock()

	// Verify cache hit
	client.cacheMutex.RLock()
	cached, exists := client.cache[cacheKey]
	client.cacheMutex.RUnlock()

	if !exists {
		t.Fatal("expected cache entry to exist")
	}
	if cached.price.PricePerHour != expectedPrice.PricePerHour {
		t.Errorf("expected cached price %.4f, got %.4f", expectedPrice.PricePerHour, cached.price.PricePerHour)
	}

	// Wait for cache to expire
	time.Sleep(150 * time.Millisecond)

	// Verify cache miss due to expiration
	client.cacheMutex.RLock()
	cached, exists = client.cache[cacheKey]
	client.cacheMutex.RUnlock()

	if exists && time.Now().Before(cached.expiresAt) {
		t.Error("expected cache entry to be expired")
	}
}

// TestRealPricingClientGetOnDemandPrices tests batch pricing lookups.
func TestRealPricingClientGetOnDemandPrices(t *testing.T) {
	ctx := context.Background()

	// Load default credential provider for testing
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion("us-east-1"))
	if err != nil {
		t.Fatalf("failed to load AWS config: %v", err)
	}

	// Create client
	client, err := NewRealPricingClient(ctx, cfg.Credentials, "")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	// Pre-populate cache with some prices
	client.cacheMutex.Lock()
	client.cache["us-west-2:m5.xlarge:Linux:Shared"] = &cachedPrice{
		price: &OnDemandPrice{
			InstanceType:    "m5.xlarge",
			Region:          "us-west-2",
			PricePerHour:    0.192,
			OperatingSystem: "Linux",
			Tenancy:         "Shared",
		},
		expiresAt: time.Now().Add(24 * time.Hour),
	}
	client.cache["us-west-2:c5.2xlarge:Linux:Shared"] = &cachedPrice{
		price: &OnDemandPrice{
			InstanceType:    "c5.2xlarge",
			Region:          "us-west-2",
			PricePerHour:    0.34,
			OperatingSystem: "Linux",
			Tenancy:         "Shared",
		},
		expiresAt: time.Now().Add(24 * time.Hour),
	}
	client.cacheMutex.Unlock()

	// Test GetOnDemandPrices with cached data
	// Note: This will only return cached results since we're not making real API calls
	instanceTypes := []string{"m5.xlarge", "c5.2xlarge", "r5.large"}
	prices, err := client.GetOnDemandPrices(ctx, "us-west-2", instanceTypes, "Linux")

	// We expect no error even if some prices aren't found
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// We should get back the 2 cached prices
	if len(prices) < 2 {
		t.Errorf("expected at least 2 prices, got %d", len(prices))
	}

	// Verify the cached prices are returned
	foundM5 := false
	foundC5 := false
	for _, p := range prices {
		if p.InstanceType == "m5.xlarge" && p.PricePerHour == 0.192 {
			foundM5 = true
		}
		if p.InstanceType == "c5.2xlarge" && p.PricePerHour == 0.34 {
			foundC5 = true
		}
	}

	if !foundM5 {
		t.Error("expected to find m5.xlarge price in results")
	}
	if !foundC5 {
		t.Error("expected to find c5.2xlarge price in results")
	}
}
