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

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid minimal config",
			yaml: `awsAccounts:
  - accountId: "123456789012"
    name: "Test Account"
    assumeRoleArn: "arn:aws:iam::123456789012:role/test-role"`,
			wantErr: false,
		},
		{
			name: "valid config with multiple accounts",
			yaml: `awsAccounts:
  - accountId: "123456789012"
    name: "Production"
    assumeRoleArn: "arn:aws:iam::123456789012:role/lumina-controller"
  - accountId: "987654321098"
    name: "Staging"
    assumeRoleArn: "arn:aws:iam::987654321098:role/lumina-controller"
defaultRegion: us-east-1
logLevel: debug`,
			wantErr: false,
		},
		{
			name: "valid config with optional fields",
			yaml: `awsAccounts:
  - accountId: "123456789012"
    name: "Test"
    assumeRoleArn: "arn:aws:iam::123456789012:role/test-role"
    region: "eu-west-1"
defaultRegion: us-west-2
logLevel: info
metricsBindAddress: ":9090"
healthProbeBindAddress: ":9091"
accountValidationInterval: "10m"`,
			wantErr: false,
		},
		{
			name:    "empty config file",
			yaml:    ``,
			wantErr: true,
			errMsg:  "at least one AWS account must be configured",
		},
		{
			name: "no accounts configured",
			yaml: `awsAccounts: []
defaultRegion: us-west-2`,
			wantErr: true,
			errMsg:  "at least one AWS account must be configured",
		},
		{
			name: "invalid account ID - too short",
			yaml: `awsAccounts:
  - accountId: "12345"
    name: "Test"
    assumeRoleArn: "arn:aws:iam::123456789012:role/test-role"`,
			wantErr: true,
			errMsg:  "invalid account ID",
		},
		{
			name: "invalid account ID - not numeric",
			yaml: `awsAccounts:
  - accountId: "12345678901a"
    name: "Test"
    assumeRoleArn: "arn:aws:iam::123456789012:role/test-role"`,
			wantErr: true,
			errMsg:  "invalid account ID",
		},
		{
			name: "missing account name",
			yaml: `awsAccounts:
  - accountId: "123456789012"
    assumeRoleArn: "arn:aws:iam::123456789012:role/test-role"`,
			wantErr: true,
			errMsg:  "account name is required",
		},
		{
			name: "empty account name",
			yaml: `awsAccounts:
  - accountId: "123456789012"
    name: ""
    assumeRoleArn: "arn:aws:iam::123456789012:role/test-role"`,
			wantErr: true,
			errMsg:  "account name is required",
		},
		{
			name: "whitespace-only account name",
			yaml: `awsAccounts:
  - accountId: "123456789012"
    name: "   "
    assumeRoleArn: "arn:aws:iam::123456789012:role/test-role"`,
			wantErr: true,
			errMsg:  "account name is required",
		},
		{
			name: "invalid ARN format",
			yaml: `awsAccounts:
  - accountId: "123456789012"
    name: "Test"
    assumeRoleArn: "not-an-arn"`,
			wantErr: true,
			errMsg:  "invalid AssumeRole ARN",
		},
		{
			name: "ARN missing role name",
			yaml: `awsAccounts:
  - accountId: "123456789012"
    name: "Test"
    assumeRoleArn: "arn:aws:iam::123456789012:role/"`,
			wantErr: true,
			errMsg:  "invalid AssumeRole ARN",
		},
		{
			name: "ARN account ID mismatch",
			yaml: `awsAccounts:
  - accountId: "123456789012"
    name: "Test"
    assumeRoleArn: "arn:aws:iam::987654321098:role/test-role"`,
			wantErr: true,
			errMsg:  "does not match configured account ID",
		},
		{
			name: "duplicate account IDs",
			yaml: `awsAccounts:
  - accountId: "123456789012"
    name: "Account 1"
    assumeRoleArn: "arn:aws:iam::123456789012:role/role1"
  - accountId: "123456789012"
    name: "Account 2"
    assumeRoleArn: "arn:aws:iam::123456789012:role/role2"`,
			wantErr: true,
			errMsg:  "duplicate account ID",
		},
		{
			name: "invalid log level",
			yaml: `awsAccounts:
  - accountId: "123456789012"
    name: "Test"
    assumeRoleArn: "arn:aws:iam::123456789012:role/test-role"
logLevel: invalid`,
			wantErr: true,
			errMsg:  "invalid log level",
		},
		{
			name: "invalid YAML syntax",
			yaml: `awsAccounts:
  - accountId: "123456789012"
    name: "Test
    assumeRoleArn: "arn:aws:iam::123456789012:role/test-role"`,
			wantErr: true,
			errMsg:  "failed to read config file", // Viper reports YAML parse errors as read errors
		},
		{
			name: "govcloud ARN",
			yaml: `awsAccounts:
  - accountId: "123456789012"
    name: "GovCloud Account"
    assumeRoleArn: "arn:aws-us-gov:iam::123456789012:role/test-role"`,
			wantErr: false,
		},
		{
			name: "china ARN",
			yaml: `awsAccounts:
  - accountId: "123456789012"
    name: "China Account"
    assumeRoleArn: "arn:aws-cn:iam::123456789012:role/test-role"`,
			wantErr: false,
		},
		{
			name: "role with path",
			yaml: `awsAccounts:
  - accountId: "123456789012"
    name: "Test"
    assumeRoleArn: "arn:aws:iam::123456789012:role/path/to/test-role"`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary config file
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "config.yaml")
			if err := os.WriteFile(configPath, []byte(tt.yaml), 0644); err != nil {
				t.Fatalf("failed to write test config: %v", err)
			}

			// Load config
			cfg, err := Load(configPath)

			// Check error expectation
			if tt.wantErr {
				if err == nil {
					t.Errorf("Load() expected error containing %q, got nil", tt.errMsg)
					return
				}
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Load() error = %q, want error containing %q", err.Error(), tt.errMsg)
				}
				return
			}

			// Success case
			if err != nil {
				t.Errorf("Load() unexpected error: %v", err)
				return
			}
			if cfg == nil {
				t.Error("Load() returned nil config")
			}
		})
	}
}

func TestLoadFileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/path/config.yaml")
	if err == nil {
		t.Error("Load() expected error for nonexistent file, got nil")
	}
	if !strings.Contains(err.Error(), "failed to read config file") {
		t.Errorf("Load() error = %q, want error containing 'failed to read config file'", err.Error())
	}
}

func TestApplyDefaults(t *testing.T) {
	yaml := `awsAccounts:
  - accountId: "123456789012"
    name: "Test"
    assumeRoleArn: "arn:aws:iam::123456789012:role/test-role"`

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(yaml), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}

	// Verify defaults
	if cfg.DefaultRegion != "us-west-2" {
		t.Errorf("DefaultRegion = %q, want 'us-west-2'", cfg.DefaultRegion)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want 'info'", cfg.LogLevel)
	}
	if cfg.MetricsBindAddress != ":8080" {
		t.Errorf("MetricsBindAddress = %q, want ':8080'", cfg.MetricsBindAddress)
	}
	if cfg.HealthProbeBindAddress != ":8081" {
		t.Errorf("HealthProbeBindAddress = %q, want ':8081'", cfg.HealthProbeBindAddress)
	}
	if cfg.AccountValidationInterval != "5m" {
		t.Errorf("AccountValidationInterval = %q, want '5m'", cfg.AccountValidationInterval)
	}
}

func TestEnvOverrides(t *testing.T) {
	yaml := `awsAccounts:
  - accountId: "123456789012"
    name: "Test"
    assumeRoleArn: "arn:aws:iam::123456789012:role/test-role"
defaultRegion: us-west-2
logLevel: info
metricsBindAddress: ":8080"
healthProbeBindAddress: ":8081"
accountValidationInterval: "5m"`

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(yaml), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	// Set environment variables
	originalEnv := map[string]string{
		"LUMINA_DEFAULT_REGION":              os.Getenv("LUMINA_DEFAULT_REGION"),
		"LUMINA_LOG_LEVEL":                   os.Getenv("LUMINA_LOG_LEVEL"),
		"LUMINA_METRICS_BIND_ADDRESS":        os.Getenv("LUMINA_METRICS_BIND_ADDRESS"),
		"LUMINA_HEALTH_PROBE_BIND_ADDRESS":   os.Getenv("LUMINA_HEALTH_PROBE_BIND_ADDRESS"),
		"LUMINA_ACCOUNT_VALIDATION_INTERVAL": os.Getenv("LUMINA_ACCOUNT_VALIDATION_INTERVAL"),
	}
	defer func() {
		// Restore original environment
		for k, v := range originalEnv {
			if v == "" {
				_ = os.Unsetenv(k)
			} else {
				_ = os.Setenv(k, v)
			}
		}
	}()

	_ = os.Setenv("LUMINA_DEFAULT_REGION", "eu-west-1")
	_ = os.Setenv("LUMINA_LOG_LEVEL", "debug")
	_ = os.Setenv("LUMINA_METRICS_BIND_ADDRESS", ":9090")
	_ = os.Setenv("LUMINA_HEALTH_PROBE_BIND_ADDRESS", ":9091")
	_ = os.Setenv("LUMINA_ACCOUNT_VALIDATION_INTERVAL", "10m")

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}

	// Verify environment overrides
	if cfg.DefaultRegion != "eu-west-1" {
		t.Errorf("DefaultRegion = %q, want 'eu-west-1' (from env)", cfg.DefaultRegion)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want 'debug' (from env)", cfg.LogLevel)
	}
	if cfg.MetricsBindAddress != ":9090" {
		t.Errorf("MetricsBindAddress = %q, want ':9090' (from env)", cfg.MetricsBindAddress)
	}
	if cfg.HealthProbeBindAddress != ":9091" {
		t.Errorf("HealthProbeBindAddress = %q, want ':9091' (from env)", cfg.HealthProbeBindAddress)
	}
	if cfg.AccountValidationInterval != "10m" {
		t.Errorf("AccountValidationInterval = %q, want '10m' (from env)", cfg.AccountValidationInterval)
	}
}

func TestValidAccountID(t *testing.T) {
	tests := []struct {
		accountID string
		want      bool
	}{
		{"123456789012", true},
		{"000000000000", true},
		{"999999999999", true},
		{"12345678901", false},   // too short
		{"1234567890123", false}, // too long
		{"12345678901a", false},  // contains letter
		{"123-456-789", false},   // contains dashes
		{"", false},
		{"   ", false},
	}

	for _, tt := range tests {
		t.Run(tt.accountID, func(t *testing.T) {
			got := isValidAccountID(tt.accountID)
			if got != tt.want {
				t.Errorf("isValidAccountID(%q) = %v, want %v", tt.accountID, got, tt.want)
			}
		})
	}
}

func TestValidIAMRoleARN(t *testing.T) {
	tests := []struct {
		arn  string
		want bool
	}{
		{"arn:aws:iam::123456789012:role/test-role", true},
		{"arn:aws:iam::123456789012:role/path/to/role", true},
		{"arn:aws:iam::123456789012:role/Role_Name-123", true},
		{"arn:aws-us-gov:iam::123456789012:role/test-role", true},
		{"arn:aws-cn:iam::123456789012:role/test-role", true},
		{"arn:aws:iam::123456789012:role/", false},     // missing role name
		{"arn:aws:iam::123456789012:user/test", false}, // not a role
		{"arn:aws:s3:::bucket", false},                 // wrong service
		{"not-an-arn", false},
		{"", false},
		{"arn:aws:iam::12345:role/test", false}, // invalid account ID length
	}

	for _, tt := range tests {
		t.Run(tt.arn, func(t *testing.T) {
			got := isValidIAMRoleARN(tt.arn)
			if got != tt.want {
				t.Errorf("isValidIAMRoleARN(%q) = %v, want %v", tt.arn, got, tt.want)
			}
		})
	}
}

func TestExtractAccountIDFromARN(t *testing.T) {
	tests := []struct {
		arn  string
		want string
	}{
		{"arn:aws:iam::123456789012:role/test-role", "123456789012"},
		{"arn:aws-us-gov:iam::987654321098:role/test", "987654321098"},
		{"arn:aws-cn:iam::111111111111:role/path/to/role", "111111111111"},
		{"not-an-arn", ""},
		{"arn:aws:iam::", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.arn, func(t *testing.T) {
			got := extractAccountIDFromARN(tt.arn)
			if got != tt.want {
				t.Errorf("extractAccountIDFromARN(%q) = %q, want %q", tt.arn, got, tt.want)
			}
		})
	}
}

func TestConfigValidateLogLevels(t *testing.T) {
	validLevels := []string{"debug", "info", "warn", "error"}
	for _, level := range validLevels {
		t.Run("valid_"+level, func(t *testing.T) {
			cfg := &Config{
				AWSAccounts: []AWSAccount{
					{
						AccountID:     "123456789012",
						Name:          "Test",
						AssumeRoleARN: "arn:aws:iam::123456789012:role/test",
					},
				},
				LogLevel: level,
			}
			if err := cfg.Validate(); err != nil {
				t.Errorf("Validate() unexpected error for log level %q: %v", level, err)
			}
		})
	}

	// Test invalid log level
	cfg := &Config{
		AWSAccounts: []AWSAccount{
			{
				AccountID:     "123456789012",
				Name:          "Test",
				AssumeRoleARN: "arn:aws:iam::123456789012:role/test",
			},
		},
		LogLevel: "invalid",
	}
	err := cfg.Validate()
	if err == nil {
		t.Error("Validate() expected error for invalid log level, got nil")
	}
	if !strings.Contains(err.Error(), "invalid log level") {
		t.Errorf("Validate() error = %q, want error containing 'invalid log level'", err.Error())
	}
}

func TestAWSAccountValidate(t *testing.T) {
	tests := []struct {
		name    string
		account AWSAccount
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid account",
			account: AWSAccount{
				AccountID:     "123456789012",
				Name:          "Test Account",
				AssumeRoleARN: "arn:aws:iam::123456789012:role/test-role",
			},
			wantErr: false,
		},
		{
			name: "invalid account ID",
			account: AWSAccount{
				AccountID:     "invalid",
				Name:          "Test",
				AssumeRoleARN: "arn:aws:iam::123456789012:role/test-role",
			},
			wantErr: true,
			errMsg:  "invalid account ID",
		},
		{
			name: "missing name",
			account: AWSAccount{
				AccountID:     "123456789012",
				Name:          "",
				AssumeRoleARN: "arn:aws:iam::123456789012:role/test-role",
			},
			wantErr: true,
			errMsg:  "account name is required",
		},
		{
			name: "invalid ARN",
			account: AWSAccount{
				AccountID:     "123456789012",
				Name:          "Test",
				AssumeRoleARN: "not-an-arn",
			},
			wantErr: true,
			errMsg:  "invalid AssumeRole ARN",
		},
		{
			name: "ARN account mismatch",
			account: AWSAccount{
				AccountID:     "123456789012",
				Name:          "Test",
				AssumeRoleARN: "arn:aws:iam::999999999999:role/test-role",
			},
			wantErr: true,
			errMsg:  "does not match configured account ID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.account.Validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("Validate() expected error containing %q, got nil", tt.errMsg)
					return
				}
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Validate() error = %q, want error containing %q", err.Error(), tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("Validate() unexpected error: %v", err)
				}
			}
		})
	}
}

// TestReconciliationIntervalValidation tests validation of reconciliation interval fields.
func TestReconciliationIntervalValidation(t *testing.T) {
	tests := []struct {
		name           string
		reconciliation ReconciliationConfig
		wantErr        bool
		errMsg         string
	}{
		{
			name: "valid RISP interval",
			reconciliation: ReconciliationConfig{
				RISP: "1h",
			},
			wantErr: false,
		},
		{
			name: "valid EC2 interval",
			reconciliation: ReconciliationConfig{
				EC2: "5m",
			},
			wantErr: false,
		},
		{
			name: "valid both intervals",
			reconciliation: ReconciliationConfig{
				RISP: "30m",
				EC2:  "2m",
			},
			wantErr: false,
		},
		{
			name: "empty intervals (use defaults)",
			reconciliation: ReconciliationConfig{
				RISP: "",
				EC2:  "",
			},
			wantErr: false,
		},
		{
			name: "invalid RISP interval",
			reconciliation: ReconciliationConfig{
				RISP: "invalid",
			},
			wantErr: true,
			errMsg:  "invalid RISP reconciliation interval",
		},
		{
			name: "invalid EC2 interval",
			reconciliation: ReconciliationConfig{
				EC2: "not-a-duration",
			},
			wantErr: true,
			errMsg:  "invalid EC2 reconciliation interval",
		},
		{
			name: "negative RISP interval",
			reconciliation: ReconciliationConfig{
				RISP: "-1h",
			},
			wantErr: false, // time.ParseDuration accepts negative values
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				AWSAccounts: []AWSAccount{
					{
						AccountID:     "123456789012",
						Name:          "test-account",
						AssumeRoleARN: "arn:aws:iam::123456789012:role/test-role",
					},
				},
				Reconciliation: tt.reconciliation,
			}
			err := cfg.Validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("Validate() expected error containing %q, got nil", tt.errMsg)
					return
				}
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Validate() error = %q, want error containing %q", err.Error(), tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("Validate() unexpected error: %v", err)
				}
			}
		})
	}
}
