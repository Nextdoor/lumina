# AWS Test Data Scenarios

This package contains comprehensive test data fixtures for validating AWS cost calculation logic.

## Overview

The test scenarios provide realistic AWS environments with multiple accounts, regions, instances, Reserved Instances, Savings Plans, and spot pricing data. These scenarios can be loaded into `MockClient` instances for integration testing.

## Available Scenarios

### SimpleScenario

A single-account setup for basic testing:

- **Accounts**: 1 (Production)
- **Regions**: 1 (us-west-2)
- **Instances**: 10 total
  - 5 covered by Regional Reserved Instances (m5.xlarge)
  - 3 covered by Compute Savings Plan (m5.2xlarge)
  - 2 on-demand instances (t3.large)
- **Reserved Instances**: 2 (Regional RIs for flexibility)
- **Savings Plans**: 1 (Compute Savings Plan with $2/hour commitment)
- **Expected Monthly Cost**: ~$1,168
- **Expected Utilization**: 51% SP, 100% RI

**Use cases**:
- Unit testing cost calculation logic
- Testing basic RI and SP allocation
- Validating on-demand pricing fallback

### ComplexScenario

A realistic multi-account enterprise setup:

- **Accounts**: 3 (Production, Staging, Development)
- **Regions**: 3 (us-west-2, us-east-1, eu-west-1)
- **Instances**: 52 total
  - Production: 33 instances (web, app, data tiers + DR)
  - Staging: 4 instances (pre-production testing)
  - Development: 15 instances (5 on-demand + 10 spot for CI/CD)
- **Reserved Instances**: 4 total across accounts
  - Production: Regional and zonal RIs
  - Staging: Minimal RI coverage
  - Development: No RIs (spot + on-demand)
- **Savings Plans**: 2 in production
  - Organization-wide Compute Savings Plan ($10/hour)
  - EC2 Instance Savings Plan for r5 family ($2.50/hour)
- **Expected Monthly Cost**: ~$15,000
  - Production: $10,000 (66%)
  - Staging: $3,000 (20%)
  - Development: $2,000 (14%)
- **Expected Utilization**: 85% SP, 92% RI

**Use cases**:
- Testing cross-account SP allocation
- Testing multi-region deployments
- Testing DR capacity planning
- Testing spot instance pricing
- Validating cost attribution across accounts

## Usage

### Loading a Scenario

```go
import (
    "github.com/nextdoor/lumina/pkg/aws"
    "github.com/nextdoor/lumina/pkg/aws/testdata"
)

func TestCostCalculation(t *testing.T) {
    // Create a mock client
    client := &aws.MockClient{
        EC2Clients:          make(map[string]*aws.MockEC2Client),
        SavingsPlansClients: make(map[string]*aws.MockSavingsPlansClient),
        PricingClientInstance: &aws.MockPricingClient{
            OnDemandPrices: make(map[string]*aws.OnDemandPrice),
        },
    }

    // Load the scenario
    testdata.LoadScenario(testdata.SimpleScenario, client)

    // Now use the client for testing
    ec2Client, _ := client.EC2(ctx, aws.AccountConfig{
        AccountID: "111111111111",
        Region:    "us-west-2",
    })

    instances, _ := ec2Client.DescribeInstances(ctx, []string{"us-west-2"})
    // instances now contains all 10 instances from SimpleScenario
}
```

### Accessing Expected Outcomes

Each scenario includes expected outcomes for validation:

```go
scenario := testdata.ComplexScenario

// Validate total cost
if calculatedCost != scenario.Expected.TotalMonthlyCost {
    t.Errorf("expected total cost %f, got %f",
        scenario.Expected.TotalMonthlyCost, calculatedCost)
}

// Validate per-account costs
for accountID, expectedCost := range scenario.Expected.CostByAccount {
    if accountCosts[accountID] != expectedCost {
        t.Errorf("account %s: expected cost %f, got %f",
            accountID, expectedCost, accountCosts[accountID])
    }
}

// Validate Savings Plan utilization
if spUtilization != scenario.Expected.SavingsPlanUtilization {
    t.Errorf("expected SP utilization %f%%, got %f%%",
        scenario.Expected.SavingsPlanUtilization, spUtilization)
}
```

## Scenario Structure

Each scenario consists of:

### Accounts
- `ID`: AWS account ID (12 digits)
- `Name`: Human-readable account name
- `Region`: Default region for the account
- `Instances`: List of EC2 instances
- `ReservedInstances`: List of RIs owned by this account
- `SavingsPlans`: List of SPs owned by this account
- `SpotPrices`: Current spot prices for this account's regions

### Expected Outcomes
- `TotalMonthlyCost`: Expected total monthly cost across all accounts
- `CostByAccount`: Expected costs broken down by account ID
- `CostByRegion`: Expected costs broken down by region
- `SavingsPlanUtilization`: Expected SP utilization percentage
- `ReservedInstanceUtilization`: Expected RI utilization percentage

## Pricing Data

All scenarios include realistic pricing data:

- **On-demand prices**: Set via `getDefaultOnDemandPrice()` helper
- **Spot prices**: Current market rates per AZ
- **RI pricing**: Reflected in instance allocation
- **SP pricing**: Hourly commitment amounts

Pricing is automatically loaded into `MockPricingClient` when using `LoadScenario()`.

## Instance Details

All instances include comprehensive metadata:

- **Basic info**: Instance ID, type, region, AZ, state
- **Launch info**: Launch time, platform, lifecycle (on-demand/spot)
- **Network info**: Private DNS name, private IP
- **Tags**: Environment, tier, team, purpose, etc.
- **Spot info**: Spot request ID (for spot instances)

This allows testing scenarios that correlate instances with Kubernetes nodes via private DNS names or IP addresses.

## Adding New Scenarios

To add a new scenario:

1. Create a new file `scenario_<name>.go` in this package
2. Define your scenario using the `Scenario` struct:

```go
var MyScenario = Scenario{
    Name:        "my-scenario",
    Description: "Description of what this tests",
    Accounts: []Account{
        {
            ID:     "123456789012",
            Name:   "my-account",
            Region: "us-west-2",
            Instances:         []aws.Instance{ /* ... */ },
            ReservedInstances: []aws.ReservedInstance{ /* ... */ },
            SavingsPlans:      []aws.SavingsPlan{ /* ... */ },
            SpotPrices:        []aws.SpotPrice{ /* ... */ },
        },
    },
    Expected: ExpectedOutcomes{
        TotalMonthlyCost: 5000.0,
        // ... more expected values
    },
}
```

3. Add integration tests using the new scenario
4. Update this README with scenario details

## Best Practices

1. **Use realistic data**: Base instance types, regions, and pricing on actual AWS offerings
2. **Include edge cases**: Test scenarios should cover boundary conditions
3. **Document assumptions**: Explain expected outcomes and how they were calculated
4. **Keep scenarios focused**: Each scenario should test specific cost calculation behaviors
5. **Update expected outcomes**: As cost calculation logic evolves, update expected values

## Testing Philosophy

These scenarios follow Lumina's testing philosophy:

- **100% coverage required**: All scenarios must have corresponding tests
- **Integration over unit**: Test realistic workflows, not isolated functions
- **Public-ready code**: All data must be generic (no internal references)
- **Documented expectations**: Expected outcomes must be clear and justified
