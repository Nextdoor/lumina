---
title: "Debug Endpoints"
description: "HTTP debug API for inspecting Lumina's internal cache state"
weight: 40
---

Lumina provides HTTP debug endpoints for inspecting internal cache state. These endpoints are useful for troubleshooting cost calculation issues, verifying pricing data, and understanding what data Lumina has loaded.

{{% pageinfo color="warning" %}}
Debug endpoints expose internal cache data including AWS account IDs, instance details, and pricing information. Ensure these endpoints are only accessible in development/staging environments or protected by appropriate authentication in production.
{{% /pageinfo %}}

All debug endpoints are available at `http://localhost:8080/debug/cache/` (or your configured metrics bind address). They return JSON responses and are read-only.

## Available Endpoints

### Index

```bash
GET /debug/cache/
```

Returns a list of all available debug endpoints with descriptions.

```bash
curl http://localhost:8080/debug/cache/ | jq
```

### EC2 Cache

```bash
GET /debug/cache/ec2
```

Lists all EC2 instances currently in cache, grouped by region.

**Response includes:** Instance ID, instance type, state, availability zone, account ID, tenancy, platform, launch time.

**Use cases:**
- Verify instance discovery is working
- Check which instances Lumina is tracking
- Confirm instance tenancy and platform values
- Debug missing instances

```bash
curl http://localhost:8080/debug/cache/ec2 | jq
```

### RISP Cache (Reserved Instances and Savings Plans)

```bash
GET /debug/cache/risp
```

Lists all Reserved Instances and Savings Plans currently in cache.

**Response includes:**

For Reserved Instances: RI ID, instance type, instance count, state, account ID, region.

For Savings Plans: SP ARN, SP ID, type (EC2Instance/Compute), state, commitment ($/hour), region, instance family, start/end dates.

```bash
curl http://localhost:8080/debug/cache/risp | jq
```

### On-Demand Pricing Cache

```bash
GET /debug/cache/pricing/ondemand
```

Lists all on-demand EC2 pricing currently in cache, grouped by region.

**Response includes:** Region, instance type, operating system, hourly rate ($/hour).

```bash
curl http://localhost:8080/debug/cache/pricing/ondemand | jq
```

### Savings Plan Rates Cache

```bash
GET /debug/cache/pricing/sp
GET /debug/cache/pricing/sp?sp=<savings-plan-arn>
```

Lists all Savings Plan rates currently in cache. Optionally filter by Savings Plan ARN.

**Response includes:** Total rates cached, rates grouped by SP ARN (instance type, region, tenancy, OS, hourly rate).

```bash
# List all SP rates
curl http://localhost:8080/debug/cache/pricing/sp | jq

# Filter by specific Savings Plan
curl "http://localhost:8080/debug/cache/pricing/sp?sp=arn:aws:savingsplans::123456789012:savingsplan/abc-123" | jq
```

### Spot Pricing Cache

```bash
GET /debug/cache/pricing/spot
```

Lists all spot prices currently in cache, showing real-time AWS spot market rates.

**Response includes:** Total spot prices cached, prices grouped by availability zone (instance type, spot price, timestamps), cache age and populated status.

```bash
curl http://localhost:8080/debug/cache/pricing/spot | jq
```

**Response format:**
```json
{
  "total_count": 150,
  "stats": {
    "is_populated": true,
    "spot_price_count": 150,
    "cache_last_updated": "2025-01-20T10:35:12Z",
    "cache_age_seconds": 298.5
  },
  "prices": [
    {
      "key": "m5.large:us-west-2a",
      "price": 0.034,
      "age": 120.3
    }
  ]
}
```

**Fields:**
- `key`: Cache key format `instanceType:availabilityZone`
- `price`: Current spot price in $/hour
- `age`: Age of this specific price in seconds

Key concepts:
- **Lazy-loading**: Only fetches prices for instance types that are actually running
- **Automatic refresh**: Stale prices (older than `spotPriceCacheExpiration`) are refreshed automatically
- **Per-AZ pricing**: Spot prices vary by availability zone, not just region

### Savings Plan Rate Lookup

```bash
GET /debug/cache/pricing/sp/lookup?instance_type=<type>&region=<region>&sp=<arn>[&tenancy=<tenancy>][&os=<os>]
```

Looks up a specific Savings Plan rate for a given instance configuration.

**Required parameters:**
- `instance_type` -- EC2 instance type (e.g., "m5.xlarge")
- `region` -- AWS region (e.g., "us-west-2")
- `sp` -- Savings Plan ARN

**Optional parameters:**
- `tenancy` -- Instance tenancy (default: "default"; also: "dedicated", "host")
- `os` -- Operating system (default: "linux"; also: "windows")

**Response includes:** Query parameters used, expected cache key, whether rate was found, rate value, pricing accuracy indicator, debug hints.

```bash
# Basic lookup (Linux, default tenancy)
curl "http://localhost:8080/debug/cache/pricing/sp/lookup?instance_type=m5.xlarge&region=us-west-2&sp=arn:aws:savingsplans::123:savingsplan/abc" | jq

# Lookup with dedicated tenancy
curl "http://localhost:8080/debug/cache/pricing/sp/lookup?instance_type=m5.xlarge&region=us-west-2&tenancy=dedicated&sp=arn:aws:savingsplans::123:savingsplan/abc" | jq

# Lookup Windows instance
curl "http://localhost:8080/debug/cache/pricing/sp/lookup?instance_type=m5.xlarge&region=us-west-2&os=windows&sp=arn:aws:savingsplans::123:savingsplan/abc" | jq
```

### Cache Statistics

```bash
GET /debug/cache/stats
```

Returns high-level statistics about all caches.

**Response includes:**
- **EC2 cache**: Total instance count
- **RISP cache**: Reserved Instance count, Savings Plan count
- **Pricing cache**: On-demand price count, SP rate count, spot price count, cache age, populated status

```bash
curl http://localhost:8080/debug/cache/stats | jq
```

## Common Debugging Scenarios

### Instance Not Showing Cost

**Problem**: An EC2 instance shows $0 cost in metrics.

1. Verify instance is in EC2 cache:
   ```bash
   curl http://localhost:8080/debug/cache/ec2 | jq '.[] | .[] | select(.instance_id=="i-1234567890abcdef0")'
   ```

2. Check on-demand pricing exists:
   ```bash
   curl http://localhost:8080/debug/cache/pricing/ondemand | jq '.["us-west-2"]["m5.xlarge"]'
   ```

3. If instance should use Savings Plan, check SP rate:
   ```bash
   curl "http://localhost:8080/debug/cache/pricing/sp/lookup?instance_type=m5.xlarge&region=us-west-2&sp=<arn>" | jq
   ```

### Savings Plan Not Applying

**Problem**: Savings Plan exists but instances show on-demand pricing.

1. Verify SP is discovered:
   ```bash
   curl http://localhost:8080/debug/cache/risp | jq '.savings_plans[] | select(.savings_plan_id=="abc-123")'
   ```

2. Check if SP rates are cached:
   ```bash
   curl "http://localhost:8080/debug/cache/pricing/sp?sp=arn:aws:savingsplans::123:savingsplan/abc" | jq
   ```

3. Look for specific rate:
   ```bash
   curl "http://localhost:8080/debug/cache/pricing/sp/lookup?instance_type=m5.xlarge&region=us-west-2&sp=<arn>" | jq
   ```

### Missing Rates for Instance Type

**Problem**: SP rates exist for some instance types but not others.

1. Check how many rates are cached for the SP:
   ```bash
   curl "http://localhost:8080/debug/cache/pricing/sp?sp=<arn>" | jq '.rates | length'
   ```

2. Check if instance type is running:
   ```bash
   curl http://localhost:8080/debug/cache/ec2 | jq '.. | select(.instance_type? == "r8g.large")'
   ```

3. Wait 15 seconds (SP rates reconciler runs every 15s) and check again -- incremental rate fetching should discover new instance types.

### Dedicated Instance Pricing Wrong

**Problem**: Dedicated instances showing incorrect costs.

1. Verify tenancy is detected:
   ```bash
   curl http://localhost:8080/debug/cache/ec2 | jq '.[] | .[] | select(.tenancy=="dedicated")'
   ```

2. Check SP rate with correct tenancy:
   ```bash
   curl "http://localhost:8080/debug/cache/pricing/sp/lookup?instance_type=m5.xlarge&region=us-west-2&tenancy=dedicated&sp=<arn>" | jq
   ```

3. Compare with default tenancy rate:
   ```bash
   curl "http://localhost:8080/debug/cache/pricing/sp/lookup?instance_type=m5.xlarge&region=us-west-2&tenancy=default&sp=<arn>" | jq
   ```

### Spot Pricing Missing or Stale

**Problem**: Spot instances showing $0 cost or using outdated spot prices.

1. Check if spot prices are cached:
   ```bash
   curl http://localhost:8080/debug/cache/stats | jq '.pricing.spot'
   ```

2. Verify spot price exists for specific instance type and AZ:
   ```bash
   curl http://localhost:8080/debug/cache/pricing/spot | jq '.prices[] | select(.key=="m5.large:us-west-2a")'
   ```

3. Check cache age and staleness:
   ```bash
   curl http://localhost:8080/debug/cache/pricing/spot | jq '.stats'
   ```

4. If prices are stale, wait 15 seconds (default reconciliation interval) for automatic refresh.

5. If prices still missing, check that instance type is in EC2 cache (lazy-loading only fetches prices for running instances):
   ```bash
   curl http://localhost:8080/debug/cache/ec2 | jq '.. | select(.instance_type? == "m5.large")'
   ```

## Cache Key Format

Savings Plan rates use the following cache key format:
```
spArn,instanceType,region,tenancy,os
```

Example:
```
arn:aws:savingsplans::123456789012:savingsplan/abc-123,m5.xlarge,us-west-2,default,linux
```

All keys are lowercase for case-insensitive lookups.

- Tenancy values: `default`, `dedicated`, `host`
- OS values: `linux`, `windows`

## Monitoring and Automation

Debug endpoints can be used in monitoring scripts:

```bash
#!/bin/bash
# Check cache health
STATS=$(curl -s http://localhost:8080/debug/cache/stats)
EC2_COUNT=$(echo $STATS | jq '.ec2.total_instances')
SP_COUNT=$(echo $STATS | jq '.risp.savings_plans_count')

echo "EC2 instances: $EC2_COUNT"
echo "Savings Plans: $SP_COUNT"

if [ "$EC2_COUNT" -eq 0 ]; then
  echo "WARNING: No EC2 instances in cache!"
  exit 1
fi
```

## Security Considerations

Debug endpoints expose:
- AWS account IDs
- Instance IDs and types
- Pricing data
- Savings Plan details

**Recommendations:**
- Only enable in non-production environments
- If enabled in production, protect with authentication
- Use network policies to restrict access
