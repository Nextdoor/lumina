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
	"bytes"
	"context"
	"fmt"
	"io"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// MetricsClient provides a clean interface to fetch metrics from the controller
// using the Kubernetes API proxy pattern instead of curl pods.
type MetricsClient struct {
	namespace   string
	serviceName string
	portName    string
	restConfig  *rest.Config
}

// NewMetricsClient creates a new metrics client.
func NewMetricsClient(namespace, serviceName, portName string) (*MetricsClient, error) {
	// Load the kubeconfig from the default location or KUBECONFIG env var
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)

	config, err := kubeConfig.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load kubeconfig: %w", err)
	}

	return &MetricsClient{
		namespace:   namespace,
		serviceName: serviceName,
		portName:    portName,
		restConfig:  config,
	}, nil
}

// GetMetrics fetches the metrics from the controller's metrics endpoint
// using the Kubernetes API proxy pattern.
//
// This uses the pattern:
// /api/v1/namespaces/{namespace}/services/{service}:{port}/proxy/metrics
func (m *MetricsClient) GetMetrics(ctx context.Context) (string, error) {
	// Create a copy of the config and set required fields for the core v1 API
	configCopy := *m.restConfig
	configCopy.APIPath = "/api"
	configCopy.GroupVersion = &schema.GroupVersion{Group: "", Version: "v1"}

	// Create a scheme and codec factory for the core API
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	configCopy.NegotiatedSerializer = serializer.NewCodecFactory(scheme)

	// Create REST client with the updated config
	restClient, err := rest.RESTClientFor(&configCopy)
	if err != nil {
		return "", fmt.Errorf("failed to create REST client: %w", err)
	}

	// Build the proxy path to /metrics
	// Pattern: /api/v1/namespaces/{ns}/services/{name}:{port}/proxy/metrics
	req := restClient.Get().
		AbsPath(
			"/api", "v1",
			"namespaces", m.namespace,
			"services", fmt.Sprintf("%s:%s", m.serviceName, m.portName),
			"proxy", "metrics",
		)

	// Execute the request
	result := req.Do(ctx)
	raw, err := result.Raw()
	if err != nil {
		return "", fmt.Errorf("failed to GET metrics via proxy: %w", err)
	}

	return string(raw), nil
}

// LogsClient provides a clean interface to fetch logs from pods
// using the native Kubernetes client instead of kubectl commands.
type LogsClient struct {
	namespace  string
	clientset  *kubernetes.Clientset
	restConfig *rest.Config
}

// NewLogsClient creates a new logs client.
func NewLogsClient(namespace string) (*LogsClient, error) {
	// Load the kubeconfig from the default location or KUBECONFIG env var
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)

	config, err := kubeConfig.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load kubeconfig: %w", err)
	}

	// Create the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create clientset: %w", err)
	}

	return &LogsClient{
		namespace:  namespace,
		clientset:  clientset,
		restConfig: config,
	}, nil
}

// GetPodLogs retrieves logs from a specific pod by name.
//
// Options:
// - tailLines: Number of lines from the end of the logs (nil = all logs)
// - container: Specific container name (empty = default container)
func (l *LogsClient) GetPodLogs(ctx context.Context, podName string, tailLines *int64, container string) (string, error) {
	opts := &corev1.PodLogOptions{
		TailLines: tailLines,
	}
	if container != "" {
		opts.Container = container
	}

	req := l.clientset.CoreV1().Pods(l.namespace).GetLogs(podName, opts)
	podLogs, err := req.Stream(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to stream logs: %w", err)
	}
	defer podLogs.Close()

	buf := new(bytes.Buffer)
	_, err = io.Copy(buf, podLogs)
	if err != nil {
		return "", fmt.Errorf("failed to copy logs: %w", err)
	}

	return buf.String(), nil
}

// GetPodLogsByLabel retrieves logs from pods matching a label selector.
// Returns logs from all matching pods concatenated together.
//
// Options:
// - labelSelector: Kubernetes label selector (e.g., "control-plane=controller-manager")
// - tailLines: Number of lines from the end of the logs (nil = all logs)
func (l *LogsClient) GetPodLogsByLabel(ctx context.Context, labelSelector string, tailLines *int64) (string, error) {
	// List pods matching the label selector
	podList, err := l.clientset.CoreV1().Pods(l.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return "", fmt.Errorf("failed to list pods: %w", err)
	}

	if len(podList.Items) == 0 {
		return "", fmt.Errorf("no pods found matching label selector: %s", labelSelector)
	}

	// Get logs from all matching pods
	var allLogs bytes.Buffer
	for i, pod := range podList.Items {
		if i > 0 {
			allLogs.WriteString("\n") // Separate logs from different pods
		}

		logs, err := l.GetPodLogs(ctx, pod.Name, tailLines, "")
		if err != nil {
			// Continue to next pod if one fails
			allLogs.WriteString(fmt.Sprintf("# Failed to get logs from pod %s: %v\n", pod.Name, err))
			continue
		}

		allLogs.WriteString(logs)
	}

	return allLogs.String(), nil
}

