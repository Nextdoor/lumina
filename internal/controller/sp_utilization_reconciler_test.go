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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/nextdoor/lumina/internal/cache"
	"github.com/nextdoor/lumina/pkg/aws"
	"github.com/nextdoor/lumina/pkg/config"
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
	reconciler := &SPUtilizationReconciler{
		AWSClient: client,
		Config: &config.Config{
			DefaultRegion: "us-east-1",
			AWSAccounts:   []config.AWSAccount{{AccountID: "111111111111", Name: "example"}},
		},
		Cache: utilizationCache,
		Log:   logr.Discard(),
		now:   func() time.Time { return now },
	}

	result, err := reconciler.Reconcile(context.Background(), ctrl.Request{})
	require.NoError(t, err)
	assert.Equal(t, time.Hour, result.RequeueAfter)
	observation, ok := utilizationCache.GetFresh(now, 2*time.Hour)
	require.True(t, ok)
	assert.Equal(t, 2.5, observation.UnusedCommitment)
	assert.Equal(t, 1, client.CostExplorerClients["111111111111"].CallCount)
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
			reconciler := &SPUtilizationReconciler{
				AWSClient: client,
				Config: &config.Config{
					DefaultRegion: "us-east-1",
					AWSAccounts:   []config.AWSAccount{{AccountID: "111111111111", Name: "example"}},
				},
				Cache: cache.NewSPUtilizationCache(),
				Log:   logr.Discard(),
			}

			result, err := reconciler.Reconcile(context.Background(), ctrl.Request{})
			require.Error(t, err)
			assert.Equal(t, time.Hour, result.RequeueAfter)
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
