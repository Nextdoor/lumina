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
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nextdoor/lumina/pkg/config"
)

func TestHealthChecker_Name(t *testing.T) {
	monitor := NewCredentialMonitor(&mockValidator{}, []config.AWSAccount{}, 10*time.Minute)
	checker := NewHealthChecker(monitor)

	if name := checker.Name(); name != "aws-account-access" {
		t.Errorf("expected name 'aws-account-access', got %q", name)
	}
}

func TestHealthChecker_CheckNoAccounts(t *testing.T) {
	validator := &mockValidator{}
	accounts := []config.AWSAccount{}
	monitor := NewCredentialMonitor(validator, accounts, 10*time.Minute)
	checker := NewHealthChecker(monitor)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	err := checker.Check(req)

	if err != nil {
		t.Errorf("expected nil error with no accounts, got: %v", err)
	}
}

func TestHealthChecker_CheckAllHealthy(t *testing.T) {
	validator := &mockValidator{}
	accounts := []config.AWSAccount{
		{AccountID: "111", Name: "account1", Region: "us-west-2"},
		{AccountID: "222", Name: "account2", Region: "us-east-1"},
	}
	monitor := NewCredentialMonitor(validator, accounts, 10*time.Minute)

	// Run initial check
	monitor.CheckAllAccounts()

	checker := NewHealthChecker(monitor)
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	err := checker.Check(req)

	if err != nil {
		t.Errorf("expected nil error when all accounts healthy, got: %v", err)
	}
}

func TestHealthChecker_CheckSomeDegraded(t *testing.T) {
	// Validator that fails for one account
	validator := &mockValidator{
		validateFunc: func(ctx context.Context, accountConfig AccountConfig) error {
			if accountConfig.AccountID == "222" {
				return errors.New("credential expired")
			}
			return nil
		},
	}

	accounts := []config.AWSAccount{
		{AccountID: "111", Name: "account1", Region: "us-west-2"},
		{AccountID: "222", Name: "account2", Region: "us-east-1"},
		{AccountID: "333", Name: "account3", Region: "eu-west-1"},
	}
	monitor := NewCredentialMonitor(validator, accounts, 10*time.Minute)

	// Run initial check
	monitor.CheckAllAccounts()

	checker := NewHealthChecker(monitor)
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	err := checker.Check(req)

	// With graceful degradation, should return nil (healthy) even though one account failed
	if err != nil {
		t.Errorf("expected nil error with graceful degradation, got: %v", err)
	}
}

func TestHealthChecker_CheckAllUnhealthy(t *testing.T) {
	// Validator that always fails
	expectedErr := errors.New("all credentials expired")
	validator := &mockValidator{
		validateFunc: func(ctx context.Context, accountConfig AccountConfig) error {
			return expectedErr
		},
	}

	accounts := []config.AWSAccount{
		{AccountID: "111", Name: "account1", Region: "us-west-2"},
		{AccountID: "222", Name: "account2", Region: "us-east-1"},
	}
	monitor := NewCredentialMonitor(validator, accounts, 10*time.Minute)

	// Run initial check
	monitor.CheckAllAccounts()

	checker := NewHealthChecker(monitor)
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	err := checker.Check(req)

	// When ALL accounts fail, should return error
	if err == nil {
		t.Fatal("expected error when all accounts unhealthy")
	}

	if err.Error() == "" {
		t.Error("expected non-empty error message")
	}
}

func TestHealthChecker_CheckBeforeMonitorRuns(t *testing.T) {
	validator := &mockValidator{}
	accounts := []config.AWSAccount{
		{AccountID: "111", Name: "account1", Region: "us-west-2"},
	}

	// Create monitor but don't run any checks
	monitor := NewCredentialMonitor(validator, accounts, 10*time.Minute)

	checker := NewHealthChecker(monitor)
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	err := checker.Check(req)

	// Before first check, should not fail (graceful startup)
	if err != nil {
		t.Errorf("expected nil error before first check, got: %v", err)
	}
}

func TestHealthChecker_CheckUsesMonitorCache(t *testing.T) {
	validator := &mockValidator{}
	accounts := []config.AWSAccount{
		{AccountID: "111", Name: "account1", Region: "us-west-2"},
	}

	monitor := NewCredentialMonitor(validator, accounts, 10*time.Minute)
	monitor.CheckAllAccounts()

	// Verify one check was performed
	initialCallCount := validator.getCallCount()
	if initialCallCount != 1 {
		t.Fatalf("expected 1 validation call, got %d", initialCallCount)
	}

	checker := NewHealthChecker(monitor)

	// Call Check() multiple times
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	for i := 0; i < 10; i++ {
		err := checker.Check(req)
		if err != nil {
			t.Errorf("unexpected error on check %d: %v", i, err)
		}
	}

	// Verify no additional validation calls were made (reading from cache)
	finalCallCount := validator.getCallCount()
	if finalCallCount != initialCallCount {
		t.Errorf("expected %d validation calls (cached), got %d", initialCallCount, finalCallCount)
	}
}
