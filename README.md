# Lumina

> Illuminate Kubernetes costs with real-time AWS Savings Plans visibility

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

Lumina is a Kubernetes controller that provides real-time cost visibility for EC2 instances by tracking AWS Savings Plans, Reserved Instances, and spot pricing across your entire AWS organization. It exposes Prometheus metrics showing the actual hourly cost of each Kubernetes node.

**Full documentation: [https://oss.nextdoor.com/lumina/docs/](https://oss.nextdoor.com/lumina/docs/)**

## Quick Install

```bash
helm repo add lumina https://oss.nextdoor.com/lumina
helm repo update
helm install lumina lumina/lumina -n lumina-system --create-namespace
```

See the [Helm chart README](charts/lumina/README.md) for all configuration options.

## Documentation

| Topic | Link |
|-------|------|
| Getting Started | [Getting Started Guide](https://oss.nextdoor.com/lumina/docs/getting-started/) |
| Configuration | [Configuration Reference](https://oss.nextdoor.com/lumina/docs/reference/configuration/) |
| Cost Calculation | [Cost Calculation Algorithm](https://oss.nextdoor.com/lumina/docs/concepts/cost-calculation/) |
| Metrics | [Prometheus Metrics Reference](https://oss.nextdoor.com/lumina/docs/reference/metrics/) |
| Debug Endpoints | [Debug Endpoints](https://oss.nextdoor.com/lumina/docs/reference/debug-endpoints/) |
| Development | [Development Guide](https://oss.nextdoor.com/lumina/docs/development/) |

## License

Apache 2.0 - See [LICENSE](LICENSE) for details.

## Contributing

Contributions are welcome! See the [Development Guide](https://oss.nextdoor.com/lumina/docs/development/) to get started.
