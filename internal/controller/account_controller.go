package controller

import (
	"context"

	platformmeshconfig "github.com/platform-mesh/golang-commons/config"
	"github.com/platform-mesh/golang-commons/logger"
	"github.com/platform-mesh/subroutines"
	"github.com/platform-mesh/subroutines/conditions"
	"github.com/platform-mesh/subroutines/lifecycle"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/account-operator/internal/config"
	"github.com/platform-mesh/account-operator/pkg/subroutines/manageaccountinfo"
	"github.com/platform-mesh/account-operator/pkg/subroutines/workspace"
	"github.com/platform-mesh/account-operator/pkg/subroutines/workspaceready"
	"github.com/platform-mesh/account-operator/pkg/subroutines/workspacetype"
)

const accountReconcilerName = "AccountReconciler"

// AccountReconciler orchestrates Account resources across logical clusters.
type AccountReconciler struct {
	cfg       config.OperatorConfig
	lifecycle *lifecycle.Lifecycle
}

func NewAccountReconciler(_ *logger.Logger, mgr mcmanager.Manager, cfg config.OperatorConfig, orgsClient client.Client) *AccountReconciler { // coverage-ignore
	localMgr := mgr.GetLocalManager()
	localCfg := rest.CopyConfig(localMgr.GetConfig())
	serverCA := string(localCfg.CAData)

	subs := []subroutines.Subroutine{}

	if cfg.Subroutines.WorkspaceType.Enabled {
		subs = append(subs, workspacetype.New(orgsClient))
	}

	if cfg.Subroutines.Workspace.Enabled {
		subs = append(subs, workspace.New(mgr, orgsClient))
	}

	if cfg.Subroutines.AccountInfo.Enabled {
		subs = append(subs, manageaccountinfo.New(mgr, serverCA))
	}

	if cfg.Subroutines.WorkspaceReady.Enabled {
		subs = append(subs, workspaceready.New(mgr))
	}

	lc := lifecycle.New(mgr, accountReconcilerName, func() client.Object { return &v1alpha1.Account{} }, subs...).
		WithConditions(conditions.NewManager())

	return &AccountReconciler{
		cfg:       cfg,
		lifecycle: lc,
	}
}

func (r *AccountReconciler) SetupWithManager(mgr mcmanager.Manager, _ *platformmeshconfig.CommonServiceConfig, _ *logger.Logger, eventPredicates ...predicate.Predicate) error { // coverage-ignore
	builder := mcbuilder.ControllerManagedBy(mgr).
		Named(accountReconcilerName).
		For(&v1alpha1.Account{})

	for _, p := range eventPredicates {
		builder = builder.WithEventFilter(p)
	}

	return builder.Complete(r)
}

func (r *AccountReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) { // coverage-ignore
	return r.lifecycle.Reconcile(mccontext.WithCluster(ctx, req.ClusterName), ctrl.Request{NamespacedName: req.NamespacedName})
}
