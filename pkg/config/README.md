# Configuration Package

Controller configuration management with YAML loading and validation.

## Quick Start

```go
cfg, _ := config.Load("/etc/lumina/config.yaml")
for _, account := range cfg.AWSAccounts {
    fmt.Printf("Account: %s (%s)\n", account.Name, account.AccountID)
}
```

## Configuration

See [config.example.yaml](../../config.example.yaml) for a complete example.

### Environment Variables

- `LUMINA_CONFIG_PATH` - Override config file path
- `LUMINA_DEFAULT_REGION` - Override default AWS region
- `LUMINA_LOG_LEVEL` - Override log level (debug/info/warn/error)
- `LUMINA_METRICS_BIND_ADDRESS` - Override metrics endpoint address
- `LUMINA_HEALTH_PROBE_BIND_ADDRESS` - Override health probe address
- `LUMINA_ACCOUNT_VALIDATION_INTERVAL` - Override validation interval

### Validation

The config loader automatically validates:
- Account IDs (must be 12 digits)
- IAM role ARNs (correct format)
- ARN account ID matches configured account ID
- No duplicate account IDs
- Valid log levels

## Status

- ✅ YAML loading with gopkg.in/yaml.v3
- ✅ Environment variable overrides
- ✅ Comprehensive validation
- ✅ 100% test coverage

See code documentation for detailed usage.
