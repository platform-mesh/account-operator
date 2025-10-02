package subroutines

import (
	"context"
	"time"

	kcptenancyv1alpha "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"
	conditionsapi "github.com/kcp-dev/kcp/sdk/apis/third_party/conditions/apis/conditions/v1alpha1"
	conditionshelper "github.com/kcp-dev/kcp/sdk/apis/third_party/conditions/util/conditions"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/runtimeobject"
	"github.com/platform-mesh/golang-commons/errors"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	corev1alpha1 "github.com/platform-mesh/account-operator/api/v1alpha1"
)

const (
	WorkspaceSubroutineName      = "WorkspaceSubroutine"
	WorkspaceSubroutineFinalizer = "account.core.platform-mesh.io/finalizer"
)

type WorkspaceSubroutine struct {
	client     client.Client
	limiter    workqueue.TypedRateLimiter[ClusteredName]
	mgr        mcmanager.Manager // For multicluster support
	baseConfig *rest.Config      // For fallback mode
}

func NewWorkspaceSubroutine(mgr ctrl.Manager) *WorkspaceSubroutine {
	exp := workqueue.NewTypedItemExponentialFailureRateLimiter[ClusteredName](1*time.Second, 120*time.Second)
	return &WorkspaceSubroutine{client: mgr.GetClient(), limiter: exp, mgr: nil, baseConfig: mgr.GetConfig()}
}

// NewMultiClusterWorkspaceSubroutine creates a WorkspaceSubroutine with multicluster support
func NewMultiClusterWorkspaceSubroutine(mgr mcmanager.Manager) *WorkspaceSubroutine {
	exp := workqueue.NewTypedItemExponentialFailureRateLimiter[ClusteredName](1*time.Second, 120*time.Second)
	return &WorkspaceSubroutine{client: mgr.GetLocalManager().GetClient(), limiter: exp, mgr: mgr, baseConfig: mgr.GetLocalManager().GetConfig()}
}

// NewWorkspaceSubroutineForTesting creates a new WorkspaceSubroutine for testing purposes
// with the provided dependencies. This constructor is intended for testing only.
func NewWorkspaceSubroutineForTesting(
	client client.Client,
	limiter workqueue.TypedRateLimiter[ClusteredName],
) *WorkspaceSubroutine {
	return &WorkspaceSubroutine{
		client:     client,
		limiter:    limiter,
		mgr:        nil, // Testing without multicluster
		baseConfig: nil, // No fallback needed in tests
	}
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

	// we need to requeue to check if the workspace was deleted
	next := r.limiter.When(cn)
	return ctrl.Result{RequeueAfter: next}, nil
}

func (r *WorkspaceSubroutine) Finalizers() []string { // coverage-ignore
	return []string{"account.core.platform-mesh.io/finalizer"}
}

func (r *WorkspaceSubroutine) Process(ctx context.Context, ro runtimeobject.RuntimeObject) (ctrl.Result, errors.OperatorError) {
	instance := ro.(*corev1alpha1.Account)
	cn := MustGetClusteredName(ctx, ro)

	// Test if namespace was already created based on status
	workspaceTypeName := generateOrganizationWorkspaceTypeName(instance.Name)
	if instance.Spec.Type == corev1alpha1.AccountTypeAccount {
		// Retrieve organization name
		accountInfo := &corev1alpha1.AccountInfo{}
		err := r.client.Get(ctx, client.ObjectKey{Name: DefaultAccountInfoName, Namespace: instance.Namespace}, accountInfo)
		if err != nil {
			if kerrors.IsNotFound(err) {
				// AccountInfo not found, requeue
				return ctrl.Result{RequeueAfter: r.limiter.When(cn)}, nil
			}
			return ctrl.Result{}, errors.NewOperatorError(err, true, true)
		}
		if accountInfo.Spec.Organization.Name == "" {
			// Requeue briefly; upstream controller may still be populating AccountInfo
			return ctrl.Result{RequeueAfter: r.limiter.When(cn)}, nil
		}
		workspaceTypeName = generateAccountWorkspaceTypeName(accountInfo.Spec.Organization.Name)
	}

	// Test if workspaceType is ready
	ready, err := r.checkWorkspaceTypeReady(ctx, workspaceTypeName)
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}
	if !ready {
		return ctrl.Result{RequeueAfter: r.limiter.When(cn)}, nil
	}

	createdWorkspace := &kcptenancyv1alpha.Workspace{ObjectMeta: metav1.ObjectMeta{Name: instance.Name}}
	_, err = controllerutil.CreateOrUpdate(ctx, r.client, createdWorkspace, func() error {
		createdWorkspace.Spec.Type = &kcptenancyv1alpha.WorkspaceTypeReference{
			Name: kcptenancyv1alpha.WorkspaceTypeName(workspaceTypeName),
			Path: orgsWorkspacePath,
		}
		return controllerutil.SetOwnerReference(instance, createdWorkspace, r.client.Scheme())
	})
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}
	return ctrl.Result{}, nil
}

func (r *WorkspaceSubroutine) checkWorkspaceTypeReady(ctx context.Context, workspaceTypeName string) (bool, error) {
	// Get orgs client from multicluster manager
	var orgsClient client.Client
	if r.mgr != nil {
		// Use multicluster approach - get root-orgs cluster client
		cluster, err := r.mgr.GetCluster(ctx, "root-orgs")
		if err != nil {
			return false, err
		}
		orgsClient = cluster.GetClient()
	} else if r.baseConfig != nil {
		// Fallback for when multicluster is not available
		// Create direct connection
		clientCfg, err := createOrganizationRestConfig(r.baseConfig)
		if err != nil {
			return false, err
		}
		orgsClient, err = client.New(clientCfg, client.Options{})
		if err != nil {
			return false, err
		}
	} else {
		// For testing mode, return early
		return true, nil
	}

	wst := &kcptenancyv1alpha.WorkspaceType{}
	err := orgsClient.Get(ctx, client.ObjectKey{Name: workspaceTypeName}, wst)
	if err != nil {
		if kerrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return conditionshelper.IsTrue(wst, conditionsapi.ReadyCondition), nil
}
