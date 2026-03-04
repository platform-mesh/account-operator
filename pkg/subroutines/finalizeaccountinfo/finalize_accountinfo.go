package finalizeaccountinfo

import (
	"context"
	"fmt"
	"time"

	"github.com/platform-mesh/subroutines"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	"github.com/platform-mesh/account-operator/api/v1alpha1"
)

var _ subroutines.Finalizer = (*FinalizeAccountInfoSubroutine)(nil)

const (
	FinalizeAccountInfoSubroutineName = "FinalizeAccountInfoSubroutine"
	AccountInfoFinalizer              = "account.core.platform-mesh.io/info"
)

type FinalizeAccountInfoSubroutine struct {
	mgr     mcmanager.Manager
	limiter workqueue.TypedRateLimiter[*v1alpha1.AccountInfo]
}

func New(mgr mcmanager.Manager) *FinalizeAccountInfoSubroutine {
	return &FinalizeAccountInfoSubroutine{
		mgr:     mgr,
		limiter: workqueue.NewTypedItemExponentialFailureRateLimiter[*v1alpha1.AccountInfo](1*time.Second, 120*time.Second),
	}
}

func (r *FinalizeAccountInfoSubroutine) GetName() string {
	return FinalizeAccountInfoSubroutineName
}

func (r *FinalizeAccountInfoSubroutine) Finalizers(_ client.Object) []string { // coverage-ignore
	return []string{AccountInfoFinalizer}
}

func (r *FinalizeAccountInfoSubroutine) Finalize(ctx context.Context, obj client.Object) (subroutines.Result, error) {
	instance := obj.(*v1alpha1.AccountInfo)
	logger := log.FromContext(ctx)

	cluster, err := r.mgr.ClusterFromContext(ctx)
	if err != nil {
		return subroutines.OK(), fmt.Errorf("getting cluster from context: %w", err)
	}
	clusterClient := cluster.GetClient()

	list := &v1alpha1.AccountList{}
	if err := clusterClient.List(ctx, list, &client.ListOptions{}); err != nil {
		if !kerrors.IsNotFound(err) && !meta.IsNoMatchError(err) {
			return subroutines.OK(), fmt.Errorf("listing child accounts: %w", err)
		}
	}

	if len(list.Items) > 0 {
		logger.Info("cannot finalize AccountInfo yet", "accountCount", len(list.Items))
		return subroutines.OKWithRequeue(r.limiter.When(instance)), nil
	}

	logger.Info("no accounts found in cluster, AccountInfo can be finalized")

	r.limiter.Forget(instance)
	return subroutines.OK(), nil
}
