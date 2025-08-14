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

	// Retrieve base workspace types
	baseOrg := &kcptenancyv1alpha.WorkspaceType{}
	if err := r.client.Get(ctx, client.ObjectKey{Name: "org"}, baseOrg); err != nil {
		if kerrors.IsNotFound(err) {
			delay := r.limiter.When(cn)
			log.Info().Str("account", acct.Name).Msg("base org WorkspaceType not found yet; requeue")
			return ctrl.Result{RequeueAfter: delay}, nil
		}
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}
	baseAcc := &kcptenancyv1alpha.WorkspaceType{}
	if err := r.client.Get(ctx, client.ObjectKey{Name: "account"}, baseAcc); err != nil {
		if kerrors.IsNotFound(err) {
			delay := r.limiter.When(cn)
			log.Info().Str("account", acct.Name).Msg("base account WorkspaceType not found yet; requeue")
			return ctrl.Result{RequeueAfter: delay}, nil
		}
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}

	// Ensure custom account workspace type
	customAccName := fmt.Sprintf("%s-acc", acct.Name)
	customAcc := &kcptenancyv1alpha.WorkspaceType{ObjectMeta: metav1.ObjectMeta{Name: customAccName}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.client, customAcc, func() error {
		customAcc.Spec = baseAcc.Spec
		return controllerutil.SetOwnerReference(acct, customAcc, r.client.Scheme())
	})
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}

	// Ensure custom org workspace type referencing custom account type as default child
	customOrgName := fmt.Sprintf("%s-org", acct.Name)
	customOrg := &kcptenancyv1alpha.WorkspaceType{ObjectMeta: metav1.ObjectMeta{Name: customOrgName}}
	_, err = controllerutil.CreateOrUpdate(ctx, r.client, customOrg, func() error {
		customOrg.Spec = baseOrg.Spec
		customOrg.Spec.DefaultChildWorkspaceType = &kcptenancyv1alpha.WorkspaceTypeReference{Name: kcptenancyv1alpha.WorkspaceTypeName(customAccName), Path: "root"}
		return controllerutil.SetOwnerReference(acct, customOrg, r.client.Scheme())
	})
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}

	r.limiter.Forget(cn)
	log.Debug().Str("customOrgWorkspaceType", customOrgName).Str("customAccountWorkspaceType", customAccName).Msg("custom workspace types ensured")
	return ctrl.Result{}, nil
}
