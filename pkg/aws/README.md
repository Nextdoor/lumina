# AWS Client Package

This package provides an abstraction layer for interacting with AWS services needed by Lumina for cost calculation.

## Overview

The AWS client package provides:
- **Interfaces** for EC2, Savings Plans, and Pricing APIs
- **Mock implementations** for testing
- **Built-in AssumeRole support** for cross-account access
- **Type-safe data structures** for AWS resources

## Design Principles

1. **Interface-based**: All AWS interactions are through interfaces, enabling easy mocking and testing
2. **Cross-account first**: AssumeRole support is a first-class feature, not an afterthought
3. **Testability**: Mock implementations mirror real implementations for realistic tests
4. **Type safety**: Strong typing for all AWS resources and responses

## Usage

### Basic Client Creation

```go
import "github.com/nextdoor/lumina/pkg/aws"

// Create client configuration
config := aws.ClientConfig{
    DefaultRegion: "us-west-2",
    MaxRetries:    3,
    RetryDelay:    time.Second,
    HTTPTimeout:   30 * time.Second,
}

// Create client (actual implementation)
client, err := aws.NewClient(config)
if err != nil {
    // handle error
}
```

### Cross-Account Access with AssumeRole

```go
// Configure account with AssumeRole
accountConfig := aws.AccountConfig{
    AccountID:     "111111111111",
    Name:          "Production",
    AssumeRoleARN: "arn:aws:iam::111111111111:role/lumina-cost-controller",
    SessionName:   "lumina-controller",
    Region:        "us-west-2",
}

// Get EC2 client for this account
ec2Client, err := client.EC2(ctx, accountConfig)
if err != nil {
    // handle error
}

// Query instances (automatically uses assumed role)
instances, err := ec2Client.DescribeInstances(ctx, []string{"us-west-2", "us-east-1"})
```

### Querying EC2 Instances

```go
// Get all running instances across all regions
instances, err := ec2Client.DescribeInstances(ctx, nil)

// Get instances in specific regions
instances, err := ec2Client.DescribeInstances(ctx, []string{"us-west-2", "us-east-1"})

// Get a specific instance by ID
instance, err := ec2Client.GetInstanceByID(ctx, "us-west-2", "i-abc123")
```

### Querying Reserved Instances

```go
// Get all active RIs
ris, err := ec2Client.DescribeReservedInstances(ctx, nil)

// Get RIs in specific regions
ris, err := ec2Client.DescribeReservedInstances(ctx, []string{"us-west-2"})
```

### Querying Spot Prices

```go
// Get current spot prices for all instance types
spotPrices, err := ec2Client.DescribeSpotPriceHistory(ctx, []string{"us-west-2"}, nil)

// Get spot prices for specific instance types
spotPrices, err := ec2Client.DescribeSpotPriceHistory(ctx,
    []string{"us-west-2"},
    []string{"m5.xlarge", "c5.2xlarge"})
```

### Querying Savings Plans

```go
// Get Savings Plans client
spClient, err := client.SavingsPlans(ctx, accountConfig)

// Get all active Savings Plans
savingsPlans, err := spClient.DescribeSavingsPlans(ctx)

// Get specific Savings Plan by ARN
sp, err := spClient.GetSavingsPlanByARN(ctx, "arn:aws:savingsplans::111111111111:savingsplan/sp-123")
```

### Querying On-Demand Prices

```go
// Get pricing client (no account-specific credentials needed)
pricingClient := client.Pricing(ctx)

// Get on-demand price for single instance type
price, err := pricingClient.GetOnDemandPrice(ctx, "us-west-2", "m5.xlarge", "Linux")

// Get prices for multiple instance types (more efficient)
prices, err := pricingClient.GetOnDemandPrices(ctx, "us-west-2",
    []string{"m5.xlarge", "c5.2xlarge", "r5.large"},
    "Linux")
```

## Testing with Mocks

The package provides comprehensive mock implementations for testing:

```go
import (
    "testing"
    "github.com/nextdoor/lumina/pkg/aws"
)

func TestMyFunction(t *testing.T) {
    // Create mock client
    mockClient := aws.NewMockClient()

    // Set up mock EC2 data for a specific account
    accountConfig := aws.AccountConfig{
        AccountID: "111111111111",
        Region:    "us-west-2",
    }

    ec2Client, _ := mockClient.EC2(context.Background(), accountConfig)
    mockEC2 := ec2Client.(*aws.MockEC2Client)

    // Add mock instances
    mockEC2.Instances = []aws.Instance{
        {
            InstanceID:   "i-abc123",
            InstanceType: "m5.xlarge",
            Region:       "us-west-2",
            State:        "running",
            Lifecycle:    "on-demand",
        },
    }

    // Add mock Reserved Instances
    mockEC2.ReservedInstances = []aws.ReservedInstance{
        {
            ReservedInstanceID: "ri-123",
            InstanceType:       "m5.xlarge",
            Region:             "us-west-2",
            State:              "active",
            InstanceCount:      1,
        },
    }

    // Add mock spot prices
    mockEC2.SpotPrices = []aws.SpotPrice{
        {
            InstanceType:     "m5.xlarge",
            AvailabilityZone: "us-west-2a",
            SpotPrice:        0.034,
            Timestamp:        time.Now(),
        },
    }

    // Set up mock Savings Plans
    spClient, _ := mockClient.SavingsPlans(context.Background(), accountConfig)
    mockSP := spClient.(*aws.MockSavingsPlansClient)

    mockSP.SavingsPlans = []aws.SavingsPlan{
        {
            SavingsPlanARN:  "arn:aws:savingsplans::111111111111:savingsplan/sp-123",
            SavingsPlanType: "EC2Instance",
            Commitment:      150.00,
            Region:          "us-west-2",
            InstanceFamily:  "m5",
            State:           "active",
        },
    }

    // Set up mock pricing
    mockClient.PricingClientInstance.SetOnDemandPrice("us-west-2", "m5.xlarge", "Linux", 0.192)

    // Now test your code with the mock client
    // ...

    // Verify AssumeRole was called correctly
    if len(mockClient.AssumeRoleCalls) != 2 {
        t.Errorf("expected 2 AssumeRole calls (EC2 + SP), got %d", len(mockClient.AssumeRoleCalls))
    }

    // Verify method call counts
    if mockEC2.DescribeInstancesCallCount != 1 {
        t.Errorf("expected 1 DescribeInstances call, got %d", mockEC2.DescribeInstancesCallCount)
    }
}
```

## Type Reference

### AccountConfig

Configuration for accessing an AWS account with optional AssumeRole:

```go
type AccountConfig struct {
    AccountID     string  // AWS account ID
    Name          string  // Human-readable name
    AssumeRoleARN string  // Role ARN for cross-account access (optional)
    ExternalID    string  // External ID for enhanced security (optional)
    SessionName   string  // Session name for AssumeRole (optional)
    Region        string  // Default region
}
```

### Instance

Represents an EC2 instance:

```go
type Instance struct {
    InstanceID       string
    InstanceType     string
    AvailabilityZone string
    Region           string
    Lifecycle        string  // "spot" or "on-demand"
    State            string  // "running", "stopped", etc.
    LaunchTime       time.Time
    AccountID        string
    Tags             map[string]string
    PrivateDNSName   string
    PrivateIPAddress string
    Platform         string
}
```

### ReservedInstance

Represents an EC2 Reserved Instance:

```go
type ReservedInstance struct {
    ReservedInstanceID string
    InstanceType       string
    AvailabilityZone   string  // or "regional"
    Region             string
    InstanceCount      int32
    State              string
    Start              time.Time
    End                time.Time
    OfferingClass      string  // "standard" or "convertible"
    AccountID          string
}
```

### SavingsPlan

Represents an AWS Savings Plan:

```go
type SavingsPlan struct{
    SavingsPlanARN    string
    SavingsPlanID     string
    SavingsPlanType   string  // "EC2Instance" or "Compute"
    State             string
    Commitment        float64  // hourly commitment in USD
    Region            string   // or "all" for Compute SPs
    InstanceFamily    string   // e.g., "m5" (EC2 Instance SPs only)
    Start             time.Time
    End               time.Time
    AccountID         string
}
```

## Implementation Status

- ✅ **Interfaces defined**: Client, EC2Client, SavingsPlansClient, PricingClient
- ✅ **Types defined**: All AWS resource types
- ✅ **Mock implementations**: Full mock support for testing
- ✅ **Tests**: Comprehensive unit tests (89.8% coverage)
- ⏳ **Real AWS implementation**: Coming in next phase (will use aws-sdk-go-v2)

## Future Enhancements

- Real AWS SDK implementation (aws-sdk-go-v2)
- Automatic retry with exponential backoff
- Rate limiting per AWS account
- Metrics collection for AWS API calls
- Caching layer for frequently accessed data
- Support for additional AWS services (Lambda, Fargate)

## Contributing

When adding new functionality:

1. **Update interfaces** in `client.go`
2. **Update mock implementations** in `client_mock.go`
3. **Add comprehensive tests** in `*_test.go` files
4. **Maintain 100% test coverage** (or justify coverage:ignore)
5. **Update this README** with usage examples

## License

Apache 2.0 - See LICENSE file for details.
