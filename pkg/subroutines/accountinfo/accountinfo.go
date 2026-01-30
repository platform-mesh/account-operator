package accountinfo

import (
	"context"
	"fmt"
	"strings"
	"time"

	kcpcorev1alpha "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/runtimeobject"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/subroutine"
	"github.com/platform-mesh/golang-commons/errors"
	"github.com/platform-mesh/golang-commons/logger"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	"github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/account-operator/pkg/clusteredname"
)

var _ subroutine.Subroutine = (*AccountInfoSubroutine)(nil)

const (
	AccountInfoSubroutineName = "AccountInfoSubroutine"
	DefaultAccountInfoName    = "account"
	AccountInfoFinalizer      = "account.core.platform-mesh.io/info"
)

type AccountInfoSubroutine struct {
	mgr      mcmanager.Manager
	serverCA string
	limiter  workqueue.TypedRateLimiter[clusteredname.ClusteredName]
}

func New(mgr mcmanager.Manager, serverCA string) *AccountInfoSubroutine {
	exp := workqueue.NewTypedItemExponentialFailureRateLimiter[clusteredname.ClusteredName](1*time.Second, 120*time.Second)
	return &AccountInfoSubroutine{mgr: mgr, serverCA: serverCA, limiter: exp}
}

func (r *AccountInfoSubroutine) GetName() string {
	return AccountInfoSubroutineName
}

func (r *AccountInfoSubroutine) Finalizers(_ runtimeobject.RuntimeObject) []string { // coverage-ignore
	return []string{AccountInfoFinalizer}
}

func (r *AccountInfoSubroutine) Finalize(ctx context.Context, ro runtimeobject.RuntimeObject) (ctrl.Result, errors.OperatorError) {
	cn := clusteredname.MustGetClusteredName(ctx, ro)
	log := logger.LoadLoggerFromContext(ctx)
	instance := ro.(*v1alpha1.Account)

	// Test if there are further child accounts in this workspace, if yes then don't remove the accountinfo finalizer yet
	cluster, err := r.mgr.ClusterFromContext(ctx)
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("getting cluster from context: %w", err), true, true)
	}

	clusterClient := cluster.GetClient()
	accountWorkspace := &kcptenancyv1alpha.Workspace{}
	if err := clusterClient.Get(ctx, client.ObjectKey{Name: instance.Name}, accountWorkspace); err != nil {
		// the workspace inclusive contained accountinfo was deleted, finalization complete
		if kerrors.IsNotFound(err) || meta.IsNoMatchError(err) {
			r.limiter.Forget(cn)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("getting Account's Workspace: %w", err), true, true)
	}

	accountCluster, err := r.mgr.GetCluster(ctx, accountWorkspace.Spec.Cluster)
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}
	accountClusterClient := accountCluster.GetClient()

	list := &v1alpha1.AccountList{}
	err = accountClusterClient.List(ctx, list, &client.ListOptions{})
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("listing child accounts: %w", err), true, true)
	}
	if len(list.Items) > 0 {
		log.Info().Msgf("Found %d accounts", len(list.Items))
		return ctrl.Result{RequeueAfter: r.limiter.When(cn)}, nil
	}
	log.Info().Msgf("No accounts found in cluster")

	accountInfo := &v1alpha1.AccountInfo{ObjectMeta: v1.ObjectMeta{Name: DefaultAccountInfoName}}
	err = accountClusterClient.Get(ctx, client.ObjectKey{Name: DefaultAccountInfoName}, accountInfo)
	if err != nil {
		if kerrors.IsNotFound(err) {
			r.limiter.Forget(cn)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("updating AccountInfo to remove finalizer: %w", err), true, true)
	}

	// Remove finalizer on an org's AccountInfo after cleanup
	if controllerutil.ContainsFinalizer(accountInfo, AccountInfoFinalizer) {
		controllerutil.RemoveFinalizer(accountInfo, AccountInfoFinalizer)
		if err := accountClusterClient.Update(ctx, accountInfo); err != nil {
			return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("updating AccountInfo to remove finalizer: %w", err), true, true)
		}
	}

	// The account info object is relevant input for other finalizers, removing the accountinfo finalizer at last
	if len(ro.GetFinalizers()) > 1 {
		return ctrl.Result{RequeueAfter: r.limiter.When(cn)}, nil
	}

	r.limiter.Forget(cn)
	return ctrl.Result{}, nil
}

func (r *AccountInfoSubroutine) Process(ctx context.Context, ro runtimeobject.RuntimeObject) (ctrl.Result, errors.OperatorError) {
	instance := ro.(*v1alpha1.Account)

	log := logger.LoadLoggerFromContext(ctx)
	cn := clusteredname.MustGetClusteredName(ctx, ro)

	clusterRef, err := r.mgr.GetCluster(ctx, string(cn.ClusterID))
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("getting cluster: %w", err), true, true)
	}
	clusterClient := clusterRef.GetClient()

	accountWorkspace := &kcptenancyv1alpha.Workspace{}
	if err := clusterClient.Get(ctx, client.ObjectKey{Name: instance.Name}, accountWorkspace); err != nil {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("getting Account's Workspace: %w", err), true, true)
	}

	if accountWorkspace.Status.Phase != kcpcorev1alpha.LogicalClusterPhaseInitializing && accountWorkspace.Status.Phase != kcpcorev1alpha.LogicalClusterPhaseReady {
		log.Info().Msg("workspace is not ready yet, retry")
		return ctrl.Result{RequeueAfter: r.limiter.When(cn)}, nil
	}

	// Retrieve logical cluster
	currentWorkspacePath, currentWorkspaceUrl, err := r.retrieveCurrentWorkspacePath(accountWorkspace)
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}

	selfAccountLocation := v1alpha1.AccountLocation{
		Name:               instance.Name,
		GeneratedClusterId: accountWorkspace.Spec.Cluster,
		OriginClusterId:    string(cn.ClusterID),
		Type:               instance.Spec.Type,
		Path:               currentWorkspacePath,
		URL:                currentWorkspaceUrl,
	}

	accountCluster, err := r.mgr.GetCluster(ctx, accountWorkspace.Spec.Cluster)
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}
	accountClusterClient := accountCluster.GetClient()

	// Create AccountInfo for an organization
	if instance.Spec.Type == v1alpha1.AccountTypeOrg {
		accountInfo := &v1alpha1.AccountInfo{ObjectMeta: v1.ObjectMeta{Name: DefaultAccountInfoName}}
		if _, err := controllerutil.CreateOrPatch(ctx, accountClusterClient, accountInfo, func() error {
			if !controllerutil.ContainsFinalizer(accountInfo, AccountInfoFinalizer) {
				accountInfo.Finalizers = append(accountInfo.Finalizers, AccountInfoFinalizer)
			}
			// the .Spec.FGA.Store.ID is set from an external workspace initializer
			accountInfo.Spec.Account = selfAccountLocation
			accountInfo.Spec.ParentAccount = nil
			accountInfo.Spec.Organization = selfAccountLocation
			accountInfo.Spec.ClusterInfo.CA = r.serverCA
			return nil
		}); err != nil {
			return ctrl.Result{}, errors.NewOperatorError(err, true, true)
		}

		r.limiter.Forget(cn)
		return ctrl.Result{}, nil
	}

	// Create AccountInfo for a non-organization Account based on its parent's
	// AccountInfo
	var parentAccountInfo v1alpha1.AccountInfo
	if err := clusterClient.Get(ctx, client.ObjectKey{Name: DefaultAccountInfoName}, &parentAccountInfo); kerrors.IsNotFound(err) {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("AccountInfo does not yet exist"), true, false)
	} else if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("getting parent AccountInfo: %w", err), true, true)
	}

	accountInfo := &v1alpha1.AccountInfo{ObjectMeta: v1.ObjectMeta{Name: DefaultAccountInfoName}}
	if _, err := controllerutil.CreateOrUpdate(ctx, accountClusterClient, accountInfo, func() error {
		if !controllerutil.ContainsFinalizer(accountInfo, AccountInfoFinalizer) {
			accountInfo.Finalizers = append(accountInfo.Finalizers, AccountInfoFinalizer)
		}
		accountInfo.Spec.Account = selfAccountLocation
		accountInfo.Spec.ParentAccount = &parentAccountInfo.Spec.Account
		accountInfo.Spec.Organization = parentAccountInfo.Spec.Organization
		accountInfo.Spec.FGA.Store.Id = parentAccountInfo.Spec.FGA.Store.Id
		accountInfo.Spec.OIDC = parentAccountInfo.Spec.OIDC
		accountInfo.Spec.ClusterInfo.CA = r.serverCA
		return nil
	}); err != nil {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("creating or updating AccountInfo %w", err), true, true)
	}

	r.limiter.Forget(cn)
	return ctrl.Result{}, nil
}

func (r *AccountInfoSubroutine) retrieveCurrentWorkspacePath(ws *kcptenancyv1alpha.Workspace) (string, string, error) {
	if ws.Spec.URL == "" {
		return "", "", fmt.Errorf("workspace URL is empty")
	}

	// Parse path from URL
	split := strings.Split(ws.Spec.URL, "/")
	if len(split) < 3 {
		return "", "", fmt.Errorf("workspace URL is invalid")
	}

	lastSegment := split[len(split)-1]
	if lastSegment == "" || strings.TrimSpace(lastSegment) == "" {
		return "", "", fmt.Errorf("workspace URL is empty")
	}
	return lastSegment, ws.Spec.URL, nil
}
