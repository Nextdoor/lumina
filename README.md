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

#### Using Helm (Recommended)

Helm charts are automatically published to the Nextdoor OSS Helm repository:

```bash
# Add the Lumina Helm repository
helm repo add lumina https://oss.nextdoor.com/lumina
helm repo update

# Install Lumina with default values
helm install lumina lumina/lumina -n lumina-system --create-namespace

# Or install a specific version
helm install lumina lumina/lumina --version 0.2.8 -n lumina-system --create-namespace

# Customize with your own values
helm install lumina lumina/lumina -n lumina-system --create-namespace \
  --set awsAccounts[0].name=production \
  --set awsAccounts[0].accountId=123456789012 \
  --set awsAccounts[0].roleArn=arn:aws:iam::123456789012:role/LuminaReadOnly

# View available versions
helm search repo lumina --versions
```

See [charts/lumina/README.md](charts/lumina/README.md) for complete Helm chart documentation and configuration options.

#### Using Pre-built Images (Alternative)

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
  spotPricing: 15s    # Spot pricing (lazy-loading)
```

### Multi-Cluster Deployments

When deploying Lumina to multiple Kubernetes clusters that report to a **shared Prometheus** endpoint, use the following configuration to prevent metric duplication:

#### Management Cluster (emits all metrics)
```yaml
metrics:
  disableInstanceMetrics: false  # Default - emit all instance metrics
  labels:
    clusterName: my_cluster_name  # Optional: customize label names
    accountName: my_account_name
    accountId: my_account_id
```

#### Worker Clusters (emit only aggregate metrics)
```yaml
metrics:
  disableInstanceMetrics: true  # Prevent duplication
```

**Why this matters:**
- Each Lumina instance discovers **all EC2 instances** across configured AWS accounts
- Without `disableInstanceMetrics`, multiple clusters would emit duplicate `ec2_instance_hourly_cost` metrics
- With this configuration, only the management cluster emits instance-level metrics
- Worker clusters can still emit aggregate metrics (if added in future)

**Metric Label Customization:**

Customize metric label names to match your organization's conventions or avoid conflicts with external relabeling:

```yaml
metrics:
  labels:
    clusterName: k8s_cluster      # Default: cluster_name
    accountName: aws_account      # Default: account_name
    accountId: aws_account_id     # Default: account_id
    region: aws_region            # Default: region
    nodeName: k8s_node            # Default: node_name
    hostName: ec2_hostname        # Default: host_name
```

All metrics (instance, Savings Plans, Reserved Instances, etc.) will use these custom label names.

**Additional Features:**
- `cluster_name` label: Automatically extracted from `kubernetes.io/cluster/*` EC2 tags
- `host_name` label: Populated from EC2 `PrivateDNSName` field
- `node_name` fallback: Uses Kubernetes correlation → EC2 Name tag → empty string

> **📖 See [pkg/config/README.md](pkg/config/README.md) for complete configuration reference including environment variables.**

## Metrics

Lumina exposes Prometheus metrics on port 8080 at `/metrics` endpoint.

**Key Metrics:**
- `ec2_instance_hourly_cost` - Per-instance cost with Savings Plans/RI discounts applied
- `savings_plan_utilization_percent` - Real-time SP utilization tracking
- `ec2_instance` / `ec2_instance_count` - Instance inventory metrics
- `lumina_data_freshness_seconds` - Data freshness monitoring

**Quick Example:**
```promql
# Total hourly cost across all instances
sum(ec2_instance_hourly_cost)

# Cost per Kubernetes node
sum by (node_name) (ec2_instance_hourly_cost{node_name!=""})
```

> **📊 See [pkg/metrics/README.md](pkg/metrics/README.md) for complete metrics reference, all labels, and example queries.**

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

**Essential Reading:**
- **[ALGORITHM.md](ALGORITHM.md)** - Cost calculation algorithms, limitations, and AWS billing differences
- **[docs/DEBUG.md](docs/DEBUG.md)** - Debug endpoints for troubleshooting cost calculations
- **[pkg/metrics/README.md](pkg/metrics/README.md)** - Complete Prometheus metrics reference
- **[pkg/config/README.md](pkg/config/README.md)** - Configuration file format and validation

**Additional Resources:**
- [docs/README.md](docs/README.md) - Documentation hub with organized guides
- [pkg/aws/README.md](pkg/aws/README.md) - AWS client package internals
- [CLAUDE.md](CLAUDE.md) - Project coding guidelines (for contributors)

## License

Apache 2.0 (to be confirmed)

## Credits

Built by the Platform Engineering team. Powered by [controller-runtime](https://github.com/kubernetes-sigs/controller-runtime).
