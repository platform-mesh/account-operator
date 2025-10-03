/*
Copyright 2024.

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
	"errors"
	"sync"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	platformmeshconfig "github.com/platform-mesh/golang-commons/config"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/controllerruntime"
	lifecyclesubroutine "github.com/platform-mesh/golang-commons/controller/lifecycle/subroutine"
	"github.com/platform-mesh/golang-commons/logger"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
	mcontroller "sigs.k8s.io/multicluster-runtime/pkg/controller"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	corev1alpha1 "github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/account-operator/internal/config"
	"github.com/platform-mesh/account-operator/pkg/subroutines"
)

const (
	operatorName          = "account-operator"
	accountReconcilerName = "AccountReconciler"
)

// AccountReconciler orchestrates Account resources across logical clusters.
type AccountReconciler struct {
	log       *logger.Logger
	cfg       config.OperatorConfig
	mcMgr     mcmanager.Manager
	fgaClient openfgav1.OpenFGAServiceClient

	baseConfig *rest.Config
	scheme     *runtime.Scheme
	serverCA   string

	lifecycles sync.Map // map[string]*controllerruntime.LifecycleManager
}

func NewAccountReconciler(log *logger.Logger, mgr mcmanager.Manager, cfg config.OperatorConfig, fgaClient openfgav1.OpenFGAServiceClient) *AccountReconciler {
	localMgr := mgr.GetLocalManager()
	localCfg := rest.CopyConfig(localMgr.GetConfig())

	return &AccountReconciler{
		log:        log,
		cfg:        cfg,
		mcMgr:      mgr,
		fgaClient:  fgaClient,
		baseConfig: localCfg,
		scheme:     localMgr.GetScheme(),
		serverCA:   string(localCfg.CAData),
	}
}

func (r *AccountReconciler) SetupWithManager(mgr mcmanager.Manager, cfg *platformmeshconfig.CommonServiceConfig, _ *logger.Logger, eventPredicates ...predicate.Predicate) error {
	builder := mcbuilder.ControllerManagedBy(mgr).
		Named(accountReconcilerName).
		For(&corev1alpha1.Account{})

	if len(eventPredicates) > 0 {
		combined := eventPredicates[0]
		for i := 1; i < len(eventPredicates); i++ {
			combined = predicate.And(combined, eventPredicates[i])
		}
		builder = builder.WithEventFilter(combined)
	}

	options := mcontroller.Options{}
	if cfg != nil && cfg.MaxConcurrentReconciles > 0 {
		options.MaxConcurrentReconciles = cfg.MaxConcurrentReconciles
	}
	builder = builder.WithOptions(options)

	return builder.Complete(r)
}

func (r *AccountReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	reqLogger := r.log.ChildLogger("cluster", req.ClusterName).ChildLogger("account", req.NamespacedName.String())
	ctx = logger.SetLoggerInContext(ctx, reqLogger)

	lifecycle, err := r.lifecycleForCluster(ctx, req.ClusterName)
	if errors.Is(err, multicluster.ErrClusterNotFound) {
		reqLogger.Warn().Msg("cluster not found; skipping request")
		return ctrl.Result{}, nil
	}
	if err != nil {
		reqLogger.Error().Err(err).Msg("failed to prepare cluster lifecycle")
		return ctrl.Result{}, err
	}

	return lifecycle.Reconcile(mccontext.WithCluster(ctx, req.ClusterName), req.Request, &corev1alpha1.Account{})
}

func (r *AccountReconciler) lifecycleForCluster(ctx context.Context, clusterName string) (*controllerruntime.LifecycleManager, error) {
	if existing, ok := r.lifecycles.Load(clusterName); ok {
		return existing.(*controllerruntime.LifecycleManager), nil
	}

	cluster, err := r.mcMgr.GetCluster(ctx, clusterName)
	if err != nil {
		return nil, err
	}

	clusterLogger := r.log.ChildLogger("cluster", clusterName)
	subs := r.buildSubroutines(cluster.GetClient())

	lifecycle := controllerruntime.NewLifecycleManager(clusterLogger, operatorName, accountReconcilerName, cluster.GetClient(), subs).WithConditionManagement()

	actual, _ := r.lifecycles.LoadOrStore(clusterName, lifecycle)
	return actual.(*controllerruntime.LifecycleManager), nil
}

func (r *AccountReconciler) buildSubroutines(clusterClient client.Client) []lifecyclesubroutine.Subroutine {
	subs := make([]lifecyclesubroutine.Subroutine, 0, 4)

	if r.cfg.Subroutines.WorkspaceType.Enabled {
		subs = append(subs, subroutines.NewWorkspaceTypeSubroutine(r.baseConfig, r.scheme))
	}

	if r.cfg.Subroutines.Workspace.Enabled {
		subs = append(subs, subroutines.NewWorkspaceSubroutine(clusterClient, r.baseConfig, r.scheme))
	}

	if r.cfg.Subroutines.AccountInfo.Enabled {
		subs = append(subs, subroutines.NewAccountInfoSubroutine(clusterClient, r.serverCA))
	}

	if r.cfg.Subroutines.FGA.Enabled {
		subs = append(subs, subroutines.NewFGASubroutine(clusterClient, r.fgaClient, r.cfg.Subroutines.FGA.CreatorRelation, r.cfg.Subroutines.FGA.ParentRelation, r.cfg.Subroutines.FGA.ObjectType))
	}

	return subs
}
