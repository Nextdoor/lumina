// Copyright 2025 Nextdoor, Inc.
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
	"errors"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/nextdoor/lumina/internal/cache"
	"github.com/nextdoor/lumina/pkg/aws"
	"github.com/nextdoor/lumina/pkg/config"
	"github.com/nextdoor/lumina/pkg/metrics"
)

func TestSPUtilizationReconcilerReconcile(t *testing.T) {
	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	client := aws.NewMockClient()
	client.CostExplorerClients["111111111111"] = &aws.MockCostExplorerClient{
		Observation: aws.SavingsPlansUtilizationObservation{
			UnusedCommitment: 2.5,
			PeriodEnd:        now.Add(-time.Hour),
		},
	}
	utilizationCache := cache.NewSPUtilizationCache()
	luminaMetrics := metrics.NewMetrics(prometheus.NewRegistry(), &config.Config{})
	defer luminaMetrics.Stop()
	reconciler := &SPUtilizationReconciler{
		AWSClient: client,
		Config: &config.Config{
			DefaultRegion: "us-east-1",
			AWSAccounts:   []config.AWSAccount{{AccountID: "111111111111", Name: "example"}},
		},
		Cache:   utilizationCache,
		Log:     logr.Discard(),
		Metrics: luminaMetrics,
		now:     func() time.Time { return now },
	}

	result, err := reconciler.Reconcile(context.Background(), ctrl.Request{})
	require.NoError(t, err)
	assert.Equal(t, time.Hour, result.RequeueAfter)
	observation, ok := utilizationCache.GetFresh(now, 2*time.Hour)
	require.True(t, ok)
	assert.Equal(t, 2.5, observation.UnusedCommitment)
	assert.Equal(t, 1, client.CostExplorerClients["111111111111"].CallCount)
	assert.Equal(t, 2.5, testutil.ToFloat64(
		luminaMetrics.SavingsPlanObservedUnusedCommitment.WithLabelValues("111111111111", "example"),
	))
	assert.Equal(t, float64(now.Add(-time.Hour).Unix()), testutil.ToFloat64(
		luminaMetrics.SavingsPlanUtilizationObservationTimestamp.WithLabelValues("111111111111", "example"),
	))
	assert.Equal(t, 1.0, testutil.ToFloat64(luminaMetrics.DataLastSuccess.WithLabelValues(
		"111111111111", "example", "", "savings_plan_utilization",
	)))
}

func TestSPUtilizationReconcilerUsesConfiguredTestData(t *testing.T) {
	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	unused := 0.0
	client := aws.NewMockClient()
	client.CostExplorerError = errors.New("Cost Explorer should not be called")
	utilizationCache := cache.NewSPUtilizationCache()
	reconciler := &SPUtilizationReconciler{
		AWSClient: client,
		Config: &config.Config{
			AWSAccounts: []config.AWSAccount{{AccountID: "111111111111", Name: "example"}},
			TestData:    &config.TestData{ComputeSavingsPlanUnusedCommitment: &unused},
		},
		Cache: utilizationCache,
		Log:   logr.Discard(),
		now:   func() time.Time { return now },
	}

	_, err := reconciler.Reconcile(context.Background(), ctrl.Request{})
	require.NoError(t, err)
	observation, ok := utilizationCache.GetFresh(now, time.Minute)
	require.True(t, ok)
	assert.Zero(t, observation.UnusedCommitment)
}

func TestSPUtilizationReconcilerReconcileErrors(t *testing.T) {
	tests := []struct {
		name        string
		clientError error
		apiError    error
	}{
		{name: "client creation", clientError: errors.New("credentials unavailable")},
		{name: "API call", apiError: errors.New("Cost Explorer unavailable")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := aws.NewMockClient()
			client.CostExplorerError = tt.clientError
			client.CostExplorerClients["111111111111"] = &aws.MockCostExplorerClient{Err: tt.apiError}
			luminaMetrics := metrics.NewMetrics(prometheus.NewRegistry(), &config.Config{})
			defer luminaMetrics.Stop()
			reconciler := &SPUtilizationReconciler{
				AWSClient: client,
				Config: &config.Config{
					DefaultRegion: "us-east-1",
					AWSAccounts:   []config.AWSAccount{{AccountID: "111111111111", Name: "example"}},
				},
				Cache:   cache.NewSPUtilizationCache(),
				Log:     logr.Discard(),
				Metrics: luminaMetrics,
			}

			result, err := reconciler.Reconcile(context.Background(), ctrl.Request{})
			require.Error(t, err)
			assert.Equal(t, time.Hour, result.RequeueAfter)
			assert.Equal(t, 0.0, testutil.ToFloat64(luminaMetrics.DataLastSuccess.WithLabelValues(
				"111111111111", "example", "", "savings_plan_utilization",
			)))
		})
	}
}

func TestSPUtilizationReconcilerRunSignalsReadyAfterInitialFailure(t *testing.T) {
	client := aws.NewMockClient()
	client.CostExplorerError = errors.New("unavailable")
	ready := make(chan struct{})
	reconciler := &SPUtilizationReconciler{
		AWSClient: client,
		Config: &config.Config{
			DefaultRegion: "us-east-1",
			AWSAccounts:   []config.AWSAccount{{AccountID: "111111111111", Name: "example"}},
		},
		Cache:     cache.NewSPUtilizationCache(),
		Log:       logr.Discard(),
		ReadyChan: ready,
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- reconciler.Run(ctx) }()

	select {
	case <-ready:
	case <-time.After(time.Second):
		t.Fatal("ready channel was not closed after initial attempt")
	}
	cancel()
	require.ErrorIs(t, <-errCh, context.Canceled)
}
