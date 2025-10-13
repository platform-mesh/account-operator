package subroutines

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/runtimeobject"
	"github.com/platform-mesh/golang-commons/errors"
	"github.com/platform-mesh/golang-commons/fga/helpers"
	"github.com/platform-mesh/golang-commons/logger"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	"github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/account-operator/pkg/subroutines/accountinfo"
)

type FGASubroutine struct {
	fgaClient       openfgav1.OpenFGAServiceClient
	mgr             mcmanager.Manager
	objectType      string
	parentRelation  string
	creatorRelation string
}

func NewFGASubroutine(mgr mcmanager.Manager, fgaClient openfgav1.OpenFGAServiceClient, creatorRelation, parentRelation, objectType string) *FGASubroutine {
	return &FGASubroutine{
		mgr:             mgr,
		fgaClient:       fgaClient,
		creatorRelation: creatorRelation,
		parentRelation:  parentRelation,
		objectType:      objectType,
	}
}

func (e *FGASubroutine) Process(ctx context.Context, _ runtimeobject.RuntimeObject) (ctrl.Result, errors.OperatorError) {
	log := logger.LoadLoggerFromContext(ctx)
	log.Debug().Msg("Skipping FGASubroutine.Process during initialization; handled by workspace initializer")
	return ctrl.Result{}, nil
}

func (e *FGASubroutine) Finalize(ctx context.Context, runtimeObj runtimeobject.RuntimeObject) (ctrl.Result, errors.OperatorError) {
	account := runtimeObj.(*v1alpha1.Account)
	log := logger.LoadLoggerFromContext(ctx)

	// Skip fga account finalization for organizations because the store is removed completely
	if account.Spec.Type != v1alpha1.AccountTypeOrg {
		clusterName, ok := mccontext.ClusterFrom(ctx)
		if !ok {
			return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("cluster client not available: ensure context carries cluster information"), true, true)
		}

		clusterRef, err := e.mgr.GetCluster(ctx, clusterName)
		if err != nil {
			return ctrl.Result{}, errors.NewOperatorError(err, true, true)
		}
		clusterClient := clusterRef.GetClient()

		accountInfo, err := e.getAccountInfo(ctx, clusterClient)
		if err != nil {
			log.Error().Err(err).Msg("Couldn't get Store Id")
			return ctrl.Result{}, errors.NewOperatorError(err, true, true)
		}

		if accountInfo.Spec.FGA.Store.Id == "" {
			log.Error().Msg("FGA Store Id is empty")
			return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("FGA Store Id is empty"), true, true)
		}

		deletes := []*openfgav1.TupleKeyWithoutCondition{}
		if account.Spec.Type != v1alpha1.AccountTypeOrg {
			parentAccountName := accountInfo.Spec.Account.Name

			deletes = append(deletes, &openfgav1.TupleKeyWithoutCondition{
				User:     fmt.Sprintf("%s:%s/%s", e.objectType, accountInfo.Spec.ParentAccount.OriginClusterId, parentAccountName),
				Relation: e.parentRelation,
				Object:   fmt.Sprintf("%s:%s/%s", e.objectType, accountInfo.Spec.Account.OriginClusterId, account.GetName()),
			})
		}

		if account.Spec.Creator != nil {
			creator := formatUser(*account.Spec.Creator)
			deletes = append(deletes, &openfgav1.TupleKeyWithoutCondition{
				User:     fmt.Sprintf("user:%s", creator),
				Relation: "assignee",
				Object:   fmt.Sprintf("role:%s/%s/%s/owner", e.objectType, accountInfo.Spec.Account.OriginClusterId, account.Name),
			})

			deletes = append(deletes, &openfgav1.TupleKeyWithoutCondition{
				User:     fmt.Sprintf("role:%s/%s/%s/owner#assignee", e.objectType, accountInfo.Spec.Account.OriginClusterId, account.Name),
				Relation: e.creatorRelation,
				Object:   fmt.Sprintf("%s:%s/%s", e.objectType, accountInfo.Spec.Account.OriginClusterId, account.Name),
			})
		}

		for _, deleteTuple := range deletes {

			_, err = e.fgaClient.Write(ctx, &openfgav1.WriteRequest{
				StoreId: accountInfo.Spec.FGA.Store.Id,
				Deletes: &openfgav1.WriteRequestDeletes{
					TupleKeys: []*openfgav1.TupleKeyWithoutCondition{deleteTuple},
				},
			})

			if helpers.IsDuplicateWriteError(err) {
				log.Info().Err(err).Msg("Open FGA write failed due to invalid input (possibly trying to deleteTuple nonexisting entry)")
				err = nil
			}

			if err != nil {
				log.Error().Err(err).Msg("Open FGA write failed")
				return ctrl.Result{}, errors.NewOperatorError(err, true, true)
			}

		}
	}

	return ctrl.Result{}, nil
}

func (e *FGASubroutine) getAccountInfo(ctx context.Context, cl client.Client) (*v1alpha1.AccountInfo, error) {
	accountInfo := &v1alpha1.AccountInfo{}
	err := cl.Get(ctx, client.ObjectKey{Name: accountinfo.DefaultAccountInfoName}, accountInfo)
	if err != nil {
		return nil, err
	}
	return accountInfo, nil
}

func (e *FGASubroutine) GetName() string { return "FGASubroutine" }

func (e *FGASubroutine) Finalizers(_ runtimeobject.RuntimeObject) []string {
	return []string{"account.core.platform-mesh.io/fga"}
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
