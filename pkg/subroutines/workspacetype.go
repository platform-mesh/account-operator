package subroutines

import (
	"context"
	"fmt"
	"time"

	kcptenancyv1alpha "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"
	corev1alpha1 "github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/runtimeobject"
	"github.com/platform-mesh/golang-commons/errors"
	"github.com/platform-mesh/golang-commons/logger"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const WorkspaceTypeSubroutineName = "WorkspaceTypeSubroutine"

type WorkspaceTypeSubroutine struct {
	client  client.Client
	limiter workqueue.TypedRateLimiter[ClusteredName]
}

func NewWorkspaceTypeSubroutine(c client.Client) *WorkspaceTypeSubroutine {
	exp := workqueue.NewTypedItemExponentialFailureRateLimiter[ClusteredName](1*time.Second, 30*time.Second)
	return &WorkspaceTypeSubroutine{client: c, limiter: exp}
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

	base := &kcptenancyv1alpha.WorkspaceType{}
	if err := r.client.Get(ctx, client.ObjectKey{Name: "org"}, base); err != nil {
		if kerrors.IsNotFound(err) {
			delay := r.limiter.When(cn)
			log.Info().Str("account", acct.Name).Msg("base org WorkspaceType not found yet; requeue")
			return ctrl.Result{RequeueAfter: delay}, nil
		}
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}

	customName := fmt.Sprintf("%s-org", acct.Name)
	custom := &kcptenancyv1alpha.WorkspaceType{ObjectMeta: metav1.ObjectMeta{Name: customName}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.client, custom, func() error {
		custom.Spec = base.Spec
		return controllerutil.SetOwnerReference(acct, custom, r.client.Scheme())
	})
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}
	r.limiter.Forget(cn)
	log.Debug().Str("customWorkspaceType", customName).Msg("custom org workspace type ensured")
	return ctrl.Result{}, nil
}
