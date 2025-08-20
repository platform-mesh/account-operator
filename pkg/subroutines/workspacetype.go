package subroutines

import (
	"context"
	"fmt"
	"time"

	kcptenancyv1alpha "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"
	"github.com/kcp-dev/logicalcluster/v3"
	corev1alpha1 "github.com/platform-mesh/account-operator/api/v1alpha1"
	commonconfig "github.com/platform-mesh/golang-commons/config"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/runtimeobject"
	"github.com/platform-mesh/golang-commons/errors"
	"github.com/platform-mesh/golang-commons/logger"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/kontext"

	operatorconfig "github.com/platform-mesh/account-operator/internal/config"
)

const WorkspaceTypeSubroutineName = "WorkspaceTypeSubroutine"

type WorkspaceTypeSubroutine struct {
	client     client.Client
	rootClient client.Client
	limiter    workqueue.TypedRateLimiter[ClusteredName]
}

func NewWorkspaceTypeSubroutine(c client.Client) *WorkspaceTypeSubroutine {
	exp := workqueue.NewTypedItemExponentialFailureRateLimiter[ClusteredName](1*time.Second, 30*time.Second)
	return &WorkspaceTypeSubroutine{client: c, rootClient: c, limiter: exp}
}

func NewWorkspaceTypeSubroutineWithRootClient(c client.Client, root client.Client) *WorkspaceTypeSubroutine {
	exp := workqueue.NewTypedItemExponentialFailureRateLimiter[ClusteredName](1*time.Second, 30*time.Second)
	return &WorkspaceTypeSubroutine{client: c, rootClient: root, limiter: exp}
}

func (r *WorkspaceTypeSubroutine) GetName() string { return WorkspaceTypeSubroutineName }

func (r *WorkspaceTypeSubroutine) Finalizers() []string { return nil }

func (r *WorkspaceTypeSubroutine) Finalize(ctx context.Context, ro runtimeobject.RuntimeObject) (ctrl.Result, errors.OperatorError) {
	return ctrl.Result{}, nil
}

func (r *WorkspaceTypeSubroutine) Process(ctx context.Context, ro runtimeobject.RuntimeObject) (ctrl.Result, errors.OperatorError) {
	acct := ro.(*corev1alpha1.Account)
	if acct.Spec.Type != corev1alpha1.AccountTypeOrg {
		return ctrl.Result{}, nil
	}
	log := logger.LoadLoggerFromContext(ctx)
	cn := MustGetClusteredName(ctx, ro)

	// Base WorkspaceTypes live in the provider (root) workspace. Prefer a root-scoped client if available
	// to READ them for inheritance/fallback. We'll CREATE custom WorkspaceTypes in the current logical
	// cluster (e.g., orgs:root-org) so tests and consumers can discover them locally.
	cfg := commonconfig.LoadConfigFromContext(ctx).(operatorconfig.OperatorConfig)
	rootCtx := kontext.WithCluster(ctx, logicalcluster.Name(cfg.Kcp.ProviderWorkspace))
	rc := r.rootClient
	// We'll create custom WorkspaceTypes in the current cluster (e.g., orgs:root-org)
	// but read base types from the provider (root) workspace for fallback copying.

	// Retrieve base workspace types (non-blocking)
	// Also capture current logical cluster path for local references
	currentCluster, _ := kontext.ClusterFrom(ctx)
	currentPath := currentCluster.String()
	var baseOrg *kcptenancyv1alpha.WorkspaceType
	var baseAcc *kcptenancyv1alpha.WorkspaceType
	{
		wt := &kcptenancyv1alpha.WorkspaceType{}
		if err := rc.Get(rootCtx, client.ObjectKey{Name: "org"}, wt); err != nil {
			if !kerrors.IsNotFound(err) {
				return ctrl.Result{}, errors.NewOperatorError(err, true, true)
			}
			log.Debug().Str("account", acct.Name).Msg("base org WorkspaceType not found yet; continuing without fallback copy")
		} else {
			baseOrg = wt
		}
	}
	{
		wt := &kcptenancyv1alpha.WorkspaceType{}
		if err := rc.Get(rootCtx, client.ObjectKey{Name: "account"}, wt); err != nil {
			if !kerrors.IsNotFound(err) {
				return ctrl.Result{}, errors.NewOperatorError(err, true, true)
			}
			log.Debug().Str("account", acct.Name).Msg("base account WorkspaceType not found yet; continuing without fallback copy")
		} else {
			baseAcc = wt
		}
	}

	// Ensure custom account workspace type using extend.with for inheritance.
	customAccName := GetAccWorkspaceTypeName(acct.Name)
	customAcc := &kcptenancyv1alpha.WorkspaceType{ObjectMeta: metav1.ObjectMeta{Name: customAccName}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.client, customAcc, func() error {
		// Build new spec relying on extension
		customAcc.Spec.Extend = kcptenancyv1alpha.WorkspaceTypeExtension{With: []kcptenancyv1alpha.WorkspaceTypeReference{{Name: "account", Path: "root"}}}
		// Do not set cross-cluster owner reference; rely on label if needed in the future.
		return nil
	})
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}

	// Fallback: if defaultAPIBindings weren't inherited (empty), copy from base.
	if baseAcc != nil && len(customAcc.Spec.DefaultAPIBindings) == 0 && len(baseAcc.Spec.DefaultAPIBindings) > 0 {
		customAcc.Spec.DefaultAPIBindings = baseAcc.Spec.DefaultAPIBindings
		if e := r.client.Update(ctx, customAcc); e != nil {
			return ctrl.Result{}, errors.NewOperatorError(e, true, true)
		}
	}

	// Ensure custom org workspace type. Do NOT extend base "org" type because it restricts allowed parents to [root:orgs],
	// which prevents creating org workspaces under an org parent. Instead, define a standalone type, copy bindings, and
	// allow parent type "org" explicitly. Default child type points to the custom account type created above.
	customOrgName := GetOrgWorkspaceTypeName(acct.Name)
	customOrg := &kcptenancyv1alpha.WorkspaceType{ObjectMeta: metav1.ObjectMeta{Name: customOrgName}}
	_, err = controllerutil.CreateOrUpdate(ctx, r.client, customOrg, func() error {
		// Implement "account" so that parent type "org" (which allows [account] children) accepts this as a child
		customOrg.Spec.Extend = kcptenancyv1alpha.WorkspaceTypeExtension{With: []kcptenancyv1alpha.WorkspaceTypeReference{{Name: "account", Path: "root"}}}
		// Default child type is the custom account type created in the current cluster; admission requires path
		customOrg.Spec.DefaultChildWorkspaceType = &kcptenancyv1alpha.WorkspaceTypeReference{Name: kcptenancyv1alpha.WorkspaceTypeName(customAccName), Path: currentPath}
		// Make it legal to create this org type under an org parent
		customOrg.Spec.LimitAllowedParents = &kcptenancyv1alpha.WorkspaceTypeSelector{Types: []kcptenancyv1alpha.WorkspaceTypeReference{{Name: "org", Path: "root"}}}
		// Do not set cross-cluster owner reference; rely on label if needed in the future.
		return nil
	})
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}

	if baseOrg != nil && len(customOrg.Spec.DefaultAPIBindings) == 0 && len(baseOrg.Spec.DefaultAPIBindings) > 0 {
		customOrg.Spec.DefaultAPIBindings = baseOrg.Spec.DefaultAPIBindings
		if e := r.client.Update(ctx, customOrg); e != nil {
			return ctrl.Result{}, errors.NewOperatorError(e, true, true)
		}
	}

	r.limiter.Forget(cn)
	log.Debug().Str("customOrgWorkspaceType", customOrgName).Str("customAccountWorkspaceType", customAccName).Msg("custom workspace types ensured (with extend)")
	return ctrl.Result{}, nil
}

func GetOrgWorkspaceTypeName(accountName string) string {
	return fmt.Sprintf("%s-org", accountName)
}

func GetAccWorkspaceTypeName(accountName string) string {
	return fmt.Sprintf("%s-acc", accountName)
}
