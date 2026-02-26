---
title: "Documentation"
description: "Comprehensive documentation for the Lumina Kubernetes cost visibility controller"
weight: 20
---

Lumina is a Kubernetes controller that provides real-time cost visibility for EC2 instances by tracking AWS Savings Plans, Reserved Instances, and spot pricing across your entire AWS organization.

## Getting Started

{{< card header="**Getting Started**" >}}
Install Lumina in your cluster and start tracking costs.

[Installation Guide]({{< relref "getting-started/installation" >}})
{{< /card >}}

## Learn the Concepts

{{< card header="**Concepts**" >}}
Understand how Lumina works, from architecture to cost calculation algorithms.

- [Architecture]({{< relref "concepts/architecture" >}}) -- Data flow, reconcilers, and caching
- [Cost Calculation]({{< relref "concepts/cost-calculation" >}}) -- Priority order, RI/SP allocation, limitations
{{< /card >}}

## Reference

{{< card header="**Reference**" >}}
Detailed reference documentation for configuration, metrics, and APIs.

- [Configuration]({{< relref "reference/configuration" >}}) -- All config options, env vars, defaults
- [Metrics]({{< relref "reference/metrics" >}}) -- Full Prometheus metrics catalog with PromQL examples
- [Helm Chart]({{< relref "reference/helm-chart" >}}) -- Helm values reference
- [Debug Endpoints]({{< relref "reference/debug-endpoints" >}}) -- HTTP debug API
{{< /card >}}

## Operations

{{< card header="**Operations**" >}}
- [Troubleshooting]({{< relref "troubleshooting" >}}) -- Common issues and debugging scenarios
- [Development]({{< relref "development" >}}) -- Local setup, testing, contributing
{{< /card >}}
