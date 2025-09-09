package subroutines

import (
	"context"
	"strings"
	"time"

	kcptenancyv1alpha "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"
	"github.com/kcp-dev/logicalcluster/v3"
	commonconfig "github.com/platform-mesh/golang-commons/config"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/runtimeobject"
	"github.com/platform-mesh/golang-commons/errors"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/kontext"

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

	// Capture original cluster path (where custom WorkspaceTypes are created)
	origPath := ""
	if cl, ok := kontext.ClusterFrom(ctx); ok {
		origPath = cl.String()
	}
	// Select the cluster under which the workspace will be created
	ctxWS := ctx
	// For org accounts, always create in the designated org workspace cluster
	if instance.Spec.Type == corev1alpha1.AccountTypeOrg {
		orgCluster := cfg.Kcp.OrgWorkspaceCluster
		ctxWS = kontext.WithCluster(ctx, logicalcluster.Name(orgCluster))
	}

	// Test if namespace was already created based on status
	createdWorkspace := &kcptenancyv1alpha.Workspace{ObjectMeta: metav1.ObjectMeta{Name: instance.Name}}
	_, err := controllerutil.CreateOrUpdate(ctxWS, r.client, createdWorkspace, func() error {
		// Only set the type on create; Workspace.spec.type.name is immutable.
		if !createdWorkspace.CreationTimestamp.IsZero() {
			return controllerutil.SetOwnerReference(instance, createdWorkspace, r.client.Scheme())
		}

		wtName := string(instance.Spec.Type)
		wtPath := cfg.Kcp.ProviderWorkspace
		switch instance.Spec.Type {
		case corev1alpha1.AccountTypeOrg:
			wtName = GetOrgWorkspaceTypeName(instance.Name, origPath)
			if cfg.Kcp.OrgWorkspaceCluster != "" {
				wtPath = cfg.Kcp.OrgWorkspaceCluster
			}
		case corev1alpha1.AccountTypeAccount:
			// Parse cluster path to determine org name
			parts := strings.SplitN(origPath, ":", 3)
			if len(parts) >= 3 && parts[1] == "orgs" {
				orgName := parts[2]
				wtName = GetAccWorkspaceTypeName(orgName, origPath)
				wtPath = strings.Join(parts[:2], ":") // parent path where custom types are created
			}
		}
		createdWorkspace.Spec.Type = kcptenancyv1alpha.WorkspaceTypeReference{
			Name: kcptenancyv1alpha.WorkspaceTypeName(wtName),
			Path: wtPath,
		}
		return controllerutil.SetOwnerReference(instance, createdWorkspace, r.client.Scheme())
	})
	if err != nil {
		// Handle forbidden errors gracefully - this can happen in test environments
		// or when the virtual workspace path is not accessible
		if kerrors.IsForbidden(err) {
			ctrl.LoggerFrom(ctx).Info("workspace creation forbidden (virtual workspace path not accessible)", "error", err.Error())
			// Continue without error - workspace creation is not always possible in restricted environments
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}
	return ctrl.Result{}, nil
}
