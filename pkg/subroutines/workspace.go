package subroutines

import (
	"context"
	"sync"
	"time"

	kcptenancyv1alpha "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"
	conditionsapi "github.com/kcp-dev/kcp/sdk/apis/third_party/conditions/apis/conditions/v1alpha1"
	conditionshelper "github.com/kcp-dev/kcp/sdk/apis/third_party/conditions/util/conditions"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/runtimeobject"
	"github.com/platform-mesh/golang-commons/errors"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	corev1alpha1 "github.com/platform-mesh/account-operator/api/v1alpha1"
)

const (
	WorkspaceSubroutineName      = "WorkspaceSubroutine"
	WorkspaceSubroutineFinalizer = "account.core.platform-mesh.io/finalizer"
)

type WorkspaceSubroutine struct {
	client     client.Client
	limiter    workqueue.TypedRateLimiter[ClusteredName]
	baseConfig *rest.Config
	scheme     *runtime.Scheme

	mu         sync.Mutex
	orgsClient client.Client
}

func NewWorkspaceSubroutine(clusterClient client.Client, baseConfig *rest.Config, scheme *runtime.Scheme) *WorkspaceSubroutine {
	exp := workqueue.NewTypedItemExponentialFailureRateLimiter[ClusteredName](1*time.Second, 120*time.Second)
	return &WorkspaceSubroutine{client: clusterClient, limiter: exp, baseConfig: baseConfig, scheme: scheme}
}

// NewWorkspaceSubroutineForTesting creates a new WorkspaceSubroutine for unit tests.
func NewWorkspaceSubroutineForTesting(client client.Client, limiter workqueue.TypedRateLimiter[ClusteredName]) *WorkspaceSubroutine {
	return &WorkspaceSubroutine{client: client, limiter: limiter}
}

func (r *WorkspaceSubroutine) GetName() string {
	return WorkspaceSubroutineName
}

func (r *WorkspaceSubroutine) Finalize(ctx context.Context, ro runtimeobject.RuntimeObject) (ctrl.Result, errors.OperatorError) {
	instance := ro.(*corev1alpha1.Account)
	cn := MustGetClusteredName(ctx, ro)

	ws := kcptenancyv1alpha.Workspace{}
	if err := r.client.Get(ctx, client.ObjectKey{Name: instance.Name}, &ws); err != nil {
		if kerrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}

	if ws.GetDeletionTimestamp() != nil {
		next := r.limiter.When(cn)
		return ctrl.Result{RequeueAfter: next}, nil
	}

	if err := r.client.Delete(ctx, &ws); err != nil {
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}

	next := r.limiter.When(cn)
	return ctrl.Result{RequeueAfter: next}, nil
}

func (r *WorkspaceSubroutine) Finalizers() []string { // coverage-ignore
	return []string{WorkspaceSubroutineFinalizer}
}

func (r *WorkspaceSubroutine) Process(ctx context.Context, ro runtimeobject.RuntimeObject) (ctrl.Result, errors.OperatorError) {
	instance := ro.(*corev1alpha1.Account)
	cn := MustGetClusteredName(ctx, ro)

	workspaceTypeName := generateOrganizationWorkspaceTypeName(instance.Name)
	if instance.Spec.Type == corev1alpha1.AccountTypeAccount {
		accountInfo := &corev1alpha1.AccountInfo{}
		if err := r.client.Get(ctx, client.ObjectKey{Name: DefaultAccountInfoName, Namespace: instance.Namespace}, accountInfo); err != nil {
			if kerrors.IsNotFound(err) {
				return ctrl.Result{RequeueAfter: r.limiter.When(cn)}, nil
			}
			return ctrl.Result{}, errors.NewOperatorError(err, true, true)
		}
		if accountInfo.Spec.Organization.Name == "" {
			return ctrl.Result{RequeueAfter: r.limiter.When(cn)}, nil
		}
		workspaceTypeName = generateAccountWorkspaceTypeName(accountInfo.Spec.Organization.Name)
	}

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
	orgsClient, err := r.getOrgsClient()
	if err != nil {
		return false, err
	}
	if orgsClient == nil {
		return true, nil
	}

	wst := &kcptenancyv1alpha.WorkspaceType{}
	if err := orgsClient.Get(ctx, client.ObjectKey{Name: workspaceTypeName}, wst); err != nil {
		if kerrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return conditionshelper.IsTrue(wst, conditionsapi.ReadyCondition), nil
}

func (r *WorkspaceSubroutine) getOrgsClient() (client.Client, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.orgsClient != nil {
		return r.orgsClient, nil
	}
	if r.baseConfig == nil {
		return nil, nil
	}

	clientCfg, err := createOrganizationRestConfig(r.baseConfig)
	if err != nil {
		return nil, err
	}

	options := client.Options{}
	if r.scheme != nil {
		options.Scheme = r.scheme
	}

	orgsClient, err := client.New(clientCfg, options)
	if err != nil {
		return nil, err
	}

	r.orgsClient = orgsClient
	return r.orgsClient, nil
}
