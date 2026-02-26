---
title: "Configuration"
description: "Complete configuration reference for the Lumina controller"
weight: 10
---

Lumina is configured via a YAML configuration file. When deployed with Helm, the configuration is managed via the `config` section in your Helm values. See also the [Helm Chart Reference]({{< relref "helm-chart" >}}).

## Full Configuration Example

```yaml
# AWS Accounts Configuration
awsAccounts:
  - accountId: "123456789012"
    name: "Production"
    assumeRoleArn: "arn:aws:iam::123456789012:role/lumina-controller"
    # region: "us-west-2"  # Optional: override default region for this account

  - accountId: "987654321098"
    name: "Staging"
    assumeRoleArn: "arn:aws:iam::987654321098:role/lumina-controller"

# Default Account for non-account-specific API calls (e.g., AWS Pricing API)
# If not specified, the first account in awsAccounts is used.
# defaultAccount:
#   accountId: "123456789012"
#   name: "Production"
#   assumeRoleArn: "arn:aws:iam::123456789012:role/lumina-controller"

# Default AWS region
defaultRegion: "us-west-2"

# Regions to query for Reserved Instances (can be overridden per-account)
regions:
  - "us-west-2"
  - "us-east-1"

# Log level: debug, info, warn, error
logLevel: "info"

# Metrics endpoint address
metricsBindAddress: ":8080"

# Health probe endpoint address
healthProbeBindAddress: ":8081"

# Account validation interval
accountValidationInterval: "10m"

# Reconciliation intervals
reconciliation:
  risp: "1h"           # Reserved Instances & Savings Plans
  ec2: "5m"            # EC2 instance inventory
  pricing: "24h"       # On-demand pricing (AWS Pricing API)
  spotPricing: "15s"   # Spot pricing (check for stale prices)

# Pricing configuration
pricing:
  operatingSystems:
    - "Linux"
    - "Windows"
  spotPriceCacheExpiration: "1h"
  # defaultDiscounts:
  #   ec2Instance: 0.72  # EC2 Instance SP multiplier (28% discount)
  #   compute: 0.72      # Compute SP multiplier (28% discount)

# Metrics configuration
metrics:
  disableInstanceMetrics: false
  # labels:
  #   clusterName: "cluster_name"
  #   accountName: "account_name"
  #   accountId: "account_id"
  #   region: "region"
  #   nodeName: "node_name"
  #   hostName: "host_name"
```

## AWS Account Configuration

Each AWS account requires:

| Field | Required | Description |
|-------|----------|-------------|
| `accountId` | Yes | 12-digit AWS account ID |
| `name` | Yes | Human-readable account name (used in metric labels) |
| `assumeRoleArn` | Yes | IAM role ARN to assume for API access |
| `region` | No | Override default region for this account |
| `regions` | No | Override global `regions` list for this account |

### Validation

The config loader automatically validates:
- Account IDs must be 12 digits
- IAM role ARNs must have correct format
- ARN account ID must match configured account ID
- No duplicate account IDs
- Valid log levels
- Valid duration formats for all intervals
- Valid operating systems in pricing config
- Valid Savings Plan discount multipliers (0-1 range)

## Reconciliation Intervals

| Setting | Default | Description |
|---------|---------|-------------|
| `reconciliation.risp` | `1h` | Reserved Instances and Savings Plans discovery |
| `reconciliation.ec2` | `5m` | EC2 instance inventory |
| `reconciliation.pricing` | `24h` | On-demand pricing from AWS Pricing API |
| `reconciliation.spotPricing` | `15s` | How often to check for stale spot prices |

### Spot Price Caching

Spot prices use two config values together:

- `reconciliation.spotPricing`: How often to CHECK for stale prices (default: 15s)
- `pricing.spotPriceCacheExpiration`: How old prices must be before considered stale (default: 1h)

With defaults, the reconciler checks every 15 seconds for prices older than 1 hour and refreshes them.

**Tuning advice:**
- Tight budget: shorter expiration (more accurate, more API calls)
- Loose budget: longer expiration (less accurate, fewer API calls)

## Metrics Configuration

### Disable Instance Metrics

For multi-cluster deployments with a shared Prometheus endpoint, set `disableInstanceMetrics: true` on worker clusters to prevent duplication.

When true, disables:
- `ec2_instance`
- `ec2_instance_count`
- `ec2_instance_hourly_cost`

Always enabled regardless:
- Savings Plans metrics (utilization, commitment, etc.)
- Reserved Instance metrics
- Controller health and data freshness metrics

### Label Customization

Customize metric label names to match your organization's conventions:

```yaml
metrics:
  labels:
    clusterName: "k8s_cluster"      # Default: cluster_name
    accountName: "aws_account"      # Default: account_name
    accountId: "aws_account_id"     # Default: account_id
    region: "aws_region"            # Default: region
    nodeName: "k8s_node"            # Default: node_name
    hostName: "ec2_hostname"        # Default: host_name
```

Only these labels can be customized. Non-configurable labels (`instance_type`, `availability_zone`, `lifecycle`, `cost_type`, etc.) remain fixed.

## Environment Variables

All environment variables override their corresponding config file values:

| Variable | Description |
|----------|-------------|
| `LUMINA_CONFIG_PATH` | Override config file path |
| `LUMINA_DEFAULT_REGION` | Override default AWS region |
| `LUMINA_LOG_LEVEL` | Override log level (debug/info/warn/error) |
| `LUMINA_METRICS_BIND_ADDRESS` | Override metrics endpoint address |
| `LUMINA_HEALTH_PROBE_BIND_ADDRESS` | Override health probe address |
| `LUMINA_ACCOUNT_VALIDATION_INTERVAL` | Override validation interval |
| `LUMINA_RECONCILIATION_RISP` | Override RISP reconciliation interval |
| `LUMINA_RECONCILIATION_EC2` | Override EC2 reconciliation interval |
| `LUMINA_RECONCILIATION_PRICING` | Override pricing reconciliation interval |
| `LUMINA_RECONCILIATION_SPOT_PRICING` | Override spot pricing reconciliation interval |

## Pricing Configuration

### Operating Systems

Configure which OS pricing data to load:

```yaml
pricing:
  operatingSystems:
    - "Linux"
    - "Windows"
```

Valid values: `Linux`, `Windows`, `RHEL`, `SUSE`. Use `["Linux"]` for Linux-only environments to reduce memory usage and startup time.

### Default Discount Multipliers

Fallback discount rates when actual SP rates are not yet cached (Tier 2 pricing):

```yaml
pricing:
  defaultDiscounts:
    ec2Instance: 0.72  # 28% discount (you pay 72%)
    compute: 0.72      # 28% discount (you pay 72%)
```

These are **multipliers** (what you pay), not discount percentages. Typical values:
- 1-year commitment: ~28% OFF, multiplier 0.72
- 3-year commitment: ~50% OFF, multiplier 0.50

## IAM Permissions Required

For IAM policy setup, including the required permissions and trust relationship, see the [Installation Guide]({{< relref "../getting-started/installation#iam-setup" >}}).
