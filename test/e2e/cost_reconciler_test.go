//go:build e2e
// +build e2e

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

package e2e

import (
	"context"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Cost Reconciler", Ordered, func() {
	var controllerPodName string

	// Get controller pod name before running tests
	BeforeAll(func() {
		By("getting controller pod name for cost tests")
		Eventually(func(g Gomega) {
			client, err := NewResourceClient(namespace)
			g.Expect(err).NotTo(HaveOccurred(), "Failed to create resource client")

			ctx := context.Background()
			podList, err := client.GetPodsByLabel(ctx, "control-plane=controller-manager")
			g.Expect(err).NotTo(HaveOccurred())

			// Filter out pods that are being deleted
			var runningPods []string
			for _, pod := range podList.Items {
				if pod.DeletionTimestamp == nil {
					runningPods = append(runningPods, pod.Name)
				}
			}

			g.Expect(runningPods).To(HaveLen(1), "expected 1 controller pod running")
			controllerPodName = runningPods[0]
		}, 2*time.Minute, 2*time.Second).Should(Succeed())
	})

	Context("Cost Calculation", func() {
		It("should run cost calculation cycle", func() {
			By("waiting for cost reconciler to run")
			Eventually(func(g Gomega) {
				output, err := getPodLogs(controllerPodName, nil)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("starting cost calculation cycle"),
					"Cost reconciler should have started")
			}, 3*time.Minute, 5*time.Second).Should(Succeed())

			By("verifying cost calculation completed successfully")
			Eventually(func(g Gomega) {
				output, err := getPodLogs(controllerPodName, nil)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("cost calculation completed"),
					"Cost reconciler should have completed successfully")
			}, 3*time.Minute, 5*time.Second).Should(Succeed())

			By("verifying cost metrics were emitted")
			Eventually(func(g Gomega) {
				output, err := getPodLogs(controllerPodName, nil)
				g.Expect(err).NotTo(HaveOccurred())
				// The controller should log that it updated cost metrics
				g.Expect(output).To(ContainSubstring("updated cost metrics"),
					"Controller should log cost metrics update")
			}, 30*time.Second, 2*time.Second).Should(Succeed())
		})
	})

	Context("Instance Cost Metrics", func() {
		It("should expose ec2_instance_hourly_cost metric", func() {
			By("waiting for instance cost metrics to be available")
			Eventually(func(g Gomega) {
				metricsOutput, err := getMetricsOutput()
				g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve metrics")

				// Debug: Print all Lumina metrics if the cost metric isn't found
				if !strings.Contains(metricsOutput, "ec2_instance_hourly_cost{") {
					GinkgoWriter.Printf("\n=== All Lumina Metrics (DEBUG) ===\n")
					lines := strings.Split(metricsOutput, "\n")
					for _, line := range lines {
						// Print all lines that start with "lumina_" or contain cost/pricing/ec2
						if strings.HasPrefix(line, "lumina_") ||
							strings.Contains(line, "ec2") ||
							strings.Contains(line, "cost") ||
							strings.Contains(line, "pricing") {
							GinkgoWriter.Printf("%s\n", line)
						}
					}
					GinkgoWriter.Printf("=== End Lumina Metrics ===\n\n")
				}

				// Verify ec2_instance_hourly_cost metric exists
				g.Expect(metricsOutput).To(ContainSubstring("ec2_instance_hourly_cost{"),
					"ec2_instance_hourly_cost metric should be present")
			}, 3*time.Minute, 5*time.Second).Should(Succeed())

			By("verifying ec2_instance_hourly_cost metric has correct labels")
			Eventually(func(g Gomega) {
				metricsOutput, err := getMetricsOutput()
				g.Expect(err).NotTo(HaveOccurred())

				// Find an instance cost metric line
				lines := strings.Split(metricsOutput, "\n")
				var metricLine string
				for _, line := range lines {
					if strings.HasPrefix(line, "ec2_instance_hourly_cost{") {
						metricLine = line
						break
					}
				}

				g.Expect(metricLine).NotTo(BeEmpty(), "Should find at least one ec2_instance_hourly_cost metric")

				// Verify required labels are present
				g.Expect(metricLine).To(ContainSubstring("instance_id="))
				g.Expect(metricLine).To(ContainSubstring("account_id="))
				g.Expect(metricLine).To(ContainSubstring("region="))
				g.Expect(metricLine).To(ContainSubstring("instance_type="))
				g.Expect(metricLine).To(ContainSubstring("cost_type="))
				g.Expect(metricLine).To(ContainSubstring("availability_zone="))
			}, 30*time.Second, 2*time.Second).Should(Succeed())

			By("verifying instance cost metric values are numeric")
			Eventually(func(g Gomega) {
				metricsOutput, err := getMetricsOutput()
				g.Expect(err).NotTo(HaveOccurred())

				lines := strings.Split(metricsOutput, "\n")
				foundNumericValue := false
				for _, line := range lines {
					if strings.HasPrefix(line, "ec2_instance_hourly_cost{") {
						// Line format: metric{labels} value
						parts := strings.Fields(line)
						if len(parts) >= 2 {
							// Verify the value is parseable as a number (not NaN or Inf)
							value := parts[1]
							g.Expect(value).NotTo(Equal("NaN"))
							g.Expect(value).NotTo(Equal("Inf"))
							g.Expect(value).NotTo(Equal("-Inf"))
							foundNumericValue = true
							break
						}
					}
				}
				g.Expect(foundNumericValue).To(BeTrue(), "Should find at least one cost metric with numeric value")
			}, 30*time.Second, 2*time.Second).Should(Succeed())
		})
	})

	Context("Savings Plans Utilization Metrics", func() {
		It("should expose savings plan utilization metrics", func() {
			// Note: These tests will only pass if there are Savings Plans in the test environment
			// If no SPs exist, these metrics won't be present
			By("checking for savings plan utilization metrics")
			Eventually(func(g Gomega) {
				metricsOutput, err := getMetricsOutput()
				g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve metrics")

				// Check if any SP utilization metrics exist
				// These may not exist if there are no Savings Plans in the test environment
				hasSPMetrics := strings.Contains(metricsOutput, "savings_plan_current_utilization_rate{") ||
					strings.Contains(metricsOutput, "savings_plan_remaining_capacity{") ||
					strings.Contains(metricsOutput, "savings_plan_utilization_percent{")

				if !hasSPMetrics {
					Skip("No Savings Plans in test environment, skipping SP utilization metric tests")
				}
			}, 30*time.Second, 2*time.Second).Should(Succeed())

			By("verifying savings_plan_current_utilization_rate metric format")
			Eventually(func(g Gomega) {
				metricsOutput, err := getMetricsOutput()
				g.Expect(err).NotTo(HaveOccurred())

				if strings.Contains(metricsOutput, "savings_plan_current_utilization_rate{") {
					lines := strings.Split(metricsOutput, "\n")
					for _, line := range lines {
						if strings.HasPrefix(line, "savings_plan_current_utilization_rate{") {
							// Verify required labels
							g.Expect(line).To(ContainSubstring("savings_plan_arn="))
							g.Expect(line).To(ContainSubstring("account_id="))
							g.Expect(line).To(ContainSubstring("type="))
							break
						}
					}
				}
			}, 30*time.Second, 2*time.Second).Should(Succeed())

			By("verifying savings_plan_remaining_capacity metric format")
			Eventually(func(g Gomega) {
				metricsOutput, err := getMetricsOutput()
				g.Expect(err).NotTo(HaveOccurred())

				if strings.Contains(metricsOutput, "savings_plan_remaining_capacity{") {
					lines := strings.Split(metricsOutput, "\n")
					for _, line := range lines {
						if strings.HasPrefix(line, "savings_plan_remaining_capacity{") {
							// Verify required labels
							g.Expect(line).To(ContainSubstring("savings_plan_arn="))
							g.Expect(line).To(ContainSubstring("account_id="))
							g.Expect(line).To(ContainSubstring("type="))
							break
						}
					}
				}
			}, 30*time.Second, 2*time.Second).Should(Succeed())

			By("verifying savings_plan_utilization_percent metric format")
			Eventually(func(g Gomega) {
				metricsOutput, err := getMetricsOutput()
				g.Expect(err).NotTo(HaveOccurred())

				if strings.Contains(metricsOutput, "savings_plan_utilization_percent{") {
					lines := strings.Split(metricsOutput, "\n")
					for _, line := range lines {
						if strings.HasPrefix(line, "savings_plan_utilization_percent{") {
							// Verify required labels
							g.Expect(line).To(ContainSubstring("savings_plan_arn="))
							g.Expect(line).To(ContainSubstring("account_id="))
							g.Expect(line).To(ContainSubstring("type="))

							// Verify the value is a valid percentage (could be >100% if over-utilized)
							parts := strings.Fields(line)
							if len(parts) >= 2 {
								value := parts[1]
								g.Expect(value).NotTo(Equal("NaN"))
							}
							break
						}
					}
				}
			}, 30*time.Second, 2*time.Second).Should(Succeed())
		})
	})
})
