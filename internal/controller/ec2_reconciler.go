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
	"sync"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/nextdoor/lumina/internal/cache"
	"github.com/nextdoor/lumina/pkg/aws"
	"github.com/nextdoor/lumina/pkg/config"
	"github.com/nextdoor/lumina/pkg/metrics"
)

// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch

// EC2Reconciler reconciles EC2 instance data across all AWS accounts.
// It queries AWS APIs at a configurable interval to maintain fresh instance inventory.
//
// EC2 instances change frequently due to autoscaling, spot interruptions, and manual
// changes. The default 5-minute refresh cycle provides timely data for cost calculations
// while respecting AWS API rate limits (DescribeInstances allows 200 requests/second,
// well above our typical 0.2 requests/second usage).
//
// The reconciliation interval can be configured via config.Reconciliation.EC2.
type EC2Reconciler struct {
	// AWS client for making API calls
	AWSClient aws.Client

	// Configuration with AWS account details
	Config *config.Config

	// Cache for storing EC2 instance data
	Cache *cache.EC2Cache

	// Metrics for observability
	Metrics *metrics.Metrics

	// Logger
	Log logr.Logger

	// Regions to query (discovered or configured)
	// If empty, defaults to common regions
	Regions []string
}

// Reconcile performs a single reconciliation cycle.
// This is called by controller-runtime on a timer at the configured interval.
//
// The reconciler queries all account+region combinations in parallel for performance.
// Individual failures are logged but don't stop the entire reconciliation cycle,
// allowing successful queries to update the cache even when some accounts/regions fail.
func (r *EC2Reconciler) Reconcile(ctx context.Context, _ ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("reconciler", "ec2")
	log.Info("starting EC2 reconciliation cycle")

	// Track cycle timing
	startTime := time.Now()

	// Determine default regions to query for EC2 instances.
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

	// Query all account+region combinations in parallel
	// This is safe because each account+region pair is independent and the
	// cache.SetInstances() method is thread-safe
	var wg sync.WaitGroup
	errors := make(chan error, len(r.Config.AWSAccounts)*len(defaultRegions))

	for _, account := range r.Config.AWSAccounts {
		// Determine regions to query for this specific account.
		// Account-specific regions (if configured) override the global default.
		// This allows flexibility for accounts that only operate in certain regions.
		//
		// Example: Account A might use all regions, but Account B only uses us-west-2
		regions := account.Regions
		if len(regions) == 0 {
			// No account-specific override, use global default
			regions = defaultRegions
		}

		for _, region := range regions {
			wg.Add(1)
			go func(acc config.AWSAccount, reg string) {
				defer wg.Done()
				if err := r.reconcileAccountRegion(ctx, acc, reg); err != nil {
					log.Error(err, "failed to reconcile EC2 instances",
						"account_id", acc.AccountID,
						"account_name", acc.Name,
						"region", reg)
					errors <- err
				}
			}(account, region)
		}
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
	allInstances := r.Cache.GetAllInstances()
	runningInstances := r.Cache.GetRunningInstances()
	log.Info("cache statistics",
		"total_instances", len(allInstances),
		"running_instances", len(runningInstances))

	// Update Prometheus metrics with current cache state
	// This exposes EC2 instance inventory for monitoring and alerting
	r.Metrics.UpdateEC2InstanceMetrics(runningInstances)
	log.V(1).Info("updated EC2 instance metrics",
		"instance_count", len(runningInstances))

	// Parse reconciliation interval from config, with default fallback to 5 minutes
	// The interval determines how often we refresh EC2 instance inventory data
	requeueAfter := 5 * time.Minute // Default
	if r.Config.Reconciliation.EC2 != "" {
		if duration, err := time.ParseDuration(r.Config.Reconciliation.EC2); err == nil {
			requeueAfter = duration
		} else {
			log.Error(err, "invalid EC2 reconciliation interval, using default",
				"configured_interval", r.Config.Reconciliation.EC2,
				"default", "5m")
		}
	}

	// Log the configured interval (helpful for verifying configuration)
	log.V(1).Info("reconciliation interval configured", "next_run_in", requeueAfter.String())

	// Requeue after configured interval
	// This creates the periodic reconciliation loop without requiring external triggers
	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

// reconcileAccountRegion queries EC2 instances for a single account+region combination.
// This method is called concurrently for each account+region pair during reconciliation.
func (r *EC2Reconciler) reconcileAccountRegion(
	ctx context.Context,
	account config.AWSAccount,
	region string,
) error {
	log := r.Log.WithValues(
		"reconciler", "ec2",
		"account_id", account.AccountID,
		"account_name", account.Name,
		"region", region,
	)

	// Create AWS client for this account
	accountConfig := aws.AccountConfig{
		AccountID:     account.AccountID,
		Name:          account.Name,
		AssumeRoleARN: account.AssumeRoleARN,
		Region:        region,
	}

	ec2Client, err := r.AWSClient.EC2(ctx, accountConfig)
	if err != nil {
		// Record failure in metrics
		r.Metrics.DataLastSuccess.WithLabelValues(
			account.AccountID,
			region,
			"ec2_instances",
		).Set(0)
		return fmt.Errorf("failed to create EC2 client: %w", err)
	}

	startTime := time.Now()

	// Query EC2 instances in this region
	// Pass region as a slice (API accepts multiple regions)
	instances, err := ec2Client.DescribeInstances(ctx, []string{region})
	duration := time.Since(startTime)

	if err != nil {
		// Record failure in metrics
		r.Metrics.DataLastSuccess.WithLabelValues(
			account.AccountID,
			region,
			"ec2_instances",
		).Set(0)

		log.Error(err, "failed to describe instances")
		return fmt.Errorf("failed to describe instances in %s: %w", region, err)
	}

	// Update cache with new data
	// This atomically replaces all instances for this account+region combination
	r.Cache.SetInstances(account.AccountID, region, instances)

	// Record success in metrics
	r.Metrics.DataLastSuccess.WithLabelValues(
		account.AccountID,
		region,
		"ec2_instances",
	).Set(1)

	// Record the Unix timestamp of this successful collection
	r.Metrics.DataFreshness.WithLabelValues(
		account.AccountID,
		region,
		"ec2_instances",
	).Set(float64(time.Now().Unix()))

	// Log summary with instance count breakdown by state
	stateCount := make(map[string]int)
	for _, inst := range instances {
		stateCount[inst.State]++
	}

	log.V(1).Info("updated EC2 instances",
		"total_count", len(instances),
		"state_breakdown", stateCount,
		"duration_seconds", duration.Seconds())

	return nil
}

// Run runs the reconciler as a goroutine with timer-based reconciliation.
//
// Uses a simple time.Ticker for periodic reconciliation instead of controller-runtime's
// requeue mechanism. Continues running even if individual cycles fail, and stops
// gracefully when context is cancelled.
func (r *EC2Reconciler) Run(ctx context.Context) error {
	log := r.Log
	log.Info("starting EC2 reconciler")

	// Run immediately on startup
	log.Info("running initial reconciliation")
	if _, err := r.Reconcile(ctx, ctrl.Request{}); err != nil {
		log.Error(err, "initial reconciliation failed")
		// Don't exit - continue with periodic reconciliation
	}

	// Parse reconciliation interval from config, with default fallback to 5 minutes
	interval := 5 * time.Minute // Default
	if r.Config.Reconciliation.EC2 != "" {
		if duration, err := time.ParseDuration(r.Config.Reconciliation.EC2); err == nil {
			interval = duration
		} else {
			log.Error(err, "invalid EC2 reconciliation interval, using default",
				"configured_interval", r.Config.Reconciliation.EC2,
				"default", "5m")
		}
	}

	// Setup ticker for EC2 data
	log.Info("configured reconciliation interval", "interval", interval.String())
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info("shutting down EC2 reconciler")
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

// SetupWithManager sets up the reconciler with the Manager.
// coverage:ignore - controller-runtime boilerplate, tested via E2E
func (r *EC2Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	// This is a timer-based controller that requeues itself every 5 minutes using RequeueAfter.
	// We watch Nodes (which represent EC2 instances in Kubernetes) to trigger initial reconciliation,
	// but we ignore most Node events - we only trigger on the FIRST Node we see to start the cycle.
	// Once triggered, the Reconcile() method returns RequeueAfter which causes controller-runtime
	// to automatically schedule subsequent reconciliations every 5 minutes.
	//
	// Note: While we watch Nodes to establish the watch, the actual EC2 data collection queries
	// AWS APIs directly and is independent of Kubernetes Node objects. This watch is purely to
	// trigger the initial reconciliation - all subsequent runs are timer-based via RequeueAfter.
	triggered := false
	err := ctrl.NewControllerManagedBy(mgr).
		Named("ec2").
		For(&corev1.Node{}).
		WithEventFilter(predicate.NewPredicateFuncs(func(obj client.Object) bool {
			// Only trigger reconciliation once on the first Node we see
			// After that, RequeueAfter handles all subsequent reconciliations
			if !triggered {
				triggered = true
				return true
			}
			return false
		})).
		Complete(r)
	if err != nil {
		return err
	}

	return nil
}
