# Lumina Documentation

This directory contains supplementary documentation for Lumina. Start with the [main README](../README.md) for an overview and quick start.

## Core Documentation

### [Main README](../README.md)
Project overview, architecture, and getting started guide.

### [ALGORITHM.md](../ALGORITHM.md)
Detailed documentation of cost calculation algorithms, Savings Plans allocation logic, and known limitations compared to AWS billing.

### [DEVELOPMENT.md](../DEVELOPMENT.md)
Development environment setup, testing, and contribution guidelines.

## Operational Guides

### [DEBUG.md](DEBUG.md)
Debug endpoints reference for troubleshooting cost calculations and inspecting internal cache state. Essential for diagnosing pricing accuracy issues.

## Package Documentation

### [pkg/aws/README.md](../pkg/aws/README.md)
AWS client package - interfaces and implementations for EC2, Savings Plans, and Pricing APIs with cross-account support.

### [pkg/config/README.md](../pkg/config/README.md)
Configuration package - YAML loading, validation, and environment variable overrides.

### [pkg/metrics/README.md](../pkg/metrics/README.md)
Metrics package - Prometheus metrics reference for monitoring controller health and cost data.

## Quick Navigation

**Getting Started:**
1. [README.md](../README.md) - Overview and installation
2. [DEVELOPMENT.md](../DEVELOPMENT.md) - Set up dev environment
3. [pkg/config/README.md](../pkg/config/README.md) - Configure accounts

**Understanding Costs:**
1. [ALGORITHM.md](../ALGORITHM.md) - How cost calculation works
2. [DEBUG.md](DEBUG.md) - Debug cost calculation issues
3. [pkg/metrics/README.md](../pkg/metrics/README.md) - Cost metrics reference

**Development:**
1. [DEVELOPMENT.md](../DEVELOPMENT.md) - Setup and testing
2. [pkg/aws/README.md](../pkg/aws/README.md) - AWS client internals
3. [CLAUDE.md](../CLAUDE.md) - Claude Code instructions (internal)
