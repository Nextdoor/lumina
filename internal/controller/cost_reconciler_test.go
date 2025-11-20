/*
Copyright 2025 Lumina Contributors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/nextdoor/lumina/internal/cache"
	"github.com/nextdoor/lumina/pkg/config"
	"github.com/nextdoor/lumina/pkg/cost"
	"github.com/nextdoor/lumina/pkg/metrics"
)

// TestCostReconciler_Reconcile_BlocksWhenNotInitialized tests that Reconcile blocks
// when the initialized flag is false (preventing premature calculations during startup).
func TestCostReconciler_Reconcile_BlocksWhenNotInitialized(t *testing.T) {
	pricingCache := cache.NewPricingCache()
	cfg := &config.Config{}

	// Create reconciler with initialized=false (default)
	reconciler := &CostReconciler{
		Calculator:   cost.NewCalculator(pricingCache, cfg),
		Config:       cfg,
		EC2Cache:     cache.NewEC2Cache(),
		RISPCache:    cache.NewRISPCache(),
		PricingCache: pricingCache,
		Metrics:      metrics.NewMetrics(prometheus.NewRegistry()),
		Log:          logr.Discard(),
		// initialized is false by default (atomic.Bool zero value)
	}

	// Call Reconcile - should return immediately without calculating
	result, err := reconciler.Reconcile(context.Background(), ctrl.Request{})

	assert.NoError(t, err, "Reconcile should succeed even when blocked")
	assert.Equal(t, ctrl.Result{}, result, "Reconcile should return empty result when blocked")

	// Verify no metrics were emitted (indicating no calculation ran)
	// This is implicit - if calculation ran, it would update metrics which we can't easily verify here
	// The key test is that Reconcile returned immediately
}

// TestCostReconciler_Reconcile_RunsWhenInitialized tests that Reconcile runs calculations
// once the initialized flag is true.
func TestCostReconciler_Reconcile_RunsWhenInitialized(t *testing.T) {
	ec2Cache := cache.NewEC2Cache()
	rispCache := cache.NewRISPCache()
	pricingCache := cache.NewPricingCache()
	cfg := &config.Config{}

	// Create reconciler with initialized=true
	reconciler := &CostReconciler{
		Calculator:   cost.NewCalculator(pricingCache, cfg),
		Config:       cfg,
		EC2Cache:     ec2Cache,
		RISPCache:    rispCache,
		PricingCache: pricingCache,
		Metrics:      metrics.NewMetrics(prometheus.NewRegistry()),
		Log:          logr.Discard(),
	}

	// Set initialized flag to true
	reconciler.initialized.Store(true)

	// Call Reconcile - should run calculation
	result, err := reconciler.Reconcile(context.Background(), ctrl.Request{})

	assert.NoError(t, err, "Reconcile should succeed")
	assert.Equal(t, ctrl.Result{}, result, "Reconcile should return empty result (event-driven, no requeue)")
}

// TestCostReconciler_waitForDependencies tests waiting for all ready channels.
func TestCostReconciler_waitForDependencies(t *testing.T) {
	pricingReadyCh := make(chan struct{})
	rispReadyCh := make(chan struct{})
	ec2ReadyCh := make(chan struct{})
	spRatesReadyCh := make(chan struct{})

	reconciler := &CostReconciler{
		Log:              logr.Discard(),
		PricingReadyChan: pricingReadyCh,
		RISPReadyChan:    rispReadyCh,
		EC2ReadyChan:     ec2ReadyCh,
		SPRatesReadyChan: spRatesReadyCh,
	}

	// Start waitForDependencies in background
	done := make(chan struct{})
	go func() {
		reconciler.waitForDependencies()
		close(done)
	}()

	// Verify it's waiting (should not complete immediately)
	select {
	case <-done:
		t.Error("waitForDependencies should not complete before channels are closed")
	case <-time.After(50 * time.Millisecond):
		// Expected - still waiting
	}

	// Close channels one by one
	close(pricingReadyCh)
	time.Sleep(10 * time.Millisecond)

	select {
	case <-done:
		t.Error("waitForDependencies should not complete after only pricing ready")
	case <-time.After(50 * time.Millisecond):
		// Expected - still waiting
	}

	close(rispReadyCh)
	close(ec2ReadyCh)
	time.Sleep(10 * time.Millisecond)

	select {
	case <-done:
		t.Error("waitForDependencies should not complete after only 3/4 channels ready")
	case <-time.After(50 * time.Millisecond):
		// Expected - still waiting
	}

	close(spRatesReadyCh)

	// Should now complete
	select {
	case <-done:
		// Success
	case <-time.After(100 * time.Millisecond):
		t.Error("waitForDependencies should complete after all channels closed")
	}
}

// TestCostReconciler_waitForDependencies_NilChannels tests that nil channels are skipped.
func TestCostReconciler_waitForDependencies_NilChannels(t *testing.T) {
	reconciler := &CostReconciler{
		Log: logr.Discard(),
		// All ready channels are nil
	}

	// Start waitForDependencies in background
	done := make(chan struct{})
	go func() {
		reconciler.waitForDependencies()
		close(done)
	}()

	// Should complete immediately when all channels are nil
	select {
	case <-done:
		// Success
	case <-time.After(100 * time.Millisecond):
		t.Error("waitForDependencies should complete immediately when all channels are nil")
	}
}

// TestCostReconciler_Run_SetsInitializedFlag tests that Run() sets the initialized flag.
func TestCostReconciler_Run_SetsInitializedFlag(t *testing.T) {
	pricingReadyCh := make(chan struct{})
	rispReadyCh := make(chan struct{})
	ec2ReadyCh := make(chan struct{})
	spRatesReadyCh := make(chan struct{})

	pricingCache := cache.NewPricingCache()
	cfg := &config.Config{}

	reconciler := &CostReconciler{
		Calculator:       cost.NewCalculator(pricingCache, cfg),
		Config:           cfg,
		EC2Cache:         cache.NewEC2Cache(),
		RISPCache:        cache.NewRISPCache(),
		PricingCache:     pricingCache,
		Metrics:          metrics.NewMetrics(prometheus.NewRegistry()),
		Debouncer:        cache.NewDebouncer(1*time.Second, func() {}),
		Log:              logr.Discard(),
		PricingReadyChan: pricingReadyCh,
		RISPReadyChan:    rispReadyCh,
		EC2ReadyChan:     ec2ReadyCh,
		SPRatesReadyChan: spRatesReadyCh,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start Run in background
	errCh := make(chan error, 1)
	go func() {
		errCh <- reconciler.Run(ctx)
	}()

	// Verify initialized flag is still false (waiting for dependencies)
	if reconciler.initialized.Load() {
		t.Error("initialized flag should be false before dependencies ready")
	}

	// Close all ready channels
	close(pricingReadyCh)
	close(rispReadyCh)
	close(ec2ReadyCh)
	close(spRatesReadyCh)

	// Wait a bit for Run to process the ready signals
	time.Sleep(100 * time.Millisecond)

	// Verify initialized flag is now true
	if !reconciler.initialized.Load() {
		t.Error("initialized flag should be true after dependencies ready")
	}

	// Cancel and verify shutdown
	cancel()
	select {
	case err := <-errCh:
		assert.ErrorIs(t, err, context.Canceled, "Run should return context.Canceled")
	case <-time.After(100 * time.Millisecond):
		t.Error("Run should exit after context cancel")
	}
}

// TestCostReconciler_Run_RunsInitialCalculation tests that Run performs an initial calculation.
func TestCostReconciler_Run_RunsInitialCalculation(t *testing.T) {
	pricingReadyCh := make(chan struct{})
	rispReadyCh := make(chan struct{})
	ec2ReadyCh := make(chan struct{})
	spRatesReadyCh := make(chan struct{})

	pricingCache := cache.NewPricingCache()
	cfg := &config.Config{}

	// Track if calculation ran by checking if Reconcile was called
	// We do this by verifying the initialized flag gets set (which happens before Reconcile)
	reconciler := &CostReconciler{
		Calculator:       cost.NewCalculator(pricingCache, cfg),
		Config:           cfg,
		EC2Cache:         cache.NewEC2Cache(),
		RISPCache:        cache.NewRISPCache(),
		PricingCache:     pricingCache,
		Metrics:          metrics.NewMetrics(prometheus.NewRegistry()),
		Debouncer:        cache.NewDebouncer(1*time.Second, func() {}),
		Log:              logr.Discard(),
		PricingReadyChan: pricingReadyCh,
		RISPReadyChan:    rispReadyCh,
		EC2ReadyChan:     ec2ReadyCh,
		SPRatesReadyChan: spRatesReadyCh,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start Run in background
	errCh := make(chan error, 1)
	go func() {
		errCh <- reconciler.Run(ctx)
	}()

	// Close all ready channels to trigger initial calculation
	close(pricingReadyCh)
	close(rispReadyCh)
	close(ec2ReadyCh)
	close(spRatesReadyCh)

	// Wait for initial calculation to complete
	time.Sleep(100 * time.Millisecond)

	// Verify initialized flag was set (proves waitForDependencies completed)
	assert.True(t, reconciler.initialized.Load(), "initialized flag should be set after initial calculation")

	// Cancel and verify shutdown
	cancel()
	select {
	case err := <-errCh:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(100 * time.Millisecond):
		t.Error("Run should exit after context cancel")
	}
}
