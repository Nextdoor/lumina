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
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/nextdoor/lumina/internal/cache"
	"github.com/nextdoor/lumina/pkg/config"
	"github.com/nextdoor/lumina/pkg/cost"
	"github.com/nextdoor/lumina/pkg/metrics"
)

// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch

// CostReconciler calculates per-instance costs and Savings Plans utilization.
// It uses an event-driven architecture: cost calculations trigger automatically when
// any data source (EC2, RISP, or Pricing caches) updates. A debouncer accumulates
// rapid updates and ensures calculations run once after a period of quiet.
//
// The reconciler is stateless and operates on a rate-based model ($/hour) rather than
// cumulative tracking. It calculates costs based on the current set of running instances,
// assuming they continue running for the remainder of the hour.
//
// This design means the controller can be safely restarted at any time without losing
// accuracy, and provides immediate visibility into current costs and SP utilization.
//
// Event-driven design benefits:
// - Costs update within ~1 second of any data change (EC2, RISP, Pricing)
// - No wasted calculations when data hasn't changed
// - Scales efficiently as we add more data sources (spot pricing, etc.)
type CostReconciler struct {
	// Calculator performs the cost calculation algorithm
	Calculator *cost.Calculator

	// Config contains general configuration (reconciliation intervals no longer used for cost)
	Config *config.Config

	// EC2Cache provides EC2 instance inventory data
	EC2Cache *cache.EC2Cache

	// RISPCache provides Reserved Instance and Savings Plans data
	RISPCache *cache.RISPCache

	// PricingCache provides on-demand and spot pricing data
	PricingCache *cache.PricingCache

	// Metrics for emitting cost and utilization metrics
	Metrics *metrics.Metrics

	// Debouncer accumulates rapid cache updates and triggers recalculation
	// after a period of quiet (default: 1 second)
	Debouncer *cache.Debouncer

	// Logger
	Log logr.Logger
}

// Reconcile performs a single cost calculation cycle.
// In the event-driven architecture, this is called by the debouncer when any
// cache updates (EC2, RISP, or Pricing). It can also be called manually for
// testing or initial calculation.
//
// The reconciler gathers data from all caches, runs the cost calculation algorithm,
// and updates Prometheus metrics with the results.
func (r *CostReconciler) Reconcile(ctx context.Context, _ ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("reconciler", "cost")
	log.Info("starting cost calculation cycle")

	// Track cycle timing
	startTime := time.Now()

	// Gather all data needed for cost calculation
	instances := r.EC2Cache.GetRunningInstances()
	ris := r.RISPCache.GetAllReservedInstances()
	sps := r.RISPCache.GetAllSavingsPlans()

	// Build instance keys for pricing lookup
	instanceKeys := make([]cache.InstanceKey, 0, len(instances))
	for _, inst := range instances {
		instanceKeys = append(instanceKeys, cache.InstanceKey{
			InstanceType: inst.InstanceType,
			Region:       inst.Region,
		})
	}

	// Get on-demand pricing data for running instances
	// The pricing cache returns a map keyed by "instance_type:region"
	// Default to Linux OS pricing (most common). Future enhancement: detect actual OS per instance.
	onDemandPrices := r.PricingCache.GetOnDemandPricesForInstances(instanceKeys, "Linux")

	// Spot pricing will be implemented in Phase 8
	// For now, use empty map (spot instances will fall back to on-demand estimates)
	spotPrices := make(map[string]float64)

	log.V(1).Info("gathered data for cost calculation",
		"instances", len(instances),
		"reserved_instances", len(ris),
		"savings_plans", len(sps),
		"on_demand_prices", len(onDemandPrices))

	// Build calculation input
	input := cost.CalculationInput{
		Instances:         instances,
		ReservedInstances: ris,
		SavingsPlans:      sps,
		OnDemandPrices:    onDemandPrices,
		SpotPrices:        spotPrices,
	}

	// Run cost calculation algorithm
	result := r.Calculator.Calculate(input)

	log.Info("cost calculation completed",
		"duration_seconds", time.Since(startTime).Seconds(),
		"total_estimated_cost", result.TotalEstimatedCost,
		"total_shelf_price", result.TotalShelfPrice,
		"total_savings", result.TotalSavings,
		"instance_costs", len(result.InstanceCosts),
		"sp_utilization", len(result.SavingsPlanUtilization))

	// Update Prometheus metrics with cost calculation results
	// This emits ec2_instance_hourly_cost and savings_plan_* utilization metrics
	r.Metrics.UpdateInstanceCostMetrics(result)
	log.V(1).Info("updated cost metrics")

	// Event-driven reconciliation: no requeue needed
	// The debouncer will trigger the next calculation when caches update
	return ctrl.Result{}, nil
}

// Run runs the reconciler as a goroutine with event-driven reconciliation.
//
// Runs an initial calculation on startup, then waits for the debouncer to trigger
// subsequent calculations when cache data updates (EC2, RISP, or Pricing).
func (r *CostReconciler) Run(ctx context.Context) error {
	log := r.Log
	log.Info("starting cost reconciler")

	// Run immediately on startup
	log.Info("running initial cost calculation")
	if _, err := r.Reconcile(ctx, ctrl.Request{}); err != nil {
		log.Error(err, "initial cost calculation failed")
		// Don't exit - future cache updates will trigger recalculation
	}

	log.Info("cost reconciler ready, waiting for cache updates to trigger recalculation")

	// Wait for shutdown signal
	// The debouncer will automatically trigger Reconcile() when caches update
	<-ctx.Done()
	log.Info("shutting down cost reconciler")

	// Stop the debouncer to prevent callbacks during shutdown
	if r.Debouncer != nil {
		r.Debouncer.Stop()
	}

	return ctx.Err()
}

// SetupWithManager sets up the reconciler with the Manager.
// coverage:ignore - controller-runtime boilerplate, tested via E2E
func (r *CostReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// This is an event-driven controller that triggers via cache update notifications.
	// We watch Nodes to trigger initial reconciliation, but only trigger on the FIRST Node.
	// After the initial calculation, subsequent calculations are triggered automatically
	// when cache data updates (via the debouncer).
	triggered := false
	err := ctrl.NewControllerManagedBy(mgr).
		Named("cost").
		For(&corev1.Node{}).
		WithEventFilter(predicate.NewPredicateFuncs(func(obj client.Object) bool {
			// Only trigger on the first Node we see (for initial calculation)
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
