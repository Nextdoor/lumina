# Lumina Development Guide

Quick guide for local development and testing.

## Prerequisites

- **Go 1.24+**
- **AWS CLI** configured with credentials
- AWS IAM permissions to assume roles and query EC2/Savings Plans

## Setup

```bash
# Clone and install dependencies
git clone git@github.com:Nextdoor/lumina.git
cd lumina
go mod download

# Create local config
cp config.example.yaml config.yaml
# Edit config.yaml with your AWS accounts
```

## Running Locally

Run the controller in standalone mode (no Kubernetes required):

```bash
make run-local
```

This runs the controller with:
- AWS data collection only (no Kubernetes)
- Debug logging enabled
- Metrics on http://localhost:8080/metrics
- Health checks on http://localhost:8081/healthz

### Verify It's Working

```bash
# Check metrics
curl http://localhost:8080/metrics | grep savings_plan

# Check health
curl http://localhost:8081/readyz
```

## Building and Testing

```bash
# Build binary
make build

# Run tests
go test ./...

# Run tests with race detection
go test -race ./...

# Run linter
make lint

# Run E2E tests
make test-e2e
```

## Pre-Commit Checklist

Always run before committing:

```bash
make lint              # Must pass
go test -race ./...    # Must pass
git commit
```

---

See [CLAUDE.md](CLAUDE.md) for coding standards and [README.md](README.md) for project overview.
