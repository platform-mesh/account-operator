package subroutines

import (
	"context"
	"fmt"
	"sync"

	kcptenancyv1alpha "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/runtimeobject"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/subroutine"
	"github.com/platform-mesh/golang-commons/errors"
	"github.com/platform-mesh/golang-commons/logger"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

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
	baseConfig *rest.Config
	scheme     *runtime.Scheme

	mu         sync.Mutex
	orgsClient client.Client
}

func (w *WorkspaceTypeSubroutine) Process(ctx context.Context, ro runtimeobject.RuntimeObject) (ctrl.Result, errors.OperatorError) {
	instance := ro.(*v1alpha1.Account)
	log := logger.LoadLoggerFromContext(ctx)

	if instance.Spec.Type != v1alpha1.AccountTypeOrg {
		return ctrl.Result{}, nil
	}

	orgWorkspaceTypeName := generateOrganizationWorkspaceTypeName(instance.Name)
	accountWorkspaceTypeName := generateAccountWorkspaceTypeName(instance.Name)
	orgWst := generateOrgWorkspaceType(instance, orgWorkspaceTypeName, accountWorkspaceTypeName)
	accWst := generateAccountWorkspaceType(instance, orgWorkspaceTypeName, accountWorkspaceTypeName)

	if err := w.createOrUpdateWorkspaceType(ctx, orgWst); err != nil {
		log.Error().Err(err).Str("name", orgWst.Name).Msg("failed to create or update org workspace type")
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}

	if err := w.createOrUpdateWorkspaceType(ctx, accWst); err != nil {
		log.Error().Err(err).Str("name", accWst.Name).Msg("failed to create or update account workspace type")
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}

	return ctrl.Result{}, nil
}

func (w *WorkspaceTypeSubroutine) createOrUpdateWorkspaceType(ctx context.Context, desiredWst kcptenancyv1alpha.WorkspaceType) error {
	orgsClient, err := w.getOrgsClient()
	if err != nil {
		return err
	}

	wst := &kcptenancyv1alpha.WorkspaceType{ObjectMeta: metav1.ObjectMeta{Name: desiredWst.Name}}
	_, err = controllerutil.CreateOrUpdate(ctx, orgsClient, wst, func() error {
		wst.Spec = desiredWst.Spec
		return nil
	})
	return err
}

func (w *WorkspaceTypeSubroutine) Finalize(ctx context.Context, ro runtimeobject.RuntimeObject) (ctrl.Result, errors.OperatorError) {
	instance := ro.(*v1alpha1.Account)
	log := logger.LoadLoggerFromContext(ctx)
	if instance.Spec.Type != v1alpha1.AccountTypeOrg {
		return ctrl.Result{}, nil
	}

	orgsClient, err := w.getOrgsClient()
	if err != nil {
		log.Error().Err(err).Msg("failed to get orgs client")
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}

	orgWorkspaceTypeName := generateOrganizationWorkspaceTypeName(instance.Name)
	accountWorkspaceTypeName := generateAccountWorkspaceTypeName(instance.Name)

	if err := orgsClient.Delete(ctx, &kcptenancyv1alpha.WorkspaceType{ObjectMeta: metav1.ObjectMeta{Name: orgWorkspaceTypeName}}); err != nil {
		if !kerrors.IsNotFound(err) {
			log.Error().Err(err).Str("name", orgWorkspaceTypeName).Msg("failed to delete org workspace type")
			return ctrl.Result{}, errors.NewOperatorError(err, true, true)
		}
	}

	if err := orgsClient.Delete(ctx, &kcptenancyv1alpha.WorkspaceType{ObjectMeta: metav1.ObjectMeta{Name: accountWorkspaceTypeName}}); err != nil {
		if !kerrors.IsNotFound(err) {
			log.Error().Err(err).Str("name", accountWorkspaceTypeName).Msg("failed to delete account workspace type")
			return ctrl.Result{}, errors.NewOperatorError(err, true, true)
		}
	}

	return ctrl.Result{}, nil
}

func (w *WorkspaceTypeSubroutine) GetName() string {
	return workspaceTypeSubroutineName
}

func (w *WorkspaceTypeSubroutine) Finalizers() []string {
	return []string{workspaceTypeSubroutineFinalizer}
}

func NewWorkspaceTypeSubroutine(baseConfig *rest.Config, scheme *runtime.Scheme) *WorkspaceTypeSubroutine {
	return &WorkspaceTypeSubroutine{baseConfig: baseConfig, scheme: scheme}
}

func NewWorkspaceTypeSubroutineWithClient(orgsClient client.Client) *WorkspaceTypeSubroutine {
	return &WorkspaceTypeSubroutine{orgsClient: orgsClient}
}

func (w *WorkspaceTypeSubroutine) getOrgsClient() (client.Client, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.orgsClient != nil {
		return w.orgsClient, nil
	}
	if w.baseConfig == nil {
		return nil, fmt.Errorf("workspace type subroutine: base config not provided")
	}

	clientCfg, err := createOrganizationRestConfig(w.baseConfig)
	if err != nil {
		return nil, err
	}

	options := client.Options{}
	if w.scheme != nil {
		options.Scheme = w.scheme
	}

	orgsClient, err := client.New(clientCfg, options)
	if err != nil {
		return nil, err
	}

	w.orgsClient = orgsClient
	return w.orgsClient, nil
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
