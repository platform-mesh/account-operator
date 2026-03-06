package manageaccountinfo

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"time"

	kcpcorev1alpha "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
	"github.com/platform-mesh/subroutines"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	"github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/account-operator/pkg/clusteredname"

	ctrl "sigs.k8s.io/controller-runtime"
)

var _ subroutines.Processor = (*ManageAccountInfoSubroutine)(nil)

const (
	ManageAccountInfoSubroutineName = "ManageAccountInfoSubroutine"
	DefaultAccountInfoName          = "account"
)

type ManageAccountInfoSubroutine struct {
	mgr      mcmanager.Manager
	serverCA string
	limiter  workqueue.TypedRateLimiter[*v1alpha1.Account]
}

func New(mgr mcmanager.Manager, serverCA string) *ManageAccountInfoSubroutine {
	exp := workqueue.NewTypedItemExponentialFailureRateLimiter[*v1alpha1.Account](1*time.Second, 120*time.Second)
	return &ManageAccountInfoSubroutine{mgr: mgr, serverCA: serverCA, limiter: exp}
}

func (r *ManageAccountInfoSubroutine) GetName() string {
	return ManageAccountInfoSubroutineName
}

func (r *ManageAccountInfoSubroutine) Process(ctx context.Context, obj client.Object) (subroutines.Result, error) {
	instance := obj.(*v1alpha1.Account)

	logger := log.FromContext(ctx)
	cn := clusteredname.MustGetClusteredName(ctx, obj)

	clusterRef, err := r.mgr.GetCluster(ctx, string(cn.ClusterID))
	if err != nil {
		return subroutines.OK(), fmt.Errorf("getting cluster: %w", err)
	}
	clusterClient := clusterRef.GetClient()

	accountWorkspace := &kcptenancyv1alpha.Workspace{}
	if err := clusterClient.Get(ctx, client.ObjectKey{Name: instance.Name}, accountWorkspace); err != nil {
		return subroutines.OK(), fmt.Errorf("getting Account's Workspace: %w", err)
	}

	if accountWorkspace.Status.Phase != kcpcorev1alpha.LogicalClusterPhaseInitializing && accountWorkspace.Status.Phase != kcpcorev1alpha.LogicalClusterPhaseReady {
		logger.Info("workspace is not ready yet, retry")
		return subroutines.OKWithRequeue(r.limiter.When(instance)), nil
	}

	// Retrieve logical cluster
	currentWorkspacePath, currentWorkspaceUrl, err := r.retrieveCurrentWorkspacePath(accountWorkspace)
	if err != nil {
		return subroutines.OK(), err
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
		return subroutines.OK(), err
	}
	accountClusterClient := accountCluster.GetClient()

	// Create AccountInfo for an organization
	if instance.Spec.Type == v1alpha1.AccountTypeOrg {
		accountInfo := &v1alpha1.AccountInfo{ObjectMeta: ctrl.ObjectMeta{Name: DefaultAccountInfoName}}
		if _, err := controllerutil.CreateOrPatch(ctx, accountClusterClient, accountInfo, func() error {
			// the .Spec.FGA.Store.ID is set from an external workspace initializer
			accountInfo.Spec.Account = selfAccountLocation
			accountInfo.Spec.ParentAccount = nil
			accountInfo.Spec.Organization = selfAccountLocation
			accountInfo.Spec.ClusterInfo.CA = r.serverCA
			return nil
		}); err != nil {
			return subroutines.OK(), err
		}

		r.limiter.Forget(instance)
		return subroutines.OK(), nil
	}

	// Create AccountInfo for a non-organization Account based on its parent's
	// AccountInfo
	var parentAccountInfo v1alpha1.AccountInfo
	if err := clusterClient.Get(ctx, client.ObjectKey{Name: DefaultAccountInfoName}, &parentAccountInfo); kerrors.IsNotFound(err) {
		logger.Info("parent AccountInfo does not yet exist, retry")
		return subroutines.OKWithRequeue(r.limiter.When(instance)), nil
	} else if err != nil {
		return subroutines.OK(), fmt.Errorf("getting parent AccountInfo: %w", err)
	}

	accountInfo := &v1alpha1.AccountInfo{ObjectMeta: v1.ObjectMeta{Name: DefaultAccountInfoName}}
	if _, err := controllerutil.CreateOrUpdate(ctx, accountClusterClient, accountInfo, func() error {
		accountInfo.Spec.Account = selfAccountLocation
		accountInfo.Spec.ParentAccount = &parentAccountInfo.Spec.Account
		accountInfo.Spec.Organization = parentAccountInfo.Spec.Organization
		accountInfo.Spec.FGA.Store.Id = parentAccountInfo.Spec.FGA.Store.Id
		accountInfo.Spec.OIDC = parentAccountInfo.Spec.OIDC
		accountInfo.Spec.ClusterInfo.CA = r.serverCA
		return nil
	}); err != nil {
		return subroutines.OK(), fmt.Errorf("creating or updating AccountInfo %w", err)
	}

	r.limiter.Forget(instance)
	return subroutines.OK(), nil
}

func (r *ManageAccountInfoSubroutine) retrieveCurrentWorkspacePath(ws *kcptenancyv1alpha.Workspace) (string, string, error) {
	if ws.Spec.URL == "" {
		return "", "", fmt.Errorf("workspace URL is empty")
	}

	parsed, err := url.Parse(ws.Spec.URL)
	if err != nil {
		return "", "", fmt.Errorf("parsing workspace URL: %w", err)
	}

	lastSegment := path.Base(parsed.Path)
	if lastSegment == "" || lastSegment == "." || lastSegment == "/" {
		return "", "", fmt.Errorf("workspace URL has no path segment")
	}
	return lastSegment, ws.Spec.URL, nil
}
