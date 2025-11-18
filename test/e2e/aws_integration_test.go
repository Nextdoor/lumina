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
	"os/exec"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/nextdoor/lumina/test/utils"
)

const (
	// LocalStack test account IDs (LocalStack uses 000000000000 as default)
	testAccountID        = "000000000000"
	testStagingAccountID = "111111111111"

	// Test IAM role ARNs (created by init script in LocalStack)
	testRoleARN        = "arn:aws:iam::000000000000:role/lumina/LuminaTestRole"
	testStagingRoleARN = "arn:aws:iam::111111111111:role/lumina/LuminaStagingRole"
)

var _ = Describe("LocalStack", Ordered, func() {
	It("should have LocalStack running and healthy", func() {
		By("checking LocalStack health endpoint")
		verifyLocalStackHealthy := func(g Gomega) {
			cmd := exec.Command("kubectl", "exec", "-n", "localstack",
				"deployment/localstack", "--",
				"curl", "-sf", "http://localhost:4566/_localstack/health")
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred(), "LocalStack health check failed")
			g.Expect(output).To(ContainSubstring(`"sts":`), "STS service not available in LocalStack")
			g.Expect(output).To(ContainSubstring(`"iam":`), "IAM service not available in LocalStack")
		}
		Eventually(verifyLocalStackHealthy, 20*time.Second, 2*time.Second).Should(Succeed())
	})

	It("should have IAM roles created", func() {
		By("listing IAM roles in LocalStack")
		cmd := exec.Command("kubectl", "exec", "-n", "localstack",
			"deployment/localstack", "--",
			"awslocal", "iam", "list-roles", "--path-prefix", "/lumina/")
		output, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to list IAM roles")
		Expect(output).To(ContainSubstring("LuminaTestRole"), "LuminaTestRole not found")
		Expect(output).To(ContainSubstring("LuminaStagingRole"), "LuminaStagingRole not found")
	})

	It("should allow STS AssumeRole operations", func() {
		By("attempting to assume the test role via STS")
		cmd := exec.Command("kubectl", "exec", "-n", "localstack",
			"deployment/localstack", "--",
			"awslocal", "sts", "assume-role",
			"--role-arn", testRoleARN,
			"--role-session-name", "e2e-test")
		output, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "AssumeRole operation failed")
		Expect(output).To(ContainSubstring("AccessKeyId"), "AssumeRole did not return credentials")
		Expect(output).To(ContainSubstring("SecretAccessKey"), "AssumeRole did not return secret key")
	})
})
