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

var _ = Describe("EC2 Reconciler", Ordered, func() {
	var controllerPodName string

	// Get controller pod name before running tests
	BeforeAll(func() {
		By("getting controller pod name for EC2 tests")
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

	Context("EC2 Data Collection", func() {
		It("should collect EC2 instance data from LocalStack", func() {
			By("waiting for EC2 reconciler to run")
			Eventually(func(g Gomega) {
				output, err := getPodLogs(controllerPodName, nil)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("starting EC2 reconciliation cycle"),
					"EC2 reconciler should have started")
			}, 2*time.Minute, 5*time.Second).Should(Succeed())

			By("verifying EC2 reconciliation completed successfully")
			Eventually(func(g Gomega) {
				output, err := getPodLogs(controllerPodName, nil)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("reconciliation cycle completed successfully"),
					"EC2 reconciler should have completed successfully")
			}, 2*time.Minute, 5*time.Second).Should(Succeed())

			By("verifying EC2 instances were collected")
			Eventually(func(g Gomega) {
				output, err := getPodLogs(controllerPodName, nil)
				g.Expect(err).NotTo(HaveOccurred())
				// The controller should log cache statistics showing instances found
				g.Expect(output).To(ContainSubstring("cache statistics"),
					"Controller should log EC2 cache statistics")
			}, 30*time.Second, 2*time.Second).Should(Succeed())
		})
	})

	Context("EC2 Metrics", func() {
		It("should expose ec2_instance metric", func() {
			By("waiting for EC2 metrics to be available")
			Eventually(func(g Gomega) {
				metricsOutput, err := getMetricsOutput()
				g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve metrics")

				// Verify ec2_instance metric exists
				g.Expect(metricsOutput).To(ContainSubstring("ec2_instance{"),
					"ec2_instance metric should be present")
			}, 2*time.Minute, 5*time.Second).Should(Succeed())

			By("verifying ec2_instance metric has correct labels")
			metricsOutput, err := getMetricsOutput()
			Expect(err).NotTo(HaveOccurred())

			// The metric should have labels: account_id, region, instance_type, availability_zone, instance_id
			Expect(metricsOutput).To(MatchRegexp(`ec2_instance\{.*account_id="[^"]+"`),
				"ec2_instance should have account_id label")
			Expect(metricsOutput).To(MatchRegexp(`ec2_instance\{.*region="[^"]+"`),
				"ec2_instance should have region label")
			Expect(metricsOutput).To(MatchRegexp(`ec2_instance\{.*instance_type="[^"]+"`),
				"ec2_instance should have instance_type label")
			Expect(metricsOutput).To(MatchRegexp(`ec2_instance\{.*availability_zone="[^"]+"`),
				"ec2_instance should have availability_zone label")
			Expect(metricsOutput).To(MatchRegexp(`ec2_instance\{.*instance_id="[^"]+"`),
				"ec2_instance should have instance_id label")
		})

		It("should expose ec2_instance_count metric", func() {
			By("verifying ec2_instance_count metric exists")
			Eventually(func(g Gomega) {
				metricsOutput, err := getMetricsOutput()
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(metricsOutput).To(ContainSubstring("ec2_instance_count{"),
					"ec2_instance_count metric should be present")
			}, 2*time.Minute, 5*time.Second).Should(Succeed())

			By("verifying ec2_instance_count metric has correct labels")
			metricsOutput, err := getMetricsOutput()
			Expect(err).NotTo(HaveOccurred())

			// The metric should have labels: account_id, region, instance_family
			Expect(metricsOutput).To(MatchRegexp(`ec2_instance_count\{.*account_id="[^"]+"`),
				"ec2_instance_count should have account_id label")
			Expect(metricsOutput).To(MatchRegexp(`ec2_instance_count\{.*region="[^"]+"`),
				"ec2_instance_count should have region label")
			Expect(metricsOutput).To(MatchRegexp(`ec2_instance_count\{.*instance_family="[^"]+"`),
				"ec2_instance_count should have instance_family label")

			By("verifying ec2_instance_count has non-zero values")
			// Extract the metric values - they should be greater than 0 if instances exist
			lines := strings.Split(metricsOutput, "\n")
			foundNonZeroCount := false
			for _, line := range lines {
				if strings.HasPrefix(line, "ec2_instance_count{") {
					// Extract the value (after the space)
					parts := strings.Fields(line)
					if len(parts) >= 2 {
						value := parts[1]
						if value != "0" && value != "0.0" {
							foundNonZeroCount = true
							break
						}
					}
				}
			}
			Expect(foundNonZeroCount).To(BeTrue(), "ec2_instance_count should have at least one non-zero value")
		})

		It("should expose ec2_running_instance_count metric", func() {
			By("verifying ec2_running_instance_count metric exists")
			Eventually(func(g Gomega) {
				metricsOutput, err := getMetricsOutput()
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(metricsOutput).To(ContainSubstring("ec2_running_instance_count{"),
					"ec2_running_instance_count metric should be present")
			}, 2*time.Minute, 5*time.Second).Should(Succeed())

			By("verifying ec2_running_instance_count metric has correct labels")
			metricsOutput, err := getMetricsOutput()
			Expect(err).NotTo(HaveOccurred())

			// The metric should have labels: account_id, region
			Expect(metricsOutput).To(MatchRegexp(`ec2_running_instance_count\{.*account_id="[^"]+"`),
				"ec2_running_instance_count should have account_id label")
			Expect(metricsOutput).To(MatchRegexp(`ec2_running_instance_count\{.*region="[^"]+"`),
				"ec2_running_instance_count should have region label")

			By("verifying ec2_running_instance_count has non-zero values")
			// Extract the metric values - they should be greater than 0 if instances exist
			lines := strings.Split(metricsOutput, "\n")
			foundNonZeroCount := false
			for _, line := range lines {
				if strings.HasPrefix(line, "ec2_running_instance_count{") {
					// Extract the value (after the space)
					parts := strings.Fields(line)
					if len(parts) >= 2 {
						value := parts[1]
						if value != "0" && value != "0.0" {
							foundNonZeroCount = true
							break
						}
					}
				}
			}
			Expect(foundNonZeroCount).To(BeTrue(), "ec2_running_instance_count should have at least one non-zero value")
		})

		It("should log EC2 metrics update in controller", func() {
			By("checking for metrics update log message")
			Eventually(func(g Gomega) {
				output, err := getPodLogs(controllerPodName, nil)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("updated EC2 instance metrics"),
					"Controller should log EC2 metrics update")
			}, 2*time.Minute, 5*time.Second).Should(Succeed())
		})
	})

	Context("EC2 Metric Help Text", func() {
		It("should have correct help text for ec2_instance", func() {
			metricsOutput, err := getMetricsOutput()
			Expect(err).NotTo(HaveOccurred())

			// Verify help text for ec2_instance
			Expect(metricsOutput).To(ContainSubstring("# HELP ec2_instance"),
				"Should have help text for ec2_instance")
			Expect(metricsOutput).To(MatchRegexp(`(?i)# HELP ec2_instance.*running.*EC2.*instance`),
				"Help text should mention running EC2 instance")
		})

		It("should have correct help text for ec2_instance_count", func() {
			metricsOutput, err := getMetricsOutput()
			Expect(err).NotTo(HaveOccurred())

			// Verify help text for ec2_instance_count
			Expect(metricsOutput).To(ContainSubstring("# HELP ec2_instance_count"),
				"Should have help text for ec2_instance_count")
			Expect(metricsOutput).To(MatchRegexp(`(?i)# HELP ec2_instance_count.*count.*instance.*family`),
				"Help text should mention count by instance family")
		})

		It("should have correct help text for ec2_running_instance_count", func() {
			metricsOutput, err := getMetricsOutput()
			Expect(err).NotTo(HaveOccurred())

			// Verify help text for ec2_running_instance_count
			Expect(metricsOutput).To(ContainSubstring("# HELP ec2_running_instance_count"),
				"Should have help text for ec2_running_instance_count")
			Expect(metricsOutput).To(MatchRegexp(`(?i)# HELP ec2_running_instance_count.*total.*count.*running`),
				"Help text should mention total count of running instances")
		})
	})

	// After each test, check for failures and collect logs
	AfterEach(func() {
		specReport := CurrentSpecReport()
		if specReport.Failed() {
			By("Fetching controller manager pod logs")
			controllerLogs, err := getPodLogs(controllerPodName, nil)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Controller logs:\n %s", controllerLogs)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get Controller logs: %s", err)
			}
		}
	})
})
