---
title: "Installation"
description: "Install Lumina using Helm and configure AWS IAM access"
weight: 10
---

## Prerequisites

- Kubernetes cluster (1.30+)
- Helm 3.x
- AWS credentials with cross-account access (see [IAM Setup](#iam-setup) below)
- Prometheus (for scraping cost metrics)

## Install with Helm

Helm charts are automatically published to the Nextdoor OSS Helm repository.

```bash
# Add the Lumina Helm repository
helm repo add lumina https://oss.nextdoor.com/lumina
helm repo update

# Install Lumina with default values
helm install lumina lumina/lumina -n lumina-system --create-namespace

# Or install a specific version
helm install lumina lumina/lumina --version 0.2.8 -n lumina-system --create-namespace
```

### Customize with Your Own Values

```bash
helm install lumina lumina/lumina -n lumina-system --create-namespace \
  --set awsAccounts[0].name=production \
  --set awsAccounts[0].accountId=123456789012 \
  --set awsAccounts[0].roleArn=arn:aws:iam::123456789012:role/LuminaReadOnly
```

See the [Helm Chart Reference]({{< relref "../reference/helm-chart" >}}) for all available values.

### Using Pre-built Images (Alternative)

Container images are published to GitHub Container Registry on every commit to main and on releases:

```bash
# Deploy latest stable release
make deploy IMG=ghcr.io/nextdoor/lumina:latest

# Deploy specific version
make deploy IMG=ghcr.io/nextdoor/lumina:v0.1.0
```

Available image tags:

| Tag | Description |
|-----|-------------|
| `latest` | Latest build from main branch |
| `main` | Latest build from main branch (same as latest) |
| `v*.*.*` | Semantic version releases (e.g., v0.1.0, v0.2.1) |
| `sha-<commit>` | Specific commit builds |

Images are built for both `linux/amd64` and `linux/arm64` platforms.

## IAM Setup

Lumina requires read-only access to AWS APIs in each account it monitors. Create an IAM role in each target account with the following policy:

### IAM Policy

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "ec2:DescribeInstances",
        "ec2:DescribeReservedInstances",
        "ec2:DescribeSpotPriceHistory",
        "savingsplans:DescribeSavingsPlans",
        "savingsplans:DescribeSavingsPlansOfferingRates",
        "pricing:GetProducts"
      ],
      "Resource": "*"
    }
  ]
}
```

### Trust Relationship

Allow the Kubernetes service account (via IRSA or Pod Identity) to assume the role:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {
        "AWS": "arn:aws:iam::<CONTROLLER_ACCOUNT_ID>:role/<CONTROLLER_ROLE_NAME>"
      },
      "Action": "sts:AssumeRole"
    }
  ]
}
```

### Configure AWS Accounts

Create a values file with your AWS accounts:

```yaml
config:
  defaultRegion: us-west-2
  regions:
    - us-west-2
    - us-east-1
  awsAccounts:
    - accountId: "123456789012"
      name: "Production"
      assumeRoleArn: "arn:aws:iam::123456789012:role/lumina-controller"
    - accountId: "987654321098"
      name: "Staging"
      assumeRoleArn: "arn:aws:iam::987654321098:role/lumina-controller"

serviceAccount:
  annotations:
    eks.amazonaws.com/role-arn: "arn:aws:iam::<CONTROLLER_ACCOUNT_ID>:role/lumina-controller"
```

## Verification

After installation, verify Lumina is running correctly:

```bash
# Check pods are running
kubectl get pods -n lumina-system

# Check logs for successful startup
kubectl logs -n lumina-system deployment/lumina-controller-manager

# Verify metrics are being exposed
kubectl port-forward -n lumina-system svc/lumina-metrics-service 8080:8080
curl http://localhost:8080/metrics | grep ec2_instance_hourly_cost

# Check health endpoints
curl http://localhost:8081/healthz
curl http://localhost:8081/readyz
```

### Expected Startup Behavior

1. Controller starts and validates AWS account access
2. Discovers Reserved Instances and Savings Plans (first run may take 1-2 minutes)
3. Discovers EC2 instances across all configured accounts/regions
4. Fetches on-demand pricing from AWS Pricing API
5. Calculates costs and exposes Prometheus metrics

### Verify in Prometheus

Once Lumina is running, you should see metrics in Prometheus:

```promql
# Check if Lumina is running
lumina_controller_running

# See total hourly cost
sum(ec2_instance_hourly_cost)

# Check Savings Plan utilization
savings_plan_utilization_percent
```
