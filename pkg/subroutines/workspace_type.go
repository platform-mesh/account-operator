package subroutines

import (
	"context"

	kcptenancyv1alpha "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/runtimeobject"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/subroutine"
	"github.com/platform-mesh/golang-commons/errors"
	"github.com/platform-mesh/golang-commons/logger"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	"github.com/platform-mesh/account-operator/api/v1alpha1"
)

const (
	workspaceTypeSubroutineName           = "WorkspaceTypeSubroutine"
	workspaceTypeSubroutineFinalizer      = "workspacetype.core.platform-mesh.io/finalizer"
	rootOrgWorkspaceTypeName              = "org"
	rootOrgWorkspaceTypeWorkspacePath     = "root"
	rootAccountWorkspaceTypeName          = "account"
	rootAccountWorkspaceTypeWorkspacePath = "root"
	rootOrgsWorkspaceTypeName             = "orgs"
)

var _ subroutine.Subroutine = &WorkspaceTypeSubroutine{}

type WorkspaceTypeSubroutine struct {
	mgr        mcmanager.Manager
	baseConfig *rest.Config // For fallback mode
}

func (w WorkspaceTypeSubroutine) Process(ctx context.Context, ro runtimeobject.RuntimeObject) (ctrl.Result, errors.OperatorError) {
	instance := ro.(*v1alpha1.Account)
	log := logger.LoadLoggerFromContext(ctx)

	if instance.Spec.Type != v1alpha1.AccountTypeOrg {
		// Only process org accounts
		return ctrl.Result{}, nil
	}

	orgWorkspaceTypeName := generateOrganizationWorkspaceTypeName(instance.Name)
	accountWorkspaceTypeName := generateAccountWorkspaceTypeName(instance.Name)
	orgWst := generateOrgWorkspaceType(instance, orgWorkspaceTypeName, accountWorkspaceTypeName)
	accWst := generateAccountWorkspaceType(instance, orgWorkspaceTypeName, accountWorkspaceTypeName)

	err := w.createOrUpdateWorkspaceType(ctx, orgWst)
	if err != nil {
		log.Error().Err(err).Str("name", accWst.Name).Msg("failed to create or update org workspace type")
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}

	err = w.createOrUpdateWorkspaceType(ctx, accWst)
	if err != nil {
		log.Error().Err(err).Str("name", accWst.Name).Msg("failed to create or update account workspace type")
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}

	return ctrl.Result{}, nil
}

func (w WorkspaceTypeSubroutine) createOrUpdateWorkspaceType(ctx context.Context, desiredWst kcptenancyv1alpha.WorkspaceType) error {
	// Get orgs client from multicluster manager
	var orgsClient client.Client
	if w.mgr != nil {
		// Use multicluster approach - get root-orgs cluster client
		cluster, err := w.mgr.GetCluster(ctx, "root-orgs")
		if err != nil {
			return err
		}
		orgsClient = cluster.GetClient()
	} else if w.baseConfig != nil {
		// Fallback: create direct connection (backward compatibility)
		clientCfg, err := createOrganizationRestConfig(w.baseConfig)
		if err != nil {
			return err
		}
		orgsClient, err = client.New(clientCfg, client.Options{})
		if err != nil {
			return err
		}
	} else {
		// For testing without multicluster or config, skip creation
		return nil
	}

	wst := &kcptenancyv1alpha.WorkspaceType{ObjectMeta: metav1.ObjectMeta{Name: desiredWst.Name}}
	_, err := controllerutil.CreateOrUpdate(ctx, orgsClient, wst, func() error {
		wst.Spec = desiredWst.Spec
		return nil
	})
	return err
}

func (w WorkspaceTypeSubroutine) Finalize(ctx context.Context, ro runtimeobject.RuntimeObject) (ctrl.Result, errors.OperatorError) {
	instance := ro.(*v1alpha1.Account)
	log := logger.LoadLoggerFromContext(ctx)
	if instance.Spec.Type != v1alpha1.AccountTypeOrg {
		// Only process org accounts
		return ctrl.Result{}, nil
	}

	// Get orgs client from multicluster manager
	var orgsClient client.Client
	if w.mgr != nil {
		// Use multicluster approach - get root-orgs cluster client
		cluster, err := w.mgr.GetCluster(ctx, "root-orgs")
		if err != nil {
			log.Error().Err(err).Msg("failed to get root-orgs cluster")
			return ctrl.Result{}, errors.NewOperatorError(err, true, true)
		}
		orgsClient = cluster.GetClient()
	} else if w.baseConfig != nil {
		// Fallback: create direct connection (backward compatibility)
		clientCfg, err := createOrganizationRestConfig(w.baseConfig)
		if err != nil {
			log.Error().Err(err).Msg("failed to create orgs config")
			return ctrl.Result{}, errors.NewOperatorError(err, true, true)
		}
		orgsClient, err = client.New(clientCfg, client.Options{})
		if err != nil {
			log.Error().Err(err).Msg("failed to create orgs client")
			return ctrl.Result{}, errors.NewOperatorError(err, true, true)
		}
	} else {
		// For testing without multicluster or config, skip deletion
		return ctrl.Result{}, nil
	}

	orgWorkspaceTypeName := generateOrganizationWorkspaceTypeName(instance.Name)
	accountWorkspaceTypeName := generateAccountWorkspaceTypeName(instance.Name)

	err := orgsClient.Delete(ctx, &kcptenancyv1alpha.WorkspaceType{ObjectMeta: metav1.ObjectMeta{Name: orgWorkspaceTypeName}})
	if err != nil {
		if !kerrors.IsNotFound(err) {
			log.Error().Err(err).Str("name", orgWorkspaceTypeName).Msg("failed to delete org workspace")
			return ctrl.Result{}, errors.NewOperatorError(err, true, true)
		}
	}
	err = orgsClient.Delete(ctx, &kcptenancyv1alpha.WorkspaceType{ObjectMeta: metav1.ObjectMeta{Name: accountWorkspaceTypeName}})
	if err != nil {
		if !kerrors.IsNotFound(err) {
			log.Error().Err(err).Str("name", accountWorkspaceTypeName).Msg("failed to delete acc workspace")
			return ctrl.Result{}, errors.NewOperatorError(err, true, true)
		}
	}

	return ctrl.Result{}, nil
}

func (w WorkspaceTypeSubroutine) GetName() string {
	return workspaceTypeSubroutineName
}

func (w WorkspaceTypeSubroutine) Finalizers() []string {
	return []string{workspaceTypeSubroutineFinalizer}
}

func NewWorkspaceTypeSubroutine(mgr ctrl.Manager) *WorkspaceTypeSubroutine {
	return &WorkspaceTypeSubroutine{mgr: nil, baseConfig: mgr.GetConfig()}
}

// NewMultiClusterWorkspaceTypeSubroutine creates a WorkspaceTypeSubroutine with multicluster support
func NewMultiClusterWorkspaceTypeSubroutine(mgr mcmanager.Manager) *WorkspaceTypeSubroutine {
	return &WorkspaceTypeSubroutine{mgr: mgr, baseConfig: mgr.GetLocalManager().GetConfig()}
}

func generateOrgWorkspaceType(instance *v1alpha1.Account, orgWorkspaceTypeName, accountWorkspaceTypeName string) kcptenancyv1alpha.WorkspaceType {
	return kcptenancyv1alpha.WorkspaceType{
		ObjectMeta: metav1.ObjectMeta{Name: orgWorkspaceTypeName},
		Spec: kcptenancyv1alpha.WorkspaceTypeSpec{
			Extend: kcptenancyv1alpha.WorkspaceTypeExtension{
				With: []kcptenancyv1alpha.WorkspaceTypeReference{
					{Name: rootOrgWorkspaceTypeName, Path: rootOrgWorkspaceTypeWorkspacePath},
				},
			},
			DefaultChildWorkspaceType: &kcptenancyv1alpha.WorkspaceTypeReference{
				Name: kcptenancyv1alpha.WorkspaceTypeName(accountWorkspaceTypeName), Path: orgsWorkspacePath},
			LimitAllowedParents: &kcptenancyv1alpha.WorkspaceTypeSelector{
				Types: []kcptenancyv1alpha.WorkspaceTypeReference{
					{Name: rootOrgsWorkspaceTypeName, Path: rootOrgWorkspaceTypeWorkspacePath},
				},
			},
			LimitAllowedChildren: &kcptenancyv1alpha.WorkspaceTypeSelector{
				Types: []kcptenancyv1alpha.WorkspaceTypeReference{
					{Name: kcptenancyv1alpha.WorkspaceTypeName(accountWorkspaceTypeName), Path: orgsWorkspacePath},
				},
			},
			AuthenticationConfigurations: []kcptenancyv1alpha.AuthenticationConfigurationReference{
				{
					Name: instance.Name,
				},
			},
		},
	}
}

func generateAccountWorkspaceType(instance *v1alpha1.Account, orgWorkspaceTypeName, accountWorkspaceTypeName string) kcptenancyv1alpha.WorkspaceType {
	return kcptenancyv1alpha.WorkspaceType{
		ObjectMeta: metav1.ObjectMeta{Name: accountWorkspaceTypeName},
		Spec: kcptenancyv1alpha.WorkspaceTypeSpec{
			Extend: kcptenancyv1alpha.WorkspaceTypeExtension{
				With: []kcptenancyv1alpha.WorkspaceTypeReference{
					{Name: rootAccountWorkspaceTypeName, Path: rootAccountWorkspaceTypeWorkspacePath},
				},
			},
			DefaultChildWorkspaceType: &kcptenancyv1alpha.WorkspaceTypeReference{
				Name: kcptenancyv1alpha.WorkspaceTypeName(accountWorkspaceTypeName), Path: orgsWorkspacePath},
			LimitAllowedParents: &kcptenancyv1alpha.WorkspaceTypeSelector{
				Types: []kcptenancyv1alpha.WorkspaceTypeReference{
					{Name: kcptenancyv1alpha.WorkspaceTypeName(orgWorkspaceTypeName), Path: orgsWorkspacePath},
					{Name: kcptenancyv1alpha.WorkspaceTypeName(accountWorkspaceTypeName), Path: orgsWorkspacePath},
				},
			},
			LimitAllowedChildren: &kcptenancyv1alpha.WorkspaceTypeSelector{
				Types: []kcptenancyv1alpha.WorkspaceTypeReference{
					{Name: kcptenancyv1alpha.WorkspaceTypeName(accountWorkspaceTypeName), Path: orgsWorkspacePath},
				},
			},
			AuthenticationConfigurations: []kcptenancyv1alpha.AuthenticationConfigurationReference{
				{
					Name: instance.Name,
				},
			},
		},
	}
}
