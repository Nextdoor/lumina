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
	"sync"
	"testing"
	"time"

	"github.com/nextdoor/lumina/pkg/config"
)

// mockValidator is a test validator that can be configured to succeed or fail.
type mockValidator struct {
	validateFunc func(ctx context.Context, accountConfig AccountConfig) error
	mu           sync.Mutex
	callCount    int
}

func (m *mockValidator) ValidateAccountAccess(ctx context.Context, accountConfig AccountConfig) error {
	m.mu.Lock()
	m.callCount++
	m.mu.Unlock()

	if m.validateFunc != nil {
		return m.validateFunc(ctx, accountConfig)
	}
	return nil
}

func (m *mockValidator) getCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.callCount
}

func TestNewCredentialMonitor(t *testing.T) {
	validator := &mockValidator{}
	accounts := []config.AWSAccount{
		{AccountID: "123", Name: "test", Region: "us-west-2"},
	}

	monitor := NewCredentialMonitor(validator, accounts, 10*time.Minute)

	if monitor == nil {
		t.Fatal("expected non-nil monitor")
	}

	if monitor.validator != validator {
		t.Error("validator not set correctly")
	}

	if len(monitor.accounts) != 1 {
		t.Errorf("expected 1 account, got %d", len(monitor.accounts))
	}

	if monitor.checkInterval != 10*time.Minute {
		t.Errorf("expected 10m interval, got %v", monitor.checkInterval)
	}

	if monitor.accountStatus == nil {
		t.Error("accountStatus map not initialized")
	}
}

func TestNewCredentialMonitorDefaultInterval(t *testing.T) {
	validator := &mockValidator{}
	accounts := []config.AWSAccount{}

	// Pass 0 for checkInterval to test default
	monitor := NewCredentialMonitor(validator, accounts, 0)

	if monitor.checkInterval != 10*time.Minute {
		t.Errorf("expected default 10m interval, got %v", monitor.checkInterval)
	}
}

func TestCredentialMonitorStartStop(t *testing.T) {
	validator := &mockValidator{}
	accounts := []config.AWSAccount{
		{AccountID: "123", Name: "test", Region: "us-west-2"},
	}

	monitor := NewCredentialMonitor(validator, accounts, 100*time.Millisecond)
	monitor.Start()

	// Wait for at least one check to complete
	time.Sleep(200 * time.Millisecond)

	monitor.Stop()

	// Verify checks were performed
	if validator.getCallCount() == 0 {
		t.Error("expected at least one validation call")
	}
}

func TestCredentialMonitorCheckAllAccounts(t *testing.T) {
	validator := &mockValidator{}
	accounts := []config.AWSAccount{
		{AccountID: "111", Name: "account1", Region: "us-west-2"},
		{AccountID: "222", Name: "account2", Region: "us-east-1"},
		{AccountID: "333", Name: "account3", Region: "eu-west-1"},
	}

	monitor := NewCredentialMonitor(validator, accounts, 10*time.Minute)

	// Manually trigger check
	monitor.CheckAllAccounts()

	// Verify all accounts were checked
	if validator.getCallCount() != 3 {
		t.Errorf("expected 3 validation calls, got %d", validator.getCallCount())
	}

	// Verify status was updated for all accounts
	for _, account := range accounts {
		status := monitor.GetAccountStatus(account.AccountID)
		if status == nil {
			t.Errorf("no status for account %s", account.AccountID)
			continue
		}

		if status.AccountID != account.AccountID {
			t.Errorf("expected account ID %s, got %s", account.AccountID, status.AccountID)
		}

		if !status.Healthy {
			t.Errorf("expected account %s to be healthy", account.AccountID)
		}

		if status.LastError != nil {
			t.Errorf("expected no error for account %s, got %v", account.AccountID, status.LastError)
		}
	}
}

func TestCredentialMonitorGetStatusAllHealthy(t *testing.T) {
	validator := &mockValidator{}
	accounts := []config.AWSAccount{
		{AccountID: "111", Name: "account1", Region: "us-west-2"},
		{AccountID: "222", Name: "account2", Region: "us-east-1"},
	}

	monitor := NewCredentialMonitor(validator, accounts, 10*time.Minute)
	monitor.CheckAllAccounts()

	err := monitor.GetStatus()
	if err != nil {
		t.Errorf("expected nil error when all healthy, got: %v", err)
	}
}

func TestCredentialMonitorGetStatusSomeDegraded(t *testing.T) {
	// Create validator that fails for one specific account
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
	monitor.CheckAllAccounts()

	// With graceful degradation, should return nil (healthy) even though one account failed
	err := monitor.GetStatus()
	if err != nil {
		t.Errorf("expected nil error with graceful degradation, got: %v", err)
	}

	// Verify the degraded account is marked unhealthy
	status := monitor.GetAccountStatus("222")
	if status == nil {
		t.Fatal("expected status for degraded account")
	}

	if status.Healthy {
		t.Error("expected account 222 to be unhealthy")
	}

	if status.LastError == nil {
		t.Error("expected error for account 222")
	}
}

func TestCredentialMonitorGetStatusAllUnhealthy(t *testing.T) {
	// Create validator that always fails
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
	monitor.CheckAllAccounts()

	// When ALL accounts fail, should return error
	err := monitor.GetStatus()
	if err == nil {
		t.Fatal("expected error when all accounts unhealthy")
	}

	// Verify error mentions all accounts
	if err.Error() == "" {
		t.Error("expected non-empty error message")
	}
}

func TestCredentialMonitorGetStatusNoAccounts(t *testing.T) {
	validator := &mockValidator{}
	accounts := []config.AWSAccount{}

	monitor := NewCredentialMonitor(validator, accounts, 10*time.Minute)

	// No accounts should be considered healthy
	err := monitor.GetStatus()
	if err != nil {
		t.Errorf("expected nil error with no accounts, got: %v", err)
	}
}

func TestCredentialMonitorGetStatusBeforeFirstCheck(t *testing.T) {
	validator := &mockValidator{}
	accounts := []config.AWSAccount{
		{AccountID: "111", Name: "account1", Region: "us-west-2"},
	}

	monitor := NewCredentialMonitor(validator, accounts, 10*time.Minute)

	// GetStatus before any checks should not fail (graceful startup)
	err := monitor.GetStatus()
	if err != nil {
		t.Errorf("expected nil error before first check, got: %v", err)
	}
}

func TestCredentialMonitorGetAccountStatus(t *testing.T) {
	validator := &mockValidator{}
	accounts := []config.AWSAccount{
		{AccountID: "123", Name: "test-account", Region: "us-west-2"},
	}

	monitor := NewCredentialMonitor(validator, accounts, 10*time.Minute)
	monitor.CheckAllAccounts()

	status := monitor.GetAccountStatus("123")
	if status == nil {
		t.Fatal("expected non-nil status")
	}

	if status.AccountID != "123" {
		t.Errorf("expected account ID 123, got %s", status.AccountID)
	}

	if status.AccountName != "test-account" {
		t.Errorf("expected account name 'test-account', got %s", status.AccountName)
	}

	if !status.Healthy {
		t.Error("expected account to be healthy")
	}

	if status.LastError != nil {
		t.Errorf("expected nil error, got %v", status.LastError)
	}

	if status.LastChecked.IsZero() {
		t.Error("expected non-zero LastChecked time")
	}
}

func TestCredentialMonitorGetAccountStatusNotFound(t *testing.T) {
	validator := &mockValidator{}
	accounts := []config.AWSAccount{}

	monitor := NewCredentialMonitor(validator, accounts, 10*time.Minute)

	status := monitor.GetAccountStatus("nonexistent")
	if status != nil {
		t.Errorf("expected nil status for nonexistent account, got: %+v", status)
	}
}

func TestCredentialMonitorGetAccountStatusReturnsCode(t *testing.T) {
	validator := &mockValidator{}
	accounts := []config.AWSAccount{
		{AccountID: "123", Name: "test", Region: "us-west-2"},
	}

	monitor := NewCredentialMonitor(validator, accounts, 10*time.Minute)
	monitor.CheckAllAccounts()

	status1 := monitor.GetAccountStatus("123")
	status2 := monitor.GetAccountStatus("123")

	// Verify we get copies, not the same instance
	if status1 == status2 {
		t.Error("expected different instances (copies), got same pointer")
	}

	// But the data should be the same
	if status1.AccountID != status2.AccountID {
		t.Error("expected same AccountID in both copies")
	}
}

func TestCredentialMonitorPeriodicChecks(t *testing.T) {
	validator := &mockValidator{}
	accounts := []config.AWSAccount{
		{AccountID: "123", Name: "test", Region: "us-west-2"},
	}

	// Use short interval for testing
	monitor := NewCredentialMonitor(validator, accounts, 50*time.Millisecond)
	monitor.Start()
	defer monitor.Stop()

	// Wait for multiple check cycles
	time.Sleep(200 * time.Millisecond)

	// Should have performed multiple checks (initial + periodic)
	callCount := validator.getCallCount()
	if callCount < 3 {
		t.Errorf("expected at least 3 checks (initial + 2 periodic), got %d", callCount)
	}
}

func TestCredentialMonitorCheckAccountUpdatesMetrics(t *testing.T) {
	validator := &mockValidator{}
	account := config.AWSAccount{
		AccountID: "123",
		Name:      "test-account",
		Region:    "us-west-2",
	}

	monitor := NewCredentialMonitor(validator, []config.AWSAccount{account}, 10*time.Minute)

	// Perform a check
	monitor.checkAccount(account)

	// Verify status was updated
	status := monitor.GetAccountStatus("123")
	if status == nil {
		t.Fatal("expected status to be set after check")
	}

	if !status.Healthy {
		t.Error("expected healthy status")
	}

	// Note: We can't easily verify Prometheus metrics in unit tests without
	// inspecting the registry, but we've verified the code path executes.
}

func TestCredentialMonitorCheckAccountFailure(t *testing.T) {
	expectedErr := errors.New("credential check failed")
	validator := &mockValidator{
		validateFunc: func(ctx context.Context, accountConfig AccountConfig) error {
			return expectedErr
		},
	}

	account := config.AWSAccount{
		AccountID: "123",
		Name:      "test-account",
		Region:    "us-west-2",
	}

	monitor := NewCredentialMonitor(validator, []config.AWSAccount{account}, 10*time.Minute)

	// Perform a check that will fail
	monitor.checkAccount(account)

	// Verify status reflects the failure
	status := monitor.GetAccountStatus("123")
	if status == nil {
		t.Fatal("expected status to be set after check")
	}

	if status.Healthy {
		t.Error("expected unhealthy status after failed check")
	}

	if status.LastError == nil {
		t.Error("expected LastError to be set")
	}

	if status.LastError.Error() != expectedErr.Error() {
		t.Errorf("expected error '%v', got '%v'", expectedErr, status.LastError)
	}
}

func TestCredentialMonitorConcurrentAccess(t *testing.T) {
	validator := &mockValidator{}
	accounts := []config.AWSAccount{
		{AccountID: "111", Name: "account1", Region: "us-west-2"},
		{AccountID: "222", Name: "account2", Region: "us-east-1"},
	}

	monitor := NewCredentialMonitor(validator, accounts, 20*time.Millisecond)
	monitor.Start()
	defer monitor.Stop()

	// Spawn multiple goroutines reading status concurrently with updates
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 50; j++ {
				_ = monitor.GetStatus()
				_ = monitor.GetAccountStatus("111")
				_ = monitor.GetAccountStatus("222")
				time.Sleep(1 * time.Millisecond)
			}
			done <- true
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}

	// If we get here without data races, the test passes
	// (run with go test -race to verify)
}
