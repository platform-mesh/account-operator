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

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	platformmeshconfig "github.com/platform-mesh/golang-commons/config"
	mclifecycle "github.com/platform-mesh/golang-commons/controller/lifecycle/multicluster"
	lifecyclesubroutine "github.com/platform-mesh/golang-commons/controller/lifecycle/subroutine"
	"github.com/platform-mesh/golang-commons/logger"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
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
	log         *logger.Logger
	cfg         config.OperatorConfig
	mcMgr       mcmanager.Manager
	fgaClient   openfgav1.OpenFGAServiceClient
	baseConfig  *rest.Config
	serverCA    string
	subroutines []lifecyclesubroutine.Subroutine
	lifecycle   *mclifecycle.LifecycleManager
}

func NewAccountReconciler(log *logger.Logger, mgr mcmanager.Manager, cfg config.OperatorConfig, fgaClient openfgav1.OpenFGAServiceClient) *AccountReconciler {
	localMgr := mgr.GetLocalManager()
	localCfg := rest.CopyConfig(localMgr.GetConfig())
	localClient := localMgr.GetClient()
	serverCA := string(localCfg.CAData)

	subs := buildAccountSubroutines(cfg, mgr, localClient, localCfg, serverCA, fgaClient)
	lc := mclifecycle.NewLifecycleManager(subs, operatorName, accountReconcilerName, mgr, log)
	lc.WithConditionManagement()

	return &AccountReconciler{
		log:         log,
		cfg:         cfg,
		mcMgr:       mgr,
		fgaClient:   fgaClient,
		baseConfig:  localCfg,
		serverCA:    serverCA,
		subroutines: subs,
		lifecycle:   lc,
	}
}

func (r *AccountReconciler) SetupWithManager(mgr mcmanager.Manager, cfg *platformmeshconfig.CommonServiceConfig, _ *logger.Logger, eventPredicates ...predicate.Predicate) error {
	max := 0
	debugLabel := ""
	if cfg != nil {
		max = cfg.MaxConcurrentReconciles
		debugLabel = cfg.DebugLabelValue
	}
	return r.lifecycle.SetupWithManager(mgr, max, accountReconcilerName, &corev1alpha1.Account{}, debugLabel, r, r.log, eventPredicates...)
}

func (r *AccountReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	ctx = logger.SetLoggerInContext(ctx, r.log.ChildLogger("cluster", req.ClusterName).ChildLogger("account", req.NamespacedName.String()))
	return r.lifecycle.Reconcile(mccontext.WithCluster(ctx, req.ClusterName), req, &corev1alpha1.Account{})
}

func buildAccountSubroutines(
	cfg config.OperatorConfig,
	clusterGetter subroutines.ClusterClientGetter,
	localClient client.Client,
	baseConfig *rest.Config,
	serverCA string,
	fgaClient openfgav1.OpenFGAServiceClient,
) []lifecyclesubroutine.Subroutine {
	subs := make([]lifecyclesubroutine.Subroutine, 0, 4)

	if cfg.Subroutines.WorkspaceType.Enabled {
		subs = append(subs, subroutines.NewWorkspaceTypeSubroutine(baseConfig, localClient))
	}

	if cfg.Subroutines.Workspace.Enabled {
		subs = append(subs, subroutines.NewWorkspaceSubroutine(clusterGetter, localClient, baseConfig))
	}

	if cfg.Subroutines.AccountInfo.Enabled {
		subs = append(subs, subroutines.NewAccountInfoSubroutine(clusterGetter, localClient, serverCA))
	}

	if cfg.Subroutines.FGA.Enabled {
		subs = append(subs, subroutines.NewFGASubroutine(clusterGetter, localClient, fgaClient, cfg.Subroutines.FGA.CreatorRelation, cfg.Subroutines.FGA.ParentRelation, cfg.Subroutines.FGA.ObjectType))
	}

	return subs
}
