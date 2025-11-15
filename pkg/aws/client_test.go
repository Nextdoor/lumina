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

package aws

import (
	"testing"
	"time"
)

const (
	testRegion = "us-west-2"
)

func TestClientConfig(t *testing.T) {
	config := ClientConfig{
		DefaultRegion: testRegion,
		MaxRetries:    3,
		RetryDelay:    time.Second,
		HTTPTimeout:   30 * time.Second,
		EnableMetrics: true,
	}

	if config.DefaultRegion != testRegion {
		t.Errorf("expected DefaultRegion us-west-2, got %s", config.DefaultRegion)
	}

	if config.MaxRetries != 3 {
		t.Errorf("expected MaxRetries 3, got %d", config.MaxRetries)
	}
}

func TestNewClient(t *testing.T) {
	config := ClientConfig{
		DefaultRegion: testRegion,
		MaxRetries:    3,
	}

	// NewClient should create a real AWS client
	client, err := NewClient(config)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if client == nil {
		t.Fatal("expected non-nil client")
	}
}
