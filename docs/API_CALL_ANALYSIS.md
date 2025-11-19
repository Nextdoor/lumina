# API Call Analysis: DescribeSavingsPlansOfferingRates

## Overview

This document analyzes the API call requirements for implementing GitHub issue #64 using the `DescribeSavingsPlansOfferingRates` API to retrieve actual Savings Plan discount rates.

**Scope**: Lumina only tracks EC2 instance costs. All API queries will filter to `products=["EC2"]` and ignore other AWS compute services (Fargate, Lambda, SageMaker, etc.).

## API Capabilities

Based on AWS documentation:

- **Max results per request**: 1,000 rates
- **Pagination**: Uses `nextToken` for continuation
- **Available filters**:
  - `savingsPlanTypes`: ["Compute", "EC2Instance"]
  - `savingsPlanPaymentOptions`: ["All Upfront", "Partial Upfront", "No Upfront"]
  - `products`: ["EC2"] ← **Lumina only cares about EC2 hosts**
  - `serviceCodes`: ["AmazonEC2"]
  - `operations`: Specific operation types
  - `usageTypes`: Specific usage types

**Scope constraint**: Lumina only tracks EC2 instance costs, so we filter to `products=["EC2"]` and ignore Fargate, Lambda, SageMaker, etc.

## Scale Factors

### AWS Environment Size

- **Regions**: ~35 AWS regions globally (Lumina defaults to 2: us-west-2, us-east-1)
- **Instance types**: ~600 EC2 instance types across all families
- **Instance families**: ~60 families (m5, m6i, c5, r5, t3, etc.)
- **SP types**: 2 (EC2Instance, Compute)
- **Payment options**: 3 (All Upfront, Partial Upfront, No Upfront)

### Typical Deployment

- **Active Savings Plans**: 5-20 per organization
- **Regions in use**: 2-5 regions
- **Instance types in use**: 20-100 types (out of 600 available)

## API Call Scenarios

### Scenario A: Full Query (Minimal Filtering)

**Approach**: Query all EC2 rates for both SP types with only product=EC2 filter.

```
Query 1: savingsPlanType = "EC2Instance", products = ["EC2"]
Estimated results: 600 EC2 types × 35 regions × 3 payment options = ~63,000 rates
API calls: 63,000 ÷ 1,000 = 63 paginated calls

Query 2: savingsPlanType = "Compute", products = ["EC2"]
Estimated results: 600 EC2 types × 35 regions × 3 payment options = ~63,000 rates
API calls: 63,000 ÷ 1,000 = 63 paginated calls

Total: ~126 API calls per reconciliation cycle
```

**Pros**:
- Simple implementation
- Gets all rates in one reconciliation
- No risk of missing rates

**Cons**:
- High API call volume
- Retrieves rates for regions/types not in use
- Long reconciliation time (~2-3 minutes at AWS rate limits)

### Scenario B: Query Per Region

**Approach**: Query EC2 rates separately for each region.

```
EC2 Instance SP rates (per region):
- 35 regions × (600 EC2 types × 3 payment options ÷ 1,000 per page)
- 35 regions × ~2 API calls = 70 calls

Compute SP rates (per region):
- Same calculation = 70 calls

Total: ~140 API calls per reconciliation cycle
```

**Pros**:
- Slightly more granular than full query
- Can parallelize by region

**Cons**:
- Still retrieves rates for unused types
- Only marginally better than Scenario A

### Scenario C: Lazy-Loading with Reconciliation (RECOMMENDED)

**Approach**: Reconcile every 1-2 minutes, lazily fetching EC2 rates only for instance types we don't have cached yet.

```
Algorithm:
1. EC2 reconciler runs (every 1-5 minutes) → updates instance inventory
2. SP Rates reconciler runs (every 1-2 minutes)
   a. Get list of all instance types from EC2 inventory (spot + on-demand)
   b. Get list of regions where instances are running
   c. Check which instance types we DON'T have SP rates for yet
   d. If new types found: Query rates for (new types × active regions × SP types)
   e. Cache results
3. Reconciliation interval provides natural debouncing

Example - Steady State (no new types):
- Reconcile every 1 minute
- All instance types already cached
- API calls: 0

Example - New Instance Type Launch:
- 5 new m7i.xlarge instances launch at 10:00:00
- 3 new c7g.2xlarge instances launch at 10:00:30
- SP Rates reconciler runs at 10:01:00 (next cycle)
- Detects 2 new instance types not in cache
- Query: 2 types × 3 regions × 2 SP types × 3 payment options = ~1 API call
- Both types batched into single reconciliation

Example - Large Autoscaling Event:
- Autoscaler launches 50 instances across 10 new types in 30 seconds
- SP Rates reconciler runs at next minute boundary
- Query: 10 types × 3 regions × 2 SP types × 3 payment options = ~1 API call
- All 10 types batched together

Worst case - First Run (empty cache):
- 100 active instance types × 5 regions = 500 type-region combinations
- Query: 100 types × 5 regions × 2 SP types × 3 payment options = ~3 API calls
- Subsequent reconciliations: 0 calls (unless new types appear)
```

**Pros**:
- Truly lazy: Only fetch rates as needed
- Natural debouncing: Reconciliation interval batches new types
- Minimal API calls in steady state (0 per reconciliation)
- Scales well: Large autoscaling events get batched
- Simple logic: Just diff cache vs current instance types
- Clear logging: "Discovered 5 new instance types, fetching SP rates..."

**Cons**:
- Slight delay: New instance types won't have accurate costs for 1-2 minutes
- Need to track "which types we've priced" in cache
- First run fetches more data (but only once)

### Scenario D: Query Per Active Savings Plan

**Approach**: For each active SP, query EC2 rates for its specific scope.

```
For each EC2 Instance SP (region-specific, family-specific):
- Query: products=["EC2"], region=SP.Region, instanceFamily=SP.Family
- Estimated results per SP: ~50 EC2 instance types × 3 payment options = ~150 rates
- API calls per SP: 1 call

For each Compute SP (region-agnostic):
- Query: products=["EC2"], all regions, all EC2 families
- Estimated results: Same as Scenario A
- API calls: 63 calls

Example with 10 EC2 Instance SPs + 2 Compute SPs:
- EC2 Instance: 10 SPs × 1 call = 10 calls
- Compute: 2 SPs × 63 calls = 126 calls
- Total: ~136 API calls
```

**Pros**:
- Efficient for EC2 Instance SPs
- Directly maps to SP coverage

**Cons**:
- Compute SPs still require many calls
- More complex logic
- Not much better than Scenario C for Compute SPs

## Reconciliation Frequency Considerations

### If Using Scenario A or B (~126-140 calls)

- **Recommended interval**: 6-12 hours
- **Reason**: High API call volume, infrequent rate changes
- **AWS rate limits**: 20 TPS for Savings Plans APIs (varies by region)
- **Estimated time**: 126 calls ÷ 20 TPS = ~7 seconds (if no throttling)

### If Using Scenario C (Lazy-Loading)

- **Recommended interval**: 1-2 minutes
- **Reason**:
  - Steady state: 0 API calls (rates already cached)
  - New instance types: 1-3 calls per reconciliation
  - Natural debouncing via reconciliation interval
- **Benefits**:
  - Fast detection of new instance types (1-2 min vs hours)
  - Minimal API overhead (only call when needed)
  - Batches multiple new types into single reconciliation
- **Estimated time**: < 1 second per reconciliation

## Critical Issue: Current vs Historical Rates

**Important limitation identified in GitHub issue #64:**

The `DescribeSavingsPlansOfferingRates` API returns **current offering rates**, NOT the rates from when a Savings Plan was purchased.

### Implications

1. **Rate changes over time**: AWS periodically adjusts SP offering rates
2. **Purchase-time rates**: When you buy an SP, you lock in the rate at that time
3. **Current rates ≠ purchased rates**: The API will give us today's rates, not the rates from 6 months ago when the SP was purchased

### Example Scenario

```
Timeline:
- Jan 2024: Purchase EC2 Instance SP for m5.xlarge at 72% discount ($0.0537/hour)
- June 2024: AWS adjusts m5.xlarge SP rate to 70% discount ($0.0576/hour)
- July 2024: Lumina queries DescribeSavingsPlansOfferingRates
  → Returns current rate: $0.0576/hour
  → Actual rate we're paying: $0.0537/hour (locked at purchase time)
  → Cost calculation will be WRONG by +7%!
```

### Mitigation Strategies

1. **Accept current rates as "close enough"**
   - Rates don't change frequently (maybe quarterly)
   - Error margin typically <5%
   - Better than hard-coded 72%/66%

2. **Cache rates at SP purchase time** (not feasible)
   - Would require capturing rates when SP is bought
   - Historical data not available for existing SPs

3. **Use Cost Explorer API** (already rejected)
   - Can infer actual rates from utilization data
   - More complex, rejected in previous attempt

4. **Hybrid approach** (RECOMMENDED)
   - Use DescribeSavingsPlansOfferingRates for **new** SPs
   - Allow configuration override for **existing** SPs with known rates
   - Fallback to current rates if no override

## Recommendations

### Recommended Approach: Scenario C (Lazy-Loading) with Hybrid Rate Handling

**Implementation plan:**

1. **Query strategy**: Lazy-loading reconciliation
   - Reconciliation interval: **1-2 minutes**
   - Steady state: **0 API calls** (rates already cached)
   - New instance types: **1-3 API calls** per reconciliation
   - Algorithm:
     ```
     On each reconciliation:
     1. Get all instance types from EC2 inventory (spot + on-demand)
     2. Get all regions where instances are running
     3. Check cache: Which (type, region, SP type) combinations are missing?
     4. If missing combinations found:
        - Batch query: DescribeSavingsPlansOfferingRates for new types
        - Update cache
        - Log: "Discovered 5 new instance types, fetched SP rates"
     5. If cache is complete: Skip API call, log: "SP rates cache up to date"
     ```

2. **Rate handling**: Three-tier lookup
   ```
   Tier 1: Per-SP rate override (config-based, for SPs with known purchase rates)
   Tier 2: DescribeSavingsPlansOfferingRates cache (current offering rates)
   Tier 3: Hard-coded percentages (72% EC2, 66% Compute) - fallback only
   ```

3. **Configuration support** (optional, for existing SPs):
   ```yaml
   savingsPlans:
     - arn: "arn:aws:savingsplans::123456789012:savingsplan/abc123"
       rates:
         "m5.xlarge": 0.0537  # Override with actual purchase-time rate
         "m5.2xlarge": 0.1074
   ```

4. **Logging requirements**:
   ```
   INFO: SP rates reconciliation started
   INFO: Discovered 3 new instance types: [m7i.xlarge, c7g.2xlarge, r6i.large]
   INFO: Fetching SP rates for 3 types × 3 regions × 2 SP types (18 combinations)
   INFO: API call completed: Retrieved 54 rates in 287ms
   INFO: Cache updated: 54 new rates added, 1,234 total rates cached

   # Subsequent reconciliation (no new types)
   INFO: SP rates reconciliation started
   INFO: All instance types have cached SP rates, skipping API call
   ```

5. **Benefits**:
   - Near-zero API calls in steady state
   - Fast response to new instance types (1-2 min detection)
   - Natural debouncing via reconciliation interval
   - Clear observability via logs
   - Accurate for new SPs (current rates)
   - Accurate for existing SPs (configured overrides)
   - Graceful fallback if API unavailable

### Alternative: Accept Current Rates (Simpler Implementation)

If configuration overhead is unacceptable:

- Use lazy-loading reconciliation (0-3 API calls per cycle)
- Accept current offering rates as "close enough"
- Document limitation: rates may differ from purchase-time rates by <5%
- Better than hard-coded 72%/66% which can be 10-20% off
- Still gets all benefits of lazy-loading (minimal API calls, fast detection)

## Cost Estimate

AWS Savings Plans API pricing: **$0.00 per API call** (covered by AWS service charges)

No direct cost impact regardless of call volume.

## Conclusion

**Recommended: Lazy-Loading Reconciliation with Hybrid Rate Handling**

- **Scope**: EC2 instances only (products=["EC2"] filter on all queries)
- **API calls**:
  - Steady state: **0 per reconciliation** (rates already cached)
  - New instance types: **1-3 per reconciliation** (vs 126 for full query)
  - First run: **3-5 calls** (fetch all active types)
- **Reconciliation interval**: **1-2 minutes** (vs 2-4 hours for batch approaches)
- **Accuracy**: High (with config overrides for existing SPs)
- **Complexity**: Moderate (worth the massive API call savings + fast detection)

**Key advantages:**

1. **Near-zero API overhead**: Only call AWS when new instance types appear
2. **Fast detection**: New types detected within 1-2 minutes, not hours
3. **Natural debouncing**: Reconciliation interval batches multiple new types
4. **Clear observability**: Explicit logging when rates are fetched vs cached
5. **Production-ready**: Handles autoscaling events gracefully

**Next steps:**

1. Decide if per-SP rate configuration is acceptable for existing SPs (optional)
2. Implement SP rates cache (track which instance types we've priced)
3. Implement lazy-loading reconciler (1-2 minute interval)
4. Implement three-tier rate lookup (config → API cache → fallback)
5. Add comprehensive logging for observability
6. Add metrics for rate cache hits/misses/additions
7. Document rate accuracy limitations in user guide
