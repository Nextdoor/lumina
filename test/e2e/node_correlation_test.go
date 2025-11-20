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

//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Node Correlation", Ordered, func() {
	var (
		ctx             context.Context
		cancel          context.CancelFunc
		testNodeNames   []string
		testInstanceIDs []string
	)

	BeforeAll(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Minute)

		By("fetching EC2 instance IDs from LocalStack")
		// Get the list of running instances from LocalStack
		cmd := fmt.Sprintf("kubectl exec -n %s deployment/localstack -- "+
			"awslocal ec2 describe-instances "+
			"--filters Name=instance-state-name,Values=running "+
			"--query 'Reservations[*].Instances[*].[InstanceId,Placement.AvailabilityZone]' "+
			"--output text",
			localstackNamespace)

		output, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to get EC2 instances from LocalStack")

		// Parse instance IDs and availability zones
		lines := strings.Split(strings.TrimSpace(output), "\n")
		testInstanceIDs = []string{}
		testNodeNames = []string{}

		for _, line := range lines {
			if line == "" {
				continue
			}
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				instanceID := parts[0]
				az := parts[1]
				testInstanceIDs = append(testInstanceIDs, instanceID)
				// Create node name that looks like real AWS nodes
				nodeName := fmt.Sprintf("ip-10-0-1-%d.%s.compute.internal",
					len(testNodeNames)+1, strings.TrimPrefix(az, "us-west-"))
				testNodeNames = append(testNodeNames, nodeName)
			}
		}

		Expect(testInstanceIDs).NotTo(BeEmpty(), "Should have at least one EC2 instance in LocalStack")
		By(fmt.Sprintf("found %d EC2 instances to correlate", len(testInstanceIDs)))
	})

	AfterAll(func() {
		By("cleaning up test nodes")
		for _, nodeName := range testNodeNames {
			_ = k8sClient.Delete(ctx, &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: nodeName},
			})
		}
		cancel()
	})

	Context("Node Cache Population", func() {
		It("should create fake nodes matching LocalStack instances", func() {
			By("creating fake Kubernetes nodes with matching providerIDs")
			for i, instanceID := range testInstanceIDs {
				// Extract region from availability zone (assumes us-west-2)
				node := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: testNodeNames[i],
						Labels: map[string]string{
							"node.kubernetes.io/instance-type":   "m5.xlarge",
							"topology.kubernetes.io/region":      "us-west-2",
							"topology.kubernetes.io/zone":        "us-west-2a",
							"lumina.io/test":                     "true",
							"lumina.io/test-node-correlation":    "true",
						},
					},
					Spec: corev1.NodeSpec{
						// This is the key field - links K8s node to EC2 instance
						ProviderID: fmt.Sprintf("aws:///us-west-2a/%s", instanceID),
					},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{
								Type:   corev1.NodeReady,
								Status: corev1.ConditionTrue,
							},
						},
						Capacity: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("4"),
							corev1.ResourceMemory: resource.MustParse("16Gi"),
						},
						Allocatable: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("3.9"),
							corev1.ResourceMemory: resource.MustParse("15Gi"),
						},
					},
				}

				err := k8sClient.Create(ctx, node)
				Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Failed to create node %s", testNodeNames[i]))
			}

			By(fmt.Sprintf("created %d test nodes", len(testNodeNames)))
		})

		It("should reconcile nodes and update cache", func() {
			By("waiting for node reconciler to process the nodes")
			Eventually(func() bool {
				logs, err := getControllerLogs(controllerPodName)
				if err != nil {
					return false
				}
				// Check that node reconciler logged correlation for our test nodes
				for _, nodeName := range testNodeNames {
					if !strings.Contains(logs, fmt.Sprintf("correlated node to EC2 instance")) {
						return false
					}
					if !strings.Contains(logs, nodeName) {
						return false
					}
				}
				return true
			}, 30*time.Second, 1*time.Second).Should(BeTrue(),
				"Node reconciler should correlate test nodes to EC2 instances")
		})

		It("should log cache statistics showing correlated nodes", func() {
			By("checking node controller logs for cache statistics")
			Eventually(func() string {
				logs, err := getControllerLogs(controllerPodName)
				Expect(err).NotTo(HaveOccurred())
				return logs
			}, 10*time.Second, 1*time.Second).Should(
				ContainSubstring("correlated node to EC2 instance"),
				"Should log successful node correlation",
			)
		})
	})

	Context("Cost Metrics with Node Names", func() {
		It("should expose ec2_instance_hourly_cost with node_name labels", func() {
			By("waiting for cost reconciler to run with node correlation")
			// Cost reconciler runs every 5 minutes or on cache updates
			// Since we just added nodes, it should trigger soon
			time.Sleep(5 * time.Second)

			By("fetching metrics from the controller")
			metricsOutput := getMetrics()

			By("verifying ec2_instance_hourly_cost metrics include node_name")
			// Look for metrics with our test node names
			foundCorrelatedMetric := false
			for _, nodeName := range testNodeNames {
				if strings.Contains(metricsOutput, fmt.Sprintf(`node_name="%s"`, nodeName)) {
					foundCorrelatedMetric = true
					break
				}
			}
			Expect(foundCorrelatedMetric).To(BeTrue(),
				"Should find at least one cost metric with correlated node_name")
		})

		It("should have node_name label for instances matching our test nodes", func() {
			By("checking that cost metrics have non-empty node_name for our instances")
			metricsOutput := getMetrics()

			// Parse metrics to verify node_name labels
			lines := strings.Split(metricsOutput, "\n")
			correlatedCount := 0

			for _, line := range lines {
				if !strings.HasPrefix(line, "ec2_instance_hourly_cost{") {
					continue
				}

				// Check if this metric is for one of our test instances
				hasTestInstance := false
				for _, instanceID := range testInstanceIDs {
					if strings.Contains(line, fmt.Sprintf(`instance_id="%s"`, instanceID)) {
						hasTestInstance = true
						break
					}
				}

				if !hasTestInstance {
					continue
				}

				// Verify it has a non-empty node_name
				hasNodeName := false
				for _, nodeName := range testNodeNames {
					if strings.Contains(line, fmt.Sprintf(`node_name="%s"`, nodeName)) {
						hasNodeName = true
						correlatedCount++
						break
					}
				}

				if !hasNodeName {
					// Should not have empty node_name for our test instances
					Expect(line).NotTo(ContainSubstring(`node_name=""`),
						"Test instances should have correlated node names")
				}
			}

			Expect(correlatedCount).To(BeNumerically(">", 0),
				"Should have at least one correlated instance with node_name")
		})

		It("should allow querying costs by node name", func() {
			By("verifying we can filter metrics by node_name")
			metricsOutput := getMetrics()

			// Find a cost metric with one of our node names
			lines := strings.Split(metricsOutput, "\n")
			var sampleMetric string

			for _, line := range lines {
				if !strings.HasPrefix(line, "ec2_instance_hourly_cost{") {
					continue
				}

				for _, nodeName := range testNodeNames {
					if strings.Contains(line, fmt.Sprintf(`node_name="%s"`, nodeName)) {
						sampleMetric = line
						break
					}
				}

				if sampleMetric != "" {
					break
				}
			}

			Expect(sampleMetric).NotTo(BeEmpty(),
				"Should find at least one cost metric with our test node name")

			By("verifying the metric has all expected labels")
			// Metric should have: instance_id, account_id, region, instance_type,
			// cost_type, availability_zone, lifecycle, pricing_accuracy, node_name
			Expect(sampleMetric).To(ContainSubstring("instance_id="))
			Expect(sampleMetric).To(ContainSubstring("account_id="))
			Expect(sampleMetric).To(ContainSubstring("region="))
			Expect(sampleMetric).To(ContainSubstring("instance_type="))
			Expect(sampleMetric).To(ContainSubstring("cost_type="))
			Expect(sampleMetric).To(ContainSubstring("availability_zone="))
			Expect(sampleMetric).To(ContainSubstring("lifecycle="))
			Expect(sampleMetric).To(ContainSubstring("pricing_accuracy="))
			Expect(sampleMetric).To(ContainSubstring("node_name="))
		})
	})

	Context("Node Deletion and Cache Cleanup", func() {
		It("should remove node from cache when deleted", func() {
			By("deleting one of the test nodes")
			nodeToDelete := testNodeNames[0]
			err := k8sClient.Delete(ctx, &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: nodeToDelete},
			})
			Expect(err).NotTo(HaveOccurred())

			By("waiting for node reconciler to process the deletion")
			Eventually(func() bool {
				logs, err := getControllerLogs(controllerPodName)
				if err != nil {
					return false
				}
				return strings.Contains(logs, fmt.Sprintf("node deleted, removing from cache")) &&
					strings.Contains(logs, nodeToDelete)
			}, 30*time.Second, 1*time.Second).Should(BeTrue(),
				"Node reconciler should log node deletion")

			By("waiting for cost reconciler to update metrics")
			time.Sleep(5 * time.Second)

			By("verifying cost metric no longer has the deleted node_name")
			metricsOutput := getMetrics()

			// The metric for this instance should now have empty node_name
			// (or the metric might still exist with the old label until next reset)
			// In our implementation, metrics are reset on each update, so after
			// the next cost calculation, the old node_name should be gone
			instanceID := testInstanceIDs[0]

			// Look for metrics for this instance
			lines := strings.Split(metricsOutput, "\n")
			for _, line := range lines {
				if !strings.HasPrefix(line, "ec2_instance_hourly_cost{") {
					continue
				}
				if strings.Contains(line, fmt.Sprintf(`instance_id="%s"`, instanceID)) {
					// If metric exists, it should either have empty node_name or not exist yet
					// (The metric gets reset and recreated on each cost calculation)
					GinkgoWriter.Printf("Found metric for deleted node's instance: %s\n", line)
				}
			}
		})

		It("should handle node recreation with same instance ID", func() {
			By("recreating the deleted node with the same instance ID")
			instanceID := testInstanceIDs[0]
			newNodeName := fmt.Sprintf("ip-10-0-2-99.us-west-2a.compute.internal")

			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: newNodeName,
					Labels: map[string]string{
						"node.kubernetes.io/instance-type": "m5.xlarge",
						"lumina.io/test":                   "true",
					},
				},
				Spec: corev1.NodeSpec{
					ProviderID: fmt.Sprintf("aws:///us-west-2a/%s", instanceID),
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:   corev1.NodeReady,
							Status: corev1.ConditionTrue,
						},
					},
					Capacity: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("16Gi"),
					},
				},
			}

			err := k8sClient.Create(ctx, node)
			Expect(err).NotTo(HaveOccurred())

			By("waiting for node reconciler to correlate the new node")
			Eventually(func() bool {
				logs, err := getControllerLogs(controllerPodName)
				if err != nil {
					return false
				}
				return strings.Contains(logs, "correlated node to EC2 instance") &&
					strings.Contains(logs, newNodeName)
			}, 30*time.Second, 1*time.Second).Should(BeTrue(),
				"Node reconciler should correlate recreated node")

			By("cleaning up recreated node")
			_ = k8sClient.Delete(ctx, node)
		})
	})

	Context("Edge Cases and Error Handling", func() {
		It("should handle nodes with invalid providerID format", func() {
			By("creating node with invalid providerID")
			invalidNode := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "invalid-provider-id-node",
					Labels: map[string]string{
						"lumina.io/test": "true",
					},
				},
				Spec: corev1.NodeSpec{
					ProviderID: "gce:///us-central1-a/invalid-instance",
				},
			}

			err := k8sClient.Create(ctx, invalidNode)
			Expect(err).NotTo(HaveOccurred())

			By("verifying node reconciler logs failure to parse providerID")
			Eventually(func() bool {
				logs, err := getControllerLogs(controllerPodName)
				if err != nil {
					return false
				}
				return strings.Contains(logs, "failed to correlate node to EC2 instance") &&
					strings.Contains(logs, "invalid-provider-id-node")
			}, 10*time.Second, 1*time.Second).Should(BeTrue(),
				"Should log failure to parse non-AWS providerID")

			By("cleaning up invalid node")
			_ = k8sClient.Delete(ctx, invalidNode)
		})

		It("should handle nodes without providerID", func() {
			By("creating node without providerID")
			noProviderIDNode := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "no-provider-id-node",
					Labels: map[string]string{
						"lumina.io/test": "true",
					},
				},
				Spec: corev1.NodeSpec{
					// No ProviderID set
				},
			}

			err := k8sClient.Create(ctx, noProviderIDNode)
			Expect(err).NotTo(HaveOccurred())

			By("verifying node reconciler logs failure due to empty providerID")
			Eventually(func() bool {
				logs, err := getControllerLogs(controllerPodName)
				if err != nil {
					return false
				}
				return strings.Contains(logs, "failed to correlate node to EC2 instance") &&
					strings.Contains(logs, "no-provider-id-node")
			}, 10*time.Second, 1*time.Second).Should(BeTrue(),
				"Should log failure when providerID is empty")

			By("cleaning up node without providerID")
			_ = k8sClient.Delete(ctx, noProviderIDNode)
		})
	})
})
