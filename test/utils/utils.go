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

// Package utils provides test utilities for E2E tests.
//
// Coverage: Excluded - these utilities are kubebuilder-generated scaffolding
// used only in E2E tests with Kind clusters. They are tested through actual
// E2E test execution, not unit tests.

package utils

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	. "github.com/onsi/ginkgo/v2" // nolint:revive,staticcheck
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/nextdoor/lumina/test/e2e/localstack/seed"
)

const (
	certmanagerVersion = "v1.19.1"
	certmanagerURLTmpl = "https://github.com/cert-manager/cert-manager/releases/download/%s/cert-manager.yaml"

	defaultKindBinary  = "kind"
	defaultKindCluster = "kind"
)

func warnError(err error) {
	_, _ = fmt.Fprintf(GinkgoWriter, "warning: %v\n", err)
}

// Run executes the provided command within this context
func Run(cmd *exec.Cmd) (string, error) {
	dir, _ := GetProjectDir()
	cmd.Dir = dir

	if err := os.Chdir(cmd.Dir); err != nil {
		_, _ = fmt.Fprintf(GinkgoWriter, "chdir dir: %q\n", err)
	}

	cmd.Env = append(os.Environ(), "GO111MODULE=on")
	command := strings.Join(cmd.Args, " ")
	_, _ = fmt.Fprintf(GinkgoWriter, "running: %q\n", command)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("%q failed with error %q: %w", command, string(output), err)
	}

	return string(output), nil
}

// UninstallCertManager uninstalls the cert manager
func UninstallCertManager() {
	url := fmt.Sprintf(certmanagerURLTmpl, certmanagerVersion)
	cmd := exec.Command("kubectl", "delete", "-f", url)
	if _, err := Run(cmd); err != nil {
		warnError(err)
	}

	// Delete leftover leases in kube-system (not cleaned by default)
	kubeSystemLeases := []string{
		"cert-manager-cainjector-leader-election",
		"cert-manager-controller",
	}
	for _, lease := range kubeSystemLeases {
		cmd = exec.Command("kubectl", "delete", "lease", lease,
			"-n", "kube-system", "--ignore-not-found", "--force", "--grace-period=0")
		if _, err := Run(cmd); err != nil {
			warnError(err)
		}
	}
}

// InstallCertManager installs the cert manager bundle.
func InstallCertManager() error {
	url := fmt.Sprintf(certmanagerURLTmpl, certmanagerVersion)
	cmd := exec.Command("kubectl", "apply", "-f", url)
	if _, err := Run(cmd); err != nil {
		return err
	}
	// Wait for cert-manager-webhook to be ready, which can take time if cert-manager
	// was re-installed after uninstalling on a cluster.
	cmd = exec.Command("kubectl", "wait", "deployment.apps/cert-manager-webhook",
		"--for", "condition=Available",
		"--namespace", "cert-manager",
		"--timeout", "5m",
	)

	_, err := Run(cmd)
	return err
}

// IsCertManagerCRDsInstalled checks if any Cert Manager CRDs are installed
// by verifying the existence of key CRDs related to Cert Manager.
func IsCertManagerCRDsInstalled() bool {
	// List of common Cert Manager CRDs
	certManagerCRDs := []string{
		"certificates.cert-manager.io",
		"issuers.cert-manager.io",
		"clusterissuers.cert-manager.io",
		"certificaterequests.cert-manager.io",
		"orders.acme.cert-manager.io",
		"challenges.acme.cert-manager.io",
	}

	// Execute the kubectl command to get all CRDs
	cmd := exec.Command("kubectl", "get", "crds")
	output, err := Run(cmd)
	if err != nil {
		return false
	}

	// Check if any of the Cert Manager CRDs are present
	crdList := GetNonEmptyLines(output)
	for _, crd := range certManagerCRDs {
		for _, line := range crdList {
			if strings.Contains(line, crd) {
				return true
			}
		}
	}

	return false
}

// LoadImageToKindClusterWithName loads a local docker image to the kind cluster
func LoadImageToKindClusterWithName(name string) error {
	cluster := defaultKindCluster
	if v, ok := os.LookupEnv("KIND_CLUSTER"); ok {
		cluster = v
	}
	kindOptions := []string{"load", "docker-image", name, "--name", cluster}
	kindBinary := defaultKindBinary
	if v, ok := os.LookupEnv("KIND"); ok {
		kindBinary = v
	}
	cmd := exec.Command(kindBinary, kindOptions...)
	_, err := Run(cmd)
	return err
}

// GetNonEmptyLines converts given command output string into individual objects
// according to line breakers, and ignores the empty elements in it.
func GetNonEmptyLines(output string) []string {
	var res []string
	elements := strings.Split(output, "\n")
	for _, element := range elements {
		if element != "" {
			res = append(res, element)
		}
	}

	return res
}

// GetProjectDir will return the directory where the project is
func GetProjectDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return wd, fmt.Errorf("failed to get current working directory: %w", err)
	}
	wd = strings.ReplaceAll(wd, "/test/e2e", "")
	return wd, nil
}

// InstallLocalStack installs LocalStack into the cluster using kubectl apply.
func InstallLocalStack() error {
	dir, err := GetProjectDir()
	if err != nil {
		return err
	}

	manifestPath := fmt.Sprintf("%s/test/e2e/localstack", dir)
	cmd := exec.Command("kubectl", "apply", "-k", manifestPath)
	if _, err := Run(cmd); err != nil {
		return fmt.Errorf("failed to apply LocalStack manifests: %w", err)
	}

	// Wait for LocalStack deployment to be ready
	cmd = exec.Command("kubectl", "wait", "deployment/localstack",
		"--for", "condition=Available",
		"--namespace", "localstack",
		"--timeout", "3m",
	)
	if _, err := Run(cmd); err != nil {
		return fmt.Errorf("failed waiting for LocalStack deployment: %w", err)
	}

	// Wait for LocalStack pod to be ready
	cmd = exec.Command("kubectl", "wait", "pod",
		"--selector=app=localstack",
		"--for", "condition=Ready",
		"--namespace", "localstack",
		"--timeout", "3m",
	)
	if _, err := Run(cmd); err != nil {
		return fmt.Errorf("failed waiting for LocalStack pod: %w", err)
	}

	return nil
}

// SeedLocalStack seeds test data into LocalStack using native AWS SDK calls through
// the Kubernetes API proxy. This function should be called after InstallLocalStack()
// and after LocalStack is ready.
//
// Uses the Kubernetes REST client to proxy AWS SDK HTTP requests to LocalStack's
// localhost:4566, following the pattern from metrics_helper.go.
func SeedLocalStack() error {
	// Load kubeconfig to get Kubernetes REST client
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)

	restConfig, err := kubeConfig.ClientConfig()
	if err != nil {
		return fmt.Errorf("failed to load kubeconfig: %w", err)
	}

	// Create an HTTP client that proxies through the Kubernetes API to LocalStack
	// Pattern: /api/v1/namespaces/localstack/services/localstack:4566/proxy/
	proxyHTTPClient := &kubeProxyRoundTripper{
		restConfig: restConfig,
		namespace:  "localstack",
		service:    "localstack",
		port:       "4566",
	}

	// Configure AWS SDK to use the Kubernetes proxy as the HTTP client
	// The kubeProxyRoundTripper handles all routing, so we just need a dummy base URL
	ctx := context.Background()
	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion("us-west-2"),
		awsconfig.WithHTTPClient(&http.Client{
			Transport: proxyHTTPClient,
		}),
		awsconfig.WithCredentialsProvider(aws.CredentialsProviderFunc(func(ctx context.Context) (aws.Credentials, error) {
			return aws.Credentials{
				AccessKeyID:     "test",
				SecretAccessKey: "test",
			}, nil
		})),
	)
	if err != nil {
		return fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Call the seed package which makes native AWS SDK calls
	if err := seed.SeedAll(ctx, cfg); err != nil {
		return fmt.Errorf("failed to seed LocalStack: %w", err)
	}

	return nil
}

// kubeProxyRoundTripper implements http.RoundTripper to proxy AWS SDK HTTP requests
// through the Kubernetes API server to LocalStack's localhost:4566.
type kubeProxyRoundTripper struct {
	restConfig *rest.Config
	namespace  string
	service    string
	port       string
}

// RoundTrip proxies the HTTP request through the Kubernetes API to LocalStack.
func (k *kubeProxyRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// Create a copy of the config and set required fields for the core v1 API
	// This matches the pattern from metrics_helper.go
	configCopy := *k.restConfig
	configCopy.APIPath = "/api"
	configCopy.GroupVersion = &schema.GroupVersion{Group: "", Version: "v1"}

	// Create a scheme and codec factory for the core API
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	configCopy.NegotiatedSerializer = serializer.NewCodecFactory(scheme)

	// Create REST client with the updated config
	restClient, err := rest.RESTClientFor(&configCopy)
	if err != nil {
		return nil, fmt.Errorf("failed to create REST client: %w", err)
	}

	// Build the proxy path through kube-apiserver to LocalStack
	// Pattern: /api/v1/namespaces/{ns}/services/{name}:{port}/proxy{path}
	proxyPath := fmt.Sprintf("/api/v1/namespaces/%s/services/%s:%s/proxy%s",
		k.namespace, k.service, k.port, req.URL.Path)

	// Forward the request through the proxy
	proxyReq := restClient.Verb(req.Method).
		AbsPath(proxyPath)

	// Copy headers from the original request
	for key, values := range req.Header {
		for _, value := range values {
			proxyReq = proxyReq.SetHeader(key, value)
		}
	}

	// Add query parameters
	for key, values := range req.URL.Query() {
		for _, value := range values {
			proxyReq = proxyReq.Param(key, value)
		}
	}

	// Set request body if present
	if req.Body != nil {
		bodyBytes, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read request body: %w", err)
		}
		_ = req.Body.Close() // Best effort close
		if len(bodyBytes) > 0 {
			proxyReq = proxyReq.Body(bodyBytes)
		}
	}

	// Execute the request
	result := proxyReq.Do(req.Context())

	// Get the response
	statusCode := 0
	result.StatusCode(&statusCode)

	body, err := result.Raw()
	if err != nil {
		return nil, err
	}

	// Build HTTP response
	resp := &http.Response{
		StatusCode: statusCode,
		Body:       io.NopCloser(bytes.NewReader(body)),
		Header:     make(http.Header),
		Request:    req,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
	}

	return resp, nil
}

// UninstallLocalStack uninstalls LocalStack from the cluster.
func UninstallLocalStack() {
	dir, err := GetProjectDir()
	if err != nil {
		warnError(err)
		return
	}

	manifestPath := fmt.Sprintf("%s/test/e2e/localstack", dir)
	cmd := exec.Command("kubectl", "delete", "-k", manifestPath, "--ignore-not-found")
	if _, err := Run(cmd); err != nil {
		warnError(err)
	}
}

// IsLocalStackInstalled checks if LocalStack is installed in the cluster.
func IsLocalStackInstalled() bool {
	cmd := exec.Command("kubectl", "get", "deployment", "localstack", "-n", "localstack")
	_, err := Run(cmd)
	return err == nil
}

// UncommentCode searches for target in the file and remove the comment prefix
// of the target content. The target content may span multiple lines.
func UncommentCode(filename, target, prefix string) error {
	// false positive
	// nolint:gosec
	content, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("failed to read file %q: %w", filename, err)
	}
	strContent := string(content)

	idx := strings.Index(strContent, target)
	if idx < 0 {
		return fmt.Errorf("unable to find the code %q to be uncomment", target)
	}

	out := new(bytes.Buffer)
	_, err = out.Write(content[:idx])
	if err != nil {
		return fmt.Errorf("failed to write to output: %w", err)
	}

	scanner := bufio.NewScanner(bytes.NewBufferString(target))
	if !scanner.Scan() {
		return nil
	}
	for {
		if _, err = out.WriteString(strings.TrimPrefix(scanner.Text(), prefix)); err != nil {
			return fmt.Errorf("failed to write to output: %w", err)
		}
		// Avoid writing a newline in case the previous line was the last in target.
		if !scanner.Scan() {
			break
		}
		if _, err = out.WriteString("\n"); err != nil {
			return fmt.Errorf("failed to write to output: %w", err)
		}
	}

	if _, err = out.Write(content[idx+len(target):]); err != nil {
		return fmt.Errorf("failed to write to output: %w", err)
	}

	// false positive
	// nolint:gosec
	if err = os.WriteFile(filename, out.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write file %q: %w", filename, err)
	}

	return nil
}
