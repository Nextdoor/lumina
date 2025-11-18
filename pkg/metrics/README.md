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

### Data Freshness

**`lumina_data_freshness_seconds`** (gauge)
- Unix timestamp of last successful data collection
- Labels: `account_id`, `region`, `data_type`
- Values: Unix timestamp (seconds since epoch)
- Data types: `ec2_instances`, `reserved_instances`, `savings_plans`, `pricing`
- Use: Calculate data staleness with `time() - lumina_data_freshness_seconds`

**`lumina_data_last_success`** (gauge)
- Last data collection success indicator
- Labels: `account_id`, `region`, `data_type`
- Values: 1 = success, 0 = failed
- Use: Alert on collection failures

### Reserved Instances (Phase 3)

**`ec2_reserved_instance`** (gauge)
- Indicates presence of a Reserved Instance
- Labels: `account_id`, `region`, `instance_type`, `availability_zone`
- Value: 1 = RI exists, metric absent = RI does not exist
- Use: Track RI inventory, identify specific RIs

**`ec2_reserved_instance_count`** (gauge)
- Count of Reserved Instances by instance family
- Labels: `account_id`, `region`, `instance_family`
- Value: Number of RIs in this family
- Use: High-level RI inventory view, capacity planning

### Savings Plans Inventory (Phase 3)

**`savings_plan_hourly_commitment`** (gauge)
- Fixed hourly commitment amount ($/hour) for a Savings Plan
- Labels: `savings_plan_arn`, `account_id`, `type`, `region`, `instance_family`
- Value: Commitment amount in dollars per hour
- Use: Track SP inventory, identify commitments

**`savings_plan_remaining_hours`** (gauge)
- Number of hours remaining until Savings Plan expires
- Labels: `savings_plan_arn`, `account_id`, `type`
- Value: Hours until expiration
- Use: Alert on upcoming expirations for renewal planning

**Label Values:**
- `type`: `ec2_instance` or `compute`
- `region`: Specific region (e.g., `us-west-2`) for EC2 Instance SPs, `all` for Compute SPs
- `instance_family`: Specific family (e.g., `m5`) for EC2 Instance SPs, `all` for Compute SPs

**Note:** SP utilization metrics (current usage, remaining capacity, utilization %) will be added in Phase 6 after cost calculation is implemented.

### EC2 Instance Inventory (Phase 5)

**`ec2_instance`** (gauge)
- Indicates presence of a running EC2 instance
- Labels: `account_id`, `region`, `instance_type`, `availability_zone`, `instance_id`
- Value: 1 = instance exists and is running, metric absent = instance stopped or terminated
- Use: Track specific instance inventory, monitor fleet composition

**`ec2_instance_count`** (gauge)
- Count of running instances by instance family
- Labels: `account_id`, `region`, `instance_family`
- Value: Number of running instances in this family
- Use: High-level capacity planning, aggregate fleet view

**`ec2_running_instance_count`** (gauge)
- Total count of running instances
- Labels: `account_id`, `region`
- Value: Total number of running instances
- Use: Fleet-wide capacity tracking, cost forecasting

**Notes:**
- Only running instances are included in metrics (stopped instances don't incur compute costs)
- Metrics are updated every 5 minutes by the EC2 reconciler
- Instance family is extracted from instance type (e.g., `m5.xlarge` → `m5`)

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

### Updating RI Metrics (Phase 3)

```go
// Called by RISP reconciler after cache update (hourly)
allRIs := rispCache.GetAllReservedInstances()
m.UpdateReservedInstanceMetrics(allRIs)
```

This function:
- Resets all existing RI metrics (clean slate approach)
- Sets new values for all currently active RIs
- Automatically removes metrics for expired/deleted RIs
- Aggregates counts by instance family

### Updating SP Inventory Metrics (Phase 3)

```go
// Called by RISP reconciler after cache update (hourly)
allSPs := rispCache.GetAllSavingsPlans()
m.UpdateSavingsPlansInventoryMetrics(allSPs)
```

This function:
- Resets all existing SP metrics (clean slate approach)
- Sets new values for all currently active SPs
- Automatically removes metrics for expired/deleted SPs
- Calculates remaining hours until expiration
- Handles EC2 Instance vs Compute SP type differences

### Updating EC2 Instance Metrics (Phase 5)

```go
// Called by EC2 reconciler after cache update (every 5 minutes)
runningInstances := ec2Cache.GetRunningInstances()
m.UpdateEC2InstanceMetrics(runningInstances)
```

This function:
- Resets all existing EC2 metrics (clean slate approach)
- Sets new values for all currently running instances
- Automatically removes metrics for stopped/terminated instances
- Aggregates counts by instance family
- Aggregates counts by account+region

## Example Prometheus Queries

```promql
# Alert if controller is down
absent(lumina_controller_running{cluster="prod-us1"})

# Alert if any account validation is failing
lumina_account_validation_status == 0

# Alert if account hasn't been validated in 10 minutes
time() - lumina_account_validation_last_success_timestamp > 600

# Alert if data collection is stale (>10 minutes old)
time() - lumina_data_freshness_seconds > 600

# Data staleness by data type
time() - lumina_data_freshness_seconds

# Alert if pricing data is stale (>1 hour)
time() - lumina_data_freshness_seconds{data_type="pricing"} > 3600

# Alert if EC2 instance data is stale (>10 minutes)
time() - lumina_data_freshness_seconds{data_type="ec2_instances"} > 600

# Alert if any data collection is failing
lumina_data_last_success == 0

# Average validation time per account
rate(lumina_account_validation_duration_seconds_sum[5m])
  / rate(lumina_account_validation_duration_seconds_count[5m])

# P95 validation latency
histogram_quantile(0.95,
  rate(lumina_account_validation_duration_seconds_bucket[5m]))

# Total RI count across all accounts
count(ec2_reserved_instance)

# RI count by account
sum by (account_id) (ec2_reserved_instance)

# RI count by instance family
sum by (instance_family) (ec2_reserved_instance_count)

# Total RIs in m5 family across all accounts
sum(ec2_reserved_instance_count{instance_family="m5"})

# Alert if RI count drops unexpectedly (possible expiration)
rate(ec2_reserved_instance_count[1h]) < -5

# Total SP commitment across all accounts ($/hour)
sum(savings_plan_hourly_commitment)

# SP commitment by account
sum by (account_id) (savings_plan_hourly_commitment)

# SP commitment by type
sum by (type) (savings_plan_hourly_commitment)

# Total Compute SP commitment (global)
sum(savings_plan_hourly_commitment{type="compute"})

# EC2 Instance SPs by region and family
sum by (region, instance_family) (savings_plan_hourly_commitment{type="ec2_instance"})

# SPs expiring within 30 days (720 hours)
count(savings_plan_remaining_hours < 720)

# SPs expiring within 7 days (168 hours) - critical
count(savings_plan_remaining_hours < 168)

# SP commitment expiring within 30 days
sum(savings_plan_hourly_commitment and savings_plan_remaining_hours < 720)

# Total running instances across all accounts
sum(ec2_running_instance_count)

# Running instances by account
sum by (account_id) (ec2_running_instance_count)

# Running instances by region
sum by (region) (ec2_running_instance_count)

# Instance count by family
sum by (instance_family) (ec2_instance_count)

# Top 10 instance families by count
topk(10, sum by (instance_family) (ec2_instance_count))

# Specific instance type count
count(ec2_instance{instance_type="m5.xlarge"})

# Instances in a specific AZ
count(ec2_instance{availability_zone="us-west-2a"})

# Instance count by account and family
sum by (account_id, instance_family) (ec2_instance_count)

# Alert: Large instance count change (more than 10 instances/min)
abs(rate(ec2_running_instance_count[5m])) > 10

# Alert: Fleet size drops below threshold
sum(ec2_running_instance_count) < 100

# Instance type diversity (number of different instance types)
count by (account_id) (count by (account_id, instance_type) (ec2_instance))
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
