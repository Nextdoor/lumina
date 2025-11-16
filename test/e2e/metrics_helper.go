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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
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

