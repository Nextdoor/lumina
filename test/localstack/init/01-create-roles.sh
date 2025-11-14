#!/bin/bash
#
# LocalStack initialization script to create IAM roles for testing.
# This script runs when LocalStack starts and creates the necessary
# IAM roles and trust policies for AssumeRole testing.

set -e

echo "Creating IAM roles for e2e testing..."

# Create a trust policy that allows any principal to assume the role
# (LocalStack doesn't enforce strict IAM policies, making testing easier)
TRUST_POLICY='{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {
        "AWS": "*"
      },
      "Action": "sts:AssumeRole"
    }
  ]
}'

# Create IAM role for production account
awslocal iam create-role \
  --role-name LuminaTestRole \
  --assume-role-policy-document "$TRUST_POLICY" \
  --path "/lumina/" \
  --description "Test role for Lumina e2e testing"

# Create IAM role for staging account (different account ID)
awslocal iam create-role \
  --role-name LuminaStagingRole \
  --assume-role-policy-document "$TRUST_POLICY" \
  --path "/lumina/" \
  --description "Test role for staging account"

# Create a managed policy with permissions for EC2 and Savings Plans access
PERMISSIONS_POLICY='{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "ec2:DescribeInstances",
        "ec2:DescribeReservedInstances",
        "ec2:DescribeSpotPriceHistory",
        "savingsplans:DescribeSavingsPlans",
        "pricing:GetProducts"
      ],
      "Resource": "*"
    }
  ]
}'

awslocal iam create-policy \
  --policy-name LuminaReadOnlyPolicy \
  --policy-document "$PERMISSIONS_POLICY" \
  --description "Read-only access to EC2 and Savings Plans"

# Attach the policy to both roles
awslocal iam attach-role-policy \
  --role-name LuminaTestRole \
  --policy-arn "arn:aws:iam::000000000000:policy/LuminaReadOnlyPolicy"

awslocal iam attach-role-policy \
  --role-name LuminaStagingRole \
  --policy-arn "arn:aws:iam::000000000000:policy/LuminaReadOnlyPolicy"

echo "IAM roles created successfully:"
awslocal iam list-roles --path-prefix "/lumina/" --query 'Roles[].RoleName'
