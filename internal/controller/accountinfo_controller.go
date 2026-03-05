package controller

import (
	"context"

	"github.com/platform-mesh/subroutines"
	"github.com/platform-mesh/subroutines/lifecycle"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	"github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/account-operator/internal/config"
	"github.com/platform-mesh/account-operator/pkg/subroutines/finalizeaccountinfo"
)

const accountInfoReconcilerName = "AccountInfoReconciler"

// AccountInfoReconciler orchestrates AccountInfo resources across logical clusters.
type AccountInfoReconciler struct {
	cfg       config.OperatorConfig
	lifecycle *lifecycle.Lifecycle
}

func NewAccountInfoReconciler(mgr mcmanager.Manager, cfg config.OperatorConfig) *AccountInfoReconciler { // coverage-ignore
	subs := []subroutines.Subroutine{}

	if cfg.Controllers.AccountInfo.Enabled {
		subs = append(subs, finalizeaccountinfo.New(mgr))
	}

	lc := lifecycle.New(mgr, accountInfoReconcilerName, func() client.Object { return &v1alpha1.AccountInfo{} }, subs...)

	return &AccountInfoReconciler{
		cfg:       cfg,
		lifecycle: lc,
	}
}

func (r *AccountInfoReconciler) SetupWithManager(mgr mcmanager.Manager, eventPredicates ...predicate.Predicate) error { // coverage-ignore
	builder := mcbuilder.ControllerManagedBy(mgr).
		Named(accountInfoReconcilerName).
		For(&v1alpha1.AccountInfo{})

	for _, p := range eventPredicates {
		builder = builder.WithEventFilter(p)
	}

	return builder.Complete(r)
}

func (r *AccountInfoReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (reconcile.Result, error) { // coverage-ignore
	return r.lifecycle.Reconcile(ctx, req)
}
