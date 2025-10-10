package subroutines

import (
	"context"
	"fmt"
	"testing"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/kontext"

	"github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/account-operator/pkg/subroutines/mocks"
)

func TestFGASubroutine_GetName(t *testing.T) {
	routine := NewFGASubroutine(nil, nil, "", "", "")
	assert.Equal(t, "FGASubroutine", routine.GetName())
}

func TestFGASubroutine_Finalizers(t *testing.T) {
	routine := NewFGASubroutine(nil, nil, "", "", "")
	assert.Equal(t, []string{"account.core.platform-mesh.io/fga"}, routine.Finalizers())
}

func TestFGASubroutine_Process_NoOp(t *testing.T) {
	account := &v1alpha1.Account{
		ObjectMeta: metav1.ObjectMeta{Name: "test-account"},
	}

	clientMock := mocks.NewClient(t)
	fgaMock := mocks.NewOpenFGAServiceClient(t)
	routine := NewFGASubroutine(clientMock, fgaMock, "owner", "parent", "core_platform-mesh_io_account")

	res, err := routine.Process(context.Background(), account)
	assert.Nil(t, err)
	assert.Equal(t, ctrl.Result{}, res)

	fgaMock.AssertNotCalled(t, "Write", mock.Anything, mock.Anything)
	clientMock.AssertNotCalled(t, "Get", mock.Anything, mock.Anything, mock.Anything)
}

func TestFGASubroutine_Finalize_SkipsForOrganizations(t *testing.T) {
	account := &v1alpha1.Account{
		ObjectMeta: metav1.ObjectMeta{Name: "org-account"},
		Spec:       v1alpha1.AccountSpec{Type: v1alpha1.AccountTypeOrg},
	}

	clientMock := mocks.NewClient(t)
	fgaMock := mocks.NewOpenFGAServiceClient(t)
	routine := NewFGASubroutine(clientMock, fgaMock, "owner", "parent", "core_platform-mesh_io_account")

	res, err := routine.Finalize(context.Background(), account)
	assert.Nil(t, err)
	assert.Equal(t, ctrl.Result{}, res)

	fgaMock.AssertNotCalled(t, "Write", mock.Anything, mock.Anything)
	clientMock.AssertNotCalled(t, "Get", mock.Anything, mock.Anything, mock.Anything)
}

func TestFGASubroutine_Finalize_RemovesTuplesForAccounts(t *testing.T) {
	account := &v1alpha1.Account{
		ObjectMeta: metav1.ObjectMeta{Name: "team-account"},
		Spec: v1alpha1.AccountSpec{
			Type:    v1alpha1.AccountTypeAccount,
			Creator: ptr.To("system:serviceaccount:tenant:creator"),
		},
	}

	clientMock := mocks.NewClient(t)
	fgaMock := mocks.NewOpenFGAServiceClient(t)

	clientMock.EXPECT().
		Get(mock.Anything, mock.Anything, mock.Anything).
		RunAndReturn(func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			accountInfo := obj.(*v1alpha1.AccountInfo)
			*accountInfo = v1alpha1.AccountInfo{
				Spec: v1alpha1.AccountInfoSpec{
					FGA: v1alpha1.FGAInfo{
						Store: v1alpha1.StoreInfo{Id: "store-id"},
					},
					Account: v1alpha1.AccountLocation{
						Name:            account.Name,
						OriginClusterId: "origin-cluster",
					},
					ParentAccount: &v1alpha1.AccountLocation{
						OriginClusterId: "parent-cluster",
					},
				},
			}
			return nil
		}).Once()

	expectedParentUser := fmt.Sprintf("core_platform-mesh_io_account:%s/%s", "parent-cluster", account.Name)
	expectedAccountObject := fmt.Sprintf("core_platform-mesh_io_account:%s/%s", "origin-cluster", account.Name)
	expectedOwnerObject := fmt.Sprintf("role:core_platform-mesh_io_account/%s/%s/owner", "origin-cluster", account.Name)
	expectedOwnerUser := fmt.Sprintf("role:core_platform-mesh_io_account/%s/%s/owner#assignee", "origin-cluster", account.Name)

	fgaMock.EXPECT().
		Write(mock.Anything, mock.MatchedBy(func(req *openfgav1.WriteRequest) bool {
			return req.StoreId == "store-id" &&
				req.Deletes != nil &&
				len(req.Deletes.TupleKeys) == 1 &&
				req.Deletes.TupleKeys[0].User == expectedParentUser &&
				req.Deletes.TupleKeys[0].Relation == "parent" &&
				req.Deletes.TupleKeys[0].Object == expectedAccountObject
		})).
		Return(&openfgav1.WriteResponse{}, nil).Once()

	fgaMock.EXPECT().
		Write(mock.Anything, mock.MatchedBy(func(req *openfgav1.WriteRequest) bool {
			return req.StoreId == "store-id" &&
				req.Deletes != nil &&
				len(req.Deletes.TupleKeys) == 1 &&
				req.Deletes.TupleKeys[0].User == "user:system.serviceaccount.tenant.creator" &&
				req.Deletes.TupleKeys[0].Relation == "assignee" &&
				req.Deletes.TupleKeys[0].Object == expectedOwnerObject
		})).
		Return(&openfgav1.WriteResponse{}, nil).Once()

	fgaMock.EXPECT().
		Write(mock.Anything, mock.MatchedBy(func(req *openfgav1.WriteRequest) bool {
			return req.StoreId == "store-id" &&
				req.Deletes != nil &&
				len(req.Deletes.TupleKeys) == 1 &&
				req.Deletes.TupleKeys[0].User == expectedOwnerUser &&
				req.Deletes.TupleKeys[0].Relation == "owner" &&
				req.Deletes.TupleKeys[0].Object == expectedAccountObject
		})).
		Return(&openfgav1.WriteResponse{}, nil).Once()

	routine := NewFGASubroutine(clientMock, fgaMock, "owner", "parent", "core_platform-mesh_io_account")
	ctx := kontext.WithCluster(context.Background(), "child-cluster")

	res, err := routine.Finalize(ctx, account)
	assert.Nil(t, err)
	assert.Equal(t, ctrl.Result{}, res)
}

func TestFGASubroutine_Finalize_ReturnsErrorWhenStoreMissing(t *testing.T) {
	account := &v1alpha1.Account{
		ObjectMeta: metav1.ObjectMeta{Name: "team-account"},
		Spec:       v1alpha1.AccountSpec{Type: v1alpha1.AccountTypeAccount},
	}

	clientMock := mocks.NewClient(t)
	clientMock.EXPECT().
		Get(mock.Anything, mock.Anything, mock.Anything).
		RunAndReturn(func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			accountInfo := obj.(*v1alpha1.AccountInfo)
			*accountInfo = v1alpha1.AccountInfo{
				Spec: v1alpha1.AccountInfoSpec{
					FGA: v1alpha1.FGAInfo{Store: v1alpha1.StoreInfo{Id: ""}},
				},
			}
			return nil
		}).Once()

	fgaMock := mocks.NewOpenFGAServiceClient(t)
	routine := NewFGASubroutine(clientMock, fgaMock, "owner", "parent", "core_platform-mesh_io_account")

	_, err := routine.Finalize(context.Background(), account)
	assert.NotNil(t, err)
}

func TestFGASubroutine_Finalize_PropagatesGetError(t *testing.T) {
	account := &v1alpha1.Account{
		ObjectMeta: metav1.ObjectMeta{Name: "team-account"},
		Spec:       v1alpha1.AccountSpec{Type: v1alpha1.AccountTypeAccount},
	}

	clientMock := mocks.NewClient(t)
	clientMock.EXPECT().
		Get(mock.Anything, mock.Anything, mock.Anything).
		RunAndReturn(func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			return assert.AnError
		}).Once()

	fgaMock := mocks.NewOpenFGAServiceClient(t)
	routine := NewFGASubroutine(clientMock, fgaMock, "owner", "parent", "core_platform-mesh_io_account")

	_, err := routine.Finalize(context.Background(), account)
	assert.NotNil(t, err)
}
