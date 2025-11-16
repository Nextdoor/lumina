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
	"sync"
)

// MockClient is a mock implementation of the Client interface for testing.
// It provides configurable responses and tracks method calls.
type MockClient struct {
	mu sync.RWMutex

	// EC2Clients maps AccountID to MockEC2Client
	EC2Clients map[string]*MockEC2Client

	// SavingsPlansClients maps AccountID to MockSavingsPlansClient
	SavingsPlansClients map[string]*MockSavingsPlansClient

	// PricingClientInstance is the mock pricing client
	PricingClientInstance *MockPricingClient

	// AssumeRoleCalls tracks all AssumeRole attempts
	AssumeRoleCalls []AssumeRoleCall

	// Errors can be set to simulate AWS API errors
	EC2Error          error
	SavingsPlansError error
	PricingError      error
}

// AssumeRoleCall records an AssumeRole operation for testing.
type AssumeRoleCall struct {
	AccountID     string
	AssumeRoleARN string
	SessionName   string
}

// NewMockClient creates a new MockClient with initialized maps.
func NewMockClient() *MockClient {
	return &MockClient{
		EC2Clients:            make(map[string]*MockEC2Client),
		SavingsPlansClients:   make(map[string]*MockSavingsPlansClient),
		PricingClientInstance: NewMockPricingClient(),
		AssumeRoleCalls:       []AssumeRoleCall{},
	}
}

// EC2 returns a mock EC2Client for the specified account.
func (m *MockClient) EC2(ctx context.Context, accountConfig AccountConfig) (EC2Client, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.EC2Error != nil {
		return nil, m.EC2Error
	}

	// Track AssumeRole call if ARN is specified
	if accountConfig.AssumeRoleARN != "" {
		m.AssumeRoleCalls = append(m.AssumeRoleCalls, AssumeRoleCall{
			AccountID:     accountConfig.AccountID,
			AssumeRoleARN: accountConfig.AssumeRoleARN,
			SessionName:   accountConfig.SessionName,
		})
	}

	// Return existing client or create new one
	client, exists := m.EC2Clients[accountConfig.AccountID]
	if !exists {
		client = NewMockEC2Client()
		m.EC2Clients[accountConfig.AccountID] = client
	}

	return client, nil
}

// SavingsPlans returns a mock SavingsPlansClient for the specified account.
func (m *MockClient) SavingsPlans(ctx context.Context, accountConfig AccountConfig) (SavingsPlansClient, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.SavingsPlansError != nil {
		return nil, m.SavingsPlansError
	}

	// Track AssumeRole call if ARN is specified
	if accountConfig.AssumeRoleARN != "" {
		m.AssumeRoleCalls = append(m.AssumeRoleCalls, AssumeRoleCall{
			AccountID:     accountConfig.AccountID,
			AssumeRoleARN: accountConfig.AssumeRoleARN,
			SessionName:   accountConfig.SessionName,
		})
	}

	// Return existing client or create new one
	client, exists := m.SavingsPlansClients[accountConfig.AccountID]
	if !exists {
		client = NewMockSavingsPlansClient()
		m.SavingsPlansClients[accountConfig.AccountID] = client
	}

	return client, nil
}

// Pricing returns the mock PricingClient.
func (m *MockClient) Pricing(ctx context.Context) PricingClient {
	return m.PricingClientInstance
}

// MockEC2Client is a mock implementation of EC2Client for testing.
type MockEC2Client struct {
	mu sync.RWMutex

	// Instances is the mock instance data
	Instances []Instance

	// ReservedInstances is the mock RI data
	ReservedInstances []ReservedInstance

	// SpotPrices is the mock spot price data
	SpotPrices []SpotPrice

	// Error injection for testing error paths
	DescribeInstancesError         error
	DescribeReservedInstancesError error
	DescribeSpotPriceHistoryError  error
	GetInstanceByIDError           error

	// CallCounts tracks method call counts
	DescribeInstancesCallCount         int
	DescribeReservedInstancesCallCount int
	DescribeSpotPriceHistoryCallCount  int
	GetInstanceByIDCallCount           int
}

// NewMockEC2Client creates a new MockEC2Client.
func NewMockEC2Client() *MockEC2Client {
	return &MockEC2Client{
		Instances:         []Instance{},
		ReservedInstances: []ReservedInstance{},
		SpotPrices:        []SpotPrice{},
	}
}

// DescribeInstances returns the mock instance data.
func (m *MockEC2Client) DescribeInstances(ctx context.Context, regions []string) ([]Instance, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.DescribeInstancesCallCount++

	// Filter by region if specified
	if len(regions) == 0 {
		return m.Instances, nil
	}

	regionMap := make(map[string]bool)
	for _, r := range regions {
		regionMap[r] = true
	}

	filtered := []Instance{}
	for _, instance := range m.Instances {
		if regionMap[instance.Region] {
			filtered = append(filtered, instance)
		}
	}

	return filtered, nil
}

// DescribeReservedInstances returns the mock RI data.
func (m *MockEC2Client) DescribeReservedInstances(ctx context.Context, regions []string) ([]ReservedInstance, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.DescribeReservedInstancesCallCount++

	// Return error if set (for testing error paths)
	if m.DescribeReservedInstancesError != nil {
		return nil, m.DescribeReservedInstancesError
	}

	// Filter by region if specified
	if len(regions) == 0 {
		return m.ReservedInstances, nil
	}

	regionMap := make(map[string]bool)
	for _, r := range regions {
		regionMap[r] = true
	}

	filtered := []ReservedInstance{}
	for _, ri := range m.ReservedInstances {
		if regionMap[ri.Region] {
			filtered = append(filtered, ri)
		}
	}

	return filtered, nil
}

// DescribeSpotPriceHistory returns the mock spot price data.
func (m *MockEC2Client) DescribeSpotPriceHistory(
	ctx context.Context,
	regions []string,
	instanceTypes []string,
) ([]SpotPrice, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.DescribeSpotPriceHistoryCallCount++

	filtered := m.SpotPrices

	// Filter by region if specified
	if len(regions) > 0 {
		regionMap := make(map[string]bool)
		for _, r := range regions {
			regionMap[r] = true
		}

		regionFiltered := []SpotPrice{}
		for _, sp := range filtered {
			// Extract region from AZ (e.g., "us-west-2a" -> "us-west-2")
			region := sp.AvailabilityZone[:len(sp.AvailabilityZone)-1]
			if regionMap[region] {
				regionFiltered = append(regionFiltered, sp)
			}
		}
		filtered = regionFiltered
	}

	// Filter by instance type if specified
	if len(instanceTypes) > 0 {
		typeMap := make(map[string]bool)
		for _, t := range instanceTypes {
			typeMap[t] = true
		}

		typeFiltered := []SpotPrice{}
		for _, sp := range filtered {
			if typeMap[sp.InstanceType] {
				typeFiltered = append(typeFiltered, sp)
			}
		}
		filtered = typeFiltered
	}

	return filtered, nil
}

// GetInstanceByID returns a specific instance by ID.
func (m *MockEC2Client) GetInstanceByID(ctx context.Context, region string, instanceID string) (*Instance, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	m.GetInstanceByIDCallCount++

	for _, instance := range m.Instances {
		if instance.InstanceID == instanceID && instance.Region == region {
			return &instance, nil
		}
	}

	return nil, nil
}

// MockSavingsPlansClient is a mock implementation of SavingsPlansClient for testing.
type MockSavingsPlansClient struct {
	mu sync.RWMutex

	// SavingsPlans is the mock Savings Plans data
	SavingsPlans []SavingsPlan

	// Error injection for testing error paths
	DescribeSavingsPlansError error
	GetSavingsPlanByARNError  error

	// CallCounts tracks method call counts
	DescribeSavingsPlansCallCount int
	GetSavingsPlanByARNCallCount  int
}

// NewMockSavingsPlansClient creates a new MockSavingsPlansClient.
func NewMockSavingsPlansClient() *MockSavingsPlansClient {
	return &MockSavingsPlansClient{
		SavingsPlans: []SavingsPlan{},
	}
}

// DescribeSavingsPlans returns the mock Savings Plans data.
func (m *MockSavingsPlansClient) DescribeSavingsPlans(ctx context.Context) ([]SavingsPlan, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	m.DescribeSavingsPlansCallCount++

	// Return error if set (for testing error paths)
	if m.DescribeSavingsPlansError != nil {
		return nil, m.DescribeSavingsPlansError
	}

	return m.SavingsPlans, nil
}

// GetSavingsPlanByARN returns a specific Savings Plan by ARN.
func (m *MockSavingsPlansClient) GetSavingsPlanByARN(ctx context.Context, arn string) (*SavingsPlan, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	m.GetSavingsPlanByARNCallCount++

	for _, sp := range m.SavingsPlans {
		if sp.SavingsPlanARN == arn {
			return &sp, nil
		}
	}

	return nil, nil
}

// MockPricingClient is a mock implementation of PricingClient for testing.
type MockPricingClient struct {
	mu sync.RWMutex

	// OnDemandPrices maps "region:instanceType:os" to price
	OnDemandPrices map[string]*OnDemandPrice

	// CallCounts tracks method call counts
	GetOnDemandPriceCallCount  int
	GetOnDemandPricesCallCount int
}

// NewMockPricingClient creates a new MockPricingClient.
func NewMockPricingClient() *MockPricingClient {
	return &MockPricingClient{
		OnDemandPrices: make(map[string]*OnDemandPrice),
	}
}

// GetOnDemandPrice returns the mock on-demand price.
func (m *MockPricingClient) GetOnDemandPrice(
	ctx context.Context,
	region string,
	instanceType string,
	operatingSystem string,
) (*OnDemandPrice, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	m.GetOnDemandPriceCallCount++

	key := fmt.Sprintf("%s:%s:%s", region, instanceType, operatingSystem)
	price, exists := m.OnDemandPrices[key]
	if !exists {
		return nil, fmt.Errorf("on-demand price not found for %s", key)
	}

	return price, nil
}

// GetOnDemandPrices returns mock on-demand prices for multiple instance types.
func (m *MockPricingClient) GetOnDemandPrices(
	ctx context.Context,
	region string,
	instanceTypes []string,
	operatingSystem string,
) ([]OnDemandPrice, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	m.GetOnDemandPricesCallCount++

	prices := []OnDemandPrice{}
	for _, instanceType := range instanceTypes {
		key := fmt.Sprintf("%s:%s:%s", region, instanceType, operatingSystem)
		price, exists := m.OnDemandPrices[key]
		if exists {
			prices = append(prices, *price)
		}
	}

	return prices, nil
}

// SetOnDemandPrice sets a mock on-demand price (helper for tests).
func (m *MockPricingClient) SetOnDemandPrice(
	region string,
	instanceType string,
	operatingSystem string,
	pricePerHour float64,
) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := fmt.Sprintf("%s:%s:%s", region, instanceType, operatingSystem)
	m.OnDemandPrices[key] = &OnDemandPrice{
		InstanceType:    instanceType,
		Region:          region,
		PricePerHour:    pricePerHour,
		OperatingSystem: operatingSystem,
		Tenancy:         "Shared",
	}
}
