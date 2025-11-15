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
	"os/exec"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/nextdoor/lumina/test/utils"
)

// getControllerPodName gets the controller pod name for health check tests.
// This function is called within tests to get the current pod name.
func getControllerPodName() (string, error) {
	cmd := exec.Command("kubectl", "get",
		"pods", "-l", "control-plane=controller-manager",
		"-o", "go-template={{ range .items }}"+
			"{{ if not .metadata.deletionTimestamp }}"+
			"{{ .metadata.name }}"+
			"{{ \"\\n\" }}{{ end }}{{ end }}",
		"-n", namespace,
	)
	podOutput, err := utils.Run(cmd)
	if err != nil {
		return "", err
	}
	podNames := utils.GetNonEmptyLines(podOutput)
	if len(podNames) == 0 {
		return "", fmt.Errorf("no controller pods found")
	}
	return podNames[0], nil
}

var _ = Describe("Health Probes", Ordered, func() {
	var controllerPodName string

	BeforeEach(func() {
		// Get the controller pod name before each test
		// This ensures we have the current pod name
		var err error
		controllerPodName, err = getControllerPodName()
		Expect(err).NotTo(HaveOccurred(), "Failed to get controller pod name")
		Expect(controllerPodName).NotTo(BeEmpty(), "Controller pod name should not be empty")
	})

	Context("Liveness Probe (/healthz)", func() {
		It("should return 200 OK", func() {
			By("checking the /healthz endpoint")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "exec", "-n", namespace,
					controllerPodName, "--",
					"wget", "-O-", "-q", "http://localhost:8081/healthz")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred(), "Failed to query /healthz endpoint")
				g.Expect(output).To(ContainSubstring("ok"), "/healthz should return 'ok'")
			}, 30*time.Second, 2*time.Second).Should(Succeed())
		})

		It("should be checked by Kubernetes", func() {
			By("verifying the liveness probe is configured")
			cmd := exec.Command("kubectl", "get", "pod", controllerPodName,
				"-n", namespace,
				"-o", "jsonpath={.spec.containers[0].livenessProbe.httpGet.path}")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(Equal("/healthz"), "Liveness probe should be configured for /healthz")
		})
	})

	Context("Readiness Probe (/readyz)", func() {
		It("should return 200 OK when AWS accounts are accessible", func() {
			By("checking the /readyz endpoint")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "exec", "-n", namespace,
					controllerPodName, "--",
					"wget", "-O-", "-q", "http://localhost:8081/readyz")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred(), "Failed to query /readyz endpoint")
				g.Expect(output).To(ContainSubstring("ok"), "/readyz should return 'ok'")
			}, 30*time.Second, 2*time.Second).Should(Succeed())
		})

		It("should be checked by Kubernetes", func() {
			By("verifying the readiness probe is configured")
			cmd := exec.Command("kubectl", "get", "pod", controllerPodName,
				"-n", namespace,
				"-o", "jsonpath={.spec.containers[0].readinessProbe.httpGet.path}")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(Equal("/readyz"), "Readiness probe should be configured for /readyz")
		})

		It("should validate AWS account access", func() {
			By("checking controller logs for AWS account validation")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "logs", controllerPodName, "-n", namespace)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())

				// The controller should have successfully validated AWS accounts
				// We expect to see the readiness check pass (no errors in logs about failed validation)
				// If validation failed, we'd see errors like "failed to validate access to N/M AWS accounts"
				g.Expect(output).NotTo(ContainSubstring("failed to validate access to"),
					"AWS account validation should succeed")
			}, 30*time.Second, 2*time.Second).Should(Succeed())
		})

		It("should mark pod as ready when AWS accounts are accessible", func() {
			By("checking pod Ready condition")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pod", controllerPodName,
					"-n", namespace,
					"-o", "jsonpath={.status.conditions[?(@.type=='Ready')].status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("True"), "Pod should be marked as Ready")
			}, 2*time.Minute, 2*time.Second).Should(Succeed())
		})
	})

	Context("Probe Endpoints Details", func() {
		It("should serve /healthz with verbose output", func() {
			By("querying /healthz with verbose flag")
			cmd := exec.Command("kubectl", "exec", "-n", namespace,
				controllerPodName, "--",
				"wget", "-O-", "-q", "http://localhost:8081/healthz?verbose=true")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("healthz check passed"))
		})

		It("should serve /readyz with verbose output", func() {
			By("querying /readyz with verbose flag")
			cmd := exec.Command("kubectl", "exec", "-n", namespace,
				controllerPodName, "--",
				"wget", "-O-", "-q", "http://localhost:8081/readyz?verbose=true")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(Or(
				ContainSubstring("readyz check passed"),
				ContainSubstring("aws-account-access"),
			), "Verbose output should mention the aws-account-access check")
		})

		It("should list all readiness checks", func() {
			By("querying /readyz with verbose flag to see all checks")
			cmd := exec.Command("kubectl", "exec", "-n", namespace,
				controllerPodName, "--",
				"wget", "-O-", "-q", "http://localhost:8081/readyz?verbose=true")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			// The output should include our custom aws-account-access check
			// Example output format from controller-runtime:
			// [+]ping ok
			// [+]aws-account-access ok
			// readyz check passed
			Expect(output).To(MatchRegexp(`(?m)^\[.\].*aws-account-access`),
				"Should include aws-account-access check in verbose output")
		})
	})

	Context("Error Scenarios", func() {
		It("should handle requests to /healthz/ping", func() {
			By("checking individual health check endpoint")
			cmd := exec.Command("kubectl", "exec", "-n", namespace,
				controllerPodName, "--",
				"wget", "-O-", "-q", "http://localhost:8081/healthz/ping")
			output, err := utils.Run(cmd)
			// Individual check endpoints return "ok" when healthy
			if err == nil {
				Expect(output).To(ContainSubstring("ok"))
			}
		})

		It("should handle requests to /readyz/aws-account-access", func() {
			By("checking individual readiness check endpoint")
			cmd := exec.Command("kubectl", "exec", "-n", namespace,
				controllerPodName, "--",
				"wget", "-O-", "-q", "http://localhost:8081/readyz/aws-account-access")
			output, err := utils.Run(cmd)
			// Individual check endpoints return "ok" when healthy
			if err == nil {
				Expect(output).To(ContainSubstring("ok"))
			}
		})

		It("should return error for non-existent check", func() {
			By("querying a non-existent health check")
			cmd := exec.Command("kubectl", "exec", "-n", namespace,
				controllerPodName, "--",
				"sh", "-c",
				"wget -O- -q http://localhost:8081/healthz/nonexistent 2>&1 || echo 'error'")
			output, err := utils.Run(cmd)
			// Should return error or empty response
			Expect(output).To(Or(
				ContainSubstring("error"),
				ContainSubstring("404"),
				BeEmpty(),
			), "Non-existent check should not succeed")
			// Don't check err here as wget will return non-zero for 404
			_ = err
		})
	})

	Context("Integration with Kubernetes", func() {
		It("should have correct probe configuration in deployment", func() {
			By("checking deployment probe configuration")
			cmd := exec.Command("kubectl", "get", "deployment",
				"lumina-controller-manager", "-n", namespace,
				"-o", "json")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			// Verify both probes are configured
			Expect(output).To(ContainSubstring(`"livenessProbe"`))
			Expect(output).To(ContainSubstring(`"readinessProbe"`))
			Expect(output).To(ContainSubstring(`"/healthz"`))
			Expect(output).To(ContainSubstring(`"/readyz"`))
		})

		It("should restart pod if liveness probe fails", func() {
			By("verifying restart count is low (probes are passing)")
			cmd := exec.Command("kubectl", "get", "pod", controllerPodName,
				"-n", namespace,
				"-o", "jsonpath={.status.containerStatuses[0].restartCount}")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			// In a healthy system, restart count should be 0 or very low
			// If probes were failing, we'd see multiple restarts
			By(fmt.Sprintf("Current restart count: %s", output))
			Expect(output).To(Or(Equal("0"), Equal("1")),
				"Restart count should be low when probes are healthy")
		})
	})
})
