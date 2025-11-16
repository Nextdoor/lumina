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

package seed

// IAMFixtures defines all IAM resources to seed into LocalStack.
// These fixtures create the necessary roles and policies for E2E testing.
type IAMFixtures struct {
	Roles    []IAMRole   `json:"roles"`
	Policies []IAMPolicy `json:"policies"`
}

// IAMRole represents an IAM role to be created in LocalStack.
type IAMRole struct {
	// RoleName is the name of the IAM role (e.g., "LuminaTestRole")
	RoleName string `json:"role_name"`

	// Path is the IAM path for the role (e.g., "/lumina/")
	Path string `json:"path"`

	// Description provides context about the role's purpose
	Description string `json:"description"`

	// AttachedPolicies lists policy ARNs to attach to this role
	// Example: ["arn:aws:iam::000000000000:policy/LuminaReadOnlyPolicy"]
	AttachedPolicies []string `json:"attached_policies"`
}

// IAMPolicy represents an IAM policy to be created in LocalStack.
type IAMPolicy struct {
	// PolicyName is the name of the IAM policy (e.g., "LuminaReadOnlyPolicy")
	PolicyName string `json:"policy_name"`

	// Description provides context about the policy's purpose
	Description string `json:"description"`

	// PolicyDocument is the JSON policy document defining permissions
	PolicyDocument PolicyDocument `json:"policy_document"`
}

// PolicyDocument represents an IAM policy document structure.
// This follows the standard AWS IAM policy format.
type PolicyDocument struct {
	Version   string      `json:"Version"`
	Statement []Statement `json:"Statement"`
}

// Statement represents a single statement in an IAM policy.
type Statement struct {
	Effect   string      `json:"Effect"`   // "Allow" or "Deny"
	Action   []string    `json:"Action"`   // List of actions (e.g., ["ec2:DescribeInstances"])
	Resource interface{} `json:"Resource"` // "*" or array of ARNs
}

// EC2Fixtures defines all EC2 resources to seed into LocalStack.
type EC2Fixtures struct {
	SecurityGroups []SecurityGroup `json:"security_groups"`
	Instances      []EC2Instance   `json:"instances"`
}

// SecurityGroup represents an EC2 security group to be created.
type SecurityGroup struct {
	// GroupName is the name of the security group
	GroupName string `json:"group_name"`

	// Description provides context about the security group
	Description string `json:"description"`

	// Region is the AWS region where the security group should be created
	Region string `json:"region"`
}

// EC2Instance represents an EC2 instance to be created in LocalStack.
type EC2Instance struct {
	// ImageID is the AMI ID (LocalStack accepts any value, e.g., "ami-12345678")
	ImageID string `json:"image_id"`

	// InstanceType is the EC2 instance type (e.g., "m5.xlarge")
	InstanceType string `json:"instance_type"`

	// Count is the number of instances to launch
	Count int `json:"count"`

	// Region is the AWS region where the instance should be created
	Region string `json:"region"`

	// Tags are key-value pairs to tag the instance
	// Example: [{"Key": "Name", "Value": "test-instance"}]
	Tags []Tag `json:"tags"`
}

// Tag represents an AWS resource tag.
type Tag struct {
	Key   string `json:"Key"`
	Value string `json:"Value"`
}

// ReservedInstanceFixtures defines Reserved Instance resources to seed.
type ReservedInstanceFixtures struct {
	ReservedInstances []ReservedInstance `json:"reserved_instances"`
}

// ReservedInstance represents a Reserved Instance to be created in LocalStack.
// Note: LocalStack's EC2 API may have limited support for Reserved Instances.
type ReservedInstance struct {
	// InstanceType is the EC2 instance type (e.g., "t3.large")
	InstanceType string `json:"instance_type"`

	// InstanceCount is the number of instances reserved
	InstanceCount int `json:"instance_count"`

	// AvailabilityZone is where the RI applies (e.g., "us-west-2a")
	AvailabilityZone string `json:"availability_zone"`

	// State is the RI state (e.g., "active", "retired")
	State string `json:"state"`

	// OfferingType is the payment type (e.g., "All Upfront", "Partial Upfront")
	OfferingType string `json:"offering_type"`
}

// SavingsPlansFixtures defines Savings Plans resources to seed.
type SavingsPlansFixtures struct {
	SavingsPlans []SavingsPlan `json:"savings_plans"`
}

// SavingsPlan represents a Savings Plan to be created in LocalStack.
// Note: LocalStack may have limited or no support for Savings Plans API.
type SavingsPlan struct {
	// SavingsPlanType is the type (e.g., "Compute", "EC2Instance")
	SavingsPlanType string `json:"savings_plan_type"`

	// Commitment is the hourly commitment in USD (e.g., "0.05")
	Commitment string `json:"commitment"`

	// State is the Savings Plan state (e.g., "active")
	State string `json:"state"`

	// Region is the AWS region (for EC2Instance type) or empty (for Compute type)
	Region string `json:"region,omitempty"`
}
