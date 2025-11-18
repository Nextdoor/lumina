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

// Package config provides configuration management for the Lumina controller.
//
// The controller requires configuration for:
//   - AWS accounts to monitor
//   - IAM roles to assume in each account
//   - Controller operational settings
//
// Configuration can be loaded from YAML files or environment variables.
// Uses Viper for robust configuration management with automatic env binding.
package config

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config represents the complete controller configuration.
type Config struct {
	// AWSAccounts is the list of AWS accounts to monitor for cost data.
	// Each account must have an AssumeRole ARN configured.
	AWSAccounts []AWSAccount `yaml:"awsAccounts"`

	// DefaultAccount is the AWS account used for non-account-specific API calls
	// such as pricing data retrieval. This account's AssumeRole ARN is used for
	// operations that don't target a specific monitored account.
	// If not specified, uses the first account in AWSAccounts.
	DefaultAccount *AWSAccount `yaml:"defaultAccount,omitempty"`

	// DefaultRegion is the default AWS region for API calls.
	// Can be overridden per-account if needed.
	DefaultRegion string `yaml:"defaultRegion,omitempty"`

	// Regions is the list of AWS regions to query for Reserved Instances.
	// RIs are regional resources, so we need to query each region separately.
	// If empty, defaults to config.DefaultRegions (["us-west-2", "us-east-1"])
	// Can be overridden per-account via AWSAccount.Regions.
	Regions []string `yaml:"regions,omitempty"`

	// LogLevel controls the verbosity of logs.
	// Valid values: debug, info, warn, error
	// Default: info
	LogLevel string `yaml:"logLevel,omitempty"`

	// MetricsBindAddress is the address the metrics endpoint binds to.
	// Default: :8080
	MetricsBindAddress string `yaml:"metricsBindAddress,omitempty"`

	// HealthProbeBindAddress is the address the health probe endpoint binds to.
	// Default: :8081
	HealthProbeBindAddress string `yaml:"healthProbeBindAddress,omitempty"`

	// AccountValidationInterval is how often to validate AWS account access.
	// Format: Go duration string (e.g., "5m", "10m")
	// Default: 10m
	AccountValidationInterval string `yaml:"accountValidationInterval,omitempty"`

	// Reconciliation contains settings for data collection reconciliation loops.
	Reconciliation ReconciliationConfig `yaml:"reconciliation,omitempty"`

	// Pricing contains settings for AWS pricing data collection.
	Pricing PricingConfig `yaml:"pricing,omitempty"`

	// TestData contains mock data for E2E testing.
	// When present, the RISP reconciler will use this data instead of making AWS API calls.
	// This allows testing without requiring a fully functional AWS environment.
	// IMPORTANT: This field should only be used in E2E tests, never in production.
	TestData *TestData `yaml:"testData,omitempty"`
}

// ReconciliationConfig contains settings for reconciliation intervals.
type ReconciliationConfig struct {
	// RISP is how often to reconcile Reserved Instances and Savings Plans.
	// Format: Go duration string (e.g., "1h", "30m", "2h")
	// Default: 1h
	// Recommended: 1h (hourly) - RI/SP data changes infrequently
	RISP string `yaml:"risp,omitempty"`

	// EC2 is how often to reconcile EC2 instance inventory.
	// Format: Go duration string (e.g., "5m", "10m", "1m")
	// Default: 5m
	// Recommended: 5m - EC2 instances change frequently due to autoscaling
	EC2 string `yaml:"ec2,omitempty"`

	// Pricing is how often to reconcile AWS pricing data.
	// Format: Go duration string (e.g., "24h", "12h", "48h")
	// Default: 24h
	// Recommended: 24h (daily) - AWS pricing changes monthly, daily refresh is sufficient
	Pricing string `yaml:"pricing,omitempty"`

	// Cost reconciliation is event-driven (no configurable interval needed).
	// Cost calculations trigger automatically when EC2, RISP, or Pricing caches update.
	// A 1-second debouncer prevents redundant calculations when multiple caches update simultaneously.
}

// PricingConfig contains settings for AWS pricing data collection.
type PricingConfig struct {
	// OperatingSystems is the list of operating systems to load pricing data for.
	// Valid values: "Linux", "Windows", "RHEL", "SUSE"
	// Default: ["Linux", "Windows"]
	// Examples:
	//   - ["Linux"] - Only load Linux pricing (fastest, lowest memory)
	//   - ["Linux", "Windows"] - Load both Linux and Windows pricing
	//   - [] - Empty list defaults to ["Linux", "Windows"]
	OperatingSystems []string `yaml:"operatingSystems,omitempty"`
}

// TestData contains mock data for E2E testing.
// This allows testing functionality when LocalStack doesn't support certain APIs.
// IMPORTANT: This should only be used in E2E tests, never in production.
type TestData struct {
	// SavingsPlans contains mock Savings Plans data for testing.
	// Key format: "accountID"
	SavingsPlans map[string][]TestSavingsPlan `yaml:"savingsPlans,omitempty"`

	// Pricing contains mock pricing data for testing.
	// Key format: "region:instanceType:operatingSystem"
	// Example: "us-west-2:m5.large:Linux" -> 0.096
	// This allows E2E tests to run without calling the real AWS Pricing API.
	Pricing map[string]float64 `yaml:"pricing,omitempty"`
}

// TestSavingsPlan represents a mock Savings Plan for E2E testing.
// Fields match the aws.SavingsPlan structure so they can be converted directly.
type TestSavingsPlan struct {
	SavingsPlanARN  string  `yaml:"savingsPlanArn"`
	SavingsPlanType string  `yaml:"savingsPlanType"` // "EC2Instance" or "Compute"
	State           string  `yaml:"state"`           // "active", "payment-pending", etc.
	Commitment      float64 `yaml:"commitment"`      // Hourly commitment amount ($/hour)
	Region          string  `yaml:"region,omitempty"`
	InstanceFamily  string  `yaml:"instanceFamily,omitempty"`
	Start           string  `yaml:"start"` // ISO 8601 format
	End             string  `yaml:"end"`   // ISO 8601 format
}

// AWSAccount represents a single AWS account to monitor.
type AWSAccount struct {
	// AccountID is the 12-digit AWS account ID.
	AccountID string `yaml:"accountId"`

	// Name is a human-readable name for the account.
	// Used in logs and metrics labels.
	Name string `yaml:"name"`

	// AssumeRoleARN is the IAM role ARN to assume for accessing this account.
	// Format: arn:aws:iam::ACCOUNT_ID:role/ROLE_NAME
	AssumeRoleARN string `yaml:"assumeRoleArn"`

	// Region is the AWS region for this account (optional).
	// If not set, uses the default region from Config.DefaultRegion.
	Region string `yaml:"region,omitempty"`

	// Regions is the list of AWS regions to query for Reserved Instances for this account.
	// If empty, uses Config.Regions (the global default).
	// This allows per-account region overrides (e.g., some accounts only use us-west-2).
	Regions []string `yaml:"regions,omitempty"`
}

// Load loads configuration from a YAML file and validates it.
//
// Configuration precedence (highest to lowest):
//  1. Environment variables (LUMINA_* prefix)
//  2. Configuration file values
//  3. Default values
//
// Environment variables can override any configuration value by converting
// the field name to uppercase with LUMINA_ prefix. For example:
//   - LUMINA_DEFAULT_REGION overrides defaultRegion
//   - LUMINA_LOG_LEVEL overrides logLevel
//   - LUMINA_METRICS_BIND_ADDRESS overrides metricsBindAddress
//
// Nested fields like awsAccounts[0].accountId are not overridable via env vars.
func Load(path string) (*Config, error) {
	v := viper.New()

	// Set configuration file
	v.SetConfigFile(path)

	// Set default values
	v.SetDefault("defaultRegion", "us-west-2")
	v.SetDefault("logLevel", "info")
	v.SetDefault("metricsBindAddress", ":8080")
	v.SetDefault("healthProbeBindAddress", ":8081")
	v.SetDefault("accountValidationInterval", "10m")
	v.SetDefault("reconciliation.risp", "1h")
	v.SetDefault("reconciliation.ec2", "5m")
	// Cost reconciliation is event-driven (no default interval needed)

	// Enable environment variable overrides with LUMINA_ prefix
	// Manually bind each config key to its environment variable
	// Viper's automatic mapping doesn't handle camelCase to SCREAMING_SNAKE_CASE well
	v.SetEnvPrefix("LUMINA")
	_ = v.BindEnv("defaultRegion", "LUMINA_DEFAULT_REGION")
	_ = v.BindEnv("logLevel", "LUMINA_LOG_LEVEL")
	_ = v.BindEnv("metricsBindAddress", "LUMINA_METRICS_BIND_ADDRESS")
	_ = v.BindEnv("healthProbeBindAddress", "LUMINA_HEALTH_PROBE_BIND_ADDRESS")
	_ = v.BindEnv("accountValidationInterval", "LUMINA_ACCOUNT_VALIDATION_INTERVAL")
	_ = v.BindEnv("reconciliation.risp", "LUMINA_RECONCILIATION_RISP")
	_ = v.BindEnv("reconciliation.ec2", "LUMINA_RECONCILIATION_EC2")

	// Read configuration file
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", path, err)
	}

	// Unmarshal into Config struct
	var cfg Config
	// coverage:ignore - Viper unmarshal errors are extremely rare and difficult to trigger
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file %s: %w", path, err)
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &cfg, nil
}

// Validate checks that the configuration is valid and returns an error if not.
func (c *Config) Validate() error {
	// Check that at least one AWS account is configured
	if len(c.AWSAccounts) == 0 {
		return fmt.Errorf("at least one AWS account must be configured")
	}

	// Validate each account
	accountIDs := make(map[string]bool)
	for i, account := range c.AWSAccounts {
		// Check for duplicate account IDs
		if accountIDs[account.AccountID] {
			return fmt.Errorf("duplicate account ID: %s", account.AccountID)
		}
		accountIDs[account.AccountID] = true

		// Validate account
		if err := account.Validate(); err != nil {
			return fmt.Errorf("invalid account at index %d: %w", i, err)
		}
	}

	// Validate default account if specified
	if c.DefaultAccount != nil {
		if err := c.DefaultAccount.Validate(); err != nil {
			return fmt.Errorf("invalid default account: %w", err)
		}
	}

	// Validate log level
	validLogLevels := map[string]bool{
		"debug": true,
		"info":  true,
		"warn":  true,
		"error": true,
	}
	if c.LogLevel != "" && !validLogLevels[c.LogLevel] {
		return fmt.Errorf("invalid log level %q, must be one of: debug, info, warn, error", c.LogLevel)
	}

	// Validate account validation interval
	if c.AccountValidationInterval != "" {
		if _, err := time.ParseDuration(c.AccountValidationInterval); err != nil {
			return fmt.Errorf("invalid account validation interval %q: %w", c.AccountValidationInterval, err)
		}
	}

	// Validate reconciliation intervals
	if c.Reconciliation.RISP != "" {
		if _, err := time.ParseDuration(c.Reconciliation.RISP); err != nil {
			return fmt.Errorf("invalid RISP reconciliation interval %q: %w", c.Reconciliation.RISP, err)
		}
	}
	if c.Reconciliation.EC2 != "" {
		if _, err := time.ParseDuration(c.Reconciliation.EC2); err != nil {
			return fmt.Errorf("invalid EC2 reconciliation interval %q: %w", c.Reconciliation.EC2, err)
		}
	}
	if c.Reconciliation.Pricing != "" {
		if _, err := time.ParseDuration(c.Reconciliation.Pricing); err != nil {
			return fmt.Errorf("invalid Pricing reconciliation interval %q: %w", c.Reconciliation.Pricing, err)
		}
	}
	// Cost reconciliation is event-driven (no interval validation needed)

	// Validate pricing configuration
	if len(c.Pricing.OperatingSystems) > 0 {
		validOSes := map[string]bool{
			"Linux":   true,
			"Windows": true,
			"RHEL":    true,
			"SUSE":    true,
		}
		for _, os := range c.Pricing.OperatingSystems {
			if !validOSes[os] {
				return fmt.Errorf("invalid operating system %q in pricing config, must be one of: Linux, Windows, RHEL, SUSE", os)
			}
		}
	}

	return nil
}

// Validate checks that the AWS account configuration is valid.
func (a *AWSAccount) Validate() error {
	// Validate account ID format (12 digits)
	if !isValidAccountID(a.AccountID) {
		return fmt.Errorf("invalid account ID %q: must be 12 digits", a.AccountID)
	}

	// Name is required for logs and metrics
	if strings.TrimSpace(a.Name) == "" {
		return fmt.Errorf("account name is required")
	}

	// Validate AssumeRole ARN format
	if !isValidIAMRoleARN(a.AssumeRoleARN) {
		return fmt.Errorf(
			"invalid AssumeRole ARN %q: must be in format arn:aws:iam::ACCOUNT_ID:role/ROLE_NAME",
			a.AssumeRoleARN,
		)
	}

	// Verify that the ARN's account ID matches the configured account ID
	// Extract account ID from ARN (format: arn:aws:iam::123456789012:role/RoleName)
	arnAccountID := extractAccountIDFromARN(a.AssumeRoleARN)
	if arnAccountID != a.AccountID {
		return fmt.Errorf("AssumeRole ARN account ID %q does not match configured account ID %q", arnAccountID, a.AccountID)
	}

	return nil
}

// isValidAccountID checks if a string is a valid 12-digit AWS account ID.
func isValidAccountID(accountID string) bool {
	// AWS account IDs are always exactly 12 digits
	matched, _ := regexp.MatchString(`^\d{12}$`, accountID)
	return matched
}

// isValidIAMRoleARN checks if a string is a valid IAM role ARN.
// Valid format: arn:aws:iam::123456789012:role/RoleName
// Also accepts: arn:aws-us-gov:iam::... for GovCloud
func isValidIAMRoleARN(arn string) bool {
	// Basic IAM role ARN pattern
	// Partition can be "aws" or "aws-us-gov" or "aws-cn"
	matched, _ := regexp.MatchString(`^arn:(aws|aws-us-gov|aws-cn):iam::\d{12}:role/[a-zA-Z0-9+=,.@\-_/]+$`, arn)
	return matched
}

// extractAccountIDFromARN extracts the account ID from an IAM role ARN.
// Returns empty string if the ARN is invalid.
func extractAccountIDFromARN(arn string) string {
	// ARN format: arn:aws:iam::123456789012:role/RoleName
	parts := strings.Split(arn, ":")
	if len(parts) >= 5 {
		return parts[4]
	}
	return ""
}

// GetAccountValidationInterval returns the parsed account validation interval duration.
// Returns 10 minutes if not configured (the default value).
func (c *Config) GetAccountValidationInterval() time.Duration {
	if c.AccountValidationInterval == "" {
		return 10 * time.Minute
	}
	duration, err := time.ParseDuration(c.AccountValidationInterval)
	if err != nil {
		// Should never happen since Validate() checks this
		return 10 * time.Minute
	}
	return duration
}

// GetDefaultAccount returns the default account to use for non-account-specific
// AWS API calls (e.g., pricing data). If DefaultAccount is not explicitly configured,
// returns the first account in AWSAccounts.
func (c *Config) GetDefaultAccount() AWSAccount {
	if c.DefaultAccount != nil {
		return *c.DefaultAccount
	}
	// Default to first account if default account not specified
	return c.AWSAccounts[0]
}
