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
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/nextdoor/lumina/internal/cache"
	"github.com/nextdoor/lumina/pkg/aws"
	"github.com/nextdoor/lumina/pkg/config"
	"github.com/nextdoor/lumina/pkg/metrics"
)

// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create

// RISPReconciler reconciles Reserved Instances and Savings Plans data.
// It queries AWS APIs hourly to maintain an up-to-date cache of RI/SP inventory.
type RISPReconciler struct {
	// AWS client for making API calls
	AWSClient aws.Client

	// Configuration with AWS account details
	Config *config.Config

	// Cache for storing RI/SP data
	Cache *cache.RISPCache

	// Metrics for observability
	Metrics *metrics.Metrics

	// Logger
	Log logr.Logger

	// Regions to query for RIs (RIs are regional)
	// If empty, defaults to common regions
	Regions []string

	// ReadyChan is an optional channel that will be closed after the initial
	// reconciliation completes successfully. This allows downstream reconcilers
	// (like SPRatesReconciler) to wait for RISP data to be populated before
	// they start their work.
	ReadyChan chan struct{}
}

// Reconcile performs a single reconciliation cycle.
// This is called by controller-runtime on a timer (hourly).
func (r *RISPReconciler) Reconcile(ctx context.Context, _ ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("reconciler", "risp")
	log.Info("starting RI/SP reconciliation cycle")

	// Track cycle timing
	startTime := time.Now()

	// Determine default regions to query for Reserved Instances.
	// Uses a fallback chain to ensure we always have regions to query:
	//  1. Config.Regions (from config file 'regions' field)
	//  2. r.Regions (from reconciler initialization)
	//  3. config.DefaultRegions (common US regions) as final fallback
	//
	// Individual accounts can override this default by setting their own
	// 'regions' field in the awsAccounts configuration.
	defaultRegions := r.Config.Regions
	if len(defaultRegions) == 0 {
		// Config didn't specify regions, try reconciler default
		defaultRegions = r.Regions
	}
	if len(defaultRegions) == 0 {
		// No regions configured anywhere, use the default fallback regions
		defaultRegions = config.DefaultRegions
	}

	// Query all accounts in parallel
	var wg sync.WaitGroup
	errors := make(chan error, len(r.Config.AWSAccounts)*2) // Buffer for potential errors

	// Query RIs for each account/region
	for _, account := range r.Config.AWSAccounts {
		wg.Add(1)
		go func(acc config.AWSAccount) {
			defer wg.Done()

			// Determine regions to query for this specific account.
			// Account-specific regions (if configured) override the global default.
			// This allows flexibility for accounts that only operate in certain regions.
			//
			// Example: Account A might use all regions, but Account B only uses us-west-2
			regions := acc.Regions
			if len(regions) == 0 {
				// No account-specific override, use global default
				regions = defaultRegions
			}

			if err := r.reconcileReservedInstances(ctx, acc, regions); err != nil {
				log.Error(err, "failed to reconcile RIs",
					"account_id", acc.AccountID,
					"account_name", acc.Name)
				errors <- err
			}
		}(account)
	}

	// Query SPs for each account
	for _, account := range r.Config.AWSAccounts {
		wg.Add(1)
		go func(acc config.AWSAccount) {
			defer wg.Done()
			if err := r.reconcileSavingsPlans(ctx, acc); err != nil {
				log.Error(err, "failed to reconcile SPs",
					"account_id", acc.AccountID,
					"account_name", acc.Name)
				errors <- err
			}
		}(account)
	}

	// Wait for all queries to complete
	wg.Wait()
	close(errors)

	// Check for errors
	errorCount := len(errors)
	if errorCount > 0 {
		log.Info("reconciliation cycle completed with errors",
			"error_count", errorCount,
			"duration_seconds", time.Since(startTime).Seconds())
	} else {
		log.Info("reconciliation cycle completed successfully",
			"duration_seconds", time.Since(startTime).Seconds())
	}

	// Log cache statistics
	stats := r.Cache.GetStats()
	log.Info("cache statistics",
		"reserved_instances", stats.ReservedInstanceCount,
		"savings_plans", stats.SavingsPlanCount,
		"regions", stats.RegionCount,
		"accounts", stats.AccountCount)

	// Update Prometheus metrics with latest RI data
	// This should happen after all accounts/regions have been queried
	// so metrics reflect the complete current state
	allRIs := r.Cache.GetAllReservedInstances()
	r.Metrics.UpdateReservedInstanceMetrics(allRIs)
	log.V(1).Info("updated RI metrics", "metric_count", len(allRIs))

	// Update Prometheus metrics with latest Savings Plans data
	// Phase 3: SP inventory metrics (hourly commitment, remaining hours)
	// Note: SP utilization metrics (current usage, remaining capacity) come in Phase 6
	allSPs := r.Cache.GetAllSavingsPlans()
	r.Metrics.UpdateSavingsPlansInventoryMetrics(allSPs)
	log.V(1).Info("updated SP metrics", "metric_count", len(allSPs))

	// Parse reconciliation interval from config, with default fallback to 1 hour
	// The interval determines how often we refresh RI/SP data
	requeueAfter := 1 * time.Hour // Default
	if r.Config.Reconciliation.RISP != "" {
		if duration, err := time.ParseDuration(r.Config.Reconciliation.RISP); err == nil {
			requeueAfter = duration
		} else {
			log.Error(err, "invalid RISP reconciliation interval, using default",
				"configured_interval", r.Config.Reconciliation.RISP,
				"default", "1h")
		}
	}

	// Log the configured interval (helpful for verifying configuration)
	log.V(1).Info("reconciliation interval configured", "next_run_in", requeueAfter.String())

	// Requeue after configured interval
	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

// reconcileReservedInstances queries RIs for a single account across all regions.
func (r *RISPReconciler) reconcileReservedInstances(
	ctx context.Context,
	account config.AWSAccount,
	regions []string,
) error {
	log := r.Log.WithValues(
		"reconciler", "risp",
		"account_id", account.AccountID,
		"account_name", account.Name,
		"data_type", "reserved_instances",
	)

	// Create AWS client for this account
	accountConfig := aws.AccountConfig{
		AccountID:     account.AccountID,
		Name:          account.Name,
		AssumeRoleARN: account.AssumeRoleARN,
		Region:        r.Config.DefaultRegion,
	}

	ec2Client, err := r.AWSClient.EC2(ctx, accountConfig)
	if err != nil {
		return fmt.Errorf("failed to create EC2 client: %w", err)
	}

	// Query each region
	for _, region := range regions {
		startTime := time.Now()

		// Query RIs in this region
		ris, err := ec2Client.DescribeReservedInstances(ctx, []string{region})
		duration := time.Since(startTime)

		if err != nil {
			// Record failure in metrics
			r.Metrics.DataLastSuccess.WithLabelValues(
				account.AccountID,
				account.Name,
				region,
				"reserved_instances",
			).Set(0)

			log.Error(err, "failed to describe reserved instances", "region", region)
			return fmt.Errorf("failed to describe RIs in %s: %w", region, err)
		}

		// Update cache with new data
		r.Cache.UpdateReservedInstances(region, account.AccountID, ris)

		// Log details about each RI for observability
		for _, ri := range ris {
			log.V(1).Info("reserved instance details",
				"region", region,
				"ri_id", ri.ReservedInstanceID,
				"instance_type", ri.InstanceType,
				"availability_zone", ri.AvailabilityZone,
				"instance_count", ri.InstanceCount,
				"state", ri.State)
		}

		// Record success in metrics
		r.Metrics.DataLastSuccess.WithLabelValues(
			account.AccountID,
			account.Name,
			region,
			"reserved_instances",
		).Set(1)

		// Mark that reserved_instances data was updated for this account+region
		r.Metrics.MarkDataUpdated(account.AccountID, account.Name, region, "reserved_instances")

		log.V(1).Info("updated reserved instances",
			"region", region,
			"count", len(ris),
			"duration_seconds", duration.Seconds())
	}

	return nil
}

// reconcileSavingsPlans queries SPs for a single account (organization-wide).
// If testData is configured, uses mock data instead of making AWS API calls.
func (r *RISPReconciler) reconcileSavingsPlans(
	ctx context.Context,
	account config.AWSAccount,
) error {
	log := r.Log.WithValues(
		"reconciler", "risp",
		"account_id", account.AccountID,
		"account_name", account.Name,
		"data_type", "savings_plans",
	)

	startTime := time.Now()
	var sps []aws.SavingsPlan

	// Check if we have test data for this account
	if r.Config.TestData != nil && r.Config.TestData.SavingsPlans != nil {
		testSPs, hasTestData := r.Config.TestData.SavingsPlans[account.AccountID]
		if hasTestData {
			log.Info("using test data for savings plans", "count", len(testSPs))
			sps = convertTestSavingsPlans(testSPs, account.AccountID, account.Name)
		} else {
			// No test data for this account, return empty list
			log.Info("no test data configured for this account, using empty list")
			sps = []aws.SavingsPlan{}
		}
	} else {
		// No test data, query AWS API
		accountConfig := aws.AccountConfig{
			AccountID:     account.AccountID,
			Name:          account.Name,
			AssumeRoleARN: account.AssumeRoleARN,
			Region:        r.Config.DefaultRegion,
		}

		spClient, err := r.AWSClient.SavingsPlans(ctx, accountConfig)
		if err != nil {
			return fmt.Errorf("failed to create Savings Plans client: %w", err)
		}

		// Query SPs (organization-wide, not region-specific)
		sps, err = spClient.DescribeSavingsPlans(ctx)
		if err != nil {
			// Record failure in metrics
			r.Metrics.DataLastSuccess.WithLabelValues(
				account.AccountID,
				account.Name,
				"", // SPs are not regional
				"savings_plans",
			).Set(0)

			log.Error(err, "failed to describe savings plans")
			return fmt.Errorf("failed to describe SPs: %w", err)
		}
	}

	duration := time.Since(startTime)

	// Update cache with new data
	r.Cache.UpdateSavingsPlans(account.AccountID, sps)

	// Record success in metrics
	r.Metrics.DataLastSuccess.WithLabelValues(
		account.AccountID,
		account.Name,
		"", // SPs are not regional
		"savings_plans",
	).Set(1)

	// Mark that savings_plans data was updated for this account (SPs are not regional)
	r.Metrics.MarkDataUpdated(account.AccountID, account.Name, "", "savings_plans")

	log.V(1).Info("updated savings plans",
		"count", len(sps),
		"duration_seconds", duration.Seconds())

	// Log details about each SP for observability
	for _, sp := range sps {
		log.V(1).Info("savings plan details",
			"sp_arn", sp.SavingsPlanARN,
			"sp_type", sp.SavingsPlanType,
			"commitment", sp.Commitment,
			"region", sp.Region,
			"instance_family", sp.InstanceFamily,
			"state", sp.State)
	}

	return nil
}

// convertTestSavingsPlans converts test configuration SPs to aws.SavingsPlan format.
func convertTestSavingsPlans(
	testSPs []config.TestSavingsPlan,
	accountID, accountName string,
) []aws.SavingsPlan {
	result := make([]aws.SavingsPlan, 0, len(testSPs))

	for _, testSP := range testSPs {
		// Parse Start and End times
		start, _ := time.Parse(time.RFC3339, testSP.Start)
		end, _ := time.Parse(time.RFC3339, testSP.End)

		// Extract SavingsPlanID from ARN
		// ARN format: arn:aws:savingsplans::ACCOUNT:savingsplan/SP_ID
		spID := ""
		parts := strings.Split(testSP.SavingsPlanARN, "/")
		if len(parts) == 2 {
			spID = parts[1]
		}

		sp := aws.SavingsPlan{
			SavingsPlanARN:  testSP.SavingsPlanARN,
			SavingsPlanID:   spID,
			SavingsPlanType: testSP.SavingsPlanType,
			State:           testSP.State,
			Commitment:      testSP.Commitment,
			Region:          testSP.Region,
			InstanceFamily:  testSP.InstanceFamily,
			Start:           start,
			End:             end,
			AccountID:       accountID,
			AccountName:     accountName,
		}
		result = append(result, sp)
	}

	return result
}

// Run runs the reconciler as a goroutine with timer-based reconciliation.
//
// Uses a simple time.Ticker for periodic reconciliation instead of controller-runtime's
// requeue mechanism. Continues running even if individual cycles fail, and stops
// gracefully when context is cancelled.
func (r *RISPReconciler) Run(ctx context.Context) error {
	log := r.Log
	log.Info("starting RISP reconciler")

	// Run immediately on startup
	log.Info("running initial reconciliation")
	if _, err := r.Reconcile(ctx, ctrl.Request{}); err != nil {
		log.Error(err, "initial reconciliation failed")
		// Don't exit - continue with periodic reconciliation
	}

	// Signal that initial reconciliation is complete (if channel was provided)
	// This allows downstream reconcilers (e.g., SPRatesReconciler) to wait for
	// RISP data to be populated before starting their work
	if r.ReadyChan != nil {
		close(r.ReadyChan)
		log.V(1).Info("signaled that RISP cache is ready for dependent reconcilers")
	}

	// Parse reconciliation interval from config, with default fallback to 1 hour
	interval := 1 * time.Hour // Default
	if r.Config.Reconciliation.RISP != "" {
		if duration, err := time.ParseDuration(r.Config.Reconciliation.RISP); err == nil {
			interval = duration
		} else {
			log.Error(err, "invalid RISP reconciliation interval, using default",
				"configured_interval", r.Config.Reconciliation.RISP,
				"default", "1h")
		}
	}

	// Setup ticker for RISP data
	log.Info("configured reconciliation interval", "interval", interval.String())
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info("shutting down RISP reconciler")
			return ctx.Err()
		case <-ticker.C:
			log.Info("running scheduled reconciliation")
			if _, err := r.Reconcile(ctx, ctrl.Request{}); err != nil {
				log.Error(err, "scheduled reconciliation failed")
				// Don't exit - continue with next cycle
			}
		}
	}
}
