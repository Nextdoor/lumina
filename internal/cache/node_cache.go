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
	"fmt"
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"
)

// NodeCache maintains a thread-safe cache of Kubernetes nodes and their correlation
// to EC2 instances. It maps EC2 instance IDs to Kubernetes node names, enabling
// cost metrics to include node-level information.
//
// Thread-safety: All public methods use read/write locks for safe concurrent access.
type NodeCache struct {
	mu sync.RWMutex

	// instanceIDToNodeName maps EC2 instance ID → K8s node name
	// Example: "i-abc123def456" → "ip-10-0-1-42.ec2.internal"
	instanceIDToNodeName map[string]string

	// nodes stores full node objects for label/annotation extraction
	// Key is node name
	nodes map[string]*corev1.Node
}

// NewNodeCache creates a new empty NodeCache.
func NewNodeCache() *NodeCache {
	return &NodeCache{
		instanceIDToNodeName: make(map[string]string),
		nodes:                make(map[string]*corev1.Node),
	}
}

// UpsertNode adds or updates a node in the cache, extracting the EC2 instance ID
// from the node's providerID and creating the mapping.
//
// ProviderID format: "aws:///us-west-2a/i-abc123def456" or "aws:///<zone>/i-<id>"
//
// Returns:
//   - instanceID: The extracted EC2 instance ID (e.g., "i-abc123def456")
//   - err: Error if providerID is missing or malformed
func (c *NodeCache) UpsertNode(node *corev1.Node) (instanceID string, err error) {
	if node == nil {
		return "", fmt.Errorf("node is nil")
	}

	// Extract instance ID from providerID
	instanceID, err = parseProviderID(node.Spec.ProviderID)
	if err != nil {
		return "", fmt.Errorf("failed to parse providerID for node %s: %w", node.Name, err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Store mapping: instance ID → node name
	c.instanceIDToNodeName[instanceID] = node.Name

	// Store full node object
	c.nodes[node.Name] = node.DeepCopy()

	return instanceID, nil
}

// DeleteNode removes a node from the cache by name.
func (c *NodeCache) DeleteNode(nodeName string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Find and remove instance ID → node name mapping
	for instanceID, name := range c.instanceIDToNodeName {
		if name == nodeName {
			delete(c.instanceIDToNodeName, instanceID)
			break
		}
	}

	// Remove node object
	delete(c.nodes, nodeName)
}

// GetNodeName returns the Kubernetes node name for a given EC2 instance ID.
// Returns (nodeName, true) if found, ("", false) if not found.
func (c *NodeCache) GetNodeName(instanceID string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	nodeName, exists := c.instanceIDToNodeName[instanceID]
	return nodeName, exists
}

// GetNode returns a copy of the node object by name.
// Returns (node, true) if found, (nil, false) if not found.
func (c *NodeCache) GetNode(nodeName string) (*corev1.Node, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	node, exists := c.nodes[nodeName]
	if !exists {
		return nil, false
	}

	return node.DeepCopy(), true
}

// GetNodeCount returns the number of nodes currently in the cache.
func (c *NodeCache) GetNodeCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.nodes)
}

// GetCorrelatedInstanceCount returns the number of EC2 instances mapped to nodes.
func (c *NodeCache) GetCorrelatedInstanceCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.instanceIDToNodeName)
}

// Clear removes all nodes from the cache.
// This is primarily useful for testing.
func (c *NodeCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.instanceIDToNodeName = make(map[string]string)
	c.nodes = make(map[string]*corev1.Node)
}

// parseProviderID extracts the EC2 instance ID from a Kubernetes node's providerID.
//
// Expected formats:
//   - "aws:///us-west-2a/i-abc123def456"
//   - "aws:///<zone>/i-<instance-id>"
//
// Returns:
//   - instanceID: The EC2 instance ID (e.g., "i-abc123def456")
//   - error: If providerID is empty or doesn't match expected format
func parseProviderID(providerID string) (string, error) {
	if providerID == "" {
		return "", fmt.Errorf("providerID is empty")
	}

	// AWS providerID format: "aws:///zone/instance-id"
	// Example: "aws:///us-west-2a/i-abc123def456"
	if !strings.HasPrefix(providerID, "aws://") {
		return "", fmt.Errorf("providerID does not start with 'aws://': %s", providerID)
	}

	// Split by '/' and get the last segment (instance ID)
	parts := strings.Split(providerID, "/")
	if len(parts) < 2 {
		return "", fmt.Errorf("providerID has invalid format: %s", providerID)
	}

	instanceID := parts[len(parts)-1]
	if instanceID == "" || !strings.HasPrefix(instanceID, "i-") {
		return "", fmt.Errorf("providerID does not end with valid instance ID: %s", providerID)
	}

	return instanceID, nil
}
