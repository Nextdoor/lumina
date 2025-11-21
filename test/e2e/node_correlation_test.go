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
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/nextdoor/lumina/test/utils"
)

var _ = Describe("Node Correlation", Ordered, func() {
	var (
		ctx             context.Context
		cancel          context.CancelFunc
		testNodeNames   []string
		testInstanceIDs []string
		resourceClient  *ResourceClient
		controllerPodName string
	)

	BeforeAll(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Minute)

		By("creating resource client for Kubernetes API")
		var err error
		resourceClient, err = NewResourceClient(namespace)
		Expect(err).NotTo(HaveOccurred(), "Failed to create resource client")

		By("getting controller pod name")
		podList, err := resourceClient.GetPodsByLabel(ctx, "control-plane=controller-manager")
		Expect(err).NotTo(HaveOccurred())
		Expect(podList.Items).NotTo(BeEmpty())
		controllerPodName = podList.Items[0].Name

		By("fetching EC2 instance IDs from LocalStack using awslocal")
		// Use kubectl exec to run awslocal command inside LocalStack pod
		// This avoids DNS resolution issues from the test runner on the host
		// Query us-east-1 where the seed script creates test instances
		cmd := exec.Command("kubectl", "exec", "-n", "localstack",
			"deployment/localstack", "--",
			"awslocal", "ec2", "describe-instances",
			"--region", "us-east-1",
			"--filters", "Name=instance-state-name,Values=running",
			"--query", "Reservations[].Instances[].[InstanceId,Placement.AvailabilityZone]",
			"--output", "json")
		output, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to describe EC2 instances")
		By(fmt.Sprintf("awslocal returned: %s", output))

		// Parse JSON output - format is [[instanceId, az], [instanceId, az], ...]
		var instanceData [][]string
		err = json.Unmarshal([]byte(output), &instanceData)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Failed to parse EC2 instances JSON. Output was: %s", output))

		// Extract instance IDs and create node names
		testInstanceIDs = []string{}
		testNodeNames = []string{}

		for _, instanceInfo := range instanceData {
			if len(instanceInfo) < 2 {
				continue
			}
			instanceID := instanceInfo[0]
			az := instanceInfo[1]

			testInstanceIDs = append(testInstanceIDs, instanceID)

			// Create node name that looks like real AWS nodes
			// Extract region-specific suffix (e.g., "2a" from "us-east-1a")
			azSuffix := az[len(az)-2:] // Last 2 chars (e.g., "1a")
			nodeName := fmt.Sprintf("ip-10-0-1-%d.%s.compute.internal",
				len(testNodeNames)+1, azSuffix)
			testNodeNames = append(testNodeNames, nodeName)
		}

		Expect(testInstanceIDs).NotTo(BeEmpty(), "Should have at least one EC2 instance in LocalStack")
		By(fmt.Sprintf("found %d EC2 instances to correlate", len(testInstanceIDs)))
	})

	AfterAll(func() {
		By("cleaning up test nodes")
		for _, nodeName := range testNodeNames {
			_ = resourceClient.clientset.CoreV1().Nodes().Delete(ctx, nodeName, metav1.DeleteOptions{})
		}
		cancel()
	})

	Context("Node Cache Population", func() {
		It("should create fake nodes matching LocalStack instances", func() {
			By("creating fake Kubernetes nodes with matching providerIDs")
			for i, instanceID := range testInstanceIDs {
				node := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: testNodeNames[i],
						Labels: map[string]string{
							"node.kubernetes.io/instance-type": "m5.xlarge",
							"topology.kubernetes.io/region":    "us-east-1",
							"topology.kubernetes.io/zone":      "us-east-1a",
							"lumina.io/test":                   "true",
						},
					},
					Spec: corev1.NodeSpec{
						// Link K8s node to EC2 instance via providerID
						ProviderID: fmt.Sprintf("aws:///us-east-1a/%s", instanceID),
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

				_, err := resourceClient.clientset.CoreV1().Nodes().Create(ctx, node, metav1.CreateOptions{})
				Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Failed to create node %s", testNodeNames[i]))
			}

			By(fmt.Sprintf("created %d test nodes", len(testNodeNames)))
		})

		It("should reconcile nodes and log correlation", func() {
			By("waiting for node reconciler to process the nodes")
			Eventually(func() bool {
				logs, err := getPodLogs(controllerPodName, nil)
				if err != nil {
					return false
				}
				// Check that node reconciler logged correlation
				for _, nodeName := range testNodeNames {
					if !strings.Contains(logs, "correlated node to EC2 instance") ||
						!strings.Contains(logs, nodeName) {
						return false
					}
				}
				return true
			}, 30*time.Second, 1*time.Second).Should(BeTrue(),
				"Node reconciler should correlate test nodes")
		})
	})

	Context("Cost Metrics with Node Names", func() {
		It("should include node_name label in ec2_instance_hourly_cost metrics", func() {
			By("waiting for cost metrics to include node_name labels")
			// Poll for metrics with node_name labels - cost reconciler needs time to:
			// 1. Be triggered by node creation events
			// 2. Wait through 1-second debounce
			// 3. Re-run cost calculation with updated NodeCache
			// 4. Update Prometheus metrics
			Eventually(func() bool {
				metricsOutput, err := getMetricsOutput()
				if err != nil {
					return false
				}
				// Check that at least one of our test nodes appears in metrics
				for _, nodeName := range testNodeNames {
					if strings.Contains(metricsOutput, fmt.Sprintf(`node_name="%s"`, nodeName)) {
						return true
					}
				}
				return false
			}, 30*time.Second, 1*time.Second).Should(BeTrue(),
				"Cost metrics should include node_name labels after nodes are created")
		})

		It("should have node_name for our test instances", func() {
			metricsOutput, err := getMetricsOutput()
			Expect(err).NotTo(HaveOccurred())
			lines := strings.Split(metricsOutput, "\n")

			correlatedCount := 0
			for _, line := range lines {
				if !strings.HasPrefix(line, "ec2_instance_hourly_cost{") {
					continue
				}

				// Check if metric is for one of our test instances
				for i, instanceID := range testInstanceIDs {
					if strings.Contains(line, fmt.Sprintf(`instance_id="%s"`, instanceID)) {
						// Should have matching node name
						Expect(line).To(ContainSubstring(fmt.Sprintf(`node_name="%s"`, testNodeNames[i])),
							"Instance should have correlated node_name")
						correlatedCount++
						break
					}
				}
			}

			Expect(correlatedCount).To(BeNumerically(">", 0),
				"Should have correlated instances")
		})
	})

	Context("Node Deletion", func() {
		It("should remove node from cache when deleted", func() {
			By("deleting one test node")
			nodeToDelete := testNodeNames[0]
			err := resourceClient.clientset.CoreV1().Nodes().Delete(ctx, nodeToDelete, metav1.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())

			By("waiting for deletion to be logged")
			Eventually(func() bool {
				logs, err := getPodLogs(controllerPodName, nil)
				if err != nil {
					return false
				}
				return strings.Contains(logs, "node deleted, removing from cache") &&
					strings.Contains(logs, nodeToDelete)
			}, 10*time.Second, 1*time.Second).Should(BeTrue())
		})
	})

	Context("Edge Cases", func() {
		It("should handle non-AWS providerID", func() {
			By("creating node with GCE providerID")
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "gce-node",
					Labels: map[string]string{"lumina.io/test": "true"},
				},
				Spec: corev1.NodeSpec{
					ProviderID: "gce:///us-central1-a/gce-instance-123",
				},
			}

			_, err := resourceClient.clientset.CoreV1().Nodes().Create(ctx, node, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			By("verifying correlation failure is logged")
			Eventually(func() bool {
				logs, err := getPodLogs(controllerPodName, nil)
				if err != nil {
					return false
				}
				return strings.Contains(logs, "failed to correlate node to EC2 instance") &&
					strings.Contains(logs, "gce-node")
			}, 10*time.Second, 1*time.Second).Should(BeTrue())

			_ = resourceClient.clientset.CoreV1().Nodes().Delete(ctx, "gce-node", metav1.DeleteOptions{})
		})

		It("should handle missing providerID", func() {
			By("creating node without providerID")
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "no-provider-node",
					Labels: map[string]string{"lumina.io/test": "true"},
				},
				Spec: corev1.NodeSpec{
					// No ProviderID
				},
			}

			_, err := resourceClient.clientset.CoreV1().Nodes().Create(ctx, node, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			By("verifying correlation failure is logged")
			Eventually(func() bool {
				logs, err := getPodLogs(controllerPodName, nil)
				if err != nil {
					return false
				}
				return strings.Contains(logs, "failed to correlate node to EC2 instance") &&
					strings.Contains(logs, "no-provider-node")
			}, 10*time.Second, 1*time.Second).Should(BeTrue())

			_ = resourceClient.clientset.CoreV1().Nodes().Delete(ctx, "no-provider-node", metav1.DeleteOptions{})
		})
	})
})
