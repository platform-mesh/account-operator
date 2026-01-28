package fga

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	openfga "github.com/openfga/go-sdk"
	"github.com/platform-mesh/account-operator/api/v1alpha1"
)

// type TupleManager struct {
// 	c *openfgav1.OpenFGAServiceClient
// }

// // Apply creates or replaces a Tuple in OpenFGA, returning whether the tuple already existed and an optional error.
// func (m *TupleManager) Apply(t *openfgav1.TupleKey) (bool, error) {

// }

// func (m *TupleManager) Delete(t *openfgav1.TupleKey)

// TuplesForAccount constructs all Tuples to be created for a given Account.
func TuplesForAccount(a v1alpha1.Account, ai v1alpha1.AccountInfo, objectType, assigneeRelation, parentRelation, creatorRelation string) ([]openfga.Tuple, error) {
	tuples := []openfga.Tuple{}
	timestamp := time.Now()

	// Parent Relation Tuple (if account is not an org)
	if a.Spec.Type != v1alpha1.AccountTypeOrg {
		parentAccountName := ai.Spec.ParentAccount.Name

		tupleKey := openfga.NewTupleKey(
			fmt.Sprintf("%s:%s/%s", objectType, ai.Spec.ParentAccount.OriginClusterId, parentAccountName),
			parentRelation,
			fmt.Sprintf("%s:%s/%s", objectType, ai.Spec.Account.OriginClusterId, a.GetName()),
		)
		tuples = append(tuples, *openfga.NewTuple(*tupleKey, timestamp))
	}

	if a.Spec.Creator != nil {
		if !validateCreator(*a.Spec.Creator) {
			return nil, fmt.Errorf("creator in protected service account range")
		}
		creator := formatUser(*a.Spec.Creator)

		assigneeTupleKey := openfga.NewTupleKey(
			fmt.Sprintf("user:%s", creator),
			assigneeRelation,
			fmt.Sprintf("role:%s/%s/%s/owner", objectType, ai.Spec.Account.OriginClusterId, a.Name),
		)
		tuples = append(tuples, *openfga.NewTuple(*assigneeTupleKey, timestamp))

		// Creator Tuple
		creatorTupleKey := openfga.NewTupleKey(
			fmt.Sprintf("role:%s/%s/%s/owner#assignee", objectType, ai.Spec.Account.OriginClusterId, a.Name),
			creatorRelation,
			fmt.Sprintf("%s:%s/%s", objectType, ai.Spec.Account.OriginClusterId, a.Name),
		)
		tuples = append(tuples, *openfga.NewTuple(*creatorTupleKey, timestamp))
	}

	return tuples, nil
}

var saRegex = regexp.MustCompile(`^system:serviceaccount:[^:]*:[^:]*$`)

// formatUser formats the user string to be used in the FGA write request it
// replaces colons for users conforming to the kubernetes service account
// pattern with dots.
func formatUser(user string) string {
	if saRegex.MatchString(user) {
		return strings.ReplaceAll(user, ":", ".")
	}
	return user
}

// validateCreator validates the creator string to ensure if it is not in the
// service account prefix range
func validateCreator(creator string) bool {
	return !strings.HasPrefix(creator, "system:serviceaccount:")
}
