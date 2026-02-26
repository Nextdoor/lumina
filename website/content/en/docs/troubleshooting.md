---
title: "Troubleshooting"
description: "Common issues and debugging techniques for the Lumina controller"
weight: 40
---

## Controller Not Starting

### Symptoms
- Pod is in CrashLoopBackOff
- Logs show configuration errors

### Resolution

1. Check logs for configuration validation errors:
   ```bash
   kubectl logs -n lumina-system deployment/lumina-controller-manager
   ```

2. Verify the ConfigMap has valid YAML:
   ```bash
   kubectl get configmap -n lumina-system lumina-config -o yaml
   ```

3. Common config issues:
   - Account IDs must be exactly 12 digits
   - IAM role ARNs must match the format `arn:aws:iam::<account-id>:role/<role-name>`
   - ARN account ID must match the configured `accountId`
   - Duration values must be valid Go durations (e.g., "5m", "1h", "24h")

## AWS Account Access Failures

### Symptoms
- `lumina_account_validation_status == 0` in Prometheus
- Logs show AssumeRole errors

### Resolution

1. Check account validation status:
   ```promql
   lumina_account_validation_status
   ```

2. Verify the controller's service account has permission to assume the target role:
   ```bash
   # If using IRSA, check the annotation
   kubectl get sa -n lumina-system lumina-controller -o yaml
   ```

3. Verify the target IAM role trust policy allows the controller role to assume it.

4. Test AssumeRole manually:
   ```bash
   aws sts assume-role --role-arn arn:aws:iam::123456789012:role/lumina-readonly --role-session-name test
   ```

## No Cost Metrics

### Symptoms
- `ec2_instance_hourly_cost` metrics are missing or all zero
- Controller is running but no cost data

### Resolution

1. Check data freshness:
   ```promql
   lumina_data_freshness_seconds
   ```
   If values are very high, data collection may be failing.

2. Check data collection status:
   ```promql
   lumina_data_last_success == 0
   ```

3. Verify EC2 instances are in cache:
   ```bash
   curl http://localhost:8080/debug/cache/stats | jq
   ```

4. Verify on-demand pricing is loaded:
   ```bash
   curl http://localhost:8080/debug/cache/pricing/ondemand | jq '. | keys'
   ```

5. If pricing is missing, check that the controller can access the AWS Pricing API (requires `pricing:GetProducts` permission).

See the [Debug Endpoints]({{< relref "reference/debug-endpoints" >}}) reference for detailed debugging steps.

## Savings Plan Not Applying to Instances

### Symptoms
- Savings Plans exist but instances show on-demand pricing
- SP utilization is 0%

### Resolution

1. Verify the SP is discovered:
   ```bash
   curl http://localhost:8080/debug/cache/risp | jq '.savings_plans'
   ```

2. Check if SP rates are cached:
   ```bash
   curl http://localhost:8080/debug/cache/pricing/sp | jq
   ```

3. If rates are missing, wait 1-2 minutes for the SP Rates Reconciler to fetch them. Check data freshness:
   ```promql
   lumina_data_freshness_seconds{data_type="sp_rates"}
   ```

4. Verify the SP type matches your instances:
   - EC2 Instance SPs only cover the specified instance family and region
   - Compute SPs cover any instance family and region
   - Neither applies to spot instances

## Metric Duplication in Multi-Cluster Setup

### Symptoms
- Cost metrics are doubled/tripled in Prometheus
- Aggregate cost queries return inflated values

### Resolution

Set `metrics.disableInstanceMetrics: true` on all clusters except the management cluster:

```yaml
config:
  metrics:
    disableInstanceMetrics: true
```

This disables `ec2_instance`, `ec2_instance_count`, and `ec2_instance_hourly_cost` on worker clusters while keeping Savings Plans and Reserved Instance metrics enabled.

## Stale Data

### Symptoms
- `lumina_data_freshness_seconds` is very high for some data types
- Costs appear outdated

### Resolution

Check which data type is stale:

```promql
lumina_data_freshness_seconds > 600
```

Expected freshness by data type:

| Data Type | Expected Freshness | Alert Threshold |
|-----------|-------------------|-----------------|
| `ec2_instances` | ~5 minutes | >10 minutes |
| `reserved_instances` | ~1 hour | >2 hours |
| `savings_plans` | ~1 hour | >2 hours |
| `pricing` | ~24 hours | >48 hours |
| `sp_rates` | ~2 minutes | >10 minutes |
| `spot_pricing` | ~1 hour | >2 hours |

If a specific data type is consistently stale, check:
- AWS API rate limiting (check logs for throttling errors)
- Network connectivity to AWS endpoints
- IAM permissions for the specific API

## Pricing Accuracy Issues

### Symptoms
- Some instances show `pricing_accuracy="estimated"` instead of `"accurate"`

### Resolution

This is normal during cache warming (first few minutes after startup). The SP Rates Reconciler needs to fetch rates for all instance type/region combinations.

Monitor cache effectiveness:
```promql
# Percentage of instances using accurate pricing
sum(ec2_instance_hourly_cost{pricing_accuracy="accurate"}) /
sum(ec2_instance_hourly_cost) * 100
```

If estimated pricing persists:
1. Check SP rates cache:
   ```bash
   curl http://localhost:8080/debug/cache/pricing/sp | jq
   ```
2. Look for specific missing rates:
   ```bash
   curl "http://localhost:8080/debug/cache/pricing/sp/lookup?instance_type=<type>&region=<region>&sp=<arn>" | jq
   ```
3. Some rate combinations may not exist (e.g., Windows rate for a Linux-only SP), which is indicated by sentinel values.

## High Memory Usage

### Symptoms
- Controller OOMKilled
- Memory usage growing over time

### Resolution

1. Reduce pricing data by limiting operating systems:
   ```yaml
   config:
     pricing:
       operatingSystems:
         - "Linux"  # Only load Linux pricing
   ```

2. Reduce the number of configured regions if not all are needed.

3. Increase memory limits:
   ```yaml
   resources:
     limits:
       memory: 1Gi
   ```
