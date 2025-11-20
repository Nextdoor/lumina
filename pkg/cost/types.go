/*
Copyright 2025 Lumina Contributors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package cost implements the AWS Savings Plans allocation algorithm for calculating
// per-instance costs. This package replicates AWS's billing logic to estimate how
// Reserved Instances and Savings Plans are applied to running EC2 instances.
//
// The cost calculation follows AWS's documented priority order:
//  1. Reserved Instances (RIs) - applied first to exact instance type + AZ matches
//  2. EC2 Instance Savings Plans - applied to specific instance family + region
//  3. Compute Savings Plans - applied to any instance family, any region
//  4. On-Demand pricing - applied to remaining uncovered usage
//
// The implementation uses a rate-based model ($/hour) rather than cumulative tracking
// within each billing hour. This means the calculator estimates costs based on the
// current set of running instances, assuming they continue running for the remainder
// of the hour. This approach is:
//   - Stateless: Controller restart safe, no historical state needed
//   - Real-time: Immediate visibility into current SP utilization
//   - Operational: Good enough for cost-aware provisioning decisions
//   - Approximate: May not match AWS's exact cumulative billing within an hour
//
// For billing-accurate validation, reconcile with AWS Cost Explorer API.
//
// Algorithm Reference: AWS Savings Plans documentation
// https://docs.aws.amazon.com/savingsplans/latest/userguide/sp-applying.html
package cost

import (
	"time"

	"github.com/nextdoor/lumina/pkg/aws"
)

// PricingCacheInterface defines the interface for accessing pricing data.
// This allows the calculator to retrieve prices without depending on map key formats.
type PricingCacheInterface interface {
	// GetSpotPrice retrieves the current spot price for a specific instance type + AZ + OS.
	// Returns (price, true) if found, (0, false) if not found.
	GetSpotPrice(instanceType, availabilityZone, productDescription string) (float64, bool)
}

// CoverageType represents how an instance's cost is covered.
type CoverageType string

const (
	// CoverageReservedInstance indicates the instance is covered by a Reserved Instance (pre-paid)
	CoverageReservedInstance CoverageType = "reserved_instance"

	// CoverageEC2InstanceSavingsPlan indicates the instance is covered by an EC2 Instance Savings Plan
	CoverageEC2InstanceSavingsPlan CoverageType = "ec2_instance_savings_plan"

	// CoverageComputeSavingsPlan indicates the instance is covered by a Compute Savings Plan
	CoverageComputeSavingsPlan CoverageType = "compute_savings_plan"

	// CoverageSpot indicates the instance is a spot instance paying current market rates
	CoverageSpot CoverageType = "spot"

	// CoverageOnDemand indicates the instance is paying on-demand rates with no discounts
	CoverageOnDemand CoverageType = "on_demand"
)

// PricingAccuracy indicates whether the cost calculation uses actual AWS API data
// or estimated/fallback values.
type PricingAccuracy string

const (
	// PricingAccurate indicates the cost is based on actual data from AWS APIs:
	//   - Reserved Instance: actual RI rates
	//   - Savings Plan: actual SP rates from DescribeSavingsPlanRates
	//   - Spot: actual spot market price from AWS Spot Pricing API
	//   - On-demand: actual on-demand price from AWS Pricing API
	PricingAccurate PricingAccuracy = "accurate"

	// PricingEstimated indicates the cost uses fallback/estimated values:
	//   - Missing SP rates: using on-demand price as estimate
	//   - Missing spot price: using on-demand price as estimate
	//   - Missing on-demand price: no cost calculated
	PricingEstimated PricingAccuracy = "estimated"
)

// InstanceCost represents the calculated cost for a single EC2 instance,
// including the breakdown of how that cost was determined (RI, SP, or OnDemand).
type InstanceCost struct {
	// InstanceID is the EC2 instance ID (e.g., "i-abc123")
	InstanceID string

	// InstanceType is the EC2 instance type (e.g., "m5.xlarge")
	InstanceType string

	// Region is the AWS region (e.g., "us-west-2")
	Region string

	// AccountID is the AWS account ID
	AccountID string

	// AvailabilityZone is the AZ where the instance is running (e.g., "us-west-2a")
	AvailabilityZone string

	// ShelfPrice is the full on-demand hourly cost with NO discounts applied ($/hour).
	// This is the published AWS on-demand price for the instance type.
	// Always non-zero for running instances.
	ShelfPrice float64

	// EffectiveCost is the actual estimated hourly cost after applying all discounts
	// (RIs, Savings Plans, spot pricing) in $/hour.
	// This is what we estimate the instance costs per hour considering all coverage.
	EffectiveCost float64

	// CoverageType indicates how this instance's cost is primarily determined.
	// Use the Coverage* constants defined in this package.
	CoverageType CoverageType

	// PricingAccuracy indicates whether the cost calculation uses actual AWS API data
	// or estimated/fallback values. Use the PricingAccurate or PricingEstimated constants.
	PricingAccuracy PricingAccuracy

	// RICoverage is the amount of cost covered by a Reserved Instance ($/hour).
	// For RI-covered instances, this is typically equal to ShelfPrice, and
	// EffectiveCost is typically $0 (since RIs are pre-paid).
	RICoverage float64

	// SavingsPlanCoverage is the amount of cost covered by any Savings Plan
	// (EC2 Instance SP or Compute SP) in $/hour.
	// If multiple SPs apply (shouldn't happen per AWS rules), this is the total.
	SavingsPlanCoverage float64

	// SavingsPlanARN is the ARN of the Savings Plan that provided coverage,
	// if applicable. Empty string if no SP coverage.
	SavingsPlanARN string

	// OnDemandCost is the remaining cost charged at on-demand rates ($/hour)
	// after all discounts have been applied. This represents "spillover" when
	// SP commitments are exhausted.
	OnDemandCost float64

	// SpotPrice is the current spot market price if this is a spot instance ($/hour).
	// Zero for on-demand instances.
	SpotPrice float64

	// IsSpot indicates whether this instance is a spot instance
	IsSpot bool

	// Lifecycle is the EC2 instance lifecycle type (e.g., "on-demand", "spot", "scheduled")
	// This is used for metric labeling to distinguish instance types
	Lifecycle string
}

// SavingsPlanUtilization represents the current utilization state of a single
// Savings Plan, calculated based on the instances currently running.
//
// This is a rate-based calculation ($/hour), not cumulative spending over time.
// The metrics represent instantaneous utilization assuming current instances
// keep running for the remainder of the hour.
type SavingsPlanUtilization struct {
	// SavingsPlanARN is the unique identifier for this Savings Plan
	SavingsPlanARN string

	// AccountID is the AWS account that owns this Savings Plan
	AccountID string

	// Type is the Savings Plan type: "ec2_instance" or "compute"
	Type string

	// Region is the AWS region for EC2 Instance SPs (e.g., "us-west-2"),
	// or "all" for Compute SPs which apply globally.
	Region string

	// InstanceFamily is the instance family for EC2 Instance SPs (e.g., "m5"),
	// or "all" for Compute SPs which apply to any instance family.
	InstanceFamily string

	// HourlyCommitment is the fixed $/hour commitment amount for this Savings Plan.
	// This value does not change and represents the maximum discount available per hour.
	HourlyCommitment float64

	// CurrentUtilizationRate is the instantaneous $/hour rate being consumed by
	// currently running instances. This is a snapshot based on what's running NOW.
	// Can exceed HourlyCommitment (overflow goes to OnDemand rates).
	CurrentUtilizationRate float64

	// RemainingCapacity is the unused SP capacity in $/hour.
	// Calculated as: HourlyCommitment - CurrentUtilizationRate
	// Can be negative (over-utilization, spillover to OnDemand).
	RemainingCapacity float64

	// UtilizationPercent is the utilization as a percentage of commitment.
	// Calculated as: (CurrentUtilizationRate / HourlyCommitment) * 100
	// Can exceed 100% if over-utilized.
	UtilizationPercent float64

	// RemainingHours is the number of hours until this Savings Plan expires.
	// Useful for alerting on upcoming expirations.
	RemainingHours float64

	// EndTime is when this Savings Plan expires
	EndTime time.Time
}

// CalculationInput contains all the data needed to run the cost calculation algorithm.
// This represents a point-in-time snapshot of the organization's compute resources
// and discount instruments.
type CalculationInput struct {
	// Instances is the complete list of all running EC2 instances across the
	// entire AWS organization. Must include ALL instances (not just Kubernetes nodes)
	// because Savings Plans apply org-wide to all compute.
	Instances []aws.Instance

	// ReservedInstances is the list of all active Reserved Instances across
	// the organization. RIs are applied before Savings Plans.
	ReservedInstances []aws.ReservedInstance

	// SavingsPlans is the list of all active Savings Plans across the organization.
	// Includes both EC2 Instance SPs and Compute SPs.
	SavingsPlans []aws.SavingsPlan

	// PricingCache provides access to on-demand and spot pricing data.
	// The calculator uses accessor methods (GetSpotPrice, GetOnDemandPrice) to
	// retrieve prices, avoiding fragile key format dependencies.
	PricingCache PricingCacheInterface

	// OnDemandPrices maps instance-type+region to on-demand price ($/hour).
	// Key format: "instance_type:region" (e.g., "m5.xlarge:us-west-2")
	// This is the shelf price with no discounts applied.
	OnDemandPrices map[string]float64
}

// CalculationResult contains the output of running the cost calculation algorithm.
// This includes per-instance costs and Savings Plans utilization metrics.
type CalculationResult struct {
	// InstanceCosts maps instance ID to its calculated cost breakdown.
	// Only instances that are running will have entries.
	InstanceCosts map[string]InstanceCost

	// SavingsPlanUtilization maps Savings Plan ARN to its utilization state.
	// Includes all Savings Plans, even if unutilized (utilization = 0).
	SavingsPlanUtilization map[string]SavingsPlanUtilization

	// CalculatedAt is when this calculation was performed.
	// Used for tracking data freshness.
	CalculatedAt time.Time

	// TotalEstimatedCost is the sum of all instance EffectiveCosts ($/hour).
	// This is the total estimated compute cost across the organization.
	TotalEstimatedCost float64

	// TotalShelfPrice is the sum of all instance ShelfPrices ($/hour).
	// This is what the compute would cost with no discounts.
	TotalShelfPrice float64

	// TotalSavings is the difference between shelf price and effective cost ($/hour).
	// Calculated as: TotalShelfPrice - TotalEstimatedCost
	TotalSavings float64
}
