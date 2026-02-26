---
title: "Getting Started"
description: "Install and configure Lumina in your Kubernetes cluster"
weight: 10
---

Get Lumina up and running in your Kubernetes cluster to start tracking real-time EC2 costs with Savings Plans and Reserved Instance visibility.

## What You'll Need

- A Kubernetes cluster (1.30+) with Helm 3.x installed
- AWS IAM roles with read-only access to EC2, Savings Plans, and Pricing APIs in each account you want to monitor
- A Prometheus instance to scrape the cost metrics Lumina exposes

## What's Covered

The [Installation Guide]({{< relref "installation" >}}) walks through the full setup process: adding the Helm repository, deploying the controller, configuring IAM cross-account access, and verifying that cost metrics are flowing into Prometheus.
