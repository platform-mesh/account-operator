package finalizeaccountinfo

import (
	"context"
	"fmt"

	"github.com/platform-mesh/golang-commons/controller/lifecycle/ratelimiter"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/runtimeobject"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/subroutine"
	"github.com/platform-mesh/golang-commons/errors"
	"github.com/platform-mesh/golang-commons/logger"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	"github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/account-operator/pkg/clusteredname"
)

var _ subroutine.Subroutine = (*FinalizeAccountInfoSubroutine)(nil)

const (
	FinalizeAccountInfoSubroutineName = "FinalizeAccountInfoSubroutine"
	AccountInfoFinalizer              = "account.core.platform-mesh.io/info"
)

type FinalizeAccountInfoSubroutine struct {
	mgr     mcmanager.Manager
	limiter workqueue.TypedRateLimiter[clusteredname.ClusteredName]
}

func New(mgr mcmanager.Manager) *FinalizeAccountInfoSubroutine {
	rl, _ := ratelimiter.NewStaticThenExponentialRateLimiter[clusteredname.ClusteredName](ratelimiter.NewConfig()) //nolint:errcheck
	return &FinalizeAccountInfoSubroutine{mgr: mgr, limiter: rl}
}

func (r *FinalizeAccountInfoSubroutine) GetName() string {
	return FinalizeAccountInfoSubroutineName
}

func (r *FinalizeAccountInfoSubroutine) Finalizers(_ runtimeobject.RuntimeObject) []string { // coverage-ignore
	return []string{AccountInfoFinalizer}
}

func (r *FinalizeAccountInfoSubroutine) Process(_ context.Context, _ runtimeobject.RuntimeObject) (ctrl.Result, errors.OperatorError) {
	return ctrl.Result{}, nil
}

func (r *FinalizeAccountInfoSubroutine) Finalize(ctx context.Context, ro runtimeobject.RuntimeObject) (ctrl.Result, errors.OperatorError) {
	cn := clusteredname.MustGetClusteredName(ctx, ro)
	log := logger.LoadLoggerFromContext(ctx)

	cluster, err := r.mgr.ClusterFromContext(ctx)
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("getting cluster from context: %w", err), true, true)
	}
	clusterClient := cluster.GetClient()

	list := &v1alpha1.AccountList{}
	if err := clusterClient.List(ctx, list, &client.ListOptions{}); err != nil {
		if !kerrors.IsNotFound(err) && !meta.IsNoMatchError(err) {
			return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("listing child accounts: %w", err), true, true)
		}
	}

	if len(list.Items) > 0 {
		log.Info().Msgf("Found %d accounts, cannot finalize AccountInfo yet", len(list.Items))
		return ctrl.Result{RequeueAfter: r.limiter.When(cn)}, nil
	}

	log.Info().Msg("No accounts found in cluster, AccountInfo can be finalized")

	r.limiter.Forget(cn)
	return ctrl.Result{}, nil
}
