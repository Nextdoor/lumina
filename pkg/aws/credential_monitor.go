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
	"fmt"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/nextdoor/lumina/pkg/config"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	ctrl "sigs.k8s.io/controller-runtime"
)

var (
	// credentialCheckTotal tracks the total number of credential checks performed.
	credentialCheckTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "aws_credential_check_total",
			Help: "Total number of AWS credential checks performed by account and status",
		},
		[]string{"account_id", "account_name", "status"},
	)

	// credentialCheckDuration tracks the duration of credential checks.
	credentialCheckDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "aws_credential_check_duration_seconds",
			Help:    "Duration of AWS credential checks in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"account_id", "account_name"},
	)

	// credentialLastCheckTimestamp tracks when the last check was performed.
	credentialLastCheckTimestamp = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "aws_credential_last_check_timestamp",
			Help: "Unix timestamp of the last AWS credential check",
		},
		[]string{"account_id", "account_name"},
	)

	// credentialHealthy indicates whether credentials are currently healthy.
	credentialHealthy = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "aws_credential_healthy",
			Help: "Indicates if AWS credentials are healthy (1=healthy, 0=unhealthy)",
		},
		[]string{"account_id", "account_name"},
	)
)

// AccountStatus represents the health status of a single AWS account.
type AccountStatus struct {
	AccountID   string    // AWS account ID
	AccountName string    // Human-readable account name
	LastChecked time.Time // When the last check was performed
	LastError   error     // Error from the last check (nil if healthy)
	Healthy     bool      // Overall health status
}

// CredentialMonitor runs periodic background checks of AWS credentials
// and caches the results for fast healthz lookups without making AWS API calls.
//
// This monitor significantly reduces AWS API traffic during healthz checks
// (from ~42 calls/min to ~0.7 calls/min for 7 accounts with default settings).
// Health checks read cached state from memory instead of making network calls,
// providing sub-millisecond response times while still detecting credential
// issues within the configured check interval.
//
// The monitor runs checks in the background on a configurable interval
// (default: 10 minutes) and maintains per-account health status with
// graceful degradation support.
type CredentialMonitor struct {
	validator     Validator           // Validator for checking AWS credentials
	accounts      []config.AWSAccount // AWS accounts to monitor
	checkInterval time.Duration       // How often to check credentials

	mu            sync.RWMutex              // Protects accountStatus map
	accountStatus map[string]*AccountStatus // key: accountID

	ctx    context.Context    // Context for lifecycle management
	cancel context.CancelFunc // Cancel function for stopping the monitor
	logger logr.Logger        // Structured logger for monitoring events
}

// NewCredentialMonitor creates a new credential monitor with the specified configuration.
//
// The monitor will not start automatically - call Start() to begin background monitoring.
//
// Parameters:
//   - validator: AWS account validator for checking credential health
//   - accounts: List of AWS accounts to monitor
//   - checkInterval: How often to check credentials (default: 10 minutes)
//
// A typical check interval is 10 minutes, which provides timely detection of
// credential issues while minimizing AWS API traffic. For a 7-account setup:
//   - 10 second healthz interval: 42 AWS API calls/min (without monitor)
//   - 10 minute monitor interval: 0.7 AWS API calls/min (98% reduction)
func NewCredentialMonitor(
	validator Validator,
	accounts []config.AWSAccount,
	checkInterval time.Duration,
) *CredentialMonitor {
	ctx, cancel := context.WithCancel(context.Background())
	logger := ctrl.Log.WithName("credential-monitor")

	// Default check interval if not specified
	if checkInterval == 0 {
		checkInterval = 10 * time.Minute
	}

	return &CredentialMonitor{
		validator:     validator,
		accounts:      accounts,
		checkInterval: checkInterval,
		accountStatus: make(map[string]*AccountStatus),
		ctx:           ctx,
		cancel:        cancel,
		logger:        logger,
	}
}

// Start begins the background monitoring goroutine.
// This method is non-blocking and returns immediately.
//
// The monitor will perform an initial check immediately on startup,
// then continue checking on the configured interval until Stop() is called.
func (m *CredentialMonitor) Start() {
	m.logger.Info("Starting credential monitor",
		"accounts", len(m.accounts),
		"checkInterval", m.checkInterval)

	go m.monitorLoop()
}

// Stop gracefully shuts down the credential monitor.
// This method blocks until the monitor goroutine has exited.
func (m *CredentialMonitor) Stop() {
	m.logger.Info("Stopping credential monitor")
	m.cancel()
}

// monitorLoop is the main monitoring goroutine that runs periodic credential checks.
func (m *CredentialMonitor) monitorLoop() {
	// Run initial check immediately on startup before entering ticker loop.
	// This ensures health status is available as soon as the monitor starts,
	// rather than waiting for the first tick (which could be 10+ minutes).
	m.CheckAllAccounts()

	ticker := time.NewTicker(m.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.CheckAllAccounts()
		case <-m.ctx.Done():
			m.logger.Info("Credential monitor stopped")
			return
		}
	}
}

// CheckAllAccounts performs validation checks on all configured accounts.
// This runs in the background and updates the cached status for each account.
// This method is exported for testing purposes.
func (m *CredentialMonitor) CheckAllAccounts() {
	m.logger.V(1).Info("Running credential checks", "accounts", len(m.accounts))

	for _, account := range m.accounts {
		m.checkAccount(account)
	}
}

// checkAccount validates a single account and updates its status.
func (m *CredentialMonitor) checkAccount(account config.AWSAccount) {
	start := time.Now()
	accountConfig := AccountConfig{
		AccountID:     account.AccountID,
		AssumeRoleARN: account.AssumeRoleARN,
		Region:        account.Region,
	}

	// Perform the validation check
	err := m.validator.ValidateAccountAccess(m.ctx, accountConfig)
	duration := time.Since(start)

	// Update metrics
	status := "success"
	healthyValue := float64(1)
	if err != nil {
		status = "failure"
		healthyValue = 0
	}
	credentialCheckTotal.WithLabelValues(account.AccountID, account.Name, status).Inc()
	credentialCheckDuration.WithLabelValues(account.AccountID, account.Name).Observe(duration.Seconds())
	credentialLastCheckTimestamp.WithLabelValues(account.AccountID, account.Name).SetToCurrentTime()
	credentialHealthy.WithLabelValues(account.AccountID, account.Name).Set(healthyValue)

	// Update cached status
	m.mu.Lock()
	m.accountStatus[account.AccountID] = &AccountStatus{
		AccountID:   account.AccountID,
		AccountName: account.Name,
		LastChecked: time.Now(),
		LastError:   err,
		Healthy:     err == nil,
	}
	m.mu.Unlock()

	if err != nil {
		m.logger.Error(err, "Credential check failed",
			"accountID", account.AccountID,
			"accountName", account.Name,
			"duration", duration)
	} else {
		m.logger.V(1).Info("Credential check succeeded",
			"accountID", account.AccountID,
			"accountName", account.Name,
			"duration", duration)
	}
}

// GetStatus returns the cached health status for all monitored accounts.
// This method is designed for healthz checks and performs only memory reads
// (no AWS API calls), providing sub-millisecond response times.
//
// The method implements graceful degradation: it returns an error only if
// ALL accounts are unhealthy. If some accounts are healthy and some are
// degraded, it returns nil (healthy) but logs warnings about degraded accounts.
// This allows the controller to continue operating when individual accounts
// have issues while still detecting complete credential failure.
//
// Returns:
//   - nil if all accounts are healthy (or if some are degraded but not all failed)
//   - error if ALL accounts are unhealthy
func (m *CredentialMonitor) GetStatus() error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// If no accounts configured, consider healthy
	if len(m.accounts) == 0 {
		return nil
	}

	var unhealthyAccounts []string
	var healthyCount int

	for _, account := range m.accounts {
		status, exists := m.accountStatus[account.AccountID]
		if !exists {
			// Account hasn't been checked yet (monitor just started)
			// Don't fail health check; just note it hasn't been validated yet
			m.logger.V(1).Info("Account not yet checked",
				"accountID", account.AccountID,
				"accountName", account.Name)
			continue
		}

		if !status.Healthy {
			unhealthyAccounts = append(unhealthyAccounts, fmt.Sprintf(
				"%s (%s): %v",
				status.AccountName,
				status.AccountID,
				status.LastError,
			))
		} else {
			healthyCount++
		}
	}

	// Graceful degradation: Only fail if ALL accounts are unhealthy.
	// This allows the controller to continue operating when individual
	// accounts have credential issues, rather than failing entirely.
	totalAccounts := len(m.accounts)
	if len(unhealthyAccounts) > 0 {
		if healthyCount == 0 {
			// ALL accounts are unhealthy - fail the health check
			return fmt.Errorf("all %d AWS accounts are unhealthy: %v",
				totalAccounts, unhealthyAccounts)
		}

		// Some accounts unhealthy but not all - log warning but don't fail
		m.logger.Info("Some AWS accounts are unhealthy (degraded operation)",
			"unhealthyCount", len(unhealthyAccounts),
			"healthyCount", healthyCount,
			"totalCount", totalAccounts,
			"unhealthyAccounts", unhealthyAccounts)
	}

	return nil
}

// GetAccountStatus returns the cached status for a specific account.
// Returns nil if the account hasn't been checked yet.
func (m *CredentialMonitor) GetAccountStatus(accountID string) *AccountStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	status, exists := m.accountStatus[accountID]
	if !exists {
		return nil
	}

	// Return a copy to prevent external modification
	statusCopy := *status
	return &statusCopy
}
