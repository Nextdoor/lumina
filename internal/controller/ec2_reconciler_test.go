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

// newTestConfig creates a test configuration with default values for testing.
func newTestConfig() *config.Config {
	return &config.Config{
		AWSAccounts: []config.AWSAccount{
			{
				AccountID:     "123456789012",
				Name:          "Test",
				AssumeRoleARN: "arn:aws:iam::123456789012:role/test",
			},
		},
	}
}

// TestEC2Reconciler_Reconcile_Success tests successful reconciliation.
func TestEC2Reconciler_Reconcile_Success(t *testing.T) {
	// Setup test data
	testInstances := []aws.Instance{
		{
			InstanceID:   "i-uswest-123",
			InstanceType: "m5.large",
			Region:       "us-west-2",
			AccountID:    "123456789012",
			State:        "running",
		},
		{
			InstanceID:   "i-uswest-456",
			InstanceType: "m5.xlarge",
			Region:       "us-west-2",
			AccountID:    "123456789012",
			State:        "stopped",
		},
		{
			InstanceID:   "i-useast-789",
			InstanceType: "c5.2xlarge",
			Region:       "us-east-1",
			AccountID:    "123456789012",
			State:        "running",
		},
	}

	// Create mock client
	mockClient := aws.NewMockClient()
	ctx := context.Background()

	// Setup EC2 client with instance data
	ec2Client, err := mockClient.EC2(ctx, aws.AccountConfig{
		AccountID: "123456789012",
		Region:    "us-west-2",
	})
	require.NoError(t, err)
	mockEC2 := ec2Client.(*aws.MockEC2Client)
	mockEC2.Instances = testInstances

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
	ec2Cache := cache.NewEC2Cache()
	m := metrics.NewMetrics(prometheus.NewRegistry(), newTestConfig())

	// Create reconciler
	reconciler := &EC2Reconciler{
		AWSClient: mockClient,
		Config:    cfg,
		Cache:     ec2Cache,
		Metrics:   m,
		Log:       logr.Discard(),
		Regions:   []string{"us-west-2", "us-east-1"},
	}

	// Run reconciliation
	result, err := reconciler.Reconcile(ctx, ctrl.Request{})

	// Verify no error
	require.NoError(t, err)

	// Verify requeue after 5 minutes
	assert.Equal(t, 5*time.Minute, result.RequeueAfter)

	// Verify cache was populated
	allInstances := ec2Cache.GetAllInstances()
	assert.Len(t, allInstances, 3, "should have 3 instances total")

	runningInstances := ec2Cache.GetRunningInstances()
	assert.Len(t, runningInstances, 2, "should have 2 running instances")

	// Verify instances by region
	westInstances := ec2Cache.GetInstancesByRegion("us-west-2")
	assert.Len(t, westInstances, 2, "should have 2 instances in us-west-2")

	eastInstances := ec2Cache.GetInstancesByRegion("us-east-1")
	assert.Len(t, eastInstances, 1, "should have 1 instance in us-east-1")

	// Verify specific instance
	inst, found := ec2Cache.GetInstance("i-uswest-123")
	require.True(t, found)
	assert.Equal(t, "m5.large", inst.InstanceType)
	assert.Equal(t, "running", inst.State)
}

// TestEC2Reconciler_Reconcile_MultipleAccounts tests reconciliation with multiple accounts.
func TestEC2Reconciler_Reconcile_MultipleAccounts(t *testing.T) {
	// Create mock client
	mockClient := aws.NewMockClient()
	ctx := context.Background()

	// Setup EC2 client for account 1
	ec2Client1, err := mockClient.EC2(ctx, aws.AccountConfig{
		AccountID: "111111111111",
		Region:    "us-west-2",
	})
	require.NoError(t, err)
	mockEC2_1 := ec2Client1.(*aws.MockEC2Client)
	mockEC2_1.Instances = []aws.Instance{
		{
			InstanceID:   "i-account1-1",
			InstanceType: "m5.large",
			Region:       "us-west-2",
			AccountID:    "111111111111",
			State:        "running",
		},
	}

	// Setup EC2 client for account 2
	ec2Client2, err := mockClient.EC2(ctx, aws.AccountConfig{
		AccountID: "222222222222",
		Region:    "us-west-2",
	})
	require.NoError(t, err)
	mockEC2_2 := ec2Client2.(*aws.MockEC2Client)
	mockEC2_2.Instances = []aws.Instance{
		{
			InstanceID:   "i-account2-1",
			InstanceType: "c5.2xlarge",
			Region:       "us-west-2",
			AccountID:    "222222222222",
			State:        "running",
		},
	}

	// Create config with multiple accounts
	cfg := &config.Config{
		AWSAccounts: []config.AWSAccount{
			{
				AccountID: "111111111111",
				Name:      "account-1",
			},
			{
				AccountID: "222222222222",
				Name:      "account-2",
			},
		},
		DefaultRegion: "us-west-2",
	}

	// Create cache and metrics
	ec2Cache := cache.NewEC2Cache()
	m := metrics.NewMetrics(prometheus.NewRegistry(), newTestConfig())

	// Create reconciler
	reconciler := &EC2Reconciler{
		AWSClient: mockClient,
		Config:    cfg,
		Cache:     ec2Cache,
		Metrics:   m,
		Log:       logr.Discard(),
		Regions:   []string{"us-west-2"},
	}

	// Run reconciliation
	result, err := reconciler.Reconcile(ctx, ctrl.Request{})

	// Verify no error
	require.NoError(t, err)
	assert.Equal(t, 5*time.Minute, result.RequeueAfter)

	// Verify both accounts' instances are cached
	allInstances := ec2Cache.GetAllInstances()
	assert.Len(t, allInstances, 2)

	account1Instances := ec2Cache.GetInstancesByAccount("111111111111")
	assert.Len(t, account1Instances, 1)
	assert.Equal(t, "i-account1-1", account1Instances[0].InstanceID)

	account2Instances := ec2Cache.GetInstancesByAccount("222222222222")
	assert.Len(t, account2Instances, 1)
	assert.Equal(t, "i-account2-1", account2Instances[0].InstanceID)
}

// TestEC2Reconciler_Reconcile_APIError tests handling of API errors.
func TestEC2Reconciler_Reconcile_APIError(t *testing.T) {
	// Create mock client
	mockClient := aws.NewMockClient()
	ctx := context.Background()

	// Setup EC2 client that will return error on DescribeInstances
	ec2Client, err := mockClient.EC2(ctx, aws.AccountConfig{
		AccountID: "123456789012",
		Region:    "us-west-2",
	})
	require.NoError(t, err)
	mockEC2 := ec2Client.(*aws.MockEC2Client)
	mockEC2.DescribeInstancesError = assert.AnError // This will cause DescribeInstances to fail

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
	ec2Cache := cache.NewEC2Cache()
	m := metrics.NewMetrics(prometheus.NewRegistry(), newTestConfig())

	// Create reconciler
	reconciler := &EC2Reconciler{
		AWSClient: mockClient,
		Config:    cfg,
		Cache:     ec2Cache,
		Metrics:   m,
		Log:       logr.Discard(),
		Regions:   []string{"us-west-2"},
	}

	// Run reconciliation
	result, err := reconciler.Reconcile(ctx, ctrl.Request{})

	// Verify no error returned (errors are logged but don't fail reconciliation)
	require.NoError(t, err)
	assert.Equal(t, 5*time.Minute, result.RequeueAfter)

	// Verify cache is empty (API errors prevent updates)
	allInstances := ec2Cache.GetAllInstances()
	assert.Empty(t, allInstances)
}

// TestEC2Reconciler_Reconcile_EmptyResults tests handling of empty instance lists.
func TestEC2Reconciler_Reconcile_EmptyResults(t *testing.T) {
	// Create mock client
	mockClient := aws.NewMockClient()
	ctx := context.Background()

	// Setup EC2 client with no instances
	ec2Client, err := mockClient.EC2(ctx, aws.AccountConfig{
		AccountID: "123456789012",
		Region:    "us-west-2",
	})
	require.NoError(t, err)
	mockEC2 := ec2Client.(*aws.MockEC2Client)
	mockEC2.Instances = []aws.Instance{} // Empty list

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
	ec2Cache := cache.NewEC2Cache()
	m := metrics.NewMetrics(prometheus.NewRegistry(), newTestConfig())

	// Create reconciler
	reconciler := &EC2Reconciler{
		AWSClient: mockClient,
		Config:    cfg,
		Cache:     ec2Cache,
		Metrics:   m,
		Log:       logr.Discard(),
		Regions:   []string{"us-west-2"},
	}

	// Run reconciliation
	result, err := reconciler.Reconcile(ctx, ctrl.Request{})

	// Verify success with empty results
	require.NoError(t, err)
	assert.Equal(t, 5*time.Minute, result.RequeueAfter)

	// Verify cache is empty
	allInstances := ec2Cache.GetAllInstances()
	assert.Empty(t, allInstances)
}

// TestEC2Reconciler_Reconcile_DefaultRegions tests that default regions are used when none provided.
func TestEC2Reconciler_Reconcile_DefaultRegions(t *testing.T) {
	// Create mock client
	mockClient := aws.NewMockClient()
	ctx := context.Background()

	// Setup EC2 client
	ec2Client, err := mockClient.EC2(ctx, aws.AccountConfig{
		AccountID: "123456789012",
		Region:    "us-west-2",
	})
	require.NoError(t, err)
	mockEC2 := ec2Client.(*aws.MockEC2Client)
	mockEC2.Instances = []aws.Instance{
		{
			InstanceID:   "i-default-region",
			InstanceType: "m5.large",
			Region:       "us-west-2",
			AccountID:    "123456789012",
			State:        "running",
		},
	}

	// Create config with NO regions specified
	cfg := &config.Config{
		AWSAccounts: []config.AWSAccount{
			{
				AccountID: "123456789012",
				Name:      "test-account",
			},
		},
		DefaultRegion: "us-west-2",
		Regions:       nil, // No regions specified
	}

	// Create cache and metrics
	ec2Cache := cache.NewEC2Cache()
	m := metrics.NewMetrics(prometheus.NewRegistry(), newTestConfig())

	// Create reconciler with NO regions (should fall back to defaults)
	reconciler := &EC2Reconciler{
		AWSClient: mockClient,
		Config:    cfg,
		Cache:     ec2Cache,
		Metrics:   m,
		Log:       logr.Discard(),
		Regions:   nil, // No regions specified
	}

	// Run reconciliation
	result, err := reconciler.Reconcile(ctx, ctrl.Request{})

	// Verify success
	require.NoError(t, err)
	assert.Equal(t, 5*time.Minute, result.RequeueAfter)

	// Verify instances were queried using default regions
	allInstances := ec2Cache.GetAllInstances()
	assert.NotEmpty(t, allInstances, "should have instances from default regions")
}

// TestEC2Reconciler_Reconcile_AccountSpecificRegions tests account-specific region overrides.
func TestEC2Reconciler_Reconcile_AccountSpecificRegions(t *testing.T) {
	// Create mock client
	mockClient := aws.NewMockClient()
	ctx := context.Background()

	// Setup EC2 clients
	ec2Client1, err := mockClient.EC2(ctx, aws.AccountConfig{
		AccountID: "111111111111",
		Region:    "us-west-2",
	})
	require.NoError(t, err)
	mockEC2_1 := ec2Client1.(*aws.MockEC2Client)
	mockEC2_1.Instances = []aws.Instance{
		{
			InstanceID:   "i-account1-west",
			InstanceType: "m5.large",
			Region:       "us-west-2",
			AccountID:    "111111111111",
			State:        "running",
		},
	}

	ec2Client2, err := mockClient.EC2(ctx, aws.AccountConfig{
		AccountID: "222222222222",
		Region:    "eu-west-1",
	})
	require.NoError(t, err)
	mockEC2_2 := ec2Client2.(*aws.MockEC2Client)
	mockEC2_2.Instances = []aws.Instance{
		{
			InstanceID:   "i-account2-eu",
			InstanceType: "c5.2xlarge",
			Region:       "eu-west-1",
			AccountID:    "222222222222",
			State:        "running",
		},
	}

	// Create config with account-specific regions
	cfg := &config.Config{
		AWSAccounts: []config.AWSAccount{
			{
				AccountID: "111111111111",
				Name:      "account-1-us",
				Regions:   []string{"us-west-2"}, // Account-specific region
			},
			{
				AccountID: "222222222222",
				Name:      "account-2-eu",
				Regions:   []string{"eu-west-1"}, // Account-specific region
			},
		},
		DefaultRegion: "us-west-2",
		Regions:       []string{"us-east-1"}, // Global default (should be overridden)
	}

	// Create cache and metrics
	ec2Cache := cache.NewEC2Cache()
	m := metrics.NewMetrics(prometheus.NewRegistry(), newTestConfig())

	// Create reconciler
	reconciler := &EC2Reconciler{
		AWSClient: mockClient,
		Config:    cfg,
		Cache:     ec2Cache,
		Metrics:   m,
		Log:       logr.Discard(),
		Regions:   []string{"us-east-1"}, // Should be overridden by account-specific regions
	}

	// Run reconciliation
	result, err := reconciler.Reconcile(ctx, ctrl.Request{})

	// Verify success
	require.NoError(t, err)
	assert.Equal(t, 5*time.Minute, result.RequeueAfter)

	// Verify correct instances from account-specific regions
	westInstances := ec2Cache.GetInstancesByRegion("us-west-2")
	assert.Len(t, westInstances, 1)
	assert.Equal(t, "i-account1-west", westInstances[0].InstanceID)

	euInstances := ec2Cache.GetInstancesByRegion("eu-west-1")
	assert.Len(t, euInstances, 1)
	assert.Equal(t, "i-account2-eu", euInstances[0].InstanceID)

	// Verify no instances from global default region
	eastInstances := ec2Cache.GetInstancesByRegion("us-east-1")
	assert.Empty(t, eastInstances, "should not query global default region when account has specific regions")
}

// TestEC2Reconciler_Reconcile_CacheUpdate tests that cache is properly updated on successive reconciliations.
func TestEC2Reconciler_Reconcile_CacheUpdate(t *testing.T) {
	// Create mock client
	mockClient := aws.NewMockClient()
	ctx := context.Background()

	// Setup EC2 client
	ec2Client, err := mockClient.EC2(ctx, aws.AccountConfig{
		AccountID: "123456789012",
		Region:    "us-west-2",
	})
	require.NoError(t, err)
	mockEC2 := ec2Client.(*aws.MockEC2Client)

	// Initial instances
	mockEC2.Instances = []aws.Instance{
		{
			InstanceID:   "i-initial-1",
			InstanceType: "m5.large",
			Region:       "us-west-2",
			AccountID:    "123456789012",
			State:        "running",
		},
		{
			InstanceID:   "i-initial-2",
			InstanceType: "m5.xlarge",
			Region:       "us-west-2",
			AccountID:    "123456789012",
			State:        "running",
		},
	}

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
	ec2Cache := cache.NewEC2Cache()
	m := metrics.NewMetrics(prometheus.NewRegistry(), newTestConfig())

	// Create reconciler
	reconciler := &EC2Reconciler{
		AWSClient: mockClient,
		Config:    cfg,
		Cache:     ec2Cache,
		Metrics:   m,
		Log:       logr.Discard(),
		Regions:   []string{"us-west-2"},
	}

	// First reconciliation
	_, err = reconciler.Reconcile(ctx, ctrl.Request{})
	require.NoError(t, err)

	// Verify initial cache
	allInstances := ec2Cache.GetAllInstances()
	assert.Len(t, allInstances, 2)

	// Simulate instance changes (one terminated, one new)
	mockEC2.Instances = []aws.Instance{
		{
			InstanceID:   "i-initial-1",
			InstanceType: "m5.large",
			Region:       "us-west-2",
			AccountID:    "123456789012",
			State:        "running",
		},
		// i-initial-2 terminated (removed from list)
		{
			InstanceID:   "i-new-3",
			InstanceType: "c5.2xlarge",
			Region:       "us-west-2",
			AccountID:    "123456789012",
			State:        "running",
		},
	}

	// Second reconciliation
	_, err = reconciler.Reconcile(ctx, ctrl.Request{})
	require.NoError(t, err)

	// Verify cache updated
	allInstances = ec2Cache.GetAllInstances()
	assert.Len(t, allInstances, 2, "should have 2 instances after update")

	// Verify old instance gone
	_, found := ec2Cache.GetInstance("i-initial-2")
	assert.False(t, found, "terminated instance should be removed")

	// Verify new instance present
	inst, found := ec2Cache.GetInstance("i-new-3")
	require.True(t, found, "new instance should be added")
	assert.Equal(t, "c5.2xlarge", inst.InstanceType)
}

// TestEC2Reconciler_reconcileAccountRegion tests the single account+region reconciliation method.
func TestEC2Reconciler_reconcileAccountRegion(t *testing.T) {
	// Create mock client
	mockClient := aws.NewMockClient()
	ctx := context.Background()

	// Setup EC2 client
	ec2Client, err := mockClient.EC2(ctx, aws.AccountConfig{
		AccountID: "123456789012",
		Region:    "us-west-2",
	})
	require.NoError(t, err)
	mockEC2 := ec2Client.(*aws.MockEC2Client)
	mockEC2.Instances = []aws.Instance{
		{
			InstanceID:   "i-test-123",
			InstanceType: "m5.large",
			Region:       "us-west-2",
			AccountID:    "123456789012",
			State:        "running",
		},
	}

	// Create cache and metrics
	ec2Cache := cache.NewEC2Cache()
	m := metrics.NewMetrics(prometheus.NewRegistry(), newTestConfig())

	// Create reconciler
	reconciler := &EC2Reconciler{
		AWSClient: mockClient,
		Config: &config.Config{
			DefaultRegion: "us-west-2",
		},
		Cache:   ec2Cache,
		Metrics: m,
		Log:     logr.Discard(),
	}

	// Test account config
	account := config.AWSAccount{
		AccountID: "123456789012",
		Name:      "test-account",
	}

	// Run reconciliation for single account+region
	err = reconciler.reconcileAccountRegion(ctx, account, "us-west-2")
	require.NoError(t, err)

	// Verify cache updated
	inst, found := ec2Cache.GetInstance("i-test-123")
	require.True(t, found)
	assert.Equal(t, "m5.large", inst.InstanceType)
	assert.Equal(t, "123456789012", inst.AccountID)
	assert.Equal(t, "us-west-2", inst.Region)
}

// TestEC2Reconciler_reconcileAccountRegion_Error tests error handling in single region reconciliation.
func TestEC2Reconciler_reconcileAccountRegion_Error(t *testing.T) {
	// Create mock client
	mockClient := aws.NewMockClient()
	ctx := context.Background()

	// Setup EC2 client that will return error on DescribeInstances
	ec2Client, err := mockClient.EC2(ctx, aws.AccountConfig{
		AccountID: "123456789012",
		Region:    "us-west-2",
	})
	require.NoError(t, err)
	mockEC2 := ec2Client.(*aws.MockEC2Client)
	mockEC2.DescribeInstancesError = assert.AnError // This will cause DescribeInstances to fail

	// Create cache and metrics
	ec2Cache := cache.NewEC2Cache()
	m := metrics.NewMetrics(prometheus.NewRegistry(), newTestConfig())

	// Create reconciler
	reconciler := &EC2Reconciler{
		AWSClient: mockClient,
		Config: &config.Config{
			DefaultRegion: "us-west-2",
		},
		Cache:   ec2Cache,
		Metrics: m,
		Log:     logr.Discard(),
	}

	// Test account config
	account := config.AWSAccount{
		AccountID: "123456789012",
		Name:      "test-account",
	}

	// Run reconciliation (should fail)
	err = reconciler.reconcileAccountRegion(ctx, account, "us-west-2")

	// Verify error returned
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to describe instances")

	// Verify cache not updated
	allInstances := ec2Cache.GetAllInstances()
	assert.Empty(t, allInstances)
}

// TestEC2Reconciler_Reconcile_StateBreakdown tests logging of instance state breakdown.
func TestEC2Reconciler_Reconcile_StateBreakdown(t *testing.T) {
	// Create mock client
	mockClient := aws.NewMockClient()
	ctx := context.Background()

	// Setup EC2 client with instances in various states
	ec2Client, err := mockClient.EC2(ctx, aws.AccountConfig{
		AccountID: "123456789012",
		Region:    "us-west-2",
	})
	require.NoError(t, err)
	mockEC2 := ec2Client.(*aws.MockEC2Client)
	mockEC2.Instances = []aws.Instance{
		{InstanceID: "i-running-1", InstanceType: "m5.large", Region: "us-west-2", AccountID: "123456789012", State: "running"},
		{InstanceID: "i-running-2", InstanceType: "m5.xlarge", Region: "us-west-2", AccountID: "123456789012", State: "running"},
		{InstanceID: "i-stopped-1", InstanceType: "c5.2xlarge", Region: "us-west-2", AccountID: "123456789012", State: "stopped"},
		{InstanceID: "i-stopping-1", InstanceType: "t3.medium", Region: "us-west-2", AccountID: "123456789012", State: "stopping"},
	}

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
	ec2Cache := cache.NewEC2Cache()
	m := metrics.NewMetrics(prometheus.NewRegistry(), newTestConfig())

	// Create reconciler
	reconciler := &EC2Reconciler{
		AWSClient: mockClient,
		Config:    cfg,
		Cache:     ec2Cache,
		Metrics:   m,
		Log:       logr.Discard(),
		Regions:   []string{"us-west-2"},
	}

	// Run reconciliation
	result, err := reconciler.Reconcile(ctx, ctrl.Request{})

	// Verify success
	require.NoError(t, err)
	assert.Equal(t, 5*time.Minute, result.RequeueAfter)

	// Verify all instances cached
	allInstances := ec2Cache.GetAllInstances()
	assert.Len(t, allInstances, 4)

	// Verify state filtering works
	runningInstances := ec2Cache.GetRunningInstances()
	assert.Len(t, runningInstances, 2, "should have 2 running instances")
}
