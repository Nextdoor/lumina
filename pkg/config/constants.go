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

package config

// DefaultRegions is the fallback list of AWS regions to query when no regions
// are explicitly configured. These are the most commonly used US regions.
//
// This default is used when:
//   - Config.Regions is not set in the configuration file
//   - AWSAccount.Regions is not set for a specific account
//   - Reconciler.Regions is not set during initialization
//
// The controller queries these regions for regional AWS resources like
// Reserved Instances (RIs are region-specific, unlike Savings Plans).
var DefaultRegions = []string{"us-west-2", "us-east-1"}
