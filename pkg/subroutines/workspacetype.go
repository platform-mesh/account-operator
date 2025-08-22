package subroutines

import (
	"context"
	"fmt"

	kcptenancyv1alpha "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"
	"github.com/kcp-dev/logicalcluster/v3"
	corev1alpha1 "github.com/platform-mesh/account-operator/api/v1alpha1"
	commonconfig "github.com/platform-mesh/golang-commons/config"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/runtimeobject"
	"github.com/platform-mesh/golang-commons/errors"
	"github.com/platform-mesh/golang-commons/logger"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
}

func NewWorkspaceTypeSubroutine(c client.Client) *WorkspaceTypeSubroutine {
	return &WorkspaceTypeSubroutine{client: c, rootClient: c}
}

func NewWorkspaceTypeSubroutineWithRootClient(c client.Client, root client.Client) *WorkspaceTypeSubroutine {
	return &WorkspaceTypeSubroutine{client: c, rootClient: root}
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
	currentCluster, ok := kontext.ClusterFrom(ctx)
	if !ok {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("cluster missing in context"), true, false)
	}
	currentPath := currentCluster.String()
	var baseOrg *kcptenancyv1alpha.WorkspaceType
	var baseAcc *kcptenancyv1alpha.WorkspaceType
	baseOrg = &kcptenancyv1alpha.WorkspaceType{}
	if err := rc.Get(rootCtx, client.ObjectKey{Name: "org"}, baseOrg); err != nil {
		if !kerrors.IsNotFound(err) {
			return ctrl.Result{}, errors.NewOperatorError(err, true, true)
		}
		log.Debug().Str("account", acct.Name).Msg("base org WorkspaceType not found yet; continuing without fallback copy")
		baseOrg = nil
	}
	baseAcc = &kcptenancyv1alpha.WorkspaceType{}
	if err := rc.Get(rootCtx, client.ObjectKey{Name: "account"}, baseAcc); err != nil {
		if !kerrors.IsNotFound(err) {
			return ctrl.Result{}, errors.NewOperatorError(err, true, true)
		}
		log.Debug().Str("account", acct.Name).Msg("base account WorkspaceType not found yet; continuing without fallback copy")
		baseAcc = nil
	}

	// Ensure custom account workspace type using extend.with for inheritance.
	customAccName := GetAccWorkspaceTypeName(acct.Name)
	customOrgName := GetOrgWorkspaceTypeName(acct.Name)
	customAcc := &kcptenancyv1alpha.WorkspaceType{ObjectMeta: metav1.ObjectMeta{Name: customAccName}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.client, customAcc, func() error {
		// Build new spec relying on extension
		customAcc.Spec.Extend = kcptenancyv1alpha.WorkspaceTypeExtension{With: []kcptenancyv1alpha.WorkspaceTypeReference{{Name: "account", Path: "root"}}}
		// Allow creating this account type under only the custom org type (in current cluster)
		customAcc.Spec.LimitAllowedParents = &kcptenancyv1alpha.WorkspaceTypeSelector{Types: []kcptenancyv1alpha.WorkspaceTypeReference{
			{Name: kcptenancyv1alpha.WorkspaceTypeName(customOrgName), Path: currentPath},
		}}
		// Do not set cross-cluster owner reference; rely on label if needed in the future.
		return nil
	})
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}

	// Always copy defaultAPIBindings from base types to ensure proper initialization
	if baseAcc != nil {
		customAcc.Spec.DefaultAPIBindings = baseAcc.Spec.DefaultAPIBindings
		if e := r.client.Update(ctx, customAcc); e != nil {
			return ctrl.Result{}, errors.NewOperatorError(e, true, true)
		}
	}

	// Ensure custom org workspace type. Extend base "org" to inherit initializers and default bindings.
	// We'll still explicitly constrain parents to type "orgs" and set default child to the custom account type.
	// reuse computed customOrgName above
	customOrg := &kcptenancyv1alpha.WorkspaceType{ObjectMeta: metav1.ObjectMeta{Name: customOrgName}}
	_, err = controllerutil.CreateOrUpdate(ctx, r.client, customOrg, func() error {
		// Extend base "org" to inherit initializers and default bindings
		customOrg.Spec.Extend = kcptenancyv1alpha.WorkspaceTypeExtension{With: []kcptenancyv1alpha.WorkspaceTypeReference{{Name: "org", Path: "root"}}}
		// Default child type is the custom account type created in the current cluster; admission requires path
		customOrg.Spec.DefaultChildWorkspaceType = &kcptenancyv1alpha.WorkspaceTypeReference{Name: kcptenancyv1alpha.WorkspaceTypeName(customAccName), Path: currentPath}
		// Allow creating this org type under parent type "orgs"
		customOrg.Spec.LimitAllowedParents = &kcptenancyv1alpha.WorkspaceTypeSelector{
			Types: []kcptenancyv1alpha.WorkspaceTypeReference{{Name: "orgs", Path: "root"}},
		}
		// Explicitly allow custom account children under this custom org type
		customOrg.Spec.LimitAllowedChildren = &kcptenancyv1alpha.WorkspaceTypeSelector{
			Types: []kcptenancyv1alpha.WorkspaceTypeReference{
				{Name: kcptenancyv1alpha.WorkspaceTypeName(customAccName), Path: currentPath},
			},
		}
		// Do not set cross-cluster owner reference; rely on label if needed in the future.
		return nil
	})
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}

	// Always copy defaultAPIBindings from base types to ensure proper initialization
	if baseOrg != nil {
		customOrg.Spec.DefaultAPIBindings = baseOrg.Spec.DefaultAPIBindings
		if e := r.client.Update(ctx, customOrg); e != nil {
			return ctrl.Result{}, errors.NewOperatorError(e, true, true)
		}
	}

	log.Debug().Str("customOrgWorkspaceType", customOrgName).Str("customAccountWorkspaceType", customAccName).Msg("custom workspace types ensured (with extend)")
	return ctrl.Result{}, nil
}

func GetOrgWorkspaceTypeName(accountName string) string {
	return fmt.Sprintf("%s-org", accountName)
}

func GetAccWorkspaceTypeName(accountName string) string {
	return fmt.Sprintf("%s-acc", accountName)
}
