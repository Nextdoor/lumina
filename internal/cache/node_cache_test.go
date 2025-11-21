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

package cache

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestParseProviderID tests the providerID parsing logic
func TestParseProviderID(t *testing.T) {
	tests := []struct {
		name        string
		providerID  string
		expectedID  string
		expectError bool
	}{
		{
			name:        "Valid AWS providerID",
			providerID:  "aws:///us-west-2a/i-abc123def456",
			expectedID:  "i-abc123def456",
			expectError: false,
		},
		{
			name:        "Valid AWS providerID with different zone",
			providerID:  "aws:///us-east-1b/i-xyz789",
			expectedID:  "i-xyz789",
			expectError: false,
		},
		{
			name:        "Empty providerID",
			providerID:  "",
			expectedID:  "",
			expectError: true,
		},
		{
			name:        "Non-AWS providerID",
			providerID:  "gce:///us-central1-a/instance-1",
			expectedID:  "",
			expectError: true,
		},
		{
			name:        "Malformed providerID (missing instance ID)",
			providerID:  "aws:///us-west-2a/",
			expectedID:  "",
			expectError: true,
		},
		{
			name:        "Malformed providerID (invalid instance ID format)",
			providerID:  "aws:///us-west-2a/not-an-instance-id",
			expectedID:  "",
			expectError: true,
		},
		{
			name:        "Malformed providerID (no slashes)",
			providerID:  "aws://instance-id",
			expectedID:  "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			instanceID, err := parseProviderID(tt.providerID)

			if tt.expectError {
				assert.Error(t, err, "Expected error for providerID: %s", tt.providerID)
				assert.Empty(t, instanceID)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedID, instanceID)
			}
		})
	}
}

// TestNodeCache_UpsertNode tests adding and updating nodes
func TestNodeCache_UpsertNode(t *testing.T) {
	cache := NewNodeCache()

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ip-10-0-1-42.ec2.internal",
			Labels: map[string]string{
				"node.kubernetes.io/instance-type": "m5.xlarge",
			},
		},
		Spec: corev1.NodeSpec{
			ProviderID: "aws:///us-west-2a/i-abc123def456",
		},
	}

	// Test successful upsert
	instanceID, err := cache.UpsertNode(node)
	require.NoError(t, err)
	assert.Equal(t, "i-abc123def456", instanceID)

	// Verify mapping created
	nodeName, exists := cache.GetNodeName("i-abc123def456")
	assert.True(t, exists)
	assert.Equal(t, "ip-10-0-1-42.ec2.internal", nodeName)

	// Verify node stored
	storedNode, exists := cache.GetNode("ip-10-0-1-42.ec2.internal")
	require.True(t, exists)
	assert.Equal(t, node.Name, storedNode.Name)
	assert.Equal(t, node.Spec.ProviderID, storedNode.Spec.ProviderID)

	// Test counts
	assert.Equal(t, 1, cache.GetNodeCount())
	assert.Equal(t, 1, cache.GetCorrelatedInstanceCount())
}

// TestNodeCache_UpsertNode_InvalidProviderID tests error handling for invalid providerIDs
func TestNodeCache_UpsertNode_InvalidProviderID(t *testing.T) {
	cache := NewNodeCache()

	tests := []struct {
		name        string
		node        *corev1.Node
		expectError bool
	}{
		{
			name:        "Nil node",
			node:        nil,
			expectError: true,
		},
		{
			name: "Empty providerID",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
				Spec:       corev1.NodeSpec{ProviderID: ""},
			},
			expectError: true,
		},
		{
			name: "Invalid providerID format",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "node-2"},
				Spec:       corev1.NodeSpec{ProviderID: "invalid"},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := cache.UpsertNode(tt.node)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestNodeCache_UpdateNode tests updating an existing node
func TestNodeCache_UpdateNode(t *testing.T) {
	cache := NewNodeCache()

	// Add initial node
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ip-10-0-1-42.ec2.internal",
			Labels: map[string]string{
				"node-pool": "default",
			},
		},
		Spec: corev1.NodeSpec{
			ProviderID: "aws:///us-west-2a/i-abc123def456",
		},
	}

	_, err := cache.UpsertNode(node)
	require.NoError(t, err)

	// Update node with new labels
	updatedNode := node.DeepCopy()
	updatedNode.Labels["node-pool"] = "production"
	updatedNode.Labels["environment"] = "prod"

	instanceID, err := cache.UpsertNode(updatedNode)
	require.NoError(t, err)
	assert.Equal(t, "i-abc123def456", instanceID)

	// Verify updated node stored
	storedNode, exists := cache.GetNode("ip-10-0-1-42.ec2.internal")
	require.True(t, exists)
	assert.Equal(t, "production", storedNode.Labels["node-pool"])
	assert.Equal(t, "prod", storedNode.Labels["environment"])

	// Verify still only one node
	assert.Equal(t, 1, cache.GetNodeCount())
	assert.Equal(t, 1, cache.GetCorrelatedInstanceCount())
}

// TestNodeCache_DeleteNode tests node deletion
func TestNodeCache_DeleteNode(t *testing.T) {
	cache := NewNodeCache()

	// Add two nodes
	node1 := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
		Spec:       corev1.NodeSpec{ProviderID: "aws:///us-west-2a/i-111"},
	}
	node2 := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-2"},
		Spec:       corev1.NodeSpec{ProviderID: "aws:///us-west-2a/i-222"},
	}

	_, err := cache.UpsertNode(node1)
	require.NoError(t, err)
	_, err = cache.UpsertNode(node2)
	require.NoError(t, err)

	assert.Equal(t, 2, cache.GetNodeCount())
	assert.Equal(t, 2, cache.GetCorrelatedInstanceCount())

	// Delete node-1
	cache.DeleteNode("node-1")

	// Verify node-1 removed
	_, exists := cache.GetNode("node-1")
	assert.False(t, exists)

	_, exists = cache.GetNodeName("i-111")
	assert.False(t, exists)

	// Verify node-2 still exists
	_, exists = cache.GetNode("node-2")
	assert.True(t, exists)

	nodeName, exists := cache.GetNodeName("i-222")
	assert.True(t, exists)
	assert.Equal(t, "node-2", nodeName)

	assert.Equal(t, 1, cache.GetNodeCount())
	assert.Equal(t, 1, cache.GetCorrelatedInstanceCount())
}

// TestNodeCache_DeleteNode_NonExistent tests deleting a node that doesn't exist
func TestNodeCache_DeleteNode_NonExistent(t *testing.T) {
	cache := NewNodeCache()

	// Add one node
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
		Spec:       corev1.NodeSpec{ProviderID: "aws:///us-west-2a/i-111"},
	}
	_, err := cache.UpsertNode(node)
	require.NoError(t, err)

	// Delete non-existent node (should not panic or error)
	cache.DeleteNode("non-existent-node")

	// Verify original node still exists
	assert.Equal(t, 1, cache.GetNodeCount())
}

// TestNodeCache_GetNodeName tests retrieving node names by instance ID
func TestNodeCache_GetNodeName(t *testing.T) {
	cache := NewNodeCache()

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "ip-10-0-1-42.ec2.internal"},
		Spec:       corev1.NodeSpec{ProviderID: "aws:///us-west-2a/i-abc123def456"},
	}

	_, err := cache.UpsertNode(node)
	require.NoError(t, err)

	// Test getting existing node name
	nodeName, exists := cache.GetNodeName("i-abc123def456")
	assert.True(t, exists)
	assert.Equal(t, "ip-10-0-1-42.ec2.internal", nodeName)

	// Test getting non-existent node name
	nodeName, exists = cache.GetNodeName("i-nonexistent")
	assert.False(t, exists)
	assert.Empty(t, nodeName)
}

// TestNodeCache_Clear tests clearing the cache
func TestNodeCache_Clear(t *testing.T) {
	cache := NewNodeCache()

	// Add multiple nodes
	for i := 1; i <= 5; i++ {
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "node-" + string(rune('0'+i))},
			Spec: corev1.NodeSpec{
				ProviderID: "aws:///us-west-2a/i-" + string(rune('0'+i)),
			},
		}
		_, err := cache.UpsertNode(node)
		require.NoError(t, err)
	}

	assert.Equal(t, 5, cache.GetNodeCount())
	assert.Equal(t, 5, cache.GetCorrelatedInstanceCount())

	// Clear cache
	cache.Clear()

	assert.Equal(t, 0, cache.GetNodeCount())
	assert.Equal(t, 0, cache.GetCorrelatedInstanceCount())

	// Verify all mappings removed
	for i := 1; i <= 5; i++ {
		_, exists := cache.GetNodeName("i-" + string(rune('0'+i)))
		assert.False(t, exists)
	}
}

// TestNodeCache_DeepCopy tests that nodes are deep-copied
func TestNodeCache_DeepCopy(t *testing.T) {
	cache := NewNodeCache()

	originalNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node-1",
			Labels: map[string]string{
				"env": "test",
			},
		},
		Spec: corev1.NodeSpec{
			ProviderID: "aws:///us-west-2a/i-111",
		},
	}

	_, err := cache.UpsertNode(originalNode)
	require.NoError(t, err)

	// Get node from cache
	cachedNode, exists := cache.GetNode("node-1")
	require.True(t, exists)

	// Modify the cached node
	cachedNode.Labels["env"] = "modified"

	// Get node again and verify original labels preserved
	freshNode, exists := cache.GetNode("node-1")
	require.True(t, exists)
	assert.Equal(t, "test", freshNode.Labels["env"], "Original node should not be modified")
}
