# Lumina Metrics Package

Prometheus metrics for the Lumina cost visibility controller.

## Overview

This package provides operational metrics that enable monitoring and alerting for the Lumina controller. Metrics are exposed via the controller's `/metrics` endpoint and follow Prometheus naming conventions.

## Metrics

### Controller Health

**`lumina_controller_running`** (gauge)
- Indicates whether the controller is running
- Value: 1 = running
- Use: Alert if metric disappears (controller crashed)

### Account Validation

**`lumina_account_validation_status`** (gauge)
- AWS account validation status per account
- Labels: `account_id`, `account_name`
- Values: 1 = success, 0 = failed
- Use: Alert on validation failures

**`lumina_account_validation_last_success_timestamp`** (gauge)
- Unix timestamp of last successful validation
- Labels: `account_id`, `account_name`
- Use: Alert if account hasn't been validated recently

**`lumina_account_validation_duration_seconds`** (histogram)
- Time taken to validate account access via AssumeRole
- Labels: `account_id`, `account_name`
- Buckets: 0.1s, 0.25s, 0.5s, 1s, 2.5s, 5s, 10s
- Use: Identify slow or timing-out accounts

### Data Freshness (Phase 2+)

**`lumina_data_freshness_seconds`** (gauge)
- Seconds since last successful data collection
- Labels: `account_id`, `region`, `data_type`
- Status: Structure defined, not yet populated

**`lumina_data_last_success`** (gauge)
- Last data collection success indicator
- Labels: `account_id`, `region`, `data_type`
- Values: 1 = success, 0 = failed
- Status: Structure defined, not yet populated

## Usage

### Initialization

```go
import (
    ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
    "github.com/nextdoor/lumina/pkg/metrics"
)

// Initialize metrics at controller startup
m := metrics.NewMetrics(ctrlmetrics.Registry)
m.ControllerRunning.Set(1)
```

### Recording Validations

```go
start := time.Now()
err := validator.ValidateAccount(ctx, accountID, roleARN)
duration := time.Since(start)

m.RecordAccountValidation(
    accountID,
    accountName,
    err == nil, // success
    duration,
)
```

### Cleanup

```go
// Remove metrics when account is deconfigured
m.DeleteAccountMetrics(accountID, accountName)
```

## Example Prometheus Queries

```promql
# Alert if controller is down
absent(lumina_controller_running{cluster="prod-us1"})

# Alert if any account validation is failing
lumina_account_validation_status == 0

# Alert if account hasn't been validated in 10 minutes
time() - lumina_account_validation_last_success_timestamp > 600

# Average validation time per account
rate(lumina_account_validation_duration_seconds_sum[5m])
  / rate(lumina_account_validation_duration_seconds_count[5m])

# P95 validation latency
histogram_quantile(0.95,
  rate(lumina_account_validation_duration_seconds_bucket[5m]))
```

## Testing

The package includes comprehensive unit tests with 100% coverage:

```bash
go test ./pkg/metrics/... -v
go test ./pkg/metrics/... -cover
```

## Implementation Notes

- Metrics are registered with controller-runtime's global registry
- Vec metrics (GaugeVec, HistogramVec) use label-based dimensions
- Metric names follow Prometheus conventions (snake_case, units as suffix)
- All metrics include detailed help text for documentation
