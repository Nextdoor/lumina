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

	"github.com/spf13/viper"
)

// Config represents the complete controller configuration.
type Config struct {
	// AWSAccounts is the list of AWS accounts to monitor for cost data.
	// Each account must have an AssumeRole ARN configured.
	AWSAccounts []AWSAccount `yaml:"awsAccounts"`

	// DefaultRegion is the default AWS region for API calls.
	// Can be overridden per-account if needed.
	DefaultRegion string `yaml:"defaultRegion,omitempty"`

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
	// Default: 5m
	AccountValidationInterval string `yaml:"accountValidationInterval,omitempty"`
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
	v.SetDefault("accountValidationInterval", "5m")

	// Enable environment variable overrides with LUMINA_ prefix
	// Manually bind each config key to its environment variable
	// Viper's automatic mapping doesn't handle camelCase to SCREAMING_SNAKE_CASE well
	v.SetEnvPrefix("LUMINA")
	v.BindEnv("defaultRegion", "LUMINA_DEFAULT_REGION")
	v.BindEnv("logLevel", "LUMINA_LOG_LEVEL")
	v.BindEnv("metricsBindAddress", "LUMINA_METRICS_BIND_ADDRESS")
	v.BindEnv("healthProbeBindAddress", "LUMINA_HEALTH_PROBE_BIND_ADDRESS")
	v.BindEnv("accountValidationInterval", "LUMINA_ACCOUNT_VALIDATION_INTERVAL")

	// Read configuration file
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", path, err)
	}

	// Unmarshal into Config struct
	var cfg Config
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
		return fmt.Errorf("invalid AssumeRole ARN %q: must be in format arn:aws:iam::ACCOUNT_ID:role/ROLE_NAME", a.AssumeRoleARN)
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
