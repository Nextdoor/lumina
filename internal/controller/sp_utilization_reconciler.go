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
	"fmt"
	"time"

	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/nextdoor/lumina/internal/cache"
	"github.com/nextdoor/lumina/pkg/aws"
	"github.com/nextdoor/lumina/pkg/config"
)

// SPUtilizationReconciler refreshes settled Compute Savings Plans utilization
// from AWS Cost Explorer. Failed refreshes retain the last successful value.
type SPUtilizationReconciler struct {
	AWSClient     aws.Client
	Config        *config.Config
	Cache         *cache.SPUtilizationCache
	Log           logr.Logger
	ReadyChan     chan struct{}
	HealthTracker *ReconcilerHealthTracker
	now           func() time.Time
}

func (r *SPUtilizationReconciler) Reconcile(ctx context.Context, _ ctrl.Request) (ctrl.Result, error) {
	account := r.Config.GetDefaultAccount()
	accountConfig := aws.AccountConfig{
		AccountID:     account.AccountID,
		Name:          account.Name,
		AssumeRoleARN: account.AssumeRoleARN,
		Region:        r.Config.DefaultRegion,
	}
	client, err := r.AWSClient.CostExplorer(ctx, accountConfig)
	if err != nil {
		return ctrl.Result{RequeueAfter: time.Hour}, fmt.Errorf("create Cost Explorer client: %w", err)
	}

	now := time.Now()
	if r.now != nil {
		now = r.now()
	}
	requestCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	observation, err := client.GetComputeSavingsPlansUnusedCommitment(requestCtx, now)
	if err != nil {
		return ctrl.Result{RequeueAfter: time.Hour},
			fmt.Errorf("get Compute Savings Plans utilization: %w", err)
	}
	r.Cache.UpdateComputeUnusedCommitment(observation.UnusedCommitment, observation.PeriodEnd)
	return ctrl.Result{RequeueAfter: time.Hour}, nil
}

func (r *SPUtilizationReconciler) Run(ctx context.Context) error {
	// Cost Explorer is a best-effort correction to the live EC2 model. Startup
	// must not fail when Cost Explorer is delayed or temporarily unavailable;
	// without a fresh observation, the calculator keeps its instantaneous result.
	if _, err := r.Reconcile(ctx, ctrl.Request{}); err != nil {
		r.Log.Error(err, "initial SP utilization reconciliation failed; continuing without a cap")
	}
	if r.ReadyChan != nil {
		close(r.ReadyChan)
	}

	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if _, err := r.Reconcile(ctx, ctrl.Request{}); err != nil {
				r.Log.Error(err, "scheduled SP utilization reconciliation failed; retaining cached data")
			}
		}
	}
}
