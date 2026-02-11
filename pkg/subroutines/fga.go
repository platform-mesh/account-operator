package subroutines

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/platform-mesh/golang-commons/controller/lifecycle/ratelimiter"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/runtimeobject"
	"github.com/platform-mesh/golang-commons/errors"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	kcptenancyv1alpha "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
	"github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/account-operator/pkg/subroutines/manageaccountinfo"
	securityv1alpha1 "github.com/platform-mesh/security-operator/api/v1alpha1"

	"github.com/platform-mesh/security-operator/pkg/fga"
)

const fgaFinalizer = "account.core.platform-mesh.io/fga"

type FGASubroutine struct {
	mgr             mcmanager.Manager
	objectType      string
	parentRelation  string
	creatorRelation string

	orgsClient client.Client
	limiter    workqueue.TypedRateLimiter[*v1alpha1.Account]
}

func NewFGASubroutine(mgr mcmanager.Manager, orgsClient client.Client, creatorRelation, parentRelation, objectType string) *FGASubroutine {
	rcfg := ratelimiter.NewConfig()
	rcfg.StaticWindow = 5 * time.Minute
	rcfg.ExponentialMaxBackoff = 15 * time.Minute
	limiter, _ := ratelimiter.NewStaticThenExponentialRateLimiter[*v1alpha1.Account](rcfg) //nolint:errcheck
	return &FGASubroutine{
		mgr:             mgr,
		creatorRelation: creatorRelation,
		parentRelation:  parentRelation,
		objectType:      objectType,
		orgsClient:      orgsClient,
		limiter:         limiter,
	}
}

// Process does nothing, this subroutine is for finalization only.
func (e *FGASubroutine) Process(ctx context.Context, ro runtimeobject.RuntimeObject) (ctrl.Result, errors.OperatorError) {
	return ctrl.Result{}, nil
}

func (e *FGASubroutine) Finalize(ctx context.Context, runtimeObj runtimeobject.RuntimeObject) (ctrl.Result, errors.OperatorError) {
	account := runtimeObj.(*v1alpha1.Account)

	// Skip fga account finalization for organizations because the store is removed completely
	if account.Spec.Type == v1alpha1.AccountTypeOrg {
		return ctrl.Result{}, nil
	}

	clusterName, ok := mccontext.ClusterFrom(ctx)
	if !ok {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("cluster client not available: ensure context carries cluster information"), true, true)
	}

	clusterRef, err := e.mgr.GetCluster(ctx, clusterName)
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}
	clusterClient := clusterRef.GetClient()

	parentAccountInfo, err := e.getAccountInfo(ctx, clusterClient)
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}

	var ws kcptenancyv1alpha.Workspace
	if err := clusterClient.Get(ctx, client.ObjectKey{
		Name: account.Name,
	}, &ws); err != nil {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("getting Account's Workspace: %w", err), true, true)
	}
	accountClusterRef, err := e.mgr.GetCluster(ctx, ws.Spec.Cluster)
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}
	accountClusterClient := accountClusterRef.GetClient()
	accountAccountInfo, err := e.getAccountInfo(ctx, accountClusterClient)
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}

	var st securityv1alpha1.Store
	if err := e.orgsClient.Get(ctx, client.ObjectKey{
		Name: parentAccountInfo.Spec.Organization.Name,
	}, &st); err != nil {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("getting parent organisation's Store: %w", err), true, true)
	}

	tuples, err := fga.TuplesForAccount(*account, *accountAccountInfo, e.creatorRelation, e.parentRelation, e.objectType)
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("building tuples for account: %w", err), true, true)
	}
	st.Spec.Tuples = slices.DeleteFunc(st.Spec.Tuples, func(t securityv1alpha1.Tuple) bool {
		return slices.Contains(tuples, t)
	})
	if err := e.orgsClient.Update(ctx, &st); err != nil {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("updating Store with tuples: %w", err), true, true)
	}

	return ctrl.Result{}, nil
}

func (e *FGASubroutine) getAccountInfo(ctx context.Context, cl client.Client) (*v1alpha1.AccountInfo, error) {
	accountInfo := &v1alpha1.AccountInfo{}
	err := cl.Get(ctx, client.ObjectKey{Name: manageaccountinfo.DefaultAccountInfoName}, accountInfo)
	if err != nil {
		return nil, err
	}
	return accountInfo, nil
}

func (e *FGASubroutine) GetName() string { return "FGASubroutine" }

func (e *FGASubroutine) Finalizers(runtimeObj runtimeobject.RuntimeObject) []string {
	account := runtimeObj.(*v1alpha1.Account)

	// Skip fga account finalization for organizations because the store is removed completely
	if account.Spec.Type != v1alpha1.AccountTypeOrg {
		return []string{fgaFinalizer}
	}
	return []string{}
}

var saRegex = regexp.MustCompile(`^system:serviceaccount:[^:]*:[^:]*$`)

// formatUser formats the user string to be used in the FGA write request
// it replaces colons for users conforming to the kubernetes service account pattern with dots.
func formatUser(user string) string {
	if saRegex.MatchString(user) {
		return strings.ReplaceAll(user, ":", ".")
	}
	return user
}

// validateCreator validates the creator string to ensure if it is not in the service account prefix range
func validateCreator(creator string) bool {
	return !strings.HasPrefix(creator, "system:serviceaccount:")
}
