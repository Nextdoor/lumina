# Lumina

> Illuminate Kubernetes costs with real-time AWS Savings Plans visibility

Lumina is a Kubernetes controller that provides real-time cost visibility for EC2 instances by tracking AWS Savings Plans, Reserved Instances, and spot pricing across your entire AWS organization.

## Overview

Lumina exposes Prometheus metrics showing the actual hourly cost of each Kubernetes node, taking into account:
- AWS Savings Plans (EC2 Instance and Compute)
- Reserved Instances
- Spot pricing (current market rates)
- On-demand pricing

This enables cost-aware capacity management, chargeback, and cost optimization for Kubernetes workloads.

## Status

**Beta** - Core functionality complete and tested. Fargate support planned.

## Features

- ✅ Kubernetes controller with health probes and metrics endpoint
- ✅ Multi-account AWS cross-account access via AssumeRole
- ✅ Reserved Instance & Savings Plans discovery across all accounts/regions
- ✅ Savings Plans rate calculation with automatic refresh
- ✅ EC2 instance inventory and cost calculation
- ✅ Kubernetes Node correlation (instance ID → node name mapping)
- ✅ Spot price tracking with lazy-loading for running instances
- ✅ Real-time cost metrics with Savings Plans utilization tracking
- ⏳ Fargate support (planned)
- ⏳ Cost trend analysis and forecasting (planned)

## Architecture

Lumina runs as a Kubernetes controller in each cluster and:

1. **Discovers** all AWS Savings Plans and Reserved Instances across your organization
2. **Tracks** all EC2 instances in real-time (5-minute refresh)
3. **Correlates** EC2 instances with Kubernetes nodes via provider ID
4. **Calculates** effective costs per instance using AWS's Savings Plans allocation algorithm
5. **Exposes** Prometheus metrics for monitoring and alerting

Note: Fargate support is planned but not yet implemented.

> **📖 See [ALGORITHM.md](ALGORITHM.md) for detailed documentation of cost calculation algorithms, known limitations, and differences from AWS billing.**

```
┌─────────────────────────────────────┐
│   Kubernetes Cluster                │
│  ┌──────────────┐   ┌────────────┐ │
│  │   Lumina     │──▶│ Prometheus │ │
│  │  Controller  │   └────────────┘ │
│  └──────────────┘                   │
└────────┬────────────────────────────┘
         │ AssumeRole + Query APIs
         ▼
┌─────────────────────────────────────┐
│   AWS Organization (All Accounts)   │
│  • EC2 Instances                    │
│  • Reserved Instances               │
│  • Savings Plans                    │
│  • Spot Prices                      │
└─────────────────────────────────────┘
```

## Quick Start

### Prerequisites

- Go 1.24+
- Kubernetes cluster (1.30+)
- AWS credentials with cross-account access
- kubebuilder 4.10+ (for development)

### Development

```bash
# Clone the repository
git clone https://github.com/nextdoor/lumina.git
cd lumina

# Run tests
make test

# Run the controller locally (requires kubeconfig)
make run

# Build container image
make docker-build IMG=lumina-controller:dev
```

### Deployment

#### Using Pre-built Images

Container images are automatically built and published to GitHub Container Registry on every commit to main and on releases:

```bash
# Deploy latest stable release
make deploy IMG=ghcr.io/nextdoor/lumina:latest

# Deploy specific version
make deploy IMG=ghcr.io/nextdoor/lumina:v0.1.0

# View logs
kubectl logs -n lumina-system deployment/lumina-controller-manager
```

**Available image tags:**
- `latest` - Latest build from main branch
- `main` - Latest build from main branch (same as latest)
- `v*.*.*` - Semantic version releases (e.g., v0.1.0, v0.2.1)
- `sha-<commit>` - Specific commit builds

Images are built for both `linux/amd64` and `linux/arm64` platforms.

#### Building Custom Images

```bash
# Build and load into local Kind cluster
make docker-build docker-push IMG=lumina-controller:dev
make deploy IMG=lumina-controller:dev
```

## Configuration

Lumina is configured via a ConfigMap in the `lumina-system` namespace. See `config/manager/lumina-config.yaml` for the full example.

### AWS Account Configuration

```yaml
accounts:
  - name: production
    accountId: "123456789012"
    roleArn: arn:aws:iam::123456789012:role/LuminaReadOnly
    regions:
      - us-west-2
      - us-east-1
  - name: staging
    accountId: "987654321098"
    roleArn: arn:aws:iam::987654321098:role/LuminaReadOnly
    regions:
      - us-west-2
```

### IAM Permissions Required

Lumina requires read-only access to:
- `ec2:DescribeInstances`
- `ec2:DescribeReservedInstances`
- `ec2:DescribeSpotPriceHistory`
- `savingsplans:DescribeSavingsPlans`
- `savingsplans:DescribeSavingsPlansOfferingRates`
- `pricing:GetProducts`

See `config/iam/lumina-readonly-policy.json` for a sample IAM policy.

### Reconciliation Intervals

```yaml
reconciliation:
  pricing: 24h        # On-demand pricing (AWS Pricing API)
  risp: 1h            # Reserved Instances & Savings Plans
  ec2: 5m             # EC2 instance inventory
  cost: event-driven  # Cost calculations (triggered by cache updates)
  sp_rates: 15s       # Savings Plans rates
  spot: 15s           # Spot pricing (lazy-loading)
```

## Metrics

Lumina exposes Prometheus metrics on port 8080 at `/metrics`. All metrics include comprehensive labels for filtering and aggregation.

### Cost Metrics

**`ec2_instance_hourly_cost`** - Estimated hourly cost per EC2 instance
- Labels: `instance_id`, `instance_type`, `account_id`, `account_name`, `region`, `availability_zone`, `node_name`, `lifecycle` (on-demand/spot), `cost_category` (on_demand/reserved_instance/savings_plan/spot)
- Value: Hourly cost in USD

### Savings Plans Utilization Metrics

**`savings_plan_current_utilization_rate`** - Current SP utilization in $/hour
- Labels: `sp_arn`, `sp_type`, `account_id`, `account_name`
- Value: Current hourly commitment usage

**`savings_plan_commitment_amount`** - Total SP hourly commitment in $/hour
- Labels: `sp_arn`, `sp_type`, `account_id`, `account_name`
- Value: Hourly commitment amount

**`savings_plan_remaining_capacity`** - Unused SP capacity in $/hour
- Labels: `sp_arn`, `sp_type`, `account_id`, `account_name`
- Value: Remaining hourly capacity

**`savings_plan_utilization_percent`** - SP utilization as a percentage (0-100)
- Labels: `sp_arn`, `sp_type`, `account_id`, `account_name`
- Value: Utilization percentage

### Inventory Metrics

**`ec2_instance`** - EC2 instance inventory (gauge = 1 per instance)
- Labels: `instance_id`, `instance_type`, `state`, `account_id`, `account_name`, `region`, `availability_zone`

**`ec2_instance_count`** - Count of EC2 instances by state
- Labels: `account_id`, `account_name`, `region`, `state`

**`ec2_running_instance_count`** - Count of running EC2 instances
- Labels: `account_id`, `account_name`, `region`

### Data Freshness Metrics

**`data_freshness_seconds`** - Age of cached data in seconds
- Labels: `data_type` (pricing/reserved_instances/savings_plans/ec2_instances/sp_rates/spot_pricing), `account_id`, `account_name`, `region`

**`data_last_success`** - Success status of last data collection (1 = success, 0 = failure)
- Labels: `data_type`, `account_id`, `account_name`, `region`

### Example PromQL Queries

```promql
# Total hourly cost across all instances
sum(ec2_instance_hourly_cost)

# Cost breakdown by account
sum by (account_name) (ec2_instance_hourly_cost)

# Savings Plans utilization by type
sum by (sp_type) (savings_plan_utilization_percent) / count by (sp_type) (savings_plan_utilization_percent)

# Running instance count by region
sum by (region) (ec2_running_instance_count)

# Cost per node (requires node_name label)
ec2_instance_hourly_cost{node_name!=""}
```

## Testing

```bash
# Unit tests
make test

# Coverage report
make cover

# HTML coverage report
make coverhtml

# E2E tests (requires Kind cluster)
make test-e2e
```

## Contributing

This project will be open-sourced. Contributions are welcome!

### Development Principles

1. **100% code coverage required** - see [CLAUDE.md](CLAUDE.md)
2. **Integration tests are critical** - test realistic scenarios
3. **No internal references** - code must be ready for public release
4. **Conventional commits** - use `type(component): description` format

## Local Development

### Prerequisites

```bash
# Install GoReleaser locally via Make (recommended)
make goreleaser

# Or install system-wide with brew
# brew install goreleaser
```

### Building Locally

```bash
# Build everything using GoReleaser (matches CI exactly)
bin/goreleaser build --snapshot --clean

# Binaries will be in: dist/manager_linux_amd64_v1/manager
# Or: dist/manager_linux_arm64/manager (depending on your platform)

# Build Docker images using GoReleaser
IMG=lumina:dev bin/goreleaser release --snapshot --clean

# This creates:
# - Multi-arch binaries in dist/
# - Docker image tagged as lumina:dev
```

### Running Locally

```bash
# Run controller against current kubeconfig context
make run

# Run with debug logging
make run ARGS="--zap-log-level=debug"
```

### Loading into Kind

```bash
# Build image with GoReleaser
IMG=lumina:dev bin/goreleaser release --snapshot --clean

# Load into Kind cluster
kind load docker-image lumina:dev

# Deploy to Kind
make deploy IMG=lumina:dev
```

## Documentation

- [ALGORITHM.md](ALGORITHM.md) - **Cost calculation algorithms, limitations, and known differences from AWS billing**
- [CLAUDE.md](CLAUDE.md) - Project coding guidelines
- [pkg/aws/README.md](pkg/aws/README.md) - AWS client package documentation

## License

Apache 2.0 (to be confirmed)

## Credits

Built by the Platform Engineering team. Powered by [controller-runtime](https://github.com/kubernetes-sigs/controller-runtime).
