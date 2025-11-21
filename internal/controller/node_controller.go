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

// Package controller contains Kubernetes controller implementations.
//
// Coverage: NodeReconciler scaffolding is excluded from unit test coverage.
// The Reconcile() and SetupWithManager() functions are kubebuilder-generated
// stubs that will be tested through integration tests once implemented.

package controller

import (
	"context"

	"github.com/nextdoor/lumina/internal/cache"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// NodeReconciler reconciles a Node object, maintaining a cache that maps EC2 instance IDs
// to Kubernetes node names. This enables cost metrics to include node-level information.
type NodeReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// NodeCache stores the EC2 instance ID → K8s node name mappings
	NodeCache *cache.NodeCache
}

// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=nodes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core,resources=nodes/finalizers,verbs=update

// Reconcile handles Node add/update/delete events, maintaining the NodeCache
// with EC2 instance ID → node name mappings. This enables cost metrics to include
// Kubernetes node information.
//
// The reconciler:
// 1. Fetches the node from the API server
// 2. If node exists: Upserts it into the cache (extracting EC2 instance ID from providerID)
// 3. If node deleted: Removes it from the cache
// 4. Logs correlation success/failure for observability
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.22.4/pkg/reconcile
func (r *NodeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Fetch the node from the API server
	var node corev1.Node
	if err := r.Get(ctx, req.NamespacedName, &node); err != nil {
		if errors.IsNotFound(err) {
			// Node was deleted - remove from cache
			log.V(1).Info("node deleted, removing from cache", "node", req.Name)
			r.NodeCache.DeleteNode(req.Name)
			return ctrl.Result{}, nil
		}

		// Error reading node - requeue
		log.Error(err, "failed to get node")
		return ctrl.Result{}, err
	}

	// Node exists - upsert into cache
	instanceID, err := r.NodeCache.UpsertNode(&node)
	if err != nil {
		// Failed to parse providerID - log warning but don't requeue
		// This is expected for non-AWS nodes (e.g., kind, minikube, GCP, Azure)
		log.V(1).Info("failed to correlate node to EC2 instance",
			"node", node.Name,
			"error", err.Error())
		return ctrl.Result{}, nil
	}

	// Successfully correlated node to EC2 instance
	log.V(1).Info("correlated node to EC2 instance",
		"node", node.Name,
		"instance_id", instanceID,
		"provider_id", node.Spec.ProviderID)

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
// coverage:ignore - controller-runtime boilerplate, tested via E2E
func (r *NodeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Node{}).
		Named("node").
		Complete(r)
}
