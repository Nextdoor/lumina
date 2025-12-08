# Lumina Metrics Package

Prometheus metrics for the Lumina cost visibility controller.

## Overview

This package provides operational metrics that enable monitoring and alerting for the Lumina controller. Metrics are exposed via the controller's `/metrics` endpoint and follow Prometheus naming conventions.

## Programmatic Access

For external tools that need to query Lumina metrics programmatically, this package exports both metric name and label name constants:

```go
import "github.com/nextdoor/lumina/pkg/metrics"

// Type-safe metric name reference with compile-time checking
query := fmt.Sprintf("sum(%s)", metrics.MetricEC2InstanceCount)

// Type-safe label references
query := fmt.Sprintf("%s{%s=\"us-west-2\", %s=\"m5\"}",
    metrics.MetricEC2InstanceCount,
    metrics.LabelRegion,
    metrics.LabelInstanceFamily)
```

**Benefits:**
- Compile-time checking (typos caught immediately)
- IDE autocomplete for available metrics
- Refactoring safety (renaming updates all consumers)
- Self-documenting via godoc

**Available constants:**
- **Metric names:** See [names.go](names.go) - `MetricEC2InstanceCount`, `MetricSavingsPlanUtilizationPercent`, etc.
- **Label names:** See [labels.go](labels.go) - `LabelRegion`, `LabelInstanceType`, `LabelAccountID`, etc.

## Label Customization

Many metric labels can be customized via configuration to match your organization's conventions:

**Configurable Labels:**
- `account_id` - AWS account ID (default: `account_id`)
- `account_name` - AWS account name (default: `account_name`)
- `region` - AWS region (default: `region`)
- `node_name` - Kubernetes node name (default: `node_name`)
- `cluster_name` - Kubernetes cluster name (default: `cluster_name`)
- `host_name` - EC2 instance hostname (default: `host_name`)

**Configuration Example:**
```yaml
metrics:
  labels:
    accountId: aws_account_id
    accountName: aws_account
    region: aws_region
    nodeName: k8s_node
    clusterName: k8s_cluster
    hostName: ec2_hostname
```

**Non-Configurable Labels:**
These labels are fixed and cannot be customized:
- `instance_id`, `instance_type`, `instance_family`, `availability_zone`, `tenancy`, `platform`
- `lifecycle`, `cost_type`, `pricing_accuracy`
- `savings_plan_arn`, `type`, `data_type`

**Note:** Throughout this documentation, default label names are shown. If you customize labels in your configuration, replace the label names in example queries accordingly.

## Multi-Cluster Deployments

When multiple Lumina instances report to a shared Prometheus endpoint, configure them to prevent metric duplication:

**Management Cluster** (emits all metrics):
```yaml
metrics:
  disableInstanceMetrics: false  # Default - emit all instance metrics
```

**Worker Clusters** (emit only aggregate metrics):
```yaml
metrics:
  disableInstanceMetrics: true  # Prevent duplication
```

When `disableInstanceMetrics: true`:
- **Disabled:** `ec2_instance`, `ec2_instance_count`, `ec2_instance_hourly_cost`
- **Enabled:** All other metrics (Savings Plans, Reserved Instances, controller health, data freshness)

This prevents duplication because each Lumina instance discovers all EC2 instances across configured AWS accounts, not just instances in its own cluster.

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
- Age of cached data in seconds since last successful update (auto-updated every second)
- Labels: `account_id`, `account_name`, `region`, `data_type`
- Values: Age in seconds (e.g., 60 = data is 60 seconds old)
- Data types: `ec2_instances`, `reserved_instances`, `savings_plans`, `pricing`, `sp_rates`, `spot_pricing`
- Use: Direct alerting on stale data (e.g., `lumina_data_freshness_seconds > 600` alerts if data is older than 10 minutes)

**`lumina_data_last_success`** (gauge)
- Last data collection success indicator
- Labels: `account_id`, `account_name`, `region`, `data_type`
- Values: 1 = success, 0 = failed
- Use: Alert on collection failures

### Reserved Instances

**`ec2_reserved_instance`** (gauge)
- Indicates presence of a Reserved Instance
- Labels: `account_id`, `account_name`, `region`, `instance_type`, `availability_zone`
- Value: 1 = RI exists, metric absent = RI does not exist
- Use: Track RI inventory, identify specific RIs

**`ec2_reserved_instance_count`** (gauge)
- Count of Reserved Instances by instance family
- Labels: `account_id`, `account_name`, `region`, `instance_family`
- Value: Number of RIs in this family
- Use: High-level RI inventory view, capacity planning

### Savings Plans Inventory

**`savings_plan_hourly_commitment`** (gauge)
- Fixed hourly commitment amount ($/hour) for a Savings Plan
- Labels: `savings_plan_arn`, `account_id`, `account_name`, `type`, `region`, `instance_family`
- Value: Commitment amount in dollars per hour
- Use: Track SP inventory, identify commitments

**`savings_plan_remaining_hours`** (gauge)
- Number of hours remaining until Savings Plan expires
- Labels: `savings_plan_arn`, `account_id`, `account_name`, `type`
- Value: Hours until expiration
- Use: Alert on upcoming expirations for renewal planning

**Label Values:**
- `type`: `ec2_instance` or `compute`
- `region`: Specific region (e.g., `us-west-2`) for EC2 Instance SPs, empty string for Compute SPs
- `instance_family`: Specific family (e.g., `m5`) for EC2 Instance SPs, empty string for Compute SPs

### Savings Plans Utilization

**`savings_plan_current_utilization_rate`** (gauge)
- Current hourly rate ($/hour) being consumed by instances covered by this Savings Plan
- Labels: `savings_plan_arn`, `account_id`, `account_name`, `type`
- Value: Current utilization in dollars per hour
- Use: Monitor real-time SP usage

**`savings_plan_remaining_capacity`** (gauge)
- Unused capacity in $/hour for a Savings Plan
- Calculated as: HourlyCommitment - CurrentUtilizationRate
- Labels: `savings_plan_arn`, `account_id`, `account_name`, `type`
- Value: Remaining capacity (negative if over-utilized)
- Use: Alert on under-utilization (wasted money) or over-utilization (spillover to on-demand)

**`savings_plan_utilization_percent`** (gauge)
- Utilization percentage of a Savings Plan
- Calculated as: (CurrentUtilizationRate / HourlyCommitment) * 100
- Labels: `savings_plan_arn`, `account_id`, `account_name`, `type`
- Value: Utilization percentage (can exceed 100%)
- Use: Dashboard visualization, alerting on utilization thresholds

### EC2 Instance Inventory

**`ec2_instance`** (gauge)
- Indicates presence of a running EC2 instance
- Labels: `account_id`, `account_name`, `region`, `instance_type`, `availability_zone`, `instance_id`, `tenancy`, `platform`
- Value: 1 = instance exists and is running, metric absent = instance stopped or terminated
- Use: Track specific instance inventory, monitor fleet composition

**`ec2_instance_count`** (gauge)
- Count of running instances by instance family
- Labels: `account_id`, `account_name`, `region`, `instance_family`
- Value: Number of running instances in this family
- Use: High-level capacity planning, aggregate fleet view

**Notes:**
- Only running instances are included in metrics (stopped instances don't incur compute costs)
- Metrics are updated every 5 minutes by the EC2 reconciler
- Instance family is extracted from instance type (e.g., `m5.xlarge` → `m5`)

### EC2 Instance Costs

**`ec2_instance_hourly_cost`** (gauge)
- Effective hourly cost for each EC2 instance after applying all discounts
- Labels: `instance_id`, `account_id`, `account_name`, `region`, `instance_type`, `cost_type`, `availability_zone`, `lifecycle`, `pricing_accuracy`, `node_name`
- Value: Hourly cost in USD
- Use: Per-instance cost tracking, chargeback, cost optimization

**Label Values:**
- `cost_type`: `on_demand`, `reserved_instance`, `ec2_instance_savings_plan`, `compute_savings_plan`, or `spot`
- `lifecycle`: `on-demand` or `spot`
- `pricing_accuracy`: `accurate` (from API) or `estimated` (from fallback calculations)
- `node_name`: Kubernetes node name (empty if instance is not correlated to a node)

**Notes:**
- This metric represents the actual cost you pay, including all discounts
- Cost calculations run event-driven when cache data updates
- Costs are updated approximately every 5 minutes (driven by EC2 reconciliation)

## Usage

### Initialization

```go
import (
    ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
    "github.com/nextdoor/lumina/pkg/metrics"
    "github.com/nextdoor/lumina/pkg/config"
)

// Load configuration
cfg, err := config.Load("/etc/lumina/config.yaml")
if err != nil {
    log.Fatal(err)
}

// Initialize metrics at controller startup
// Pass config to enable label customization and multi-cluster settings
m := metrics.NewMetrics(ctrlmetrics.Registry, cfg)
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

### Updating RI Metrics

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

### Updating SP Inventory Metrics

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

### Updating EC2 Instance Metrics

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

### Updating Instance Cost Metrics

```go
// Called by cost calculator after calculation (event-driven)
result := calculator.Calculate(input)
m.UpdateInstanceCostMetrics(result, nodeCache)
```

This function:
- Resets all existing cost metrics (clean slate approach)
- Sets new cost values for all instances
- Updates Savings Plans utilization metrics
- Correlates instances with Kubernetes nodes (if nodeCache provided)

## Example Prometheus Queries

### Controller Health
```promql
# Alert if controller is down
absent(lumina_controller_running{cluster="prod-us1"})
```

### Account Validation
```promql
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

### Data Freshness
```promql
# Alert if data collection is stale (>10 minutes old)
lumina_data_freshness_seconds > 600

# Data staleness by data type (in seconds)
lumina_data_freshness_seconds

# Alert if pricing data is stale (>1 hour)
lumina_data_freshness_seconds{data_type="pricing"} > 3600

# Alert if EC2 instance data is stale (>10 minutes)
lumina_data_freshness_seconds{data_type="ec2_instances"} > 600

# Alert if any data collection is failing
lumina_data_last_success == 0
```

### Reserved Instances
```promql
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
```

### Savings Plans Inventory
```promql
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
```

### Savings Plans Utilization
```promql
# SP utilization percentage by account
sum by (account_id) (savings_plan_utilization_percent)
  / count by (account_id) (savings_plan_utilization_percent)

# Alert on under-utilized SPs (< 80%)
savings_plan_utilization_percent < 80

# Alert on over-utilized SPs (> 100%, spillover to on-demand)
savings_plan_utilization_percent > 100

# Wasted SP capacity (under-utilized, in $/hour)
sum(savings_plan_remaining_capacity{} > 0)

# Compute vs EC2 Instance SP utilization comparison
sum by (type) (savings_plan_utilization_percent)
  / count by (type) (savings_plan_utilization_percent)
```

### EC2 Instance Inventory
```promql
# Total running instances across all accounts
sum(ec2_instance_count)

# Running instances by account
sum by (account_id) (ec2_instance_count)

# Running instances by region
sum by (region) (ec2_instance_count)

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
abs(rate(ec2_instance_count[5m])) > 10

# Alert: Fleet size drops below threshold
sum(ec2_instance_count) < 100

# Instance type diversity (number of different instance types)
count by (account_id) (count by (account_id, instance_type) (ec2_instance))
```

### Instance Costs
```promql
# Total hourly cost across all instances
sum(ec2_instance_hourly_cost)

# Cost by account
sum by (account_id) (ec2_instance_hourly_cost)

# Cost by region
sum by (region) (ec2_instance_hourly_cost)

# Cost by cost type (on-demand vs discounted)
sum by (cost_type) (ec2_instance_hourly_cost)

# Cost per Kubernetes node
sum by (node_name) (ec2_instance_hourly_cost{node_name!=""})

# Savings from Savings Plans
sum(ec2_instance_hourly_cost{cost_type=~".*savings_plan"})

# On-demand cost (no discounts)
sum(ec2_instance_hourly_cost{cost_type="on_demand"})

# Spot instance cost
sum(ec2_instance_hourly_cost{cost_type="spot"})

# Cost breakdown by instance type
sum by (instance_type) (ec2_instance_hourly_cost)

# Top 10 most expensive instances
topk(10, ec2_instance_hourly_cost)

# Alert: High-cost instance running
ec2_instance_hourly_cost > 5
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
