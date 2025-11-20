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
- `LUMINA_RECONCILIATION_RISP` - Override RISP reconciliation interval
- `LUMINA_RECONCILIATION_EC2` - Override EC2 reconciliation interval
- `LUMINA_RECONCILIATION_PRICING` - Override pricing reconciliation interval
- `LUMINA_RECONCILIATION_SPOT_PRICING` - Override spot pricing reconciliation interval

### Reconciliation Intervals

The controller reconciles different data types at different intervals:

- **RISP** (Reserved Instances + Savings Plans): Default 1h - data changes infrequently
- **EC2** (Instance Inventory): Default 5m - instances change frequently (autoscaling)
- **Pricing** (On-Demand Prices): Default 24h - AWS pricing changes monthly
- **Spot Pricing** (Spot Price History): Default 15s - fast checks OK due to lazy-loading

### Spot Price Caching

Spot prices are cached and refreshed based on two config values:

- `reconciliation.spotPricing`: How often to CHECK for stale prices (default: 15s)
- `pricing.spotPriceCacheExpiration`: How old prices must be before stale (default: 1h)

**Example**: With `spotPriceCacheExpiration=1h` and `spotPricing=15s`, the reconciler checks every 15 seconds for prices older than 1 hour and refreshes them.

**Tuning advice**:
- Tight budget → shorter expiration (more accurate, more API calls)
- Loose budget → longer expiration (less accurate, fewer API calls)

### Validation

The config loader automatically validates:
- Account IDs (must be 12 digits)
- IAM role ARNs (correct format)
- ARN account ID matches configured account ID
- No duplicate account IDs
- Valid log levels
- Valid duration formats for all intervals
- Valid operating systems in pricing config
- Valid Savings Plan discount multipliers (0-1 range)