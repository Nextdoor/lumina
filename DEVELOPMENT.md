# Lumina Development Guide

This guide covers local development workflows for the Lumina controller.

## Table of Contents

- [Prerequisites](#prerequisites)
- [Local Development Setup](#local-development-setup)
- [Running Locally (Standalone Mode)](#running-locally-standalone-mode)
- [Development Workflow](#development-workflow)
- [Testing](#testing)
- [Debugging](#debugging)
- [Common Tasks](#common-tasks)

## Prerequisites

### Required Tools

- **Go 1.24+**: [Install Go](https://golang.org/doc/install)
- **make**: Standard on macOS/Linux
- **AWS CLI**: For configuring credentials - [Install AWS CLI](https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html)

### AWS Credentials

The controller requires AWS credentials with permissions to:
- Assume IAM roles in target accounts
- Query EC2 Reserved Instances
- Query Savings Plans

**Setup:**

```bash
# Configure AWS credentials (if not already done)
aws configure

# Verify credentials work
aws sts get-caller-identity
```

The controller will use your default AWS credentials to assume roles in the configured accounts.

## Local Development Setup

### 1. Clone the Repository

```bash
git clone git@github.com:Nextdoor/lumina.git
cd lumina
```

### 2. Install Dependencies

```bash
# Download Go dependencies
go mod download

# Install development tools (linter, code generators, etc.)
make setup-envtest
make controller-gen
make kustomize
make golangci-lint
```

### 3. Create Local Configuration

Create a `config.yaml` file in the repository root:

```bash
# Copy the example config
cp config.example.yaml config.yaml

# Edit with your AWS account details
vi config.yaml
```

**Minimal config.yaml:**

```yaml
awsAccounts:
  - accountId: "123456789012"
    name: "My-Account"
    assumeRoleArn: "arn:aws:iam::123456789012:role/lumina-access"

defaultRegion: "us-west-2"

regions:
  - "us-west-2"
  - "us-east-1"

logLevel: "debug"
```

**Important:** `config.yaml` is gitignored - it will not be committed.

## Running Locally (Standalone Mode)

Standalone mode runs the controller **without Kubernetes**, collecting AWS data and exposing Prometheus metrics.

### Quick Start

```bash
# Run with config.yaml (must exist in current directory)
make run-local
```

This command:
- Checks that `config.yaml` exists
- Runs the controller in standalone mode
- Enables debug logging (shows all RI/SP details)
- Exposes metrics on http://localhost:8080/metrics
- Exposes health checks on http://localhost:8081/healthz and /readyz

### Manual Run

```bash
# Run with custom config path
go run ./cmd/main.go \
  --config=/path/to/config.yaml \
  --no-kubernetes \
  --metrics-bind-address=:8080 \
  --health-probe-bind-address=:8081 \
  --metrics-secure=false \
  --zap-log-level=debug
```

### Verify It's Working

**Check logs:**
```bash
# Look for:
# - "starting in standalone mode (no Kubernetes integration)"
# - "started RISP reconciler in standalone mode"
# - "cache statistics" showing RI/SP counts
```

**Check metrics:**
```bash
# View all metrics
curl http://localhost:8080/metrics

# Filter for Savings Plans metrics
curl http://localhost:8080/metrics | grep savings_plan

# Filter for Reserved Instance metrics
curl http://localhost:8080/metrics | grep ec2_reserved_instance

# Check data freshness
curl http://localhost:8080/metrics | grep lumina_data_freshness
```

**Check health:**
```bash
# Liveness probe (always returns OK if process is running)
curl http://localhost:8081/healthz

# Readiness probe (validates AWS account access)
curl http://localhost:8081/readyz
```

### Understanding the Logs

**Startup logs:**
```
INFO starting in standalone mode (no Kubernetes integration)
INFO created AWS client
INFO metrics initialized
INFO initialized RI/SP cache
INFO started RISP reconciler in standalone mode
INFO metrics server ready
INFO health server ready
```

**Reconciliation cycle (every hour):**
```
INFO starting RI/SP reconciliation cycle
INFO updated reserved instances  region=us-west-2  count=5
INFO updated savings plans  count=3
INFO cache statistics  reserved_instances=5  savings_plans=3  regions=2  accounts=1
INFO reconciliation cycle completed successfully  duration_seconds=2.5
```

**Debug details (with --zap-log-level=debug):**
```
DEBUG reserved instance details  ri_id=abc-123  instance_type=m5.xlarge  availability_zone=us-west-2a  instance_count=2
DEBUG savings plan details  sp_arn=arn:aws:savingsplans::...  sp_type=Compute  commitment=150.50  region=all
```

## Development Workflow

### Pre-Commit Checklist

**Always run before committing:**

```bash
# 1. Run linter
make lint

# 2. Run tests with race detection
go test -race ./...

# 3. If both pass, commit
git add <files>
git commit -m "your message"
```

Never skip these checks - CI will fail if linting or tests fail.

### Code Quality Standards

- **100% test coverage required** (see [CLAUDE.md](CLAUDE.md))
- Use `// coverage:ignore` only for valid reasons with explanation
- Follow Go best practices and project conventions
- No internal references in code/comments (project will be open-sourced)

## Testing

### Run All Tests

```bash
# Unit tests
go test ./...

# With race detection (recommended)
go test -race ./...

# With coverage
make test
make cover       # Show coverage report
make coverhtml   # Open HTML coverage report
```

### Run Specific Package Tests

```bash
# Test a specific package
go test ./pkg/metrics

# Test with verbose output
go test -v ./pkg/config

# Run specific test function
go test -v -run TestLoadConfig ./pkg/config
```

### End-to-End Tests

```bash
# Run E2E tests (requires Docker for LocalStack)
make test-e2e

# Cleanup E2E test cluster
make cleanup-test-e2e
```

## Debugging

### Enable Debug Logging

```bash
# Run with debug logging
make run-local

# Or manually
go run ./cmd/main.go \
  --config=config.yaml \
  --no-kubernetes \
  --zap-log-level=debug
```

### Debug Specific Reconciler

```bash
# V(1) logs show detailed per-resource information
# Already enabled in make run-local via --zap-log-level=debug
```

### Common Issues

**Issue: "config file not found"**
```bash
# Solution: Create config.yaml in repository root
cp config.example.yaml config.yaml
# Edit config.yaml with your AWS account details
```

**Issue: "failed to assume role"**
```bash
# Check your AWS credentials
aws sts get-caller-identity

# Verify you can assume the role
aws sts assume-role \
  --role-arn "arn:aws:iam::ACCOUNT_ID:role/ROLE_NAME" \
  --role-session-name "test"
```

**Issue: "No RIs/SPs showing in metrics"**
- Check that you actually have RIs/SPs in the configured accounts
- Verify `regions` config matches where your RIs exist
- Check logs for AWS API errors
- Ensure IAM permissions are correct (see [config.example.yaml](config.example.yaml))

### Inspect Metrics with Prometheus

```bash
# Install Prometheus (macOS)
brew install prometheus

# Create prometheus.yml
cat > prometheus.yml <<EOF
scrape_configs:
  - job_name: 'lumina'
    static_configs:
      - targets: ['localhost:8080']
EOF

# Run Prometheus
prometheus --config.file=prometheus.yml

# Open http://localhost:9090 and query metrics:
# - savings_plan_hourly_commitment
# - ec2_reserved_instance_count
# - lumina_data_freshness_seconds
```

## Common Tasks

### Add a New Metric

1. Add metric to [pkg/metrics/metrics.go](pkg/metrics/metrics.go) `Metrics` struct
2. Initialize and register in `NewMetrics()`
3. Create update function (e.g., `UpdateXMetrics()`)
4. Call from appropriate reconciler
5. Add tests in `pkg/metrics/*_test.go`

### Add a New AWS API Call

1. Define types in [pkg/aws/types.go](pkg/aws/types.go)
2. Add interface method to [pkg/aws/client.go](pkg/aws/client.go)
3. Implement in [pkg/aws/client_*_impl.go](pkg/aws/)
4. Add mock support in [pkg/aws/client_mock.go](pkg/aws/client_mock.go)
5. Add tests in `pkg/aws/client_*_test.go`
6. Update reconciler to call new API

### Update Configuration Schema

1. Modify [pkg/config/config.go](pkg/config/config.go) structs
2. Update validation in `Validate()` methods
3. Update [config.example.yaml](config.example.yaml) with examples
4. Add tests in [pkg/config/config_test.go](pkg/config/config_test.go)

### Build Binary

```bash
# Build for current platform
make build

# Binary will be in bin/manager
./bin/manager --help
```

### Build Docker Image

```bash
# Build image
make docker-build IMG=myregistry/lumina:dev

# Run in container (requires Kubernetes config)
docker run -v ~/.aws:/root/.aws myregistry/lumina:dev
```

## IDE Setup

### VS Code

Recommended extensions:
- `golang.go` - Go language support
- `ms-kubernetes-tools.vscode-kubernetes-tools` - Kubernetes support

Settings (.vscode/settings.json):
```json
{
  "go.testFlags": ["-v", "-race"],
  "go.lintTool": "golangci-lint",
  "go.lintOnSave": "workspace"
}
```

### GoLand / IntelliJ IDEA

1. Import as Go module
2. Enable Go modules support
3. Configure golangci-lint as external tool

## Next Steps

- Read [RFC-0002](https://github.com/Nextdoor/cloudeng/blob/main/rfcs/RFC-0002-kubernetes-capacity-management-and-cost-visibility.md) for architecture details
- Review [CLAUDE.md](CLAUDE.md) for project-specific development guidelines
- Check [README.md](README.md) for high-level project overview
- Browse [pkg/](pkg/) for package documentation

## Getting Help

- Check existing tests for examples
- Review godoc comments in code
- Ask in team channels

---

**Remember:** This project will be open-sourced. Don't commit any internal references, credentials, or proprietary information.
