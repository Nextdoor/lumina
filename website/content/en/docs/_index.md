---
title: "Documentation"
description: "Comprehensive documentation for the Lumina Kubernetes cost visibility controller"
weight: 20
---

Lumina is a Kubernetes controller that provides real-time cost visibility for EC2 instances by tracking AWS Savings Plans, Reserved Instances, and spot pricing across your entire AWS organization.

```mermaid
graph TD
    classDef aws fill:#E8F0FE,stroke:#4285F4,color:#333
    classDef lumina fill:#E6F4EA,stroke:#34A853,color:#333
    classDef prom fill:#FFF3E0,stroke:#FB8C00,color:#333
    classDef consumer fill:#FCE4EC,stroke:#E91E63,color:#333

    AWS["AWS Organization<br/>EC2 · RIs · Savings Plans · Spot"]:::aws
    LUMINA["Lumina Controller<br/>Reconciles costs in real time"]:::lumina
    PROM["Prometheus<br/>Scrapes cost metrics"]:::prom
    CONSUMERS["Grafana / Alerting / Chargeback"]:::consumer

    AWS -->|AssumeRole + API queries| LUMINA
    LUMINA -->|Exposes /metrics| PROM
    PROM -->|Queries & dashboards| CONSUMERS
```

## Getting Started

Install Lumina in your cluster and start tracking costs. The [Installation Guide]({{< relref "getting-started/installation" >}}) covers prerequisites, Helm setup, IAM configuration, and verification steps.

## Concepts

Understand how Lumina works under the hood:

- [Architecture]({{< relref "concepts/architecture" >}}) -- Data flow, reconciliation loops, caching layers, and the rate-based cost model
- [Cost Calculation]({{< relref "concepts/cost-calculation" >}}) -- Priority order for discounts, RI/SP allocation algorithms, and known limitations

## Reference

Detailed reference documentation for day-to-day operations:

- [Configuration]({{< relref "reference/configuration" >}}) -- All config options, environment variables, and defaults
- [Metrics]({{< relref "reference/metrics" >}}) -- Full Prometheus metrics catalog with PromQL examples
- [Helm Chart]({{< relref "reference/helm-chart" >}}) -- Helm values reference
- [Debug Endpoints]({{< relref "reference/debug-endpoints" >}}) -- HTTP debug API for inspecting cache state

## Operations

- [Troubleshooting]({{< relref "troubleshooting" >}}) -- Common issues, symptoms, and debugging steps
- [Development]({{< relref "development" >}}) -- Local setup, testing, and contributing
