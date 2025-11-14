# LocalStack Test Environment

This directory contains the LocalStack setup for end-to-end integration testing of Lumina's AWS integration, particularly STS AssumeRole operations.

## Overview

LocalStack is a fully functional local AWS cloud stack that allows testing AWS integrations without accessing real AWS accounts. This setup:

- Creates IAM roles with trust policies for AssumeRole testing
- Provides a consistent, reproducible test environment
- Enables integration tests without AWS credentials or costs

## Quick Start

```bash
# Start LocalStack
docker-compose up -d

# Wait for LocalStack to be ready
docker-compose ps

# Run integration tests
cd ../..
go test -v ./pkg/aws/... -tags=localstack

# Stop LocalStack
docker-compose down
```

## Architecture

### Services Enabled

- **STS**: For AssumeRole operations
- **IAM**: For role and policy management
- **EC2**: For instance and Reserved Instance queries
- **Savings Plans**: For Savings Plan queries

### Pre-configured Resources

The `init/01-create-roles.sh` script creates:

1. **LuminaTestRole** (`arn:aws:iam::000000000000:role/lumina/LuminaTestRole`)
   - Trust policy allowing any principal to assume the role
   - Attached policy granting EC2 and Savings Plans read access

2. **LuminaStagingRole** (`arn:aws:iam::000000000000:role/lumina/LuminaStagingRole`)
   - Similar configuration for multi-account testing
   - Different role name to simulate cross-account scenarios

## Test Credentials

LocalStack uses fixed test credentials (not real AWS credentials):

```bash
AWS_ACCESS_KEY_ID=test
AWS_SECRET_ACCESS_KEY=test
AWS_DEFAULT_REGION=us-west-2
```

These credentials work with any LocalStack instance and do not grant access to real AWS resources.

## Network Configuration

- **LocalStack Gateway**: `http://localhost:4566`
- **Health Check Endpoint**: `http://localhost:4566/_localstack/health`

## Usage in Tests

Tests should configure the AWS SDK to use LocalStack:

```go
cfg, err := config.LoadDefaultConfig(ctx,
    config.WithRegion("us-west-2"),
    config.WithEndpointResolverWithOptions(aws.EndpointResolverWithOptionsFunc(
        func(service, region string, options ...interface{}) (aws.Endpoint, error) {
            return aws.Endpoint{
                URL: "http://localhost:4566",
            }, nil
        },
    )),
    config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
        "test", "test", "",
    )),
)
```

## Troubleshooting

### LocalStack not starting

Check Docker logs:
```bash
docker-compose logs localstack
```

### Roles not created

Verify init script ran successfully:
```bash
docker-compose exec localstack cat /var/log/localstack/init.log
```

### Connection refused errors

Ensure LocalStack is healthy:
```bash
curl http://localhost:4566/_localstack/health
```

## Limitations

LocalStack is an emulator and has some differences from real AWS:

- IAM policy evaluation is simplified (most permissions are granted)
- Some AWS API behaviors may differ slightly
- Not all AWS services are fully implemented
- Performance characteristics differ from production

These limitations are acceptable for integration testing, where we focus on testing our code's interaction with the AWS SDK rather than AWS service behavior.
