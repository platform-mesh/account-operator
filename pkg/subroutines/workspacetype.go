package subroutines

import (
	"context"

	kcptenancyv1alpha "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"
	"github.com/kcp-dev/logicalcluster/v3"
	commonconfig "github.com/platform-mesh/golang-commons/config"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/runtimeobject"
	"github.com/platform-mesh/golang-commons/errors"
	commonlogger "github.com/platform-mesh/golang-commons/logger"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/kontext"

	corev1alpha1 "github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/account-operator/internal/config"
)

const WorkspaceTypeSubroutineName = "WorkspaceTypeSubroutine"

type WorkspaceTypeSubroutine struct {
	client     client.Client
	rootClient client.Client
}

type baseWorkspaceTypes struct {
	org *kcptenancyv1alpha.WorkspaceType
	acc *kcptenancyv1alpha.WorkspaceType
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

func (r *WorkspaceTypeSubroutine) Process(ctx context.Context, runtimeObj runtimeobject.RuntimeObject) (ctrl.Result, errors.OperatorError) {
	instance := runtimeObj.(*corev1alpha1.Account)

	// Only process organization accounts
	if instance.Spec.Type != corev1alpha1.AccountTypeOrg {
		return ctrl.Result{}, nil
	}

	log := commonlogger.LoadLoggerFromContext(ctx)
	cfg := commonconfig.LoadConfigFromContext(ctx).(config.OperatorConfig)

	// Determine cluster paths
	currentPath := r.getCurrentClusterPath(ctx, cfg)
	typePath := currentPath
	if cfg.Kcp.OrgWorkspaceCluster != "" {
		typePath = cfg.Kcp.OrgWorkspaceCluster
	}
	ctxTypes := kontext.WithCluster(ctx, logicalcluster.Name(typePath))

	// Fetch base workspace types for inheritance
	baseTypes, err := r.fetchBaseWorkspaceTypes(ctx, cfg, log, instance.Name)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Generate custom workspace type names
	customOrgName := GetOrgWorkspaceTypeName(instance.Name, typePath)
	customAccName := GetAccWorkspaceTypeName(instance.Name, typePath)

	// Create custom account workspace type
	if err := r.createCustomAccountWorkspaceType(ctxTypes, instance.Name, typePath, customOrgName, baseTypes.acc); err != nil {
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}

	// Create custom org workspace type
	if err := r.createCustomOrgWorkspaceType(ctxTypes, instance.Name, currentPath, baseTypes.org); err != nil {
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}

	// Update base org type to allow custom org as child
	if err := r.updateBaseOrgWorkspaceType(ctx, cfg, baseTypes.org, customOrgName, typePath, log); err != nil {
		return ctrl.Result{}, err
	}

	log.Debug().
		Str("account", instance.Name).
		Str("customOrgWorkspaceType", customOrgName).
		Str("customAccountWorkspaceType", customAccName).
		Msg("custom workspace types created successfully")

	return ctrl.Result{}, nil
}

// getCurrentClusterPath extracts the current cluster path for creating custom types
func (r *WorkspaceTypeSubroutine) getCurrentClusterPath(ctx context.Context, cfg config.OperatorConfig) string {
	currentPath := cfg.Kcp.ProviderWorkspace
	if cl, ok := kontext.ClusterFrom(ctx); ok {
		currentPath = cl.String()
	}
	return currentPath
}

// fetchBaseWorkspaceTypes retrieves base workspace types for inheritance
func (r *WorkspaceTypeSubroutine) fetchBaseWorkspaceTypes(ctx context.Context, cfg config.OperatorConfig, log *commonlogger.Logger, accountName string) (*baseWorkspaceTypes, errors.OperatorError) {
	rootCtx := kontext.WithCluster(ctx, logicalcluster.Name(cfg.Kcp.ProviderWorkspace))

	baseOrg := &kcptenancyv1alpha.WorkspaceType{}
	if err := r.rootClient.Get(rootCtx, client.ObjectKey{Name: "org"}, baseOrg); err != nil {
		if !kerrors.IsNotFound(err) {
			return nil, errors.NewOperatorError(err, true, true)
		}
		log.Debug().Str("account", accountName).Msg("base org WorkspaceType not found; continuing without fallback copy")
		baseOrg = nil
	}

	baseAcc := &kcptenancyv1alpha.WorkspaceType{}
	if err := r.rootClient.Get(rootCtx, client.ObjectKey{Name: "account"}, baseAcc); err != nil {
		if !kerrors.IsNotFound(err) {
			return nil, errors.NewOperatorError(err, true, true)
		}
		log.Debug().Str("account", accountName).Msg("base account WorkspaceType not found; continuing without fallback copy")
		baseAcc = nil
	}

	return &baseWorkspaceTypes{org: baseOrg, acc: baseAcc}, nil
}

// createCustomAccountWorkspaceType creates the custom account workspace type
func (r *WorkspaceTypeSubroutine) createCustomAccountWorkspaceType(ctx context.Context, accountName, typePath, customOrgName string, baseAcc *kcptenancyv1alpha.WorkspaceType) error {
	customAccName := GetAccWorkspaceTypeName(accountName, typePath)
	customAcc := &kcptenancyv1alpha.WorkspaceType{ObjectMeta: metav1.ObjectMeta{Name: customAccName}}

	_, err := controllerutil.CreateOrUpdate(ctx, r.client, customAcc, func() error {
		if baseAcc != nil {
			customAcc.Spec = baseAcc.Spec
		}

		// Extend base account type
		customAcc.Spec.Extend = kcptenancyv1alpha.WorkspaceTypeExtension{
			With: []kcptenancyv1alpha.WorkspaceTypeReference{
				createWorkspaceTypeReference("account", "root"),
			},
		}

		// Allow creating this account type under the custom org type
		customAcc.Spec.LimitAllowedParents = createWorkspaceTypeSelector(
			createWorkspaceTypeReference(customOrgName, typePath),
		)
		return nil
	})
	return err
}

// createCustomOrgWorkspaceType creates the custom org workspace type
func (r *WorkspaceTypeSubroutine) createCustomOrgWorkspaceType(ctx context.Context, accountName, currentPath string, _ *kcptenancyv1alpha.WorkspaceType) error {
	customOrgName := GetOrgWorkspaceTypeName(accountName, currentPath)
	customAccName := GetAccWorkspaceTypeName(accountName, currentPath)
	customOrg := &kcptenancyv1alpha.WorkspaceType{ObjectMeta: metav1.ObjectMeta{Name: customOrgName}}

	_, err := controllerutil.CreateOrUpdate(ctx, r.client, customOrg, func() error {
		// Extend base org type
		customOrg.Spec.Extend = kcptenancyv1alpha.WorkspaceTypeExtension{
			With: []kcptenancyv1alpha.WorkspaceTypeReference{
				createWorkspaceTypeReference("org", "root"),
			},
		}

		// Default child type is the custom account type
		defaultChild := createWorkspaceTypeReference(customAccName, currentPath)
		customOrg.Spec.DefaultChildWorkspaceType = &defaultChild

		// Allow creating this org type under base org
		customOrg.Spec.LimitAllowedParents = createWorkspaceTypeSelector(
			createWorkspaceTypeReference("org", "root"),
		)

		// Allow custom account children under this custom org type
		customOrg.Spec.LimitAllowedChildren = createWorkspaceTypeSelector(
			createWorkspaceTypeReference(customAccName, currentPath),
		)

		// Set authentication configuration for the workspace type - THE 3 KEY LINES!
		authConfigName := getAuthConfigName(accountName, currentPath)
		customOrg.Spec.AuthenticationConfigurations = []kcptenancyv1alpha.AuthenticationConfigurationReference{
			{Name: authConfigName},
		}

		return nil
	})
	return err
}

// updateBaseOrgWorkspaceType updates the base org workspace type to allow custom org as child
func (r *WorkspaceTypeSubroutine) updateBaseOrgWorkspaceType(ctx context.Context, cfg config.OperatorConfig, baseOrg *kcptenancyv1alpha.WorkspaceType, customOrgName string, typePath string, log *commonlogger.Logger) errors.OperatorError {
	if baseOrg == nil {
		return nil
	}

	updateCtx := kontext.WithCluster(ctx, logicalcluster.Name(cfg.Kcp.ProviderWorkspace))

	if baseOrg.Spec.LimitAllowedChildren == nil {
		baseOrg.Spec.LimitAllowedChildren = &kcptenancyv1alpha.WorkspaceTypeSelector{}
	}

	// Use the path where the custom org type will be created (typePath)
	customOrgPath := typePath

	// Avoid duplicate entries to prevent reconcile churn
	ref := createWorkspaceTypeReference(customOrgName, customOrgPath)
	exists := false
	for _, t := range baseOrg.Spec.LimitAllowedChildren.Types {
		if t.Name == ref.Name && t.Path == ref.Path {
			exists = true
			break
		}
	}
	if !exists {
		baseOrg.Spec.LimitAllowedChildren.Types = append(
			baseOrg.Spec.LimitAllowedChildren.Types, ref,
		)
	}

	if err := r.rootClient.Update(updateCtx, baseOrg); err != nil {
		if kerrors.IsForbidden(err) {
			log.Debug().Str("customOrgWorkspaceType", customOrgName).Err(err).Msg("custom workspace types ensured (base type update forbidden)")
			return nil
		}
		return errors.NewOperatorError(err, true, true)
	}

	return nil
}

// createWorkspaceTypeReference creates a workspace type reference
func createWorkspaceTypeReference(name, path string) kcptenancyv1alpha.WorkspaceTypeReference {
	return kcptenancyv1alpha.WorkspaceTypeReference{
		Name: kcptenancyv1alpha.WorkspaceTypeName(name),
		Path: path,
	}
}

// createWorkspaceTypeSelector creates a workspace type selector with the given types
func createWorkspaceTypeSelector(refs ...kcptenancyv1alpha.WorkspaceTypeReference) *kcptenancyv1alpha.WorkspaceTypeSelector {
	return &kcptenancyv1alpha.WorkspaceTypeSelector{
		Types: refs,
	}
}
