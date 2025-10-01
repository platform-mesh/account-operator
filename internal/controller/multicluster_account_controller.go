/*
Copyright 2025.

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

package controller

import (
	"context"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/controllerruntime"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/subroutine"
	"github.com/platform-mesh/golang-commons/logger"
	ctrl "sigs.k8s.io/controller-runtime"

	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	corev1alpha1 "github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/account-operator/internal/config"
	"github.com/platform-mesh/account-operator/pkg/subroutines"
)

// MultiClusterAccountReconciler reconciles Account objects across multiple clusters
type MultiClusterAccountReconciler struct {
	log         *logger.Logger
	mgr         mcmanager.Manager
	cfg         config.OperatorConfig
	fgaClient   openfgav1.OpenFGAServiceClient
	subroutines []subroutine.Subroutine
}

func NewMultiClusterAccountReconciler(log *logger.Logger, mgr mcmanager.Manager, cfg config.OperatorConfig, fgaClient openfgav1.OpenFGAServiceClient) *MultiClusterAccountReconciler {
	var subs []subroutine.Subroutine
	if cfg.Subroutines.WorkspaceType.Enabled {
		// Note: For multi-cluster, we need to update subroutines to accept cluster-specific clients
		// For now, use the local manager for backward compatibility
		subs = append(subs, subroutines.NewWorkspaceTypeSubroutine(mgr.GetLocalManager()))
	}
	if cfg.Subroutines.Workspace.Enabled {
		subs = append(subs, subroutines.NewWorkspaceSubroutine(mgr.GetLocalManager()))
	}

	return &MultiClusterAccountReconciler{
		log:         log,
		mgr:         mgr,
		cfg:         cfg,
		fgaClient:   fgaClient,
		subroutines: subs,
	}
}

// SetupWithManager sets up the multi-cluster controller with the Manager
func (r *MultiClusterAccountReconciler) SetupWithManager(mgr mcmanager.Manager) error {
	return mcbuilder.ControllerManagedBy(mgr).
		Named("multicluster-account").
		For(&corev1alpha1.Account{}).
		Complete(mcreconcile.Func(r.Reconcile))
}

// Reconcile performs multi-cluster account reconciliation
func (r *MultiClusterAccountReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	log := r.log.ChildLogger("cluster", req.ClusterName).ChildLogger("account", req.NamespacedName.String())
	log.Info().Msg("Starting multi-cluster account reconciliation")

	// Get cluster-specific client
	cluster, err := r.mgr.GetCluster(ctx, req.ClusterName)
	if err != nil {
		log.Error().Err(err).Msg("Failed to get cluster")
		return ctrl.Result{}, err
	}

	client := cluster.GetClient()

	// Get the Account object from the specific cluster
	account := &corev1alpha1.Account{}
	if err := client.Get(ctx, req.Request.NamespacedName, account); err != nil {
		log.Error().Err(err).Msg("Failed to get Account object")
		return ctrl.Result{}, err
	}

	log.Info().
		Str("accountName", account.Name).
		Str("displayName", account.Spec.DisplayName).
		Str("type", string(account.Spec.Type)).
		Msg("Processing account in cluster")

	// Create a lifecycle manager for this specific cluster/account
	lifecycle := controllerruntime.NewLifecycleManager(
		log,
		operatorName,
		accountReconcilerName,
		client, // Use cluster-specific client
		r.subroutines,
	)

	// Execute lifecycle with cluster-specific context
	request := ctrl.Request{NamespacedName: req.Request.NamespacedName}
	return lifecycle.Reconcile(ctx, request, account)
}

// Example of how to use cluster-specific operations:
func (r *MultiClusterAccountReconciler) processAccountInCluster(ctx context.Context, clusterName string, account *corev1alpha1.Account) error {
	log := r.log.ChildLogger("cluster", clusterName).ChildLogger("account", account.Name)

	// Get cluster-specific client - this is the key pattern!
	cluster, err := r.mgr.GetCluster(ctx, clusterName)
	if err != nil {
		return err
	}
	client := cluster.GetClient()

	// Now use this client for all operations in this specific cluster
	log.Info().Msg("Processing account with cluster-specific client")

	// Example: Create workspace in specific cluster
	// workspace := &workspaceapi.Workspace{...}
	// if err := client.Create(ctx, workspace); err != nil {
	//     return err
	// }

	_ = client // Use client to avoid unused variable error
	return nil
}
