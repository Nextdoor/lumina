// Copyright 2025 Lumina Contributors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package aws provides abstractions for interacting with AWS services.
//
// This file contains pure data structure definitions with no logic.
// These types are thoroughly tested through their usage in mock tests
// and integration tests, so direct unit tests would provide no value.

package aws

import (
	"time"
)

// Platform constants for operating system types.
// These are normalized, lowercase values used throughout the codebase.
const (
	PlatformLinux   = "linux"
	PlatformWindows = "windows"
)

// Lifecycle constants for EC2 instance types.
const (
	LifecycleOnDemand = "on-demand"
	LifecycleSpot     = "spot"
)

// Tenancy constants for EC2 instance and Savings Plans tenancy types.
// EC2 API uses "default", "dedicated", "host".
// Savings Plans API uses "shared", "dedicated", "host".
const (
	TenancyDefault   = "default"   // EC2: Shared hardware (most common, ~99% of instances)
	TenancyDedicated = "dedicated" // Both: Dedicated instance
	TenancyHost      = "host"      // Both: Dedicated host
	TenancyShared    = "shared"    // Savings Plans: Shared/default tenancy
)

// AccountConfig represents configuration for accessing an AWS account.
// Supports both direct credentials and AssumeRole-based access.
type AccountConfig struct {
	// AccountID is the AWS account ID (e.g., "111111111111")
	AccountID string

	// Name is a human-readable name for this account (e.g., "Production")
	Name string

	// AssumeRoleARN is the ARN of the role to assume for cross-account access.
	// If empty, uses the default credential chain.
	// Example: "arn:aws:iam::111111111111:role/lumina-cost-controller"
	AssumeRoleARN string

	// ExternalID is an optional external ID for AssumeRole operations.
	// Used for enhanced security when assuming roles across accounts.
	ExternalID string

	// SessionName is the name to use for AssumeRole sessions.
	// Defaults to "lumina-controller" if not specified.
	SessionName string

	// Region is the default AWS region for API calls.
	// Can be overridden per-API call if needed.
	Region string
}

// Instance represents an EC2 instance with relevant cost information.
type Instance struct {
	// InstanceID is the EC2 instance ID (e.g., "i-abc123def456")
	InstanceID string

	// InstanceType is the instance type (e.g., "m5.xlarge")
	InstanceType string

	// AvailabilityZone is the AZ where the instance is running
	AvailabilityZone string

	// Region is the AWS region
	Region string

	// Lifecycle is either "spot" or "on-demand"
	Lifecycle string

	// State is the current instance state (e.g., "running", "stopped")
	State string

	// LaunchTime is when the instance was launched
	LaunchTime time.Time

	// AccountID is the AWS account that owns this instance
	AccountID string

	// Tags are the EC2 instance tags
	Tags map[string]string

	// PrivateDNSName is the internal DNS name (useful for K8s correlation)
	PrivateDNSName string

	// PrivateIPAddress is the private IP address
	PrivateIPAddress string

	// Platform is the OS platform (e.g., "linux", "windows")
	Platform string

	// Tenancy indicates whether the instance runs on shared or dedicated hardware
	// Values: "default" (shared), "dedicated", "host"
	Tenancy string

	// SpotInstanceRequestID is the spot instance request ID if this is a spot instance
	SpotInstanceRequestID string
}

// ReservedInstance represents an EC2 Reserved Instance.
type ReservedInstance struct {
	// ReservedInstanceID is the unique identifier
	ReservedInstanceID string

	// InstanceType is the instance type this RI covers
	InstanceType string

	// AvailabilityZone is the AZ (or "regional" for regional RIs)
	AvailabilityZone string

	// Region is the AWS region
	Region string

	// InstanceCount is the number of instances this RI covers
	InstanceCount int32

	// State is the RI state (e.g., "active", "retired")
	State string

	// Start is when the RI started
	Start time.Time

	// End is when the RI expires
	End time.Time

	// OfferingClass is "standard" or "convertible"
	OfferingClass string

	// OfferingType is the payment option ("All Upfront", "Partial Upfront", "No Upfront")
	OfferingType string

	// Platform is the operating system ("Linux/UNIX", "Windows", etc.)
	Platform string

	// AccountID is the AWS account that owns this RI
	AccountID string
}

// SavingsPlan represents an AWS Savings Plan.
type SavingsPlan struct {
	// SavingsPlanARN is the unique ARN identifier
	SavingsPlanARN string

	// SavingsPlanID is the short ID
	SavingsPlanID string

	// SavingsPlanType is either "EC2Instance" or "Compute"
	SavingsPlanType string

	// State is the current state (e.g., "active", "retired")
	State string

	// Commitment is the hourly commitment amount in USD (e.g., 150.00)
	Commitment float64

	// Region is the AWS region (or "all" for Compute SPs)
	Region string

	// InstanceFamily is the instance family for EC2 Instance SPs (e.g., "m5")
	// Empty for Compute SPs (applies to all families)
	InstanceFamily string

	// Start is when the SP started
	Start time.Time

	// End is when the SP expires
	End time.Time

	// AccountID is the AWS account that owns this SP
	AccountID string

	// EC2InstanceFamily is the specific instance family for EC2 Instance SPs
	// (legacy field for compatibility)
	EC2InstanceFamily string
}

// SpotPrice represents the current spot price for an instance type.
type SpotPrice struct {
	// InstanceType is the instance type
	InstanceType string

	// AvailabilityZone is the AZ
	AvailabilityZone string

	// SpotPrice is the current hourly spot price in USD
	SpotPrice float64

	// Timestamp is when AWS recorded this price change (from AWS API)
	Timestamp time.Time

	// FetchedAt is when we retrieved this data from AWS
	// This is used to determine cache staleness
	FetchedAt time.Time

	// ProductDescription is the OS type (e.g., "Linux/UNIX")
	ProductDescription string
}

// OnDemandPrice represents the on-demand price for an instance type.
type OnDemandPrice struct {
	// InstanceType is the instance type
	InstanceType string

	// Region is the AWS region
	Region string

	// PricePerHour is the on-demand hourly price in USD
	PricePerHour float64

	// OperatingSystem is the OS type (e.g., "Linux")
	OperatingSystem string

	// Tenancy is "Shared", "Dedicated", or "Host"
	Tenancy string
}

// CostEstimate represents a cost estimate for an instance.
type CostEstimate struct {
	// InstanceID is the EC2 instance ID
	InstanceID string

	// EstimatedHourlyCost is the estimated cost per hour with discounts
	EstimatedHourlyCost float64

	// ShelfPrice is the full on-demand price (no discounts)
	ShelfPrice float64

	// CoverageType indicates what's covering this instance:
	// "reserved_instance", "savings_plan", "on_demand", or "spot"
	CoverageType string

	// SavingsPlanARN is the ARN of the SP covering this (if applicable)
	SavingsPlanARN string

	// Timestamp is when this estimate was calculated
	Timestamp time.Time
}

// SavingsPlanRate represents the actual rate for a specific purchased Savings Plan.
// This is the PURCHASE-TIME rate that was locked in when the SP was bought.
type SavingsPlanRate struct {
	// SavingsPlanId is the SP ID (e.g., "a0ea018f-ddb7-44b1-ae44-ae2dd4292dda")
	SavingsPlanId string

	// SavingsPlanARN is the full SP ARN
	SavingsPlanARN string

	// InstanceType is the EC2 instance type (e.g., "c6i.large")
	InstanceType string

	// Region is the AWS region extracted from usageType (e.g., "ap-northeast-1")
	Region string

	// Rate is the Savings Plan rate in USD per hour
	// This is the actual discounted rate for this SP
	Rate float64

	// Currency is typically "USD"
	Currency string

	// Unit is typically "Hrs"
	Unit string

	// ProductType is typically "EC2"
	ProductType string

	// ServiceCode is typically "AmazonEC2"
	ServiceCode string

	// UsageType is the full usage type (e.g., "APN1-DedicatedUsage:c6i.large")
	UsageType string

	// Operation is the operation (e.g., "RunInstances:0202")
	Operation string

	// ProductDescription is the OS type (e.g., "Linux/UNIX", "Windows")
	ProductDescription string

	// Tenancy is "shared", "dedicated", or "host"
	Tenancy string
}
