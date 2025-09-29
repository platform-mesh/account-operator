package subroutines

import (
	"context"
	"time"

	kcptypes "github.com/platform-mesh/account-operator/pkg/types"
	commonconfig "github.com/platform-mesh/golang-commons/config"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/runtimeobject"
	"github.com/platform-mesh/golang-commons/errors"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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
	// In upstream controller-runtime, cluster is not injected; synthesize a stable key
	cn, _ := GetClusteredName(ctx, ro)
	if cn.NamespacedName.Name == "" {
		cn = ClusteredName{NamespacedName: types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}}
	}

	ws := kcptypes.Workspace{}
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

	// Determine workspace type name and path based on account type
	var wtName, wtPath string
	if instance.Spec.Type == corev1alpha1.AccountTypeOrg {
		// For organizations, use custom workspace type if WorkspaceType subroutine is enabled
		if cfg.Subroutines.WorkspaceType.Enabled {
			wtName = GetOrgWorkspaceTypeName(instance.Name, cfg.Kcp.ProviderWorkspace)
			wtPath = cfg.Kcp.ProviderWorkspace
		} else {
			// Fallback to base org type
			wtName = string(instance.Spec.Type)
			wtPath = cfg.Kcp.ProviderWorkspace
		}
	} else {
		// For regular accounts, check if we should use custom workspace types based on context
		// For now, use the base account type (can be enhanced when cluster context is available)
		wtName = string(instance.Spec.Type)
		wtPath = cfg.Kcp.ProviderWorkspace

		// TODO: When cluster context is available, detect if we're in an org context
		// and use GetAccWorkspaceTypeName accordingly
	}

	// Create the workspace with the determined workspace type
	createdWorkspace := &kcptypes.Workspace{ObjectMeta: metav1.ObjectMeta{Name: instance.Name}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.client, createdWorkspace, func() error {
		createdWorkspace.Spec.Type = &kcptypes.WorkspaceTypeReference{
			Name: wtName,
			Path: wtPath,
		}

		return controllerutil.SetOwnerReference(instance, createdWorkspace, r.client.Scheme())
	})
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}
	return ctrl.Result{}, nil
}
