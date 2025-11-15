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

package controller

import (
	"context"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/nextdoor/lumina/internal/cache"
	"github.com/nextdoor/lumina/pkg/aws"
	"github.com/nextdoor/lumina/pkg/config"
	"github.com/nextdoor/lumina/pkg/metrics"
)

// TestRISPReconciler_Reconcile_Success tests successful reconciliation.
func TestRISPReconciler_Reconcile_Success(t *testing.T) {
	// Setup test data
	testRIs := []aws.ReservedInstance{
		{
			ReservedInstanceID: "ri-uswest-123",
			InstanceType:       "m5.large",
			Region:             "us-west-2",
			AccountID:          "123456789012",
			InstanceCount:      2,
			State:              "active",
		},
		{
			ReservedInstanceID: "ri-useast-456",
			InstanceType:       "m5.xlarge",
			Region:             "us-east-1",
			AccountID:          "123456789012",
			InstanceCount:      1,
			State:              "active",
		},
	}

	testSPs := []aws.SavingsPlan{
		{
			SavingsPlanARN:  "arn:aws:savingsplans::123456789012:savingsplan/sp-123",
			SavingsPlanType: "Compute",
			Commitment:      100.0,
			AccountID:       "123456789012",
			Region:          "all",
			State:           "active",
		},
	}

	// Create mock client
	mockClient := aws.NewMockClient()
	ctx := context.Background()

	// Setup EC2 client with RI data
	ec2Client, err := mockClient.EC2(ctx, aws.AccountConfig{
		AccountID: "123456789012",
		Region:    "us-west-2",
	})
	require.NoError(t, err)
	mockEC2 := ec2Client.(*aws.MockEC2Client)
	mockEC2.ReservedInstances = testRIs

	// Setup SavingsPlans client with SP data
	spClient, err := mockClient.SavingsPlans(ctx, aws.AccountConfig{
		AccountID: "123456789012",
		Region:    "us-west-2",
	})
	require.NoError(t, err)
	mockSP := spClient.(*aws.MockSavingsPlansClient)
	mockSP.SavingsPlans = testSPs

	// Create config
	cfg := &config.Config{
		AWSAccounts: []config.AWSAccount{
			{
				AccountID: "123456789012",
				Name:      "test-account",
			},
		},
		DefaultRegion: "us-west-2",
	}

	// Create cache and metrics
	rispCache := cache.NewRISPCache()
	m := metrics.NewMetrics(prometheus.NewRegistry()) // nil registry for testing

	// Create reconciler
	reconciler := &RISPReconciler{
		AWSClient: mockClient,
		Config:    cfg,
		Cache:     rispCache,
		Metrics:   m,
		Log:       logr.Discard(),
		Regions:   []string{"us-west-2", "us-east-1"},
	}

	// Run reconciliation
	ctx = context.Background()
	result, err := reconciler.Reconcile(ctx, ctrl.Request{})

	// Verify no error
	require.NoError(t, err)

	// Verify requeue after 1 hour
	assert.Equal(t, 1*time.Hour, result.RequeueAfter)

	// Verify cache was populated
	stats := rispCache.GetStats()
	assert.Equal(t, 2, stats.ReservedInstanceCount, "should have 2 RIs")
	assert.Equal(t, 1, stats.SavingsPlanCount, "should have 1 SP")
	assert.Equal(t, 2, stats.RegionCount, "should have 2 regions")
	assert.Equal(t, 1, stats.AccountCount, "should have 1 account")

	// Verify RIs by region
	westRIs := rispCache.GetReservedInstances("us-west-2", "123456789012")
	assert.Len(t, westRIs, 1)
	assert.Equal(t, "ri-uswest-123", westRIs[0].ReservedInstanceID)

	eastRIs := rispCache.GetReservedInstances("us-east-1", "123456789012")
	assert.Len(t, eastRIs, 1)
	assert.Equal(t, "ri-useast-456", eastRIs[0].ReservedInstanceID)

	// Verify SPs
	sps := rispCache.GetSavingsPlans("123456789012")
	assert.Len(t, sps, 1)
	assert.Equal(t, "arn:aws:savingsplans::123456789012:savingsplan/sp-123", sps[0].SavingsPlanARN)
}

// TestRISPReconciler_Reconcile_MultipleAccounts tests reconciliation with multiple accounts.
func TestRISPReconciler_Reconcile_MultipleAccounts(t *testing.T) {
	// Create mock client
	mockClient := aws.NewMockClient()
	ctx := context.Background()

	// Setup Account 1
	ec2Client1, err := mockClient.EC2(ctx, aws.AccountConfig{
		AccountID: "111111111111",
		Region:    "us-west-2",
	})
	require.NoError(t, err)
	mockEC2_1 := ec2Client1.(*aws.MockEC2Client)
	mockEC2_1.ReservedInstances = []aws.ReservedInstance{
		{
			ReservedInstanceID: "ri-account1",
			InstanceType:       "m5.large",
			Region:             "us-west-2",
			AccountID:          "111111111111",
			InstanceCount:      3,
			State:              "active",
		},
	}

	spClient1, err := mockClient.SavingsPlans(ctx, aws.AccountConfig{
		AccountID: "111111111111",
		Region:    "us-west-2",
	})
	require.NoError(t, err)
	mockSP1 := spClient1.(*aws.MockSavingsPlansClient)
	mockSP1.SavingsPlans = []aws.SavingsPlan{
		{
			SavingsPlanARN: "arn:aws:savingsplans::111111111111:savingsplan/sp-1",
			AccountID:      "111111111111",
			Commitment:     50.0,
			State:          "active",
		},
	}

	// Setup Account 2
	ec2Client2, err := mockClient.EC2(ctx, aws.AccountConfig{
		AccountID: "222222222222",
		Region:    "us-west-2",
	})
	require.NoError(t, err)
	mockEC2_2 := ec2Client2.(*aws.MockEC2Client)
	mockEC2_2.ReservedInstances = []aws.ReservedInstance{
		{
			ReservedInstanceID: "ri-account2",
			InstanceType:       "m5.xlarge",
			Region:             "us-west-2",
			AccountID:          "222222222222",
			InstanceCount:      1,
			State:              "active",
		},
	}

	spClient2, err := mockClient.SavingsPlans(ctx, aws.AccountConfig{
		AccountID: "222222222222",
		Region:    "us-west-2",
	})
	require.NoError(t, err)
	mockSP2 := spClient2.(*aws.MockSavingsPlansClient)
	mockSP2.SavingsPlans = []aws.SavingsPlan{
		{
			SavingsPlanARN: "arn:aws:savingsplans::222222222222:savingsplan/sp-2",
			AccountID:      "222222222222",
			Commitment:     75.0,
			State:          "active",
		},
	}

	// Create config with two accounts
	cfg := &config.Config{
		AWSAccounts: []config.AWSAccount{
			{AccountID: "111111111111", Name: "account-1"},
			{AccountID: "222222222222", Name: "account-2"},
		},
		DefaultRegion: "us-west-2",
	}

	// Create cache and metrics
	rispCache := cache.NewRISPCache()
	m := metrics.NewMetrics(prometheus.NewRegistry())

	// Create reconciler
	reconciler := &RISPReconciler{
		AWSClient: mockClient,
		Config:    cfg,
		Cache:     rispCache,
		Metrics:   m,
		Log:       logr.Discard(),
		Regions:   []string{"us-west-2"},
	}

	// Run reconciliation
	result, err := reconciler.Reconcile(ctx, ctrl.Request{})

	// Verify no error
	require.NoError(t, err)
	assert.Equal(t, 1*time.Hour, result.RequeueAfter)

	// Verify cache has data from both accounts
	stats := rispCache.GetStats()
	assert.Equal(t, 2, stats.ReservedInstanceCount, "should have 2 RIs total")
	assert.Equal(t, 2, stats.SavingsPlanCount, "should have 2 SPs total")
	assert.Equal(t, 2, stats.AccountCount, "should have 2 accounts")

	// Verify each account's data
	ris1 := rispCache.GetReservedInstances("us-west-2", "111111111111")
	assert.Len(t, ris1, 1)
	assert.Equal(t, "ri-account1", ris1[0].ReservedInstanceID)

	ris2 := rispCache.GetReservedInstances("us-west-2", "222222222222")
	assert.Len(t, ris2, 1)
	assert.Equal(t, "ri-account2", ris2[0].ReservedInstanceID)

	sps1 := rispCache.GetSavingsPlans("111111111111")
	assert.Len(t, sps1, 1)

	sps2 := rispCache.GetSavingsPlans("222222222222")
	assert.Len(t, sps2, 1)
}

// TestRISPReconciler_Reconcile_EmptyData tests reconciliation with no RIs or SPs.
func TestRISPReconciler_Reconcile_EmptyData(t *testing.T) {
	// Create mock client with no data
	mockClient := aws.NewMockClient()
	ctx := context.Background()

	// Setup EC2 client with empty data
	ec2Client, err := mockClient.EC2(ctx, aws.AccountConfig{
		AccountID: "123456789012",
		Region:    "us-west-2",
	})
	require.NoError(t, err)
	_ = ec2Client // Empty data, no setup needed

	// Setup SavingsPlans client with empty data
	spClient, err := mockClient.SavingsPlans(ctx, aws.AccountConfig{
		AccountID: "123456789012",
		Region:    "us-west-2",
	})
	require.NoError(t, err)
	_ = spClient // Empty data, no setup needed

	// Create config
	cfg := &config.Config{
		AWSAccounts: []config.AWSAccount{
			{AccountID: "123456789012", Name: "test-account"},
		},
		DefaultRegion: "us-west-2",
	}

	// Create cache and metrics
	rispCache := cache.NewRISPCache()
	m := metrics.NewMetrics(prometheus.NewRegistry())

	// Create reconciler
	reconciler := &RISPReconciler{
		AWSClient: mockClient,
		Config:    cfg,
		Cache:     rispCache,
		Metrics:   m,
		Log:       logr.Discard(),
		Regions:   []string{"us-west-2"},
	}

	// Run reconciliation
	ctx = context.Background()
	result, err := reconciler.Reconcile(ctx, ctrl.Request{})

	// Should succeed even with no data
	require.NoError(t, err)
	assert.Equal(t, 1*time.Hour, result.RequeueAfter)

	// Verify cache is empty but valid
	stats := rispCache.GetStats()
	assert.Equal(t, 0, stats.ReservedInstanceCount)
	assert.Equal(t, 0, stats.SavingsPlanCount)
}

// TestRISPReconciler_Reconcile_DefaultRegions tests that default regions are used when none provided.
func TestRISPReconciler_Reconcile_DefaultRegions(t *testing.T) {
	// Create mock client with RIs in default regions
	testRIs := []aws.ReservedInstance{
		{
			ReservedInstanceID: "ri-west",
			Region:             "us-west-2",
			AccountID:          "123456789012",
			State:              "active",
		},
		{
			ReservedInstanceID: "ri-east",
			Region:             "us-east-1",
			AccountID:          "123456789012",
			State:              "active",
		},
	}

	// Create mock client
	mockClient := aws.NewMockClient()
	ctx := context.Background()

	// Setup EC2 client with RI data
	ec2Client, err := mockClient.EC2(ctx, aws.AccountConfig{
		AccountID: "123456789012",
		Region:    "us-west-2",
	})
	require.NoError(t, err)
	mockEC2 := ec2Client.(*aws.MockEC2Client)
	mockEC2.ReservedInstances = testRIs

	// Setup SavingsPlans client with empty data
	spClient, err := mockClient.SavingsPlans(ctx, aws.AccountConfig{
		AccountID: "123456789012",
		Region:    "us-west-2",
	})
	require.NoError(t, err)
	_ = spClient // Empty data, no setup needed

	// Create config
	cfg := &config.Config{
		AWSAccounts: []config.AWSAccount{
			{AccountID: "123456789012", Name: "test-account"},
		},
		DefaultRegion: "us-west-2",
	}

	// Create cache and metrics
	rispCache := cache.NewRISPCache()
	m := metrics.NewMetrics(prometheus.NewRegistry())

	// Create reconciler with NO regions (should use defaults)
	reconciler := &RISPReconciler{
		AWSClient: mockClient,
		Config:    cfg,
		Cache:     rispCache,
		Metrics:   m,
		Log:       logr.Discard(),
		Regions:   nil, // Will use defaults
	}

	// Run reconciliation
	ctx = context.Background()
	result, err := reconciler.Reconcile(ctx, ctrl.Request{})

	require.NoError(t, err)
	assert.Equal(t, 1*time.Hour, result.RequeueAfter)

	// Should have data from both default regions
	stats := rispCache.GetStats()
	assert.Equal(t, 2, stats.ReservedInstanceCount, "should query default regions us-west-2 and us-east-1")
	assert.Equal(t, 2, stats.RegionCount)
}

// TestRISPReconciler_Reconcile_UpdatesExistingData tests that subsequent reconciliations update the cache.
func TestRISPReconciler_Reconcile_UpdatesExistingData(t *testing.T) {
	// Create mock client with initial data
	initialRIs := []aws.ReservedInstance{
		{
			ReservedInstanceID: "ri-1",
			Region:             "us-west-2",
			AccountID:          "123456789012",
			InstanceCount:      1,
			State:              "active",
		},
	}

	// Create mock client
	mockClient := aws.NewMockClient()
	ctx := context.Background()

	// Setup EC2 client with initial RI data
	ec2Client, err := mockClient.EC2(ctx, aws.AccountConfig{
		AccountID: "123456789012",
		Region:    "us-west-2",
	})
	require.NoError(t, err)
	mockEC2 := ec2Client.(*aws.MockEC2Client)
	mockEC2.ReservedInstances = initialRIs

	// Setup SavingsPlans client with empty data
	spClient, err := mockClient.SavingsPlans(ctx, aws.AccountConfig{
		AccountID: "123456789012",
		Region:    "us-west-2",
	})
	require.NoError(t, err)
	_ = spClient // Empty data, no setup needed

	// Create config
	cfg := &config.Config{
		AWSAccounts: []config.AWSAccount{
			{AccountID: "123456789012", Name: "test-account"},
		},
		DefaultRegion: "us-west-2",
	}

	// Create cache and metrics
	rispCache := cache.NewRISPCache()
	m := metrics.NewMetrics(prometheus.NewRegistry())

	// Create reconciler
	reconciler := &RISPReconciler{
		AWSClient: mockClient,
		Config:    cfg,
		Cache:     rispCache,
		Metrics:   m,
		Log:       logr.Discard(),
		Regions:   []string{"us-west-2"},
	}

	// First reconciliation
	_, err = reconciler.Reconcile(ctx, ctrl.Request{})
	require.NoError(t, err)

	// Verify initial data
	stats := rispCache.GetStats()
	assert.Equal(t, 1, stats.ReservedInstanceCount)

	// Update mock client with new data
	ec2Client, err = mockClient.EC2(ctx, aws.AccountConfig{
		AccountID: "123456789012",
		Region:    "us-west-2",
	})
	require.NoError(t, err)
	mockEC2 = ec2Client.(*aws.MockEC2Client)
	mockEC2.ReservedInstances = []aws.ReservedInstance{
		{
			ReservedInstanceID: "ri-1",
			Region:             "us-west-2",
			AccountID:          "123456789012",
			InstanceCount:      1,
			State:              "active",
		},
		{
			ReservedInstanceID: "ri-2",
			Region:             "us-west-2",
			AccountID:          "123456789012",
			InstanceCount:      2,
			State:              "active",
		},
	}

	// Second reconciliation
	_, err = reconciler.Reconcile(ctx, ctrl.Request{})
	require.NoError(t, err)

	// Verify updated data
	stats = rispCache.GetStats()
	assert.Equal(t, 2, stats.ReservedInstanceCount, "should have updated count")

	ris := rispCache.GetReservedInstances("us-west-2", "123456789012")
	assert.Len(t, ris, 2, "should have both RIs")
}
