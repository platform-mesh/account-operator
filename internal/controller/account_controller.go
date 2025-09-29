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
	"strings"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	platformmeshconfig "github.com/platform-mesh/golang-commons/config"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/controllerruntime"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/subroutine"
	"github.com/platform-mesh/golang-commons/logger"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	corev1alpha1 "github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/account-operator/internal/config"
	"github.com/platform-mesh/account-operator/pkg/subroutines"
)

var (
	operatorName          = "account-operator"
	accountReconcilerName = "AccountReconciler"
)

//+kubebuilder:rbac:groups=core.platform-mesh.io,resources=accounts,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core.platform-mesh.io,resources=accounts/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=core.platform-mesh.io,resources=accounts/finalizers,verbs=update
//+kubebuilder:rbac:groups=core.platform-mesh.io,resources=accountinfos,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core.platform-mesh.io,resources=accountinfos/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=tenancy.kcp.io,resources=workspaces,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=tenancy.kcp.io,resources=workspaces/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=tenancy.kcp.io,resources=workspacetypes,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=tenancy.kcp.io,resources=workspacetypes/status,verbs=get;update;patch

// AccountReconciler reconciles a Account object
type AccountReconciler struct {
	lifecycle *controllerruntime.LifecycleManager
}

func NewAccountReconciler(log *logger.Logger, mgr ctrl.Manager, cfg config.OperatorConfig, fgaClient openfgav1.OpenFGAServiceClient) *AccountReconciler {
	var subs []subroutine.Subroutine
	if cfg.Subroutines.WorkspaceType.Enabled {
		var rootClient client.Client
		rootHost := cfg.Kcp.RootHost

		if rootHost == "" {
			h := mgr.GetConfig().Host
			base := h
			// If we're pointing at a virtual workspace, strip it to get the server base
			if idx := strings.Index(base, "/services/"); idx != -1 {
				base = base[:idx]
			}
			// If we're pointing at a specific cluster, strip it to get the server base
			if idx := strings.Index(base, "/clusters/"); idx != -1 {
				base = base[:idx]
			}
			rootHost = strings.TrimRight(base, "/") + "/clusters/root"
		}

		rootCfg := rest.CopyConfig(mgr.GetConfig())
		rootCfg.Host = rootHost
		if c, err := client.New(rootCfg, client.Options{Scheme: mgr.GetScheme()}); err == nil {
			rootClient = c
		} else {
			log.Warn().Err(err).Str("host", rootHost).Msg("failed to create derived root-scoped client; falling back to shared client")
		}

		if rootClient != nil {
			subs = append(subs, subroutines.NewWorkspaceTypeSubroutineWithRootClient(mgr.GetClient(), rootClient))
		} else {
			subs = append(subs, subroutines.NewWorkspaceTypeSubroutine(mgr.GetClient()))
		}
	}
	if cfg.Subroutines.Workspace.Enabled {
		subs = append(subs, subroutines.NewWorkspaceSubroutine(mgr.GetClient()))
	}
	if cfg.Subroutines.AccountInfo.Enabled {
		subs = append(subs, subroutines.NewAccountInfoSubroutine(mgr.GetClient(), string(mgr.GetConfig().CAData)))
	}
	if cfg.Subroutines.FGA.Enabled {
		subs = append(subs, subroutines.NewFGASubroutine(mgr.GetClient(), fgaClient, cfg.Subroutines.FGA.CreatorRelation, cfg.Subroutines.FGA.ParentRelation, cfg.Subroutines.FGA.ObjectType))
	}
	return &AccountReconciler{
		lifecycle: controllerruntime.NewLifecycleManager(log, operatorName, accountReconcilerName, mgr.GetClient(), subs).WithConditionManagement(),
	}
}

func (r *AccountReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return r.lifecycle.Reconcile(ctx, req, &corev1alpha1.Account{})
}

func (r *AccountReconciler) SetupWithManager(mgr ctrl.Manager, cfg *platformmeshconfig.CommonServiceConfig, log *logger.Logger, eventPredicates ...predicate.Predicate) error {
	builder, err := r.lifecycle.SetupWithManagerBuilder(mgr, cfg.MaxConcurrentReconciles, accountReconcilerName, &corev1alpha1.Account{}, cfg.DebugLabelValue, log, eventPredicates...)
	if err != nil {
		return err
	}
	return builder.Complete(r)
}
