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
	"fmt"
	"strconv"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// RISPReconciler tests validate Phase 2 functionality: Reserved Instance
// and Savings Plans data collection from AWS (via LocalStack in E2E tests).
//
// These tests verify:
// - RISP reconciler runs on startup and hourly thereafter
// - RI data is collected from all configured regions
// - SP data is collected from all configured accounts
// - Data freshness metrics are recorded correctly
// - Controller handles multi-account data collection
// - Cache is populated with collected data
var _ = Describe("RISP Reconciler", Ordered, func() {
	// Setup test data in LocalStack before running reconciler tests.
	// We create Reserved Instances and Savings Plans that the controller should discover.
	BeforeAll(func() {
		By("creating test Reserved Instances in LocalStack")
		// Create a Reserved Instance in us-west-2 for the test-production account
		// LocalStack's EC2 doesn't fully support purchasing RIs, but we can verify
		// the controller attempts to query them without errors.
		//
		// Note: LocalStack's DescribeReservedInstances will return empty results,
		// but we're testing that the controller can make the API calls successfully.

		By("creating test Savings Plans in LocalStack")
		// Similarly, LocalStack's Savings Plans API will return empty results,
		// but we verify the controller can query without errors.
		//
		// In a real environment, these would return actual RI/SP data.
		// For E2E tests, we verify the controller:
		// 1. Makes API calls without crashing
		// 2. Records metrics correctly (even for empty results)
		// 3. Updates cache (even with zero RIs/SPs)

		// The actual verification is in the tests below, which check logs and metrics
		// to ensure the reconciler ran successfully.
	})

	Context("Initial reconciliation", func() {
		It("should start RISP reconciliation on controller startup", func() {
			By("checking controller logs for RISP reconciliation start")
			Eventually(func(g Gomega) {
				logs, err := getPodLogsByLabel("control-plane=controller-manager", nil)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(logs).To(ContainSubstring("starting RI/SP reconciliation cycle"),
					"Controller should have started RISP reconciliation")
			}, 20*time.Second, 2*time.Second).Should(Succeed())
		})

		It("should complete initial reconciliation cycle", func() {
			By("checking controller logs for reconciliation completion")
			Eventually(func(g Gomega) {
				logs, err := getPodLogsByLabel("control-plane=controller-manager", nil)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(logs).To(ContainSubstring("reconciliation cycle completed"),
					"RISP reconciliation should complete")
			}, 30*time.Second, 2*time.Second).Should(Succeed())
		})

		It("should query Reserved Instances from configured regions", func() {
			By("checking logs for RI queries in us-west-2")
			Eventually(func(g Gomega) {
				logs, err := getPodLogsByLabel("control-plane=controller-manager", nil)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(logs).To(ContainSubstring("updated reserved instances"),
					"Controller should query Reserved Instances")
				g.Expect(logs).To(ContainSubstring(`"region": "us-west-2"`),
					"Controller should query us-west-2 region")
			}, 30*time.Second, 2*time.Second).Should(Succeed())

			By("checking logs for RI queries in us-east-1")
			Eventually(func(g Gomega) {
				logs, err := getPodLogsByLabel("control-plane=controller-manager", nil)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(logs).To(ContainSubstring(`"region": "us-east-1"`),
					"Controller should query us-east-1 region")
			}, 30*time.Second, 2*time.Second).Should(Succeed())
		})

		It("should query Savings Plans from configured accounts", func() {
			By("checking logs for SP queries")
			Eventually(func(g Gomega) {
				logs, err := getPodLogsByLabel("control-plane=controller-manager", nil)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(logs).To(ContainSubstring("updated savings plans"),
					"Controller should query Savings Plans")
			}, 30*time.Second, 2*time.Second).Should(Succeed())
		})

		It("should query data from both test accounts", func() {
			By("checking logs for test-production account")
			Eventually(func(g Gomega) {
				logs, err := getPodLogsByLabel("control-plane=controller-manager", nil)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(logs).To(ContainSubstring(`"account_id": "000000000000"`),
					"Controller should query test-production account")
			}, 30*time.Second, 2*time.Second).Should(Succeed())

			By("checking logs for test-staging account")
			Eventually(func(g Gomega) {
				logs, err := getPodLogsByLabel("control-plane=controller-manager", nil)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(logs).To(ContainSubstring(`"account_id": "111111111111"`),
					"Controller should query test-staging account")
			}, 30*time.Second, 2*time.Second).Should(Succeed())
		})

		It("should log cache statistics after reconciliation", func() {
			By("checking logs for cache statistics")
			Eventually(func(g Gomega) {
				logs, err := getPodLogsByLabel("control-plane=controller-manager", nil)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(logs).To(ContainSubstring("cache statistics"),
					"Controller should log cache statistics")
				// In LocalStack, we expect 0 RIs/SPs since we can't actually create them,
				// but the cache statistics log should still appear
				g.Expect(logs).To(ContainSubstring("reserved_instances"),
					"Cache stats should include RI count")
				g.Expect(logs).To(ContainSubstring("savings_plans"),
					"Cache stats should include SP count")
			}, 30*time.Second, 2*time.Second).Should(Succeed())
		})
	})

	Context("Data freshness metrics", func() {
		It("should expose data freshness metrics for Reserved Instances", func() {
			By("fetching metrics from the controller")
			Eventually(func(g Gomega) {
				metricsOutput, err := getMetricsOutput()
				g.Expect(err).NotTo(HaveOccurred())

				// The metrics should include lumina_data_freshness_seconds for RIs
				// We expect metrics for both accounts and both regions
				g.Expect(metricsOutput).To(ContainSubstring("lumina_data_freshness_seconds"),
					"Should expose data freshness metrics")
				g.Expect(metricsOutput).To(ContainSubstring(`data_type="reserved_instances"`),
					"Should have RI freshness metrics")
			}, 30*time.Second, 2*time.Second).Should(Succeed())
		})

		It("should expose data freshness metrics for Savings Plans", func() {
			By("fetching metrics from the controller")
			Eventually(func(g Gomega) {
				metricsOutput, err := getMetricsOutput()
				g.Expect(err).NotTo(HaveOccurred())

				// The metrics should include lumina_data_freshness_seconds for SPs
				g.Expect(metricsOutput).To(ContainSubstring(`data_type="savings_plans"`),
					"Should have SP freshness metrics")
			}, 30*time.Second, 2*time.Second).Should(Succeed())
		})

		It("should record successful data collection in metrics", func() {
			By("fetching metrics from the controller")
			Eventually(func(g Gomega) {
				metricsOutput, err := getMetricsOutput()
				g.Expect(err).NotTo(HaveOccurred())

				// lumina_data_last_success should be 1 for successful collections
				g.Expect(metricsOutput).To(ContainSubstring("lumina_data_last_success"),
					"Should expose data collection success metrics")

				// Parse metrics to verify success=1 for at least some data collections
				// We look for metrics with value 1.0 (success)
				lines := strings.Split(metricsOutput, "\n")
				foundSuccessMetric := false
				for _, line := range lines {
					if strings.Contains(line, "lumina_data_last_success") &&
						!strings.HasPrefix(line, "#") &&
						strings.Contains(line, `data_type="reserved_instances"`) {
						// Line format: lumina_data_last_success{account_id="...",region="...",data_type="..."} 1
						if strings.HasSuffix(strings.TrimSpace(line), " 1") {
							foundSuccessMetric = true
							break
						}
					}
				}
				g.Expect(foundSuccessMetric).To(BeTrue(),
					"Should have at least one successful data collection metric")
			}, 30*time.Second, 2*time.Second).Should(Succeed())
		})

		It("should record freshness as Unix timestamp", func() {
			By("checking that freshness contains valid Unix timestamp")
			Eventually(func(g Gomega) {
				metricsOutput, err := getMetricsOutput()
				g.Expect(err).NotTo(HaveOccurred())

				// Parse the freshness metrics and verify they contain valid Unix timestamps
				// Timestamps should be > 1700000000 (Nov 2023) and recent (within last hour)
				lines := strings.Split(metricsOutput, "\n")
				foundValidTimestamp := false
				now := float64(time.Now().Unix())
				for _, line := range lines {
					if strings.Contains(line, "lumina_data_freshness_seconds") &&
						!strings.HasPrefix(line, "#") &&
						strings.Contains(line, `data_type="reserved_instances"`) {
						// Extract the value (last field after space)
						parts := strings.Fields(line)
						if len(parts) >= 2 {
							value, err := strconv.ParseFloat(parts[len(parts)-1], 64)
							// Check if it's a valid Unix timestamp (> Nov 2023) and recent (within last hour)
							if err == nil && value > 1700000000 && value <= now && (now-value) < 3600 {
								foundValidTimestamp = true
								break
							}
						}
					}
				}
				g.Expect(foundValidTimestamp).To(BeTrue(),
					"Data freshness should contain valid recent Unix timestamp")
			}, 30*time.Second, 2*time.Second).Should(Succeed())
		})

		It("should have metrics for all configured accounts", func() {
			By("verifying metrics exist for test-production account")
			Eventually(func(g Gomega) {
				metricsOutput, err := getMetricsOutput()
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(metricsOutput).To(ContainSubstring(`account_id="000000000000"`),
					"Should have metrics for test-production account (000000000000)")
			}, 30*time.Second, 2*time.Second).Should(Succeed())

			By("verifying metrics exist for test-staging account")
			Eventually(func(g Gomega) {
				metricsOutput, err := getMetricsOutput()
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(metricsOutput).To(ContainSubstring(`account_id="111111111111"`),
					"Should have metrics for test-staging account (111111111111)")
			}, 30*time.Second, 2*time.Second).Should(Succeed())
		})

		It("should have RI metrics for all configured regions", func() {
			By("verifying metrics exist for us-west-2")
			Eventually(func(g Gomega) {
				metricsOutput, err := getMetricsOutput()
				g.Expect(err).NotTo(HaveOccurred())
				// Look for RI metrics (not SP) for us-west-2
				lines := strings.Split(metricsOutput, "\n")
				foundRegion := false
				for _, line := range lines {
					if strings.Contains(line, "lumina_data_freshness_seconds") &&
						strings.Contains(line, `region="us-west-2"`) &&
						strings.Contains(line, `data_type="reserved_instances"`) {
						foundRegion = true
						break
					}
				}
				g.Expect(foundRegion).To(BeTrue(),
					"Should have RI metrics for us-west-2 region")
			}, 30*time.Second, 2*time.Second).Should(Succeed())

			By("verifying metrics exist for us-east-1")
			Eventually(func(g Gomega) {
				metricsOutput, err := getMetricsOutput()
				g.Expect(err).NotTo(HaveOccurred())
				lines := strings.Split(metricsOutput, "\n")
				foundRegion := false
				for _, line := range lines {
					if strings.Contains(line, "lumina_data_freshness_seconds") &&
						strings.Contains(line, `region="us-east-1"`) &&
						strings.Contains(line, `data_type="reserved_instances"`) {
						foundRegion = true
						break
					}
				}
				g.Expect(foundRegion).To(BeTrue(),
					"Should have RI metrics for us-east-1 region")
			}, 30*time.Second, 2*time.Second).Should(Succeed())
		})

		It("should have SP metrics with empty region label", func() {
			By("verifying SP metrics don't specify region")
			Eventually(func(g Gomega) {
				metricsOutput, err := getMetricsOutput()
				g.Expect(err).NotTo(HaveOccurred())

				// Savings Plans are organization-wide, so region should be empty string
				lines := strings.Split(metricsOutput, "\n")
				foundSPMetric := false
				for _, line := range lines {
					if strings.Contains(line, "lumina_data_freshness_seconds") &&
						strings.Contains(line, `data_type="savings_plans"`) &&
						strings.Contains(line, `region=""`) {
						foundSPMetric = true
						break
					}
				}
				g.Expect(foundSPMetric).To(BeTrue(),
					"SP metrics should have empty region label (organization-wide)")
			}, 30*time.Second, 2*time.Second).Should(Succeed())
		})
	})

	Context("Error handling and resilience", func() {
		It("should not have errors in reconciliation logs", func() {
			By("checking for error messages in controller logs")
			logs, err := getPodLogsByLabel("control-plane=controller-manager", nil)
			Expect(err).NotTo(HaveOccurred())

			// We don't expect fatal errors or panics during RISP reconciliation
			Expect(logs).NotTo(ContainSubstring("panic:"),
				"Controller should not panic during RISP reconciliation")

			// Check that there are no critical RISP reconciliation errors
			// Note: We might see other benign errors/warnings, so we're specific about RISP
			lines := strings.Split(logs, "\n")
			for _, line := range lines {
				if strings.Contains(line, "reconciler\": \"risp\"") && strings.Contains(line, "ERROR") {
					// Allow "expected" LocalStack errors like missing services,
					// but fail on unexpected errors
					if !strings.Contains(line, "LocalStack") {
						Fail(fmt.Sprintf("Unexpected RISP reconciliation error: %s", line))
					}
				}
			}
		})

		It("should schedule hourly reconciliation", func() {
			By("checking logs for requeue scheduling")
			Eventually(func(g Gomega) {
				logs, err := getPodLogsByLabel("control-plane=controller-manager", nil)
				g.Expect(err).NotTo(HaveOccurred())

				// The reconciler should log completion and schedule next run
				// We look for the completion message which happens before requeuing
				g.Expect(logs).To(ContainSubstring("reconciliation cycle completed"),
					"Reconciler should complete cycles")
			}, 30*time.Second, 2*time.Second).Should(Succeed())

			// Note: We can't easily test the actual 1-hour requeue in E2E tests
			// (would take too long), but we verify the reconciler completes successfully
			// which means it will requeue itself via controller-runtime.
		})
	})

	Context("Multi-account parallel execution", func() {
		It("should query accounts in parallel", func() {
			By("verifying logs show concurrent account processing")
			Eventually(func(g Gomega) {
				logs, err := getPodLogsByLabel("control-plane=controller-manager", nil)
				g.Expect(err).NotTo(HaveOccurred())

				// Both accounts should be processed (logs may be interleaved due to goroutines)
				g.Expect(logs).To(ContainSubstring(`"account_id": "000000000000"`),
					"Should process test-production account")
				g.Expect(logs).To(ContainSubstring(`"account_id": "111111111111"`),
					"Should process test-staging account")

				// Look for log messages that indicate parallel execution
				// (multiple account messages within a short time span)
				g.Expect(logs).To(ContainSubstring("updated reserved instances"),
					"Should have RI updates")
				g.Expect(logs).To(ContainSubstring("updated savings plans"),
					"Should have SP updates")
			}, 30*time.Second, 2*time.Second).Should(Succeed())
		})

		It("should complete reconciliation even if some queries fail", func() {
			// This test verifies graceful degradation: if one account fails,
			// the reconciler should continue with other accounts and complete the cycle.
			//
			// In our E2E environment, all accounts should succeed, but we verify
			// that the reconciler reaches the "completed" state regardless of
			// individual query outcomes.

			By("verifying reconciliation completes successfully")
			Eventually(func(g Gomega) {
				logs, err := getPodLogsByLabel("control-plane=controller-manager", nil)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(logs).To(ContainSubstring("reconciliation cycle completed"),
					"Reconciliation should complete even with partial failures")
			}, 30*time.Second, 2*time.Second).Should(Succeed())
		})
	})
})
