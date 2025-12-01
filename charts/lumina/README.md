# lumina

![Version: 0.1.0](https://img.shields.io/badge/Version-0.1.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: main](https://img.shields.io/badge/AppVersion-main-informational?style=flat-square)

Lumina - Kubernetes Cost Visibility Controller

**Homepage:** <https://github.com/Nextdoor/lumina>

## Maintainers

| Name | Email | Url |
| ---- | ------ | --- |
| Lumina Maintainers |  | <https://github.com/Nextdoor/lumina> |

## Source Code

* <https://github.com/Nextdoor/lumina>

## Requirements

| Repository | Name | Version |
|------------|------|---------|
| https://localstack.github.io/helm-charts | localstack | 0.6.27 |

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| accountValidationInterval | string | `""` | How often to validate AWS account access (e.g., 5m, 10m, 1h). Leave empty to use default (10m) |
| affinity | object | `{}` | Affinity rules for pod assignment |
| awsAccounts | list | `[]` | AWS account configuration for cross-account access. Will be passed to controller via ConfigMap mounted at /etc/lumina/config/config.yaml |
| controllerManager.enableHttp2 | bool | `false` | Enable HTTP/2 for metrics and webhook servers |
| controllerManager.extraArgs | list | `[]` | Extra command-line arguments to pass to the controller |
| controllerManager.healthProbeBindAddress | string | `"0.0.0.0:8081"` | Health probe bind address (host:port) |
| controllerManager.leaderElection.enabled | bool | `true` | Enable leader election for high availability |
| controllerManager.logLevel | string | `"info"` | Controller log level (debug, info, error) |
| controllerManager.metricsAuth | bool | `false` | Enable metrics endpoint authentication (Kubernetes RBAC) |
| controllerManager.metricsBindAddress | string | `"0.0.0.0:8080"` | Metrics server bind address (host:port). Use :8080 for HTTP or :8443 for HTTPS |
| controllerManager.metricsCerts | object | `{"certName":"tls.crt","keyName":"tls.key","path":""}` | Metrics TLS certificate configuration (only used if metricsSecure: true) |
| controllerManager.metricsCerts.certName | string | `"tls.crt"` | Metrics server certificate filename |
| controllerManager.metricsCerts.keyName | string | `"tls.key"` | Metrics server key filename |
| controllerManager.metricsCerts.path | string | `""` | Directory containing metrics server certificates |
| controllerManager.metricsSecure | bool | `false` | Enable secure HTTPS metrics endpoint (requires certificates) |
| controllerManager.webhookCerts | object | `{"certName":"tls.crt","keyName":"tls.key","path":""}` | Webhook TLS certificate configuration |
| controllerManager.webhookCerts.certName | string | `"tls.crt"` | Webhook certificate filename |
| controllerManager.webhookCerts.keyName | string | `"tls.key"` | Webhook key filename |
| controllerManager.webhookCerts.path | string | `""` | Directory containing webhook certificates |
| controllerManager.zap | object | `{"devel":false,"encoder":"json","stacktraceLevel":"error","timeEncoding":"epoch"}` | Zap logging configuration |
| controllerManager.zap.devel | bool | `false` | Development mode logging (consoleEncoder, debug level) |
| controllerManager.zap.encoder | string | `"json"` | Log encoding format (json or console) |
| controllerManager.zap.stacktraceLevel | string | `"error"` | Stacktrace capture level (info, error, panic) |
| controllerManager.zap.timeEncoding | string | `"epoch"` | Time encoding format (epoch, millis, nano, iso8601, rfc3339, rfc3339nano) |
| defaultAccount | object | `{}` | If not specified, the first account in awsAccounts will be used. |
| defaultRegion | string | `"us-west-2"` | Default AWS region for API operations |
| fullnameOverride | string | `""` | Override the full name of the release |
| image.pullPolicy | string | `"IfNotPresent"` | Image pull policy |
| image.repository | string | `"ghcr.io/nextdoor/lumina"` | Container image repository |
| image.tag | string | `""` | Overrides the image tag whose default is the chart appVersion |
| imagePullSecrets | list | `[]` | Image pull secrets for private registries |
| livenessProbe.failureThreshold | int | `3` |  |
| livenessProbe.httpGet | object | `{"path":"/healthz","port":8081}` | Liveness probe configuration |
| livenessProbe.initialDelaySeconds | int | `15` |  |
| livenessProbe.periodSeconds | int | `20` |  |
| livenessProbe.timeoutSeconds | int | `1` |  |
| localstack | object | `{"enabled":false,"service":{"type":"ClusterIP"}}` | LocalStack configuration for CI/testing |
| localstack.enabled | bool | `false` | Enable LocalStack for mocking AWS services (used in CI for testing) When enabled, deploys LocalStack as a subchart and provides a mock AWS environment See ci/ci-values.yaml for an example configuration with LocalStack |
| localstack.service | object | `{"type":"ClusterIP"}` | Override service type for LocalStack (ClusterIP recommended for CI) |
| metricsService.annotations | object | `{}` | Annotations to add to the metrics service |
| metricsService.port | int | `8080` | Metrics service port |
| metricsService.type | string | `"ClusterIP"` | Metrics service type |
| nameOverride | string | `""` | Override the name of the chart |
| nodeSelector | object | `{}` | Node selector for pod assignment |
| podAnnotations | object | `{}` | Annotations to add to the pod |
| podLabels | object | `{}` | Labels to add to the pod |
| podSecurityContext.fsGroup | int | `65532` | Group ID for filesystem access |
| podSecurityContext.runAsNonRoot | bool | `true` | Run as non-root user |
| podSecurityContext.runAsUser | int | `65532` | User ID to run the container as |
| podSecurityContext.seccompProfile.type | string | `"RuntimeDefault"` | Seccomp profile type |
| pricing | object | `{"operatingSystems":[]}` | Pricing configuration |
| pricing.operatingSystems | list | `[]` | Operating systems to fetch pricing data for |
| rbac.create | bool | `true` | Create RBAC resources (ClusterRole, ClusterRoleBinding, Role, RoleBinding) |
| readinessProbe.failureThreshold | int | `3` |  |
| readinessProbe.httpGet | object | `{"path":"/readyz","port":8081}` | Readiness probe configuration |
| readinessProbe.initialDelaySeconds | int | `5` |  |
| readinessProbe.periodSeconds | int | `10` |  |
| readinessProbe.timeoutSeconds | int | `1` |  |
| reconciliation | object | `{"ec2":"","pricing":"","risp":""}` | Reconciliation intervals for different controllers |
| reconciliation.ec2 | string | `""` | EC2 reconciliation interval (e.g., 2m, 5m, 1h) |
| reconciliation.pricing | string | `""` | Pricing reconciliation interval (e.g., 24h) |
| reconciliation.risp | string | `""` | RISP (Reserved Instances/Savings Plans) reconciliation interval (e.g., 1h) |
| regions | list | `[]` | Global list of regions to query across all accounts (can be overridden per-account) |
| replicaCount | int | `2` | Number of controller replicas (leader election handles HA) |
| resources | object | `{"limits":{"cpu":"1","memory":"512Mi"},"requests":{"cpu":"200m","memory":"128Mi"}}` | Resource limits and requests for the controller |
| securityContext.allowPrivilegeEscalation | bool | `false` | Prevent privilege escalation |
| securityContext.capabilities.drop | list | `["ALL"]` | Drop all capabilities |
| securityContext.readOnlyRootFilesystem | bool | `true` | Read-only root filesystem |
| securityContext.runAsNonRoot | bool | `true` | Run as non-root user |
| securityContext.runAsUser | int | `65532` | User ID to run the container as |
| securityContext.seccompProfile.type | string | `"RuntimeDefault"` | Seccomp profile type |
| serviceAccount.annotations | object | `{}` | Annotations to add to the service account (e.g., eks.amazonaws.com/role-arn for IRSA) |
| serviceAccount.automount | bool | `true` | Automatically mount a ServiceAccount's API credentials |
| serviceAccount.create | bool | `true` | Specifies whether a service account should be created |
| serviceAccount.name | string | `""` | The name of the service account to use. If not set and create is true, a name is generated using the fullname template |
| serviceMonitor.annotations | object | `{}` | Additional annotations for the ServiceMonitor |
| serviceMonitor.enabled | bool | `true` | Create ServiceMonitor resource for Prometheus Operator |
| serviceMonitor.interval | string | `"30s"` | Scrape interval for Prometheus |
| serviceMonitor.labels | object | `{}` | Additional labels for the ServiceMonitor |
| serviceMonitor.metricRelabelings | list | `[]` | Ref: https://prometheus.io/docs/prometheus/latest/configuration/configuration/#metric_relabel_configs |
| serviceMonitor.relabelings | list | `[]` | Ref: https://prometheus.io/docs/prometheus/latest/configuration/configuration/#relabel_config |
| serviceMonitor.scrapeTimeout | string | `"10s"` | Scrape timeout for Prometheus |
| skipConfig | bool | `false` | Useful for CI/testing or when the controller should run without AWS account configuration. |
| tolerations | list | `[]` | Tolerations for pod assignment |
| volumeMounts | list | `[]` | Additional volume mounts for the deployment |
| volumes | list | `[]` | Additional volumes for the deployment |

