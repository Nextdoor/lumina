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
package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
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
// Environment variables can override configuration values:
//   - LUMINA_DEFAULT_REGION overrides defaultRegion
//   - LUMINA_LOG_LEVEL overrides logLevel
//   - LUMINA_METRICS_BIND_ADDRESS overrides metricsBindAddress
//   - LUMINA_HEALTH_PROBE_BIND_ADDRESS overrides healthProbeBindAddress
//   - LUMINA_ACCOUNT_VALIDATION_INTERVAL overrides accountValidationInterval
func Load(path string) (*Config, error) {
	// Read the configuration file
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", path, err)
	}

	// Parse YAML
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file %s: %w", path, err)
	}

	// Apply environment variable overrides
	applyEnvOverrides(&cfg)

	// Apply defaults
	applyDefaults(&cfg)

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

// applyEnvOverrides applies environment variable overrides to the configuration.
func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("LUMINA_DEFAULT_REGION"); v != "" {
		cfg.DefaultRegion = v
	}
	if v := os.Getenv("LUMINA_LOG_LEVEL"); v != "" {
		cfg.LogLevel = v
	}
	if v := os.Getenv("LUMINA_METRICS_BIND_ADDRESS"); v != "" {
		cfg.MetricsBindAddress = v
	}
	if v := os.Getenv("LUMINA_HEALTH_PROBE_BIND_ADDRESS"); v != "" {
		cfg.HealthProbeBindAddress = v
	}
	if v := os.Getenv("LUMINA_ACCOUNT_VALIDATION_INTERVAL"); v != "" {
		cfg.AccountValidationInterval = v
	}
}

// applyDefaults sets default values for optional configuration fields.
func applyDefaults(cfg *Config) {
	if cfg.DefaultRegion == "" {
		cfg.DefaultRegion = "us-west-2"
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = "info"
	}
	if cfg.MetricsBindAddress == "" {
		cfg.MetricsBindAddress = ":8080"
	}
	if cfg.HealthProbeBindAddress == "" {
		cfg.HealthProbeBindAddress = ":8081"
	}
	if cfg.AccountValidationInterval == "" {
		cfg.AccountValidationInterval = "5m"
	}
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
