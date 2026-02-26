---
title: "Concepts"
description: "Understand how Lumina works under the hood"
weight: 20
---

Lumina introduces several key concepts that differentiate it from traditional cost reporting tools: a rate-based cost model that provides instantaneous dollar-per-hour snapshots, multi-account reconciliation that discovers Savings Plans and Reserved Instances across your entire AWS organization, and an in-memory caching architecture that keeps cost data fresh with minimal API overhead.

## Architecture

The [Architecture]({{< relref "architecture" >}}) page covers Lumina's data flow from AWS APIs through reconciliation loops into Prometheus metrics. It explains the multi-tier caching system, how Kubernetes nodes are correlated with EC2 instances, and how to configure multi-cluster deployments.

## Cost Calculation

The [Cost Calculation]({{< relref "cost-calculation" >}}) page details the algorithms Lumina uses to allocate Savings Plans and Reserved Instances to running instances. It covers the strict priority order (Spot, RI, EC2 Instance SP, Compute SP, On-Demand), partial coverage handling, and documents known differences from AWS's actual billing behavior.
