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
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Spot Pricing Reconciler", Ordered, func() {
	var controllerPodName string

	// Get controller pod name before running tests
	BeforeAll(func() {
		By("getting controller pod name for Spot Pricing tests")
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
		}, 20*time.Second, 2*time.Second).Should(Succeed())
	})

	Context("Spot Pricing Data Collection", func() {
		It("should wait for EC2 cache to be ready before starting", func() {
			By("waiting for spot pricing reconciler to start")
			Eventually(func(g Gomega) {
				output, err := getPodLogs(controllerPodName, nil)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("starting spot pricing reconciler"),
					"Spot pricing reconciler should have started")
			}, 30*time.Second, 2*time.Second).Should(Succeed())

			By("verifying reconciler waits for EC2 cache")
			Eventually(func(g Gomega) {
				output, err := getPodLogs(controllerPodName, nil)
				g.Expect(err).NotTo(HaveOccurred())
				// The reconciler should log that it's waiting or that EC2 cache is ready
				g.Expect(output).To(Or(
					ContainSubstring("waiting for EC2 cache"),
					ContainSubstring("starting spot pricing reconciliation cycle"),
				), "Reconciler should wait for EC2 cache or start after it's ready")
			}, 30*time.Second, 2*time.Second).Should(Succeed())
		})

		It("should perform initial lazy-loading reconciliation", func() {
			By("waiting for initial spot pricing reconciliation")
			Eventually(func(g Gomega) {
				output, err := getPodLogs(controllerPodName, nil)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("starting spot pricing reconciliation cycle"),
					"Initial reconciliation should have started")
			}, 30*time.Second, 2*time.Second).Should(Succeed())

			By("verifying reconciler discovered running instances")
			Eventually(func(g Gomega) {
				output, err := getPodLogs(controllerPodName, nil)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("discovered running instances"),
					"Reconciler should discover running instances from EC2 cache")
			}, 30*time.Second, 2*time.Second).Should(Succeed())

			By("verifying reconciler fetched spot prices")
			Eventually(func(g Gomega) {
				output, err := getPodLogs(controllerPodName, nil)
				g.Expect(err).NotTo(HaveOccurred())
				// Reconciler should log either:
				// - "fetching missing spot prices" (first run)
				// - "all spot prices are cached" (if already populated)
				g.Expect(output).To(Or(
					ContainSubstring("fetching missing spot prices"),
					ContainSubstring("all spot prices are cached"),
				), "Reconciler should fetch spot prices or use cached data")
			}, 40*time.Second, 2*time.Second).Should(Succeed())
		})

		It("should complete initial reconciliation successfully", func() {
			By("verifying initial spot pricing reconciliation completed")
			Eventually(func(g Gomega) {
				output, err := getPodLogs(controllerPodName, nil)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("initial spot pricing reconciliation completed successfully"),
					"Initial reconciliation should complete successfully")
			}, 40*time.Second, 2*time.Second).Should(Succeed())
		})

		It("should perform lazy-loading in subsequent reconciliations", func() {
			By("waiting for subsequent reconciliation cycles")
			time.Sleep(20 * time.Second) // Wait for a few reconciliation cycles (15s interval)

			By("verifying steady-state reconciliation uses cached data")
			Eventually(func(g Gomega) {
				output, err := getPodLogs(controllerPodName, nil)
				g.Expect(err).NotTo(HaveOccurred())
				// In steady state with no new instance types, reconciler should use cached prices
				// Count occurrences of "all spot prices are cached"
				count := strings.Count(output, "all spot prices are cached")
				g.Expect(count).To(BeNumerically(">", 0),
					"Reconciler should use cached spot prices in steady state")
			}, 20*time.Second, 2*time.Second).Should(Succeed())
		})

		It("should update spot pricing cache statistics", func() {
			By("verifying cache statistics are logged")
			Eventually(func(g Gomega) {
				output, err := getPodLogs(controllerPodName, nil)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("updated spot pricing cache"),
					"Reconciler should log cache updates")
			}, 30*time.Second, 2*time.Second).Should(Succeed())
		})
	})

	Context("Spot Pricing Metrics", func() {
		It("should expose spot pricing data freshness metric", func() {
			By("waiting for spot pricing metrics to be available")
			Eventually(func(g Gomega) {
				metricsOutput, err := getMetricsOutput()
				g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve metrics")

				// Verify data_freshness metric for spot-pricing exists
				g.Expect(metricsOutput).To(MatchRegexp(`lumina_data_freshness_seconds\{.*data_type="spot-pricing"`),
					"data_freshness metric for spot-pricing should be present")
			}, 30*time.Second, 2*time.Second).Should(Succeed())

			By("verifying spot pricing data freshness metric has recent timestamp")
			metricsOutput, err := getMetricsOutput()
			Expect(err).NotTo(HaveOccurred())

			// Extract the timestamp value for spot-pricing data_freshness
			lines := strings.Split(metricsOutput, "\n")
			foundRecentTimestamp := false
			currentTime := time.Now().Unix()

			for _, line := range lines {
				if strings.Contains(line, "lumina_data_freshness_seconds") && strings.Contains(line, `data_type="spot-pricing"`) {
					parts := strings.Fields(line)
					if len(parts) >= 2 {
						var timestamp float64
						_, err := fmt.Sscanf(parts[1], "%f", &timestamp)
						if err == nil {
							// Timestamp should be within the last 2 minutes
							if int64(timestamp) >= currentTime-120 {
								foundRecentTimestamp = true
								break
							}
						}
					}
				}
			}
			Expect(foundRecentTimestamp).To(BeTrue(),
				"data_freshness for spot-pricing should have a recent timestamp")
		})

		It("should expose spot pricing last success metric", func() {
			By("verifying data_last_success metric for spot-pricing exists")
			Eventually(func(g Gomega) {
				metricsOutput, err := getMetricsOutput()
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(metricsOutput).To(MatchRegexp(`lumina_data_last_success\{.*data_type="spot-pricing"`),
					"data_last_success metric for spot-pricing should be present")
			}, 30*time.Second, 2*time.Second).Should(Succeed())

			By("verifying spot pricing last success metric indicates success")
			metricsOutput, err := getMetricsOutput()
			Expect(err).NotTo(HaveOccurred())

			// Extract the value - should be 1 (success) or 0 (failure)
			lines := strings.Split(metricsOutput, "\n")
			foundSuccess := false

			for _, line := range lines {
				if strings.Contains(line, "lumina_data_last_success") && strings.Contains(line, `data_type="spot-pricing"`) {
					parts := strings.Fields(line)
					if len(parts) >= 2 {
						// Value should be 1 (success)
						if parts[1] == "1" || parts[1] == "1.0" {
							foundSuccess = true
							break
						}
					}
				}
			}
			Expect(foundSuccess).To(BeTrue(),
				"data_last_success for spot-pricing should be 1 (success)")
		})
	})

	Context("Spot Pricing Cache Behavior", func() {
		It("should populate cache with spot prices for running instances", func() {
			By("verifying cache contains spot prices")
			Eventually(func(g Gomega) {
				output, err := getPodLogs(controllerPodName, nil)
				g.Expect(err).NotTo(HaveOccurred())

				// Look for cache statistics or spot price updates
				g.Expect(output).To(Or(
					ContainSubstring("updated spot pricing cache"),
					ContainSubstring("new_prices"),
				), "Cache should be populated with spot prices")
			}, 40*time.Second, 2*time.Second).Should(Succeed())
		})

		It("should use fast reconciliation interval for lazy-loading", func() {
			By("verifying reconciliation interval is configured")
			Eventually(func(g Gomega) {
				output, err := getPodLogs(controllerPodName, nil)
				g.Expect(err).NotTo(HaveOccurred())
				// Default interval is 15 seconds for lazy-loading
				g.Expect(output).To(ContainSubstring("reconciliation interval configured"),
					"Reconciliation interval should be configured")
			}, 20*time.Second, 2*time.Second).Should(Succeed())
		})

		It("should handle missing spot prices gracefully", func() {
			By("checking for any spot pricing errors in logs")
			output, err := getPodLogs(controllerPodName, nil)
			Expect(err).NotTo(HaveOccurred())

			// If there are spot pricing errors, they should be handled gracefully
			if strings.Contains(output, "failed to describe spot prices") {
				// Verify the reconciler continues despite errors
				Expect(output).To(ContainSubstring("reconciliation cycle completed"),
					"Reconciler should continue even with spot pricing errors")
			}
		})
	})

	Context("LocalStack Integration", func() {
		It("should successfully query LocalStack for spot prices", func() {
			By("verifying spot pricing queries to LocalStack")
			Eventually(func(g Gomega) {
				output, err := getPodLogs(controllerPodName, nil)
				g.Expect(err).NotTo(HaveOccurred())

				// Look for evidence of successful spot pricing queries
				// LocalStack returns synthetic spot pricing data
				g.Expect(output).To(Or(
					ContainSubstring("updated spot pricing cache"),
					ContainSubstring("fetching missing spot prices"),
					ContainSubstring("all spot prices are cached"),
				), "Should successfully query LocalStack for spot prices")
			}, 40*time.Second, 2*time.Second).Should(Succeed())
		})

		It("should receive synthetic spot price data from LocalStack", func() {
			By("verifying spot prices were received")
			Eventually(func(g Gomega) {
				output, err := getPodLogs(controllerPodName, nil)
				g.Expect(err).NotTo(HaveOccurred())

				// If spot prices were fetched, cache should be updated
				if strings.Contains(output, "updated spot pricing cache") {
					// Verify new_prices count is mentioned
					g.Expect(output).To(MatchRegexp(`new_prices":\d+`),
						"Cache update should include new_prices count")
				}
			}, 40*time.Second, 2*time.Second).Should(Succeed())
		})
	})

	// After each test, check for failures and collect logs
	AfterEach(func() {
		specReport := CurrentSpecReport()
		if specReport.Failed() {
			By("Fetching controller manager pod logs for failed test")
			controllerLogs, err := getPodLogs(controllerPodName, nil)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Controller logs:\n%s", controllerLogs)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get Controller logs: %s", err)
			}
		}
	})
})
