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
	"os"
	"os/exec"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/nextdoor/lumina/test/utils"
)

var (
	// Optional Environment Variables:
	// - CERT_MANAGER_INSTALL_SKIP=true: Skips CertManager installation during test setup.
	// - LOCALSTACK_INSTALL_SKIP=true: Skips LocalStack installation during test setup.
	// These variables are useful if dependencies are already installed, avoiding
	// re-installation and conflicts.
	skipCertManagerInstall = os.Getenv("CERT_MANAGER_INSTALL_SKIP") == "true"
	skipLocalStackInstall  = os.Getenv("LOCALSTACK_INSTALL_SKIP") == "true"

	// isCertManagerAlreadyInstalled will be set true when CertManager CRDs be found on the cluster
	isCertManagerAlreadyInstalled = false
	// isLocalStackAlreadyInstalled will be set true when LocalStack is found in the cluster
	isLocalStackAlreadyInstalled = false

	// projectImage is the name of the image which will be build and loaded
	// with the code source changes to be tested.
	projectImage = "example.com/lumina:v0.0.1"
)

// TestE2E runs the end-to-end (e2e) test suite for the project. These tests execute in an isolated,
// temporary environment to validate project changes with the purpose of being used in CI jobs.
// The default setup requires Kind, builds/loads the Manager Docker image locally, and installs
// CertManager.
func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	_, _ = fmt.Fprintf(GinkgoWriter, "Starting lumina integration test suite\n")
	RunSpecs(t, "e2e suite")
}

var _ = BeforeSuite(func() {
	// The tests-e2e are intended to run on a temporary cluster that is created and destroyed for testing.
	// To prevent errors when tests run in environments with dependencies already installed,
	// we check for their presence before execution.

	// Run Docker build, CertManager install, and LocalStack install in parallel for faster setup
	type setupResult struct {
		name string
		err  error
	}
	resultsChan := make(chan setupResult, 3)
	setupsNeeded := 0

	// Build and load Docker image in parallel
	By("starting parallel setup: docker build, CertManager, LocalStack")
	go func() {
		By("building the manager(Operator) image")
		// Use simple docker build for E2E tests to avoid slow multi-platform builds
		cmd := exec.Command("docker", "build", "-t", projectImage, ".")
		_, err := utils.Run(cmd)
		if err != nil {
			resultsChan <- setupResult{name: "Docker build", err: err}
			return
		}

		// TODO(user): If you want to change the e2e test vendor from Kind, ensure the image is
		// built and available before running the tests. Also, remove the following block.
		By("loading the manager(Operator) image on Kind")
		err = utils.LoadImageToKindClusterWithName(projectImage)
		resultsChan <- setupResult{name: "Docker build+load", err: err}
	}()
	setupsNeeded++

	// Setup CertManager in parallel if not skipped and if not already installed
	if !skipCertManagerInstall {
		By("checking if cert manager is installed already")
		isCertManagerAlreadyInstalled = utils.IsCertManagerCRDsInstalled()
		if !isCertManagerAlreadyInstalled {
			go func() {
				_, _ = fmt.Fprintf(GinkgoWriter, "Installing CertManager...\n")
				err := utils.InstallCertManager()
				resultsChan <- setupResult{name: "CertManager", err: err}
			}()
			setupsNeeded++
		} else {
			_, _ = fmt.Fprintf(GinkgoWriter, "WARNING: CertManager is already installed. Skipping installation...\n")
		}
	}

	// Setup LocalStack in parallel if not skipped and if not already installed
	if !skipLocalStackInstall {
		By("checking if LocalStack is installed already")
		isLocalStackAlreadyInstalled = utils.IsLocalStackInstalled()
		if !isLocalStackAlreadyInstalled {
			go func() {
				_, _ = fmt.Fprintf(GinkgoWriter, "Installing LocalStack...\n")
				err := utils.InstallLocalStack()
				resultsChan <- setupResult{name: "LocalStack", err: err}
			}()
			setupsNeeded++
		} else {
			_, _ = fmt.Fprintf(GinkgoWriter, "WARNING: LocalStack is already installed. Skipping installation...\n")
		}
	}

	// Wait for all parallel setups to complete
	By(fmt.Sprintf("waiting for %d parallel setup tasks to complete", setupsNeeded))
	for i := 0; i < setupsNeeded; i++ {
		result := <-resultsChan
		ExpectWithOffset(1, result.err).NotTo(HaveOccurred(), fmt.Sprintf("Failed: %s", result.name))
		_, _ = fmt.Fprintf(GinkgoWriter, "Completed: %s\n", result.name)
	}

	// Deploy the controller once for all tests to use
	// This runs after CertManager and LocalStack are installed
	By("creating manager namespace")
	cmd := exec.Command("kubectl", "create", "ns", namespace, "--dry-run=client", "-o", "yaml")
	yamlOutput, err := utils.Run(cmd)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to generate namespace YAML")

	cmd = exec.Command("kubectl", "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(yamlOutput)
	_, err = utils.Run(cmd)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to create namespace")

	By("labeling the namespace to enforce the restricted security policy")
	cmd = exec.Command("kubectl", "label", "--overwrite", "ns", namespace,
		"pod-security.kubernetes.io/enforce=restricted")
	_, err = utils.Run(cmd)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to label namespace with restricted policy")

	By("installing CRDs")
	cmd = exec.Command("make", "install")
	_, err = utils.Run(cmd)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to install CRDs")

	By("deploying the controller-manager with e2e configuration")
	// Use kubectl apply with e2e kustomization that includes LocalStack config
	cmd = exec.Command("kubectl", "apply", "-k", "config/e2e")
	_, err = utils.Run(cmd)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to deploy the controller-manager with e2e config")

	// Update the image in the deployment to use our test image
	cmd = exec.Command("kubectl", "set", "image",
		"deployment/lumina-controller-manager",
		fmt.Sprintf("manager=%s", projectImage),
		"-n", namespace)
	_, err = utils.Run(cmd)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to set controller-manager image")
})

var _ = AfterSuite(func() {
	// Teardown controller before tearing down dependencies
	By("undeploying the controller-manager")
	cmd := exec.Command("make", "undeploy")
	_, _ = utils.Run(cmd)

	By("uninstalling CRDs")
	cmd = exec.Command("make", "uninstall")
	_, _ = utils.Run(cmd)

	By("removing manager namespace")
	cmd = exec.Command("kubectl", "delete", "ns", namespace, "--ignore-not-found=true")
	_, _ = utils.Run(cmd)

	// Teardown LocalStack after the suite if not skipped and if it was not already installed
	if !skipLocalStackInstall && !isLocalStackAlreadyInstalled {
		_, _ = fmt.Fprintf(GinkgoWriter, "Uninstalling LocalStack...\n")
		utils.UninstallLocalStack()
	}

	// Teardown CertManager after the suite if not skipped and if it was not already installed
	if !skipCertManagerInstall && !isCertManagerAlreadyInstalled {
		_, _ = fmt.Fprintf(GinkgoWriter, "Uninstalling CertManager...\n")
		utils.UninstallCertManager()
	}
})
