---
title: "Development"
description: "Local development setup, testing, and contributing to Lumina"
weight: 50
---

## Prerequisites

- **Go 1.24+**
- **AWS CLI** configured with credentials
- AWS IAM permissions to assume roles and query EC2/Savings Plans
- **kubebuilder 4.10+** (for development)

## Setup

```bash
# Clone and install dependencies
git clone https://github.com/nextdoor/lumina.git
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

### Verify It Is Working

```bash
# Check metrics
curl http://localhost:8080/metrics | grep savings_plan

# Check health
curl http://localhost:8081/readyz
```

### Running with Kubernetes

```bash
# Run controller against current kubeconfig context
make run

# Run with debug logging
make run ARGS="--zap-log-level=debug"
```

## Building

```bash
# Build binary
make build

# Build using GoReleaser (matches CI exactly)
bin/goreleaser build --snapshot --clean

# Build Docker images using GoReleaser
IMG=lumina:dev bin/goreleaser release --snapshot --clean
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

## Testing

```bash
# Run tests
go test ./...

# Run tests with race detection
go test -race ./...

# Run linter
make lint

# Run E2E tests (requires Kind cluster)
make test-e2e

# Coverage report
make cover

# HTML coverage report
make coverhtml
```

### Test Requirements

- **100% code coverage is mandatory** for all code in this repository
- Use `// coverage:ignore` comments only when 100% coverage is genuinely not possible
- Every `// coverage:ignore` must have a clear comment explaining why
- CI fails if coverage drops below 100%

### Testing Strategy

- **Unit tests**: Test individual functions and methods in isolation
- **Integration tests**: Test component interactions and real workflows with realistic scenarios
- **Table-driven tests**: Use Go's table-driven test pattern for multiple scenarios
- Unit tests go in `*_test.go` files alongside source
- Integration tests go in `integration_test.go` or separate `integration/` directory

## Pre-Commit Checklist

Always run before committing:

```bash
# 1. Run the linter
make lint

# 2. Run all tests with race detection
go test -race ./...

# 3. If both pass, stage and commit
git add <files>
git commit -m "type(component): description"
```

## Contributing

### Commit Messages

Use conventional commits format with a component value:

```
feat(api): add new endpoint
fix(cost): correct SP allocation for partial coverage
docs(readme): update installation instructions
test(metrics): add integration tests for label customization
refactor(cache): simplify pricing cache key builder
```

Valid types: `feat`, `fix`, `docs`, `test`, `refactor`, `chore`, `ci`

### Pull Requests

- Open PRs in draft mode initially
- Include comprehensive descriptions explaining changes
- Reference related issues
- Ensure all CI checks pass (including coverage) before requesting review

### Development Principles

1. **100% code coverage required**
2. **Integration tests are critical** -- test realistic scenarios
3. **No internal references** -- code must be ready for public release
4. **Conventional commits** -- use `type(component): description` format

## Project Structure

Key directories:

| Path | Description |
|------|-------------|
| `pkg/config/` | Configuration loading and validation |
| `pkg/cost/` | Cost calculation algorithms |
| `pkg/metrics/` | Prometheus metrics definitions |
| `pkg/aws/` | AWS client interfaces and implementations |
| `internal/cache/` | In-memory caching (EC2, RISP, pricing) |
| `internal/controller/` | Reconciliation controllers |
| `charts/lumina/` | Helm chart |
| `website/` | Documentation site (Hugo + Docsy) |
