package subroutines

import (
	"context"
	"time"

	kcptenancyv1alpha "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"
	commonconfig "github.com/platform-mesh/golang-commons/config"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/runtimeobject"
	"github.com/platform-mesh/golang-commons/errors"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	corev1alpha1 "github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/account-operator/internal/config"
)

const (
	WorkspaceSubroutineName      = "WorkspaceSubroutine"
	WorkspaceSubroutineFinalizer = "account.core.platform-mesh.io/finalizer"
)

type WorkspaceSubroutine struct {
	client  client.Client
	limiter workqueue.TypedRateLimiter[ClusteredName]
}

func NewWorkspaceSubroutine(client client.Client) *WorkspaceSubroutine {
	exp := workqueue.NewTypedItemExponentialFailureRateLimiter[ClusteredName](1*time.Second, 120*time.Second)
	return &WorkspaceSubroutine{client: client, limiter: exp}
}

func (r *WorkspaceSubroutine) GetName() string {
	return WorkspaceSubroutineName
}

func (r *WorkspaceSubroutine) Finalize(ctx context.Context, ro runtimeobject.RuntimeObject) (ctrl.Result, errors.OperatorError) {
	instance := ro.(*corev1alpha1.Account)
	cn := MustGetClusteredName(ctx, ro)

	ws := kcptenancyv1alpha.Workspace{}
	err := r.client.Get(ctx, client.ObjectKey{Name: instance.Name}, &ws)
	if kerrors.IsNotFound(err) {
		return ctrl.Result{}, nil
	}
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}

	if ws.GetDeletionTimestamp() != nil {
		next := r.limiter.When(cn)
		return ctrl.Result{RequeueAfter: next}, nil
	}

	err = r.client.Delete(ctx, &ws)
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}

	// we need to requeue to check if the namespace was deleted
	next := r.limiter.When(cn)
	return ctrl.Result{RequeueAfter: next}, nil
}

func (r *WorkspaceSubroutine) Finalizers() []string { // coverage-ignore
	return []string{"account.core.platform-mesh.io/finalizer"}
}

func (r *WorkspaceSubroutine) Process(ctx context.Context, runtimeObj runtimeobject.RuntimeObject) (ctrl.Result, errors.OperatorError) {
	instance := runtimeObj.(*corev1alpha1.Account)
	cfg := commonconfig.LoadConfigFromContext(ctx).(config.OperatorConfig)

	// Test if namespace was already created based on status
	createdWorkspace := &kcptenancyv1alpha.Workspace{ObjectMeta: metav1.ObjectMeta{Name: instance.Name}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.client, createdWorkspace, func() error {
		// Only set the type on create; Workspace.spec.type.name is immutable.
		if createdWorkspace.CreationTimestamp.IsZero() {
			wtName := string(instance.Spec.Type)
			wtPath := cfg.Kcp.ProviderWorkspace
			switch instance.Spec.Type {
			case corev1alpha1.AccountTypeOrg:
				wtName = GetOrgWorkspaceTypeName(instance.Name)
				// Local custom type: omit path to resolve in current logical cluster
				wtPath = ""
			case corev1alpha1.AccountTypeAccount:
				// Use the provider (root) 'account' WorkspaceType. It is not installed in the current cluster.
				wtPath = cfg.Kcp.ProviderWorkspace
			}
			createdWorkspace.Spec.Type = kcptenancyv1alpha.WorkspaceTypeReference{
				Name: kcptenancyv1alpha.WorkspaceTypeName(wtName),
				Path: wtPath,
			}
		}
		return controllerutil.SetOwnerReference(instance, createdWorkspace, r.client.Scheme())
	})
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}
	return ctrl.Result{}, nil
}
