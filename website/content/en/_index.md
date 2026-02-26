---
title: Lumina
---

{{< blocks/cover title="Lumina" image_anchor="top" height="med" color="primary" >}}
<p class="lead mt-4">Real-time Kubernetes Cost Visibility</p>
<a class="btn btn-lg btn-secondary me-3 mb-4" href="https://github.com/nextdoor/lumina">
View on GitHub <i class="fab fa-github ms-2"></i>
</a>
{{< /blocks/cover >}}

{{% blocks/lead color="dark" %}}
Lumina is a Kubernetes controller that provides **real-time cost visibility** for EC2 instances.
It tracks AWS Savings Plans, Reserved Instances, and spot pricing across your entire AWS organization — giving you accurate, up-to-the-minute cost data directly in your cluster.
{{% /blocks/lead %}}

{{% blocks/section color="white" type="row" %}}

{{% blocks/feature icon="fa-solid fa-chart-line" title="Real-Time Cost Metrics" %}}
Continuously reconciles EC2 pricing data — including Savings Plans, Reserved Instances, and on-demand rates — and exposes it as Prometheus metrics.
{{% /blocks/feature %}}

{{% blocks/feature icon="fa-solid fa-building" title="Organization-Wide Coverage" %}}
Queries across your entire AWS Organization to capture shared Savings Plans and Reserved Instances, regardless of which account owns them.
{{% /blocks/feature %}}

{{% blocks/feature icon="fa-solid fa-puzzle-piece" title="Kubernetes Native" %}}
Runs as a standard Kubernetes controller with Helm installation. Integrates naturally with your existing Prometheus and Grafana stack.
{{% /blocks/feature %}}

{{% /blocks/section %}}

{{% blocks/section color="light" %}}
## Quick Start

```bash
helm repo add lumina https://oss.nextdoor.com/lumina
helm repo update
helm install lumina lumina/lumina
```
{{% /blocks/section %}}
