---
title: "Reference"
description: "Detailed reference documentation for Lumina configuration, metrics, and APIs"
weight: 30
---

This section contains detailed reference documentation for operating and configuring Lumina. Use these pages when you need specific values, formats, or API details.

- [Configuration]({{< relref "configuration" >}}) -- Complete YAML configuration reference including AWS account setup, reconciliation intervals, pricing tuning, and environment variable overrides. Start here when customizing Lumina's behavior.

- [Metrics]({{< relref "metrics" >}}) -- Full catalog of every Prometheus metric Lumina exposes, with labels, descriptions, and ready-to-use PromQL queries for alerting and dashboards.

- [Helm Chart]({{< relref "helm-chart" >}}) -- All available Helm values for deploying Lumina, including resource limits, service account configuration, and multi-cluster settings.

- [Debug Endpoints]({{< relref "debug-endpoints" >}}) -- HTTP endpoints for inspecting Lumina's internal cache state, useful for verifying that EC2 instances, Savings Plans, and pricing data are being collected correctly.
