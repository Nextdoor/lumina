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
- ✅ AWS account cross-account access (Phase 1)
- ✅ Reserved Instance & Savings Plans discovery (Phase 2)
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

Lumina will require IAM permissions to assume roles in all AWS accounts. Configuration format TBD.

## Metrics

Prometheus metrics will expose node costs and Savings Plans utilization. Metrics design TBD.

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
