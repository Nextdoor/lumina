---
title: "Helm Chart"
description: "Helm chart values reference for the Lumina controller"
weight: 30
---

Lumina is distributed as a Helm chart published to the Nextdoor OSS Helm repository.

## Installation

```bash
helm repo add lumina https://oss.nextdoor.com/lumina
helm repo update
helm install lumina lumina/lumina -n lumina-system --create-namespace
```

## Values Reference

### Image

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `image.repository` | string | `ghcr.io/nextdoor/lumina` | Container image repository |
| `image.pullPolicy` | string | `IfNotPresent` | Image pull policy |
| `image.tag` | string | `""` | Overrides the image tag (default is chart appVersion) |
| `imagePullSecrets` | list | `[]` | Image pull secrets for private registries |

### Replicas and Naming

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `replicaCount` | int | `2` | Number of controller replicas (leader election handles HA) |
| `nameOverride` | string | `""` | Override the name of the chart |
| `fullnameOverride` | string | `""` | Override the full name of the release |

### Service Account

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `serviceAccount.create` | bool | `true` | Create a service account |
| `serviceAccount.automount` | bool | `true` | Automount API credentials |
| `serviceAccount.annotations` | object | `{}` | Annotations (e.g., `eks.amazonaws.com/role-arn` for IRSA) |
| `serviceAccount.name` | string | `""` | Service account name (auto-generated if empty) |

### Pod Configuration

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `podAnnotations` | object | `{}` | Annotations to add to the pod |
| `podLabels` | object | `{}` | Labels to add to the pod |
| `nodeSelector` | object | `{}` | Node selector for pod assignment |
| `tolerations` | list | `[]` | Tolerations for pod assignment |
| `affinity` | object | `{}` | Affinity rules for pod assignment |
| `volumes` | list | `[]` | Additional volumes for the deployment |
| `volumeMounts` | list | `[]` | Additional volume mounts for the deployment |

### Security Context

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `podSecurityContext.runAsNonRoot` | bool | `true` | Run as non-root user |
| `podSecurityContext.runAsUser` | int | `65532` | User ID |
| `podSecurityContext.fsGroup` | int | `65532` | Group ID for filesystem access |
| `podSecurityContext.seccompProfile.type` | string | `RuntimeDefault` | Seccomp profile |
| `securityContext.allowPrivilegeEscalation` | bool | `false` | Prevent privilege escalation |
| `securityContext.capabilities.drop` | list | `["ALL"]` | Drop all capabilities |
| `securityContext.readOnlyRootFilesystem` | bool | `true` | Read-only root filesystem |

### Resources

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `resources.limits.cpu` | string | `"1"` | CPU limit |
| `resources.limits.memory` | string | `512Mi` | Memory limit |
| `resources.requests.cpu` | string | `200m` | CPU request |
| `resources.requests.memory` | string | `128Mi` | Memory request |

### Health Probes

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `livenessProbe.httpGet.path` | string | `/healthz` | Liveness probe path |
| `livenessProbe.httpGet.port` | int | `8081` | Liveness probe port |
| `livenessProbe.initialDelaySeconds` | int | `15` | Initial delay |
| `livenessProbe.periodSeconds` | int | `20` | Check period |
| `readinessProbe.httpGet.path` | string | `/readyz` | Readiness probe path |
| `readinessProbe.httpGet.port` | int | `8081` | Readiness probe port |
| `readinessProbe.initialDelaySeconds` | int | `5` | Initial delay |
| `readinessProbe.periodSeconds` | int | `10` | Check period |

### Controller Manager

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `controllerManager.leaderElection.enabled` | bool | `true` | Enable leader election for HA |
| `controllerManager.metricsBindAddress` | string | `0.0.0.0:8080` | Metrics server bind address |
| `controllerManager.metricsSecure` | bool | `false` | Enable HTTPS metrics endpoint |
| `controllerManager.healthProbeBindAddress` | string | `0.0.0.0:8081` | Health probe bind address |
| `controllerManager.logLevel` | string | `info` | Log level (debug, info, error) |
| `controllerManager.metricsAuth` | bool | `false` | Enable metrics RBAC authentication |
| `controllerManager.enableHttp2` | bool | `false` | Enable HTTP/2 |
| `controllerManager.extraArgs` | list | `[]` | Extra command-line arguments |

### Controller Manager -- Logging

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `controllerManager.zap.devel` | bool | `false` | Development mode logging |
| `controllerManager.zap.encoder` | string | `json` | Log encoding (json or console) |
| `controllerManager.zap.stacktraceLevel` | string | `error` | Stacktrace capture level |
| `controllerManager.zap.timeEncoding` | string | `epoch` | Time encoding format |

### Controller Manager -- TLS

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `controllerManager.metricsCerts.path` | string | `""` | Metrics certificate directory |
| `controllerManager.metricsCerts.certName` | string | `tls.crt` | Certificate filename |
| `controllerManager.metricsCerts.keyName` | string | `tls.key` | Key filename |
| `controllerManager.webhookCerts.path` | string | `""` | Webhook certificate directory |
| `controllerManager.webhookCerts.certName` | string | `tls.crt` | Certificate filename |
| `controllerManager.webhookCerts.keyName` | string | `tls.key` | Key filename |

### Lumina Configuration

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `skipConfig` | bool | `false` | Skip creating the ConfigMap (useful for CI/testing) |
| `config` | object | See below | Controller configuration (maps to config.yaml) |

The `config` section maps directly to the Lumina configuration file. See the [Configuration Reference]({{< relref "configuration" >}}) for all options.

### Metrics Service

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `metricsService.type` | string | `ClusterIP` | Service type |
| `metricsService.port` | int | `8080` | Service port |
| `metricsService.annotations` | object | `{}` | Service annotations |

### RBAC

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `rbac.create` | bool | `true` | Create RBAC resources |

### ServiceMonitor (Prometheus Operator)

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `serviceMonitor.enabled` | bool | `true` | Create ServiceMonitor resource |
| `serviceMonitor.interval` | string | `30s` | Scrape interval |
| `serviceMonitor.scrapeTimeout` | string | `10s` | Scrape timeout |
| `serviceMonitor.labels` | object | `{}` | Additional labels |
| `serviceMonitor.annotations` | object | `{}` | Additional annotations |
| `serviceMonitor.relabelings` | list | `[]` | RelabelConfigs |
| `serviceMonitor.metricRelabelings` | list | `[]` | MetricRelabelConfigs |

### LocalStack (CI/Testing)

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `localstack.enabled` | bool | `false` | Enable LocalStack for mocking AWS services |
| `localstack.service.type` | string | `ClusterIP` | LocalStack service type |

## Example: Production Values

```yaml
replicaCount: 2

serviceAccount:
  annotations:
    eks.amazonaws.com/role-arn: "arn:aws:iam::123456789012:role/lumina-controller"

config:
  defaultRegion: us-west-2
  regions:
    - us-west-2
    - us-east-1
  awsAccounts:
    - accountId: "123456789012"
      name: "Production"
      assumeRoleArn: "arn:aws:iam::123456789012:role/lumina-readonly"
    - accountId: "987654321098"
      name: "Staging"
      assumeRoleArn: "arn:aws:iam::987654321098:role/lumina-readonly"

resources:
  limits:
    cpu: "1"
    memory: 512Mi
  requests:
    cpu: 200m
    memory: 128Mi

serviceMonitor:
  enabled: true
  interval: 30s
```

## Example: Worker Cluster Values

For clusters that share a Prometheus endpoint with a management cluster:

```yaml
config:
  metrics:
    disableInstanceMetrics: true  # Prevent metric duplication
  awsAccounts:
    - accountId: "123456789012"
      name: "Production"
      assumeRoleArn: "arn:aws:iam::123456789012:role/lumina-readonly"
```
