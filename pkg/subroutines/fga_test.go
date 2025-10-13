package subroutines

import (
	"context"
	"testing"
	"time"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	pmconfig "github.com/platform-mesh/golang-commons/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"

	"github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/account-operator/internal/config"
	"github.com/platform-mesh/account-operator/pkg/subroutines/accountinfo"
	"github.com/platform-mesh/account-operator/pkg/subroutines/mocks"
)

func TestFGASubroutine_Finalizers(t *testing.T) {
	routine := NewFGASubroutine(nil, nil, "owner", "parent", "core_platform-mesh_io_account")
	assert.Equal(t, []string{"account.core.platform-mesh.io/fga"}, routine.Finalizers(nil))
}

func TestFGASubroutine_ProcessNoOp(t *testing.T) {
	routine := NewFGASubroutine(nil, nil, "owner", "parent", "core_platform-mesh_io_account")
	res, err := routine.Process(context.Background(), &v1alpha1.Account{})
	assert.Nil(t, err)
	assert.Equal(t, ctrl.Result{}, res)
}

func TestFGASubroutine_Finalize_RequeuesWhenChildAccountsExist(t *testing.T) {
	mgr := mocks.NewManager(t)
	cluster := mocks.NewCluster(t)
	clientMock := mocks.NewClient(t)

	mgr.EXPECT().GetCluster(mock.Anything, "root:orgs").Return(cluster, nil)
	cluster.EXPECT().GetClient().Return(clientMock)

	clientMock.EXPECT().List(mock.Anything, mock.AnythingOfType("*github.com/platform-mesh/account-operator/api/v1alpha1.AccountList"), mock.Anything).RunAndReturn(
		func(ctx context.Context, list client.ObjectList, _ ...client.ListOption) error {
			l := list.(*v1alpha1.AccountList)
			l.Items = append(l.Items, v1alpha1.Account{})
			return nil
		},
	)

	fgaMock := mocks.NewOpenFGAServiceClient(t)
	routine := NewFGASubroutine(mgr, fgaMock, "owner", "parent", "core_platform-mesh_io_account")

	baseCtx := pmconfig.SetConfigInContext(context.Background(), config.OperatorConfig{})
	ctx := mccontext.WithCluster(baseCtx, "root:orgs")

	account := &v1alpha1.Account{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Finalizers: []string{"account.core.platform-mesh.io/fga"}},
		Spec:       v1alpha1.AccountSpec{Type: v1alpha1.AccountTypeAccount},
	}

	res, err := routine.Finalize(ctx, account)
	require.Nil(t, err)
	assert.Greater(t, res.RequeueAfter, 0*time.Second)
}

func TestFGASubroutine_Finalize_RemovesTuples(t *testing.T) {
	mgr := mocks.NewManager(t)
	cluster := mocks.NewCluster(t)
	clientMock := mocks.NewClient(t)

	mgr.EXPECT().GetCluster(mock.Anything, "root:orgs").Return(cluster, nil)
	cluster.EXPECT().GetClient().Return(clientMock)

	clientMock.EXPECT().List(mock.Anything, mock.AnythingOfType("*github.com/platform-mesh/account-operator/api/v1alpha1.AccountList"), mock.Anything).RunAndReturn(
		func(ctx context.Context, list client.ObjectList, _ ...client.ListOption) error {
			return nil
		},
	)

	clientMock.EXPECT().Get(mock.Anything, types.NamespacedName{Name: accountinfo.DefaultAccountInfoName}, mock.Anything).RunAndReturn(
		func(ctx context.Context, key types.NamespacedName, obj client.Object, _ ...client.GetOption) error {
			info := obj.(*v1alpha1.AccountInfo)
			info.Spec = v1alpha1.AccountInfoSpec{
				Account: v1alpha1.AccountLocation{
					Name:               "demo",
					OriginClusterId:    "root:orgs",
					GeneratedClusterId: "account-cluster",
				},
				ParentAccount: &v1alpha1.AccountLocation{
					Name:            "parent",
					OriginClusterId: "root:orgs",
				},
				FGA: v1alpha1.FGAInfo{Store: v1alpha1.StoreInfo{Id: "store-1"}},
			}
			return nil
		},
	)

	fgaMock := mocks.NewOpenFGAServiceClient(t)
	fgaMock.EXPECT().Write(mock.Anything, mock.Anything).Return(&openfgav1.WriteResponse{}, nil).Times(2)

	routine := NewFGASubroutine(mgr, fgaMock, "owner", "parent", "core_platform-mesh_io_account")

	baseCtx := pmconfig.SetConfigInContext(context.Background(), config.OperatorConfig{})
	ctx := mccontext.WithCluster(baseCtx, "root:orgs")

	account := &v1alpha1.Account{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Finalizers: []string{"account.core.platform-mesh.io/fga"}},
		Spec:       v1alpha1.AccountSpec{Type: v1alpha1.AccountTypeAccount, Creator: ptr.To("alice")},
	}

	res, err := routine.Finalize(ctx, account)
	require.Nil(t, err)
	assert.Equal(t, ctrl.Result{}, res)
}
