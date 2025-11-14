# AWS Client Package

Abstraction layer for AWS services needed by Lumina for cost calculation.

## Overview

Provides interfaces for EC2, Savings Plans, and Pricing APIs with built-in AssumeRole support for cross-account access.

## Key Components

- **Client interfaces**: `Client`, `EC2Client`, `SavingsPlansClient`, `PricingClient`
- **Mock implementations**: Full-featured mocks for testing (`MockClient`, `MockEC2Client`, etc.)
- **Types**: AWS resource structures (`Instance`, `ReservedInstance`, `SavingsPlan`, etc.)
- **AssumeRole support**: Cross-account access via `AccountConfig`

## Quick Start

```go
// Create client
client, _ := aws.NewClient(aws.ClientConfig{DefaultRegion: "us-west-2"})

// Cross-account access with AssumeRole
accountConfig := aws.AccountConfig{
    AccountID:     "111111111111",
    AssumeRoleARN: "arn:aws:iam::111111111111:role/lumina-controller",
    Region:        "us-west-2",
}

// Get EC2 client (automatically assumes role)
ec2Client, _ := client.EC2(ctx, accountConfig)
instances, _ := ec2Client.DescribeInstances(ctx, []string{"us-west-2"})
```

## Testing

```go
mockClient := aws.NewMockClient()
ec2Client, _ := mockClient.EC2(ctx, accountConfig)
mockEC2 := ec2Client.(*aws.MockEC2Client)

// Set up test data
mockEC2.Instances = []aws.Instance{{InstanceID: "i-abc123", ...}}

// Verify AssumeRole calls
if len(mockClient.AssumeRoleCalls) != 1 { /* ... */ }
```

## Status

- ✅ Interfaces and types defined
- ✅ Mock implementations complete
- ✅ Tests: 89.8% coverage
- ⏳ Real AWS SDK implementation (coming next)

## Design Principles

1. **Interface-first** - Easy mocking and testing
2. **AssumeRole built-in** - Not an afterthought
3. **Thread-safe mocks** - Safe for concurrent tests
4. **No logic in types** - Pure data structures

See code documentation for detailed usage.
