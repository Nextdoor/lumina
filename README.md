# Lumina

> Illuminate Kubernetes costs with real-time AWS Savings Plans visibility

Lumina is a Kubernetes controller that provides real-time cost visibility for EC2 instances and Fargate pods by tracking AWS Savings Plans, Reserved Instances, and spot pricing across your entire AWS organization.

## Overview

Lumina exposes Prometheus metrics showing the actual hourly cost of each Kubernetes node, taking into account:
- AWS Savings Plans (EC2 Instance and Compute)
- Reserved Instances
- Spot pricing (current market rates)
- On-demand pricing

This enables cost-aware capacity management, chargeback, and cost optimization for Kubernetes workloads.

## Status

**Active Development** - This project is in early development.

## Features (Planned)

- ✅ Kubernetes controller scaffold (Phase 1)
- ⏳ AWS account cross-account access (Phase 1)
- ⏳ Reserved Instance & Savings Plans discovery (Phase 2-3)
- ⏳ EC2 instance cost calculation (Phase 4-7)
- ⏳ Kubernetes Node correlation (Phase 8)
- ⏳ Spot price tracking (Phase 9)
- ⏳ Fargate support (Phase 10)
- ⏳ Cost reconciliation (Phase 11-12)

## Architecture

Lumina runs as a Kubernetes controller in each cluster and:

1. **Discovers** all AWS Savings Plans and Reserved Instances across your organization
2. **Tracks** all EC2 instances and Fargate pods in real-time (5-minute refresh)
3. **Calculates** effective costs per instance using AWS's Savings Plans allocation algorithm
4. **Exposes** Prometheus metrics for monitoring and alerting

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

```bash
# Deploy to cluster
make deploy IMG=lumina-controller:latest

# View logs
kubectl logs -n lumina-system deployment/lumina-controller-manager
```

## Configuration

Lumina requires IAM permissions to assume roles in all AWS accounts. See [docs/iam-setup.md](docs/iam-setup.md) for details.

Example configuration:

```yaml
awsAccounts:
  - accountId: "111111111111"
    name: "Production"
    assumeRoleArn: "arn:aws:iam::111111111111:role/lumina-cost-controller"
  - accountId: "222222222222"
    name: "Staging"
    assumeRoleArn: "arn:aws:iam::222222222222:role/lumina-cost-controller"
```

## Metrics

Lumina exposes the following Prometheus metrics:

### Node Costs
```
# Estimated hourly cost with all discounts applied
ec2_node_hourly_cost_estimate{instance_id, node_name, instance_type, lifecycle, provisioner}

# Full on-demand price (no discounts)
ec2_node_hourly_cost_shelf_price{instance_id, node_name, instance_type}

# Cost breakdown by source (RI, SP, OnDemand, Spot)
ec2_node_cost_source{instance_id, node_name, source}
```

### Savings Plans Utilization
```
# Fixed hourly commitment amount
savings_plan_hourly_commitment{savings_plan_arn, type, account_id}

# Current utilization rate ($/hour)
savings_plan_current_utilization_rate{savings_plan_arn, type, account_id}

# Remaining capacity ($/hour)
savings_plan_remaining_capacity{savings_plan_arn, type, account_id}

# Utilization percentage
savings_plan_utilization_percent{savings_plan_arn, type, account_id}
```

See [docs/metrics.md](docs/metrics.md) for complete metric reference.

## Testing

Lumina uses a multi-tier testing strategy:

### Unit Tests
```bash
make test
```

### Integration Tests (with fake AWS backend)
```bash
make test-integration
```

### E2E Tests (requires cluster)
```bash
make test-e2e
```

## Contributing

This project will be open-sourced. Contributions are welcome!

### Development Principles

1. **100% code coverage required** - see [CLAUDE.md](CLAUDE.md)
2. **Integration tests are critical** - test realistic scenarios
3. **No internal references** - code must be ready for public release
4. **Conventional commits** - use `type(component): description` format

### Running Locally

```bash
# Install dependencies
go mod download

# Run controller against current kubeconfig context
make run

# Run with debug logging
make run ARGS="--zap-log-level=debug"
```

## Documentation

- [CLAUDE.md](CLAUDE.md) - Project coding guidelines
- [docs/architecture.md](docs/architecture.md) - Architecture deep-dive
- [docs/metrics.md](docs/metrics.md) - Complete metrics reference
- [docs/iam-setup.md](docs/iam-setup.md) - AWS IAM configuration

## License

Apache 2.0 (to be confirmed)

## Credits

Built by the Platform Engineering team. Powered by [controller-runtime](https://github.com/kubernetes-sigs/controller-runtime).
