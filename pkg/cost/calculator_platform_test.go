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

package cost

import (
	"testing"

	"github.com/nextdoor/lumina/pkg/aws"
	"github.com/stretchr/testify/assert"
)

// TestPlatformToProductDescription verifies the Platform → ProductDescription mapping.
// This function is critical for spot pricing lookups - it must correctly map EC2 Platform
// values to AWS ProductDescription format used in the Spot Pricing API.
func TestPlatformToProductDescription(t *testing.T) {
	tests := []struct {
		name     string
		platform string
		expected string
	}{
		{
			name:     "Empty string maps to Linux/UNIX",
			platform: "",
			expected: aws.ProductDescriptionLinuxUnix,
		},
		{
			name:     "linux maps to Linux/UNIX",
			platform: "linux",
			expected: aws.ProductDescriptionLinuxUnix,
		},
		{
			name:     "Linux (capitalized) maps to Linux/UNIX",
			platform: "Linux",
			expected: aws.ProductDescriptionLinuxUnix,
		},
		{
			name:     "LINUX (uppercase) maps to Linux/UNIX",
			platform: "LINUX",
			expected: aws.ProductDescriptionLinuxUnix,
		},
		{
			name:     "windows maps to Windows",
			platform: "windows",
			expected: aws.ProductDescriptionWindows,
		},
		{
			name:     "Windows (capitalized) maps to Windows",
			platform: "Windows",
			expected: aws.ProductDescriptionWindows,
		},
		{
			name:     "WINDOWS (uppercase) maps to Windows",
			platform: "WINDOWS",
			expected: aws.ProductDescriptionWindows,
		},
		{
			name:     "Whitespace is trimmed: ' linux ' maps to Linux/UNIX",
			platform: " linux ",
			expected: aws.ProductDescriptionLinuxUnix,
		},
		{
			name:     "Whitespace is trimmed: ' windows ' maps to Windows",
			platform: " windows ",
			expected: aws.ProductDescriptionWindows,
		},
		{
			name:     "Unknown platform defaults to Linux/UNIX",
			platform: "rhel",
			expected: aws.ProductDescriptionLinuxUnix,
		},
		{
			name:     "Another unknown platform defaults to Linux/UNIX",
			platform: "suse",
			expected: aws.ProductDescriptionLinuxUnix,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := platformToProductDescription(tt.platform)
			assert.Equal(t, tt.expected, actual, "Platform '%s' should map to '%s'", tt.platform, tt.expected)
		})
	}
}
