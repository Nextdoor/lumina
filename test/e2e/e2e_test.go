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
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/nextdoor/lumina/test/utils"
)

// namespace where the project is deployed in
const namespace = "lumina-system"

// serviceAccountName created for the project
const serviceAccountName = "lumina-controller-manager"

// metricsServiceName is the name of the metrics service of the project
const metricsServiceName = "lumina-controller-manager-metrics-service"

// metricsRoleBindingName is the name of the RBAC that will be created to allow get the metrics data
const metricsRoleBindingName = "lumina-metrics-binding"

// controllerPodName is shared across all test suites
var controllerPodName string

var _ = Describe("Manager", Ordered, func() {

	// Before running the tests, set up the environment by creating the namespace,
	// enforce the restricted security policy to the namespace, installing CRDs,
	// and deploying the controller.
	BeforeAll(func() {
		By("creating manager namespace")
		cmd := exec.Command("kubectl", "create", "ns", namespace)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create namespace")

		By("labeling the namespace to enforce the restricted security policy")
		cmd = exec.Command("kubectl", "label", "--overwrite", "ns", namespace,
			"pod-security.kubernetes.io/enforce=restricted")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to label namespace with restricted policy")

		By("installing CRDs")
		cmd = exec.Command("make", "install")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to install CRDs")

		By("deploying the controller-manager with e2e configuration")
		// Use kubectl apply with e2e kustomization that includes LocalStack config
		cmd = exec.Command("kubectl", "apply", "-k", "config/e2e")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to deploy the controller-manager with e2e config")

		// Update the image in the deployment to use our test image
		cmd = exec.Command("kubectl", "set", "image",
			"deployment/lumina-controller-manager",
			fmt.Sprintf("manager=%s", projectImage),
			"-n", namespace)
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to set controller-manager image")
	})

	// After all tests have been executed, clean up by undeploying the controller, uninstalling CRDs,
	// and deleting the namespace.
	AfterAll(func() {
		By("cleaning up the curl pod for metrics")
		cmd := exec.Command("kubectl", "delete", "pod", "curl-metrics", "-n", namespace)
		_, _ = utils.Run(cmd)

		By("undeploying the controller-manager")
		cmd = exec.Command("make", "undeploy")
		_, _ = utils.Run(cmd)

		By("uninstalling CRDs")
		cmd = exec.Command("make", "uninstall")
		_, _ = utils.Run(cmd)

		By("removing manager namespace")
		cmd = exec.Command("kubectl", "delete", "ns", namespace)
		_, _ = utils.Run(cmd)
	})

	// After each test, check for failures and collect logs, events,
	// and pod descriptions for debugging.
	AfterEach(func() {
		specReport := CurrentSpecReport()
		if specReport.Failed() {
			By("Fetching controller manager pod logs")
			cmd := exec.Command("kubectl", "logs", controllerPodName, "-n", namespace)
			controllerLogs, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Controller logs:\n %s", controllerLogs)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get Controller logs: %s", err)
			}

			By("Fetching Kubernetes events")
			cmd = exec.Command("kubectl", "get", "events", "-n", namespace, "--sort-by=.lastTimestamp")
			eventsOutput, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Kubernetes events:\n%s", eventsOutput)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get Kubernetes events: %s", err)
			}

			By("Fetching curl-metrics logs")
			cmd = exec.Command("kubectl", "logs", "curl-metrics", "-n", namespace)
			metricsOutput, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Metrics logs:\n %s", metricsOutput)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get curl-metrics logs: %s", err)
			}

			By("Fetching controller manager pod description")
			cmd = exec.Command("kubectl", "describe", "pod", controllerPodName, "-n", namespace)
			podDescription, err := utils.Run(cmd)
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
				cmd := exec.Command("kubectl", "get",
					"pods", "-l", "control-plane=controller-manager",
					"-o", "go-template={{ range .items }}"+
						"{{ if not .metadata.deletionTimestamp }}"+
						"{{ .metadata.name }}"+
						"{{ \"\\n\" }}{{ end }}{{ end }}",
					"-n", namespace,
				)

				podOutput, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve controller-manager pod information")
				podNames := utils.GetNonEmptyLines(podOutput)
				g.Expect(podNames).To(HaveLen(1), "expected 1 controller pod running")
				controllerPodName = podNames[0]
				g.Expect(controllerPodName).To(ContainSubstring("controller-manager"))

				// Validate the pod's status
				cmd = exec.Command("kubectl", "get",
					"pods", controllerPodName, "-o", "jsonpath={.status.phase}",
					"-n", namespace,
				)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Running"), "Incorrect controller-manager pod status")
			}
			Eventually(verifyControllerUp).Should(Succeed())
		})

		It("should ensure the metrics endpoint is serving metrics", func() {
			By("creating a ClusterRoleBinding for the service account to allow access to metrics")
			cmd := exec.Command("kubectl", "create", "clusterrolebinding", metricsRoleBindingName,
				"--clusterrole=lumina-metrics-reader",
				fmt.Sprintf("--serviceaccount=%s:%s", namespace, serviceAccountName),
			)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create ClusterRoleBinding")

			By("validating that the metrics service is available")
			cmd = exec.Command("kubectl", "get", "service", metricsServiceName, "-n", namespace)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Metrics service should exist")

			By("getting the service account token")
			token, err := serviceAccountToken()
			Expect(err).NotTo(HaveOccurred())
			Expect(token).NotTo(BeEmpty())

			By("waiting for the metrics endpoint to be ready")
			verifyMetricsEndpointReady := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "endpoints", metricsServiceName, "-n", namespace)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("8443"), "Metrics endpoint is not ready")
			}
			Eventually(verifyMetricsEndpointReady).Should(Succeed())

			By("verifying that the controller manager is serving the metrics server")
			verifyMetricsServerStarted := func(g Gomega) {
				cmd := exec.Command("kubectl", "logs", controllerPodName, "-n", namespace)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("controller-runtime.metrics\tServing metrics server"),
					"Metrics server not yet started")
			}
			Eventually(verifyMetricsServerStarted).Should(Succeed())

			By("creating the curl-metrics pod to access the metrics endpoint")
			cmd = exec.Command("kubectl", "run", "curl-metrics", "--restart=Never",
				"--namespace", namespace,
				"--image=curlimages/curl:latest",
				"--overrides",
				fmt.Sprintf(`{
					"spec": {
						"containers": [{
							"name": "curl",
							"image": "curlimages/curl:latest",
							"command": ["/bin/sh", "-c"],
							"args": ["curl -v -k -H 'Authorization: Bearer %s' https://%s.%s.svc.cluster.local:8443/metrics"],
							"securityContext": {
								"readOnlyRootFilesystem": true,
								"allowPrivilegeEscalation": false,
								"capabilities": {
									"drop": ["ALL"]
								},
								"runAsNonRoot": true,
								"runAsUser": 1000,
								"seccompProfile": {
									"type": "RuntimeDefault"
								}
							}
						}],
						"serviceAccountName": "%s"
					}
				}`, token, metricsServiceName, namespace, serviceAccountName))
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create curl-metrics pod")

			By("waiting for the curl-metrics pod to complete.")
			verifyCurlUp := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pods", "curl-metrics",
					"-o", "jsonpath={.status.phase}",
					"-n", namespace)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Succeeded"), "curl pod in wrong status")
			}
			Eventually(verifyCurlUp, 5*time.Minute).Should(Succeed())

			By("getting the metrics by checking curl-metrics logs")
			verifyMetricsAvailable := func(g Gomega) {
				metricsOutput, err := getMetricsOutput()
				g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve logs from curl pod")
				g.Expect(metricsOutput).NotTo(BeEmpty())
				g.Expect(metricsOutput).To(ContainSubstring("< HTTP/1.1 200 OK"))
			}
			Eventually(verifyMetricsAvailable, 2*time.Minute).Should(Succeed())
		})

		It("should have AWS configuration for LocalStack", func() {
			By("checking controller environment variables")
			cmd := exec.Command("kubectl", "get", "deployment",
				"lumina-controller-manager", "-n", namespace,
				"-o", "jsonpath={.spec.template.spec.containers[0].env[*].name}")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to get controller env vars")
			Expect(output).To(ContainSubstring("AWS_ENDPOINT_URL"), "AWS_ENDPOINT_URL not configured")
			Expect(output).To(ContainSubstring("AWS_ACCESS_KEY_ID"), "AWS_ACCESS_KEY_ID not configured")

			By("verifying AWS_ENDPOINT_URL points to LocalStack")
			cmd = exec.Command("kubectl", "get", "deployment",
				"lumina-controller-manager", "-n", namespace,
				"-o", "jsonpath={.spec.template.spec.containers[0].env[?(@.name=='AWS_ENDPOINT_URL')].value}")
			output, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to get AWS_ENDPOINT_URL value")
			Expect(output).To(ContainSubstring("localstack"), "AWS_ENDPOINT_URL does not point to LocalStack")
		})

		It("should successfully load config from ConfigMap", func() {
			By("checking controller logs for config loading")
			verifyConfigLoaded := func(g Gomega) {
				cmd := exec.Command("kubectl", "logs", controllerPodName, "-n", namespace)
				output, err := utils.Run(cmd)
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
					cmd := exec.Command("kubectl", "get",
						"pods", "-l", "control-plane=controller-manager",
						"-o", "go-template={{ range .items }}"+
							"{{ if not .metadata.deletionTimestamp }}"+
							"{{ .metadata.name }}"+
							"{{ \"\\n\" }}{{ end }}{{ end }}",
						"-n", namespace,
					)
					podOutput, err := utils.Run(cmd)
					g.Expect(err).NotTo(HaveOccurred())
					podNames := utils.GetNonEmptyLines(podOutput)
					g.Expect(podNames).To(HaveLen(1), "expected 1 controller pod running")
					controllerPodName = podNames[0]
				}, 2*time.Minute, 2*time.Second).Should(Succeed())
			}
			Expect(controllerPodName).NotTo(BeEmpty(), "Controller pod name must be set")
		})

		It("should have liveness probe passing (pod not restarting)", func() {
			By("verifying the pod has low restart count")
			// Since the controller uses a distroless image without curl/wget,
			// we verify the liveness probe is working by checking the pod hasn't restarted.
			// If the liveness probe was failing, Kubernetes would restart the pod.
			cmd := exec.Command("kubectl", "get", "pod", controllerPodName,
				"-n", namespace,
				"-o", "jsonpath={.status.containerStatuses[0].restartCount}")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(Or(Equal("0"), Equal("1")),
				"Restart count should be low when liveness probe is healthy")
		})

		It("should have liveness probe configured in pod", func() {
			By("verifying the liveness probe is configured")
			cmd := exec.Command("kubectl", "get", "pod", controllerPodName,
				"-n", namespace,
				"-o", "jsonpath={.spec.containers[0].livenessProbe.httpGet.path}")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(Equal("/healthz"), "Liveness probe should be configured for /healthz")
		})

		It("should have readiness probe passing (pod marked Ready)", func() {
			By("verifying the pod is marked as Ready")
			// Since the controller uses a distroless image without curl/wget,
			// we verify the readiness probe is working by checking the pod is Ready.
			// If the readiness probe was failing, the pod would not be marked Ready.
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pod", controllerPodName,
					"-n", namespace,
					"-o", "jsonpath={.status.conditions[?(@.type=='Ready')].status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("True"), "Pod should be marked as Ready")
			}, 2*time.Minute, 2*time.Second).Should(Succeed())
		})

		It("should have readiness probe configured in pod", func() {
			By("verifying the readiness probe is configured")
			cmd := exec.Command("kubectl", "get", "pod", controllerPodName,
				"-n", namespace,
				"-o", "jsonpath={.spec.containers[0].readinessProbe.httpGet.path}")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(Equal("/readyz"), "Readiness probe should be configured for /readyz")
		})

		It("should validate AWS account access in readiness probe", func() {
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
	})
})

// serviceAccountToken returns a token for the specified service account in the given namespace.
// It uses the Kubernetes TokenRequest API to generate a token by directly sending a request
// and parsing the resulting token from the API response.
func serviceAccountToken() (string, error) {
	const tokenRequestRawString = `{
		"apiVersion": "authentication.k8s.io/v1",
		"kind": "TokenRequest"
	}`

	// Temporary file to store the token request
	secretName := fmt.Sprintf("%s-token-request", serviceAccountName)
	tokenRequestFile := filepath.Join("/tmp", secretName)
	err := os.WriteFile(tokenRequestFile, []byte(tokenRequestRawString), os.FileMode(0o644))
	if err != nil {
		return "", err
	}

	var out string
	verifyTokenCreation := func(g Gomega) {
		// Execute kubectl command to create the token
		cmd := exec.Command("kubectl", "create", "--raw", fmt.Sprintf(
			"/api/v1/namespaces/%s/serviceaccounts/%s/token",
			namespace,
			serviceAccountName,
		), "-f", tokenRequestFile)

		output, err := cmd.CombinedOutput()
		g.Expect(err).NotTo(HaveOccurred())

		// Parse the JSON output to extract the token
		var token tokenRequest
		err = json.Unmarshal(output, &token)
		g.Expect(err).NotTo(HaveOccurred())

		out = token.Status.Token
	}
	Eventually(verifyTokenCreation).Should(Succeed())

	return out, err
}

// getMetricsOutput retrieves and returns the logs from the curl pod used to access the metrics endpoint.
func getMetricsOutput() (string, error) {
	By("getting the curl-metrics logs")
	cmd := exec.Command("kubectl", "logs", "curl-metrics", "-n", namespace)
	return utils.Run(cmd)
}

// tokenRequest is a simplified representation of the Kubernetes TokenRequest API response,
// containing only the token field that we need to extract.
type tokenRequest struct {
	Status struct {
		Token string `json:"token"`
	} `json:"status"`
}
