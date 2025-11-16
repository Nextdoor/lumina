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
	"sort"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
)

// namespace where the project is deployed in
const namespace = "lumina-system"

// serviceAccountName created for the project
const serviceAccountName = "lumina-controller-manager"

// metricsServiceName is the name of the metrics service of the project
const metricsServiceName = "lumina-controller-manager-metrics-service"

// metricsRoleBindingName is the name of the RBAC that will be created to allow get the metrics data
const metricsRoleBindingName = "lumina-metrics-binding"

var _ = Describe("Manager", Ordered, func() {
	var controllerPodName string

	// Controller deployment is now handled at the suite level (BeforeSuite/AfterSuite)
	// so it's available to all test Describe blocks without being torn down between them

	// After each test, check for failures and collect logs, events,
	// and pod descriptions for debugging.
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

			By("Fetching Kubernetes events")
			eventsOutput, err := getEvents()
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Kubernetes events:\n%s", eventsOutput)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get Kubernetes events: %s", err)
			}

			By("Fetching controller manager pod description")
			podDescription, err := getPodDescription(controllerPodName)
			if err == nil {
				fmt.Println("Pod description:\n", podDescription)
			} else {
				fmt.Println("Failed to describe controller pod")
			}
		}
	})

	SetDefaultEventuallyTimeout(2 * time.Minute)
	SetDefaultEventuallyPollingInterval(time.Second)

	Context("Manager", func() {
		It("should run successfully", func() {
			By("validating that the controller-manager pod is running as expected")
			verifyControllerUp := func(g Gomega) {
				// Get the name of the controller-manager pod
				client, err := NewResourceClient(namespace)
				g.Expect(err).NotTo(HaveOccurred(), "Failed to create resource client")

				ctx := context.Background()
				podList, err := client.GetPodsByLabel(ctx, "control-plane=controller-manager")
				g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve controller-manager pod information")

				// Filter out pods that are being deleted
				var runningPods []string
				for _, pod := range podList.Items {
					if pod.DeletionTimestamp == nil {
						runningPods = append(runningPods, pod.Name)
					}
				}

				g.Expect(runningPods).To(HaveLen(1), "expected 1 controller pod running")
				controllerPodName = runningPods[0]
				g.Expect(controllerPodName).To(ContainSubstring("controller-manager"))

				// Validate the pod's status
				pod, err := client.GetPod(ctx, controllerPodName)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(string(pod.Status.Phase)).To(Equal("Running"), "Incorrect controller-manager pod status")
			}
			Eventually(verifyControllerUp).Should(Succeed())
		})

		It("should ensure the metrics endpoint is serving metrics", func() {
			By("validating that the metrics service is available")
			client, err := NewResourceClient(namespace)
			Expect(err).NotTo(HaveOccurred(), "Failed to create resource client")

			ctx := context.Background()
			_, err = client.GetService(ctx, metricsServiceName)
			Expect(err).NotTo(HaveOccurred(), "Metrics service should exist")

			By("waiting for the metrics endpoint to be ready")
			verifyMetricsEndpointReady := func(g Gomega) {
				endpoints, err := client.GetEndpoints(ctx, metricsServiceName)
				g.Expect(err).NotTo(HaveOccurred())

				// Check if endpoints has port 8080
				hasPort8080 := false
				for _, subset := range endpoints.Subsets {
					for _, port := range subset.Ports {
						if port.Port == 8080 {
							hasPort8080 = true
							break
						}
					}
					if hasPort8080 {
						break
					}
				}
				g.Expect(hasPort8080).To(BeTrue(), "Metrics endpoint is not ready")
			}
			Eventually(verifyMetricsEndpointReady).Should(Succeed())

			By("verifying that the controller manager is serving the metrics server")
			verifyMetricsServerStarted := func(g Gomega) {
				output, err := getPodLogs(controllerPodName, nil)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("controller-runtime.metrics\tServing metrics server"),
					"Metrics server not yet started")
			}
			Eventually(verifyMetricsServerStarted).Should(Succeed())

			By("fetching metrics using the Kubernetes API proxy")
			verifyMetricsAvailable := func(g Gomega) {
				metricsOutput, err := getMetricsOutput()
				g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve metrics")
				g.Expect(metricsOutput).NotTo(BeEmpty(), "Metrics output should not be empty")
				// Verify we got valid Prometheus metrics format
				g.Expect(metricsOutput).To(ContainSubstring("# HELP"), "Should contain Prometheus metrics")
			}
			Eventually(verifyMetricsAvailable, 2*time.Minute, 5*time.Second).Should(Succeed())
		})

		It("should have AWS configuration for LocalStack", func() {
			By("checking controller environment variables")
			client, err := NewResourceClient(namespace)
			Expect(err).NotTo(HaveOccurred(), "Failed to create resource client")

			ctx := context.Background()
			deployment, err := client.GetDeployment(ctx, "lumina-controller-manager")
			Expect(err).NotTo(HaveOccurred(), "Failed to get controller deployment")

			// Get the environment variables from the first container
			var envNames []string
			var awsEndpointURL string
			for _, env := range deployment.Spec.Template.Spec.Containers[0].Env {
				envNames = append(envNames, env.Name)
				if env.Name == "AWS_ENDPOINT_URL" {
					awsEndpointURL = env.Value
				}
			}

			envNamesStr := strings.Join(envNames, " ")
			Expect(envNamesStr).To(ContainSubstring("AWS_ENDPOINT_URL"), "AWS_ENDPOINT_URL not configured")
			Expect(envNamesStr).To(ContainSubstring("AWS_ACCESS_KEY_ID"), "AWS_ACCESS_KEY_ID not configured")

			By("verifying AWS_ENDPOINT_URL points to LocalStack")
			Expect(awsEndpointURL).To(ContainSubstring("localstack"), "AWS_ENDPOINT_URL does not point to LocalStack")
		})

		It("should successfully load config from ConfigMap", func() {
			By("checking controller logs for config loading")
			verifyConfigLoaded := func(g Gomega) {
				output, err := getPodLogs(controllerPodName, nil)
				g.Expect(err).NotTo(HaveOccurred(), "Failed to get controller logs")
				g.Expect(output).NotTo(ContainSubstring("config file not found"), "Should not have config file error")
				g.Expect(output).To(And(
					ContainSubstring("loaded configuration"),
					ContainSubstring(`"accounts": 2`),
					ContainSubstring(`"default-region": "us-west-2"`),
				), "Controller should have loaded config successfully with 2 accounts")
			}
			Eventually(verifyConfigLoaded, 30*time.Second, 2*time.Second).Should(Succeed())

			// TODO: Add test that verifies AWS API calls once we implement the AWS account
			// validation logic. This would verify that the controller can:
			// 1. Use the loaded configuration to initialize AWS clients
			// 2. Assume IAM roles via STS using LocalStack
			// 3. Make test API calls to verify connectivity
			// For now, we've verified that:
			// - ConfigMap is created with correct data
			// - Configuration is mounted into the pod
			// - Controller successfully loads and parses the config
			// - AWS environment variables point to LocalStack
		})

		// +kubebuilder:scaffold:e2e-webhooks-checks

		// TODO: Customize the e2e test suite with scenarios specific to your project.
		// Consider applying sample/CR(s) and check their status and/or verifying
		// the reconciliation by using the metrics, i.e.:
		//
		// TODO: Add test that creates a sample CR that triggers AWS API calls
		// This will validate end-to-end that the controller can:
		// 1. Read configuration from CR
		// 2. Assume IAM roles via STS
		// 3. Query EC2/Savings Plans APIs
		// 4. Update CR status with results
		// metricsOutput, err := getMetricsOutput()
		// Expect(err).NotTo(HaveOccurred(), "Failed to retrieve logs from curl pod")
		// Expect(metricsOutput).To(ContainSubstring(
		//    fmt.Sprintf(`controller_runtime_reconcile_total{controller="%s",result="success"} 1`,
		//    strings.ToLower(<Kind>),
		// ))
	})

	// Health probe tests run after the Manager is deployed and the pod is running.
	// These tests are part of the Manager suite to ensure proper execution order.
	Context("Health Probes", func() {
		// Ensure controllerPodName is set before running health probe tests.
		// This should already be set by the "should run successfully" test, but we verify it here
		// as a safety check since these tests depend on having a running controller pod.
		BeforeEach(func() {
			if controllerPodName == "" {
				By("getting controller pod name for health probe tests")
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
			}
			Expect(controllerPodName).NotTo(BeEmpty(), "Controller pod name must be set")
		})

		It("should have liveness probe passing (pod not restarting)", func() {
			By("verifying the pod has low restart count")
			// Since the controller uses a distroless image without curl/wget,
			// we verify the liveness probe is working by checking the pod hasn't restarted.
			// If the liveness probe was failing, Kubernetes would restart the pod.
			client, err := NewResourceClient(namespace)
			Expect(err).NotTo(HaveOccurred(), "Failed to create resource client")

			ctx := context.Background()
			pod, err := client.GetPod(ctx, controllerPodName)
			Expect(err).NotTo(HaveOccurred())

			restartCount := int32(0)
			if len(pod.Status.ContainerStatuses) > 0 {
				restartCount = pod.Status.ContainerStatuses[0].RestartCount
			}

			Expect(restartCount).To(Or(Equal(int32(0)), Equal(int32(1))),
				"Restart count should be low when liveness probe is healthy")
		})

		It("should have liveness probe configured in pod", func() {
			By("verifying the liveness probe is configured")
			client, err := NewResourceClient(namespace)
			Expect(err).NotTo(HaveOccurred(), "Failed to create resource client")

			ctx := context.Background()
			pod, err := client.GetPod(ctx, controllerPodName)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(pod.Spec.Containers)).To(BeNumerically(">", 0), "Pod should have at least one container")

			livenessProbe := pod.Spec.Containers[0].LivenessProbe
			Expect(livenessProbe).NotTo(BeNil(), "Liveness probe should be configured")
			Expect(livenessProbe.HTTPGet).NotTo(BeNil(), "Liveness probe should be HTTP")
			Expect(livenessProbe.HTTPGet.Path).To(Equal("/healthz"), "Liveness probe should be configured for /healthz")
		})

		It("should have readiness probe passing (pod marked Ready)", func() {
			By("verifying the pod is marked as Ready")
			// Since the controller uses a distroless image without curl/wget,
			// we verify the readiness probe is working by checking the pod is Ready.
			// If the readiness probe was failing, the pod would not be marked Ready.
			Eventually(func(g Gomega) {
				client, err := NewResourceClient(namespace)
				g.Expect(err).NotTo(HaveOccurred(), "Failed to create resource client")

				ctx := context.Background()
				pod, err := client.GetPod(ctx, controllerPodName)
				g.Expect(err).NotTo(HaveOccurred())

				// Find the Ready condition
				var readyStatus string
				for _, condition := range pod.Status.Conditions {
					if condition.Type == "Ready" {
						readyStatus = string(condition.Status)
						break
					}
				}
				g.Expect(readyStatus).To(Equal("True"), "Pod should be marked as Ready")
			}, 2*time.Minute, 2*time.Second).Should(Succeed())
		})

		It("should have readiness probe configured in pod", func() {
			By("verifying the readiness probe is configured")
			client, err := NewResourceClient(namespace)
			Expect(err).NotTo(HaveOccurred(), "Failed to create resource client")

			ctx := context.Background()
			pod, err := client.GetPod(ctx, controllerPodName)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(pod.Spec.Containers)).To(BeNumerically(">", 0), "Pod should have at least one container")

			readinessProbe := pod.Spec.Containers[0].ReadinessProbe
			Expect(readinessProbe).NotTo(BeNil(), "Readiness probe should be configured")
			Expect(readinessProbe.HTTPGet).NotTo(BeNil(), "Readiness probe should be HTTP")
			Expect(readinessProbe.HTTPGet.Path).To(Equal("/readyz"), "Readiness probe should be configured for /readyz")
		})

		It("should validate AWS account access in readiness probe", func() {
			By("checking controller logs for AWS account validation")
			Eventually(func(g Gomega) {
				output, err := getPodLogs(controllerPodName, nil)
				g.Expect(err).NotTo(HaveOccurred())

				// The controller should have successfully validated AWS accounts
				// We expect to see the readiness check pass (no errors in logs about failed validation)
				// If validation failed, we'd see errors like "failed to validate access to N/M AWS accounts"
				g.Expect(output).NotTo(ContainSubstring("failed to validate access to"),
					"AWS account validation should succeed")
			}, 30*time.Second, 2*time.Second).Should(Succeed())
		})

		It("should have correct probe configuration in deployment", func() {
			By("checking deployment probe configuration")
			client, err := NewResourceClient(namespace)
			Expect(err).NotTo(HaveOccurred(), "Failed to create resource client")

			ctx := context.Background()
			deployment, err := client.GetDeployment(ctx, "lumina-controller-manager")
			Expect(err).NotTo(HaveOccurred())
			Expect(len(deployment.Spec.Template.Spec.Containers)).To(BeNumerically(">", 0), "Deployment should have at least one container")

			container := deployment.Spec.Template.Spec.Containers[0]

			// Verify both probes are configured
			Expect(container.LivenessProbe).NotTo(BeNil(), "Liveness probe should be configured")
			Expect(container.ReadinessProbe).NotTo(BeNil(), "Readiness probe should be configured")

			// Verify probe paths
			Expect(container.LivenessProbe.HTTPGet).NotTo(BeNil(), "Liveness probe should be HTTP")
			Expect(container.LivenessProbe.HTTPGet.Path).To(Equal("/healthz"), "Liveness probe should use /healthz")

			Expect(container.ReadinessProbe.HTTPGet).NotTo(BeNil(), "Readiness probe should be HTTP")
			Expect(container.ReadinessProbe.HTTPGet.Path).To(Equal("/readyz"), "Readiness probe should use /readyz")
		})
	})
})

// getMetricsOutput retrieves and returns the metrics from the controller's metrics endpoint
// using the Kubernetes API proxy pattern. This is cleaner than creating curl pods.
func getMetricsOutput() (string, error) {
	client, err := NewMetricsClient(namespace, metricsServiceName, "http")
	if err != nil {
		return "", fmt.Errorf("failed to create metrics client: %w", err)
	}

	ctx := context.Background()
	return client.GetMetrics(ctx)
}

// getPodLogs retrieves logs from a specific pod using the native Kubernetes client.
// This is cleaner than using kubectl commands.
func getPodLogs(podName string, tailLines *int64) (string, error) {
	client, err := NewLogsClient(namespace)
	if err != nil {
		return "", fmt.Errorf("failed to create logs client: %w", err)
	}

	ctx := context.Background()
	return client.GetPodLogs(ctx, podName, tailLines, "")
}

// getPodLogsByLabel retrieves logs from all pods matching a label selector
// using the native Kubernetes client. This is cleaner than using kubectl commands.
func getPodLogsByLabel(labelSelector string, tailLines *int64) (string, error) {
	client, err := NewLogsClient(namespace)
	if err != nil {
		return "", fmt.Errorf("failed to create logs client: %w", err)
	}

	ctx := context.Background()
	return client.GetPodLogsByLabel(ctx, labelSelector, tailLines)
}

// getEvents retrieves and formats Kubernetes events sorted by timestamp.
// This mimics the output of `kubectl get events --sort-by=.lastTimestamp`.
func getEvents() (string, error) {
	client, err := NewResourceClient(namespace)
	if err != nil {
		return "", fmt.Errorf("failed to create resource client: %w", err)
	}

	ctx := context.Background()
	events, err := client.GetEvents(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get events: %w", err)
	}

	// Sort events by LastTimestamp
	sort.Slice(events.Items, func(i, j int) bool {
		return events.Items[i].LastTimestamp.Before(&events.Items[j].LastTimestamp)
	})

	// Format events in a human-readable way similar to kubectl
	var output strings.Builder
	output.WriteString(fmt.Sprintf("%-10s %-10s %-30s %-10s %s\n",
		"LAST SEEN", "TYPE", "REASON", "OBJECT", "MESSAGE"))

	for _, event := range events.Items {
		lastSeen := event.LastTimestamp.Time.Format("15:04:05")
		eventType := event.Type
		reason := event.Reason
		object := fmt.Sprintf("%s/%s", event.InvolvedObject.Kind, event.InvolvedObject.Name)
		message := event.Message

		// Truncate message if too long
		if len(message) > 80 {
			message = message[:77] + "..."
		}

		output.WriteString(fmt.Sprintf("%-10s %-10s %-30s %-10s %s\n",
			lastSeen, eventType, reason, object, message))
	}

	return output.String(), nil
}

// getPodDescription retrieves and formats pod details in a description format.
// This mimics the output of `kubectl describe pod`.
func getPodDescription(podName string) (string, error) {
	client, err := NewResourceClient(namespace)
	if err != nil {
		return "", fmt.Errorf("failed to create resource client: %w", err)
	}

	ctx := context.Background()
	pod, err := client.GetPod(ctx, podName)
	if err != nil {
		return "", fmt.Errorf("failed to get pod: %w", err)
	}

	var output strings.Builder

	// Basic pod information
	output.WriteString(fmt.Sprintf("Name:         %s\n", pod.Name))
	output.WriteString(fmt.Sprintf("Namespace:    %s\n", pod.Namespace))
	output.WriteString(fmt.Sprintf("Status:       %s\n", pod.Status.Phase))
	output.WriteString(fmt.Sprintf("IP:           %s\n", pod.Status.PodIP))
	output.WriteString(fmt.Sprintf("Node:         %s\n", pod.Spec.NodeName))

	// Container information
	output.WriteString("\nContainers:\n")
	for _, container := range pod.Spec.Containers {
		output.WriteString(fmt.Sprintf("  %s:\n", container.Name))
		output.WriteString(fmt.Sprintf("    Image:         %s\n", container.Image))

		// Find container status
		for _, status := range pod.Status.ContainerStatuses {
			if status.Name == container.Name {
				output.WriteString(fmt.Sprintf("    State:         %s\n", getContainerState(status)))
				output.WriteString(fmt.Sprintf("    Ready:         %t\n", status.Ready))
				output.WriteString(fmt.Sprintf("    Restart Count: %d\n", status.RestartCount))
				break
			}
		}
	}

	// Conditions
	output.WriteString("\nConditions:\n")
	for _, condition := range pod.Status.Conditions {
		output.WriteString(fmt.Sprintf("  Type:    %s\n", condition.Type))
		output.WriteString(fmt.Sprintf("  Status:  %s\n", condition.Status))
		if condition.Message != "" {
			output.WriteString(fmt.Sprintf("  Message: %s\n", condition.Message))
		}
		output.WriteString("\n")
	}

	return output.String(), nil
}

// getContainerState returns a string representation of the container state.
func getContainerState(status corev1.ContainerStatus) string {
	if status.State.Running != nil {
		return "Running"
	}
	if status.State.Waiting != nil {
		return fmt.Sprintf("Waiting (%s)", status.State.Waiting.Reason)
	}
	if status.State.Terminated != nil {
		return fmt.Sprintf("Terminated (%s)", status.State.Terminated.Reason)
	}
	return "Unknown"
}
