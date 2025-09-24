package subroutines_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	kcptypes "github.com/platform-mesh/account-operator/pkg/types"
	platformmeshcontext "github.com/platform-mesh/golang-commons/context"
	"github.com/platform-mesh/golang-commons/logger"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/account-operator/internal/config"
	"github.com/platform-mesh/account-operator/pkg/subroutines"
	"github.com/platform-mesh/account-operator/pkg/subroutines/mocks"
)

type AccountInfoSubroutineTestSuite struct {
	suite.Suite

	// Tested Object(s)
	testObj *subroutines.AccountInfoSubroutine

	// Mocks
	clientMock *mocks.Client
	context    context.Context
	log        *logger.Logger
}

func (suite *AccountInfoSubroutineTestSuite) SetupTest() {
	// Setup Mocks
	suite.clientMock = new(mocks.Client)

	// Initialize Tested Object(s)
	suite.testObj = subroutines.NewAccountInfoSubroutine(suite.clientMock, "some-ca")

	utilruntime.Must(v1alpha1.AddToScheme(scheme.Scheme))
	utilruntime.Must(corev1.AddToScheme(scheme.Scheme))

	cfg := config.OperatorConfig{}
	var err error
	suite.log, err = logger.New(logger.DefaultConfig())
	suite.Require().NoError(err)
	suite.context, _, _ = platformmeshcontext.StartContext(suite.log, cfg, 1*time.Minute)
}

func TestAccountInfoSubroutineTestSuite(t *testing.T) {
	suite.Run(t, new(AccountInfoSubroutineTestSuite))
}

func (suite *AccountInfoSubroutineTestSuite) TestProcessing_OK_ForOrganization() {
	// Given
	testAccount := &v1alpha1.Account{
		ObjectMeta: v1.ObjectMeta{
			Name: "root-org",
			Annotations: map[string]string{
				"kcp.io/cluster": "asd",
			},
		},
		Spec: v1alpha1.AccountSpec{
			Type: v1alpha1.AccountTypeOrg,
		},
	}
	expectedAccountInfo := v1alpha1.AccountInfo{
		ObjectMeta: v1.ObjectMeta{
			Name: "account",
		},
		Spec: v1alpha1.AccountInfoSpec{
			ClusterInfo: v1alpha1.ClusterInfo{
				CA: "some-ca",
			},
			Organization: v1alpha1.AccountLocation{
				Name:               "root-org",
				GeneratedClusterId: "some-cluster-id-root-org",
				OriginClusterId:    "asd",
				Path:               "root:platform-mesh:orgs:root-org",
				URL:                "https://example.com/root:platform-mesh:orgs:root-org",
				Type:               "org",
			},
			Account: v1alpha1.AccountLocation{
				Name:               "root-org",
				GeneratedClusterId: "some-cluster-id-root-org",
				OriginClusterId:    "asd",
				Path:               "root:platform-mesh:orgs:root-org",
				URL:                "https://example.com/root:platform-mesh:orgs:root-org",
				Type:               "org",
			},
		},
	}

	suite.mockGetWorkspaceByName(kcptypes.LogicalClusterPhaseReady, "root:platform-mesh:orgs:root-org")
	suite.mockGetAccountInfoCallNotFound()
	suite.mockCreateAccountInfoCall(expectedAccountInfo)
	ctx := context.Background()
	// When
	res, err := suite.testObj.Process(ctx, testAccount)

	// Then
	suite.Nil(err)
	suite.Assert().Zero(res.RequeueAfter)
	suite.clientMock.AssertExpectations(suite.T())
}

func (suite *AccountInfoSubroutineTestSuite) TestProcessing_ForOrganization_Missing_Context() {
	// Given
	testAccount := &v1alpha1.Account{
		ObjectMeta: v1.ObjectMeta{
			Name: "root-org",
			Annotations: map[string]string{
				"kcp.io/cluster": "asd",
			},
		},
		Spec: v1alpha1.AccountSpec{
			Type: v1alpha1.AccountTypeOrg,
		},
	}
	expectedAccountInfo := v1alpha1.AccountInfo{
		ObjectMeta: v1.ObjectMeta{
			Name: "account",
		},
		Spec: v1alpha1.AccountInfoSpec{
			ClusterInfo: v1alpha1.ClusterInfo{
				CA: "some-ca",
			},
			Organization: v1alpha1.AccountLocation{
				Name:               "root-org",
				GeneratedClusterId: "some-cluster-id-root-org",
				OriginClusterId:    "asd",
				Path:               "root:platform-mesh:orgs:root-org",
				URL:                "https://example.com/root:platform-mesh:orgs:root-org",
				Type:               "org",
			},
			Account: v1alpha1.AccountLocation{
				Name:               "root-org",
				GeneratedClusterId: "some-cluster-id-root-org",
				OriginClusterId:    "asd",
				Path:               "root:platform-mesh:orgs:root-org",
				URL:                "https://example.com/root:platform-mesh:orgs:root-org",
				Type:               "org",
			},
		},
	}

	suite.mockGetWorkspaceByName(kcptypes.LogicalClusterPhaseReady, "root:platform-mesh:orgs:root-org")
	suite.mockGetAccountInfoCallNotFound()
	suite.mockCreateAccountInfoCall(expectedAccountInfo)

	// When
	res, err := suite.testObj.Process(context.Background(), testAccount)

	// Then
	suite.Nil(err)
	suite.Assert().Zero(res.RequeueAfter)
	suite.clientMock.AssertExpectations(suite.T())
}

func (suite *AccountInfoSubroutineTestSuite) TestProcessing_ForOrganization_Workspace_Not_Ready() {
	// Given
	testAccount := &v1alpha1.Account{
		ObjectMeta: v1.ObjectMeta{
			Name: "root-org",
		},
		Spec: v1alpha1.AccountSpec{
			Type: v1alpha1.AccountTypeOrg,
		},
	}
	ctx := context.Background()

	suite.mockGetWorkspaceByName(kcptypes.LogicalClusterPhaseInitializing, "root:platform-mesh:orgs")

	// When
	res, err := suite.testObj.Process(ctx, testAccount)

	// Then
	suite.Nil(err)
	suite.Assert().NotZero(res.RequeueAfter)
	suite.clientMock.AssertExpectations(suite.T())
}

func (suite *AccountInfoSubroutineTestSuite) TestProcessing_ForOrganization_Workspace_Not_Ready_no_Context() {
	// Given
	testAccount := &v1alpha1.Account{
		ObjectMeta: v1.ObjectMeta{
			Name: "root-org",
		},
		Spec: v1alpha1.AccountSpec{
			Type: v1alpha1.AccountTypeOrg,
		},
	}
	ctx := context.Background()

	suite.mockGetWorkspaceByName(kcptypes.LogicalClusterPhaseInitializing, "root:platform-mesh:orgs")

	// When
	res, err := suite.testObj.Process(ctx, testAccount)

	// Then
	suite.Nil(err)
	suite.Assert().NotZero(res.RequeueAfter)
	suite.clientMock.AssertExpectations(suite.T())
}

func (suite *AccountInfoSubroutineTestSuite) TestProcessing_ForOrganization_No_Workspace() {
	// Given
	testAccount := &v1alpha1.Account{
		ObjectMeta: v1.ObjectMeta{
			Name: "root-org",
		},
		Spec: v1alpha1.AccountSpec{
			Type: v1alpha1.AccountTypeOrg,
		},
	}

	suite.mockGetWorkspaceNotFound()
	ctx := suite.context

	// When
	_, err := suite.testObj.Process(ctx, testAccount)

	// Then
	suite.NotNil(err)
	suite.Equal("workspace does not exist:  \"\" not found", err.Err().Error())
	suite.Error(err.Err())
	suite.True(err.Retry())
	suite.True(err.Sentry())
	suite.clientMock.AssertExpectations(suite.T())
}

func (suite *AccountInfoSubroutineTestSuite) TestProcessing_OK_No_Path() {
	// Given
	testAccount := &v1alpha1.Account{
		ObjectMeta: v1.ObjectMeta{
			Name: "root-org",
		},
		Spec: v1alpha1.AccountSpec{
			Type: v1alpha1.AccountTypeOrg,
		},
	}
	suite.mockGetWorkspaceByName(kcptypes.LogicalClusterPhaseReady, "")
	ctx := suite.context

	// When
	_, err := suite.testObj.Process(ctx, testAccount)

	// Then
	suite.NotNil(err)
	suite.Equal("workspace URL is empty", err.Err().Error())
	suite.Error(err.Err())
	suite.True(err.Retry())
	suite.True(err.Sentry())
	suite.clientMock.AssertExpectations(suite.T())
}

func (suite *AccountInfoSubroutineTestSuite) TestProcessing_OK_Empty_Path() {
	// Given
	testAccount := &v1alpha1.Account{
		ObjectMeta: v1.ObjectMeta{
			Name: "root-org",
		},
		Spec: v1alpha1.AccountSpec{
			Type: v1alpha1.AccountTypeOrg,
		},
	}
	suite.mockGetWorkspaceByName(kcptypes.LogicalClusterPhaseReady, " ")
	ctx := suite.context

	// When
	_, err := suite.testObj.Process(ctx, testAccount)

	// Then
	suite.NotNil(err)
	suite.Equal("workspace URL is empty", err.Err().Error())
	suite.Error(err.Err())
	suite.True(err.Retry())
	suite.True(err.Sentry())
	suite.clientMock.AssertExpectations(suite.T())
}

func (suite *AccountInfoSubroutineTestSuite) TestProcessing_OK_Invalid_Path() {
	// Given
	testAccount := &v1alpha1.Account{
		ObjectMeta: v1.ObjectMeta{
			Name: "root-org",
		},
		Spec: v1alpha1.AccountSpec{
			Type: v1alpha1.AccountTypeOrg,
		},
	}
	suite.mockGetWorkspaceByWrongPath(kcptypes.LogicalClusterPhaseReady)
	ctx := suite.context

	// When
	_, err := suite.testObj.Process(ctx, testAccount)

	// Then
	suite.NotNil(err)
	suite.Equal("workspace URL is invalid", err.Err().Error())
	suite.Error(err.Err())
	suite.True(err.Retry())
	suite.True(err.Sentry())
	suite.clientMock.AssertExpectations(suite.T())
}

func (suite *AccountInfoSubroutineTestSuite) TestProcessing_OK_ForAccount() {
	// Given
	testAccount := &v1alpha1.Account{
		ObjectMeta: v1.ObjectMeta{
			Name: "example-account",
			Annotations: map[string]string{
				"kcp.io/cluster": "asd",
			},
		},
		Spec: v1alpha1.AccountSpec{
			Type: v1alpha1.AccountTypeAccount,
		},
	}
	expectedAccountInfo := v1alpha1.AccountInfo{
		ObjectMeta: v1.ObjectMeta{
			Name: "account",
		},
		Spec: v1alpha1.AccountInfoSpec{
			ClusterInfo: v1alpha1.ClusterInfo{CA: "some-ca"},
			Organization: v1alpha1.AccountLocation{
				Name:               "root-org",
				GeneratedClusterId: "some-cluster-id-root-org",
				Path:               "root:platform-mesh:orgs:root-org",
				Type:               "org",
				URL:                "https://example.com/root:platform-mesh:orgs:root-org",
			},
			Account: v1alpha1.AccountLocation{
				Name:               "example-account",
				GeneratedClusterId: "some-cluster-id-example-account",
				OriginClusterId:    "asd",
				Path:               "root:platform-mesh:orgs:root-org:example-account",
				Type:               "account",
				URL:                "https://example.com/root:platform-mesh:orgs:root-org:example-account",
			},
			ParentAccount: &v1alpha1.AccountLocation{
				Name:               "root-org",
				GeneratedClusterId: "some-cluster-id-root-org",
				Path:               "root:platform-mesh:orgs:root-org",
				URL:                "https://example.com/root:platform-mesh:orgs:root-org",
				Type:               "org",
			},
			FGA: v1alpha1.FGAInfo{
				Store: v1alpha1.StoreInfo{
					Id: "1",
				},
			},
		},
	}

	suite.mockGetWorkspaceByName(kcptypes.LogicalClusterPhaseReady, "root:platform-mesh:orgs:root-org:example-account")
	parentAccountInfoSpec := v1alpha1.AccountInfoSpec{
		Organization:  expectedAccountInfo.Spec.Organization,
		ParentAccount: nil,
		Account:       expectedAccountInfo.Spec.Organization,
		FGA:           v1alpha1.FGAInfo{Store: v1alpha1.StoreInfo{Id: "1"}},
	}
	suite.mockGetAccountInfo(parentAccountInfoSpec)
	suite.mockGetAccountInfoCallNotFound()
	suite.mockCreateAccountInfoCall(expectedAccountInfo)
	ctx := suite.context

	// When
	_, err := suite.testObj.Process(ctx, testAccount)

	// Then
	suite.Nil(err)
	suite.clientMock.AssertExpectations(suite.T())
}

func (suite *AccountInfoSubroutineTestSuite) TestProcessing_ForAccount_No_Parent() {
	// Given
	testAccount := &v1alpha1.Account{
		ObjectMeta: v1.ObjectMeta{
			Name: "example-account",
			Annotations: map[string]string{
				"kcp.io/cluster": "asd",
			},
		},
		Spec: v1alpha1.AccountSpec{
			Type: v1alpha1.AccountTypeAccount,
		},
	}

	suite.mockGetWorkspaceByName(kcptypes.LogicalClusterPhaseReady, "root:platform-mesh:orgs:root-org")
	suite.mockGetAccountInfoCallNotFound()

	ctx := suite.context
	// When
	_, err := suite.testObj.Process(ctx, testAccount)

	// Then
	suite.NotNil(err)
	suite.Equal("AccountInfo does not yet exist. Retry another time", err.Err().Error())
	suite.Error(err.Err())
	suite.True(err.Retry())
	suite.False(err.Sentry())
	suite.clientMock.AssertExpectations(suite.T())
}

func (suite *AccountInfoSubroutineTestSuite) TestProcessing_ForAccount_Parent_Lookup_Failed() {
	// Given
	testAccount := &v1alpha1.Account{
		ObjectMeta: v1.ObjectMeta{
			Name: "example-account",
			Annotations: map[string]string{
				"kcp.io/cluster": "asd",
			},
		},
		Spec: v1alpha1.AccountSpec{
			Type: v1alpha1.AccountTypeAccount,
		},
	}

	suite.mockGetWorkspaceByName(kcptypes.LogicalClusterPhaseReady, "root:platform-mesh:orgs:root-org")
	suite.mockGetAccountInfoCallFailed()
	ctx := suite.context

	// When
	_, err := suite.testObj.Process(ctx, testAccount)

	// Then
	suite.NotNil(err)
	suite.Equal("Internal error occurred: failed", err.Err().Error())
	suite.Error(err.Err())
	suite.True(err.Retry())
	suite.True(err.Sentry())
	suite.clientMock.AssertExpectations(suite.T())
}

func (suite *AccountInfoSubroutineTestSuite) TestGetName_OK() {
	// When
	result := suite.testObj.GetName()

	// Then
	suite.Equal(subroutines.AccountInfoSubroutineName, result)
}

func (suite *AccountInfoSubroutineTestSuite) TestGetFinalizerName() {
	// When
	finalizers := suite.testObj.Finalizers()

	// Then
	suite.Len(finalizers, 1)
}

func (suite *AccountInfoSubroutineTestSuite) TestFinalize() {
	// When
	ctx := context.Background()
	// keep simple background context
	res, err := suite.testObj.Finalize(ctx, &v1alpha1.Account{
		ObjectMeta: v1.ObjectMeta{
			Name:       "example-account",
			Finalizers: []string{"account.core.platform-mesh.io/info", "account.core.platform-mesh.io/abc"},
		},
	})

	// Then
	suite.Nil(err)
	suite.Assert().NotZero(res.RequeueAfter)
}

func (suite *AccountInfoSubroutineTestSuite) TestFinalizeNoContext() {
	// When
	ctx := context.Background()
	res, err := suite.testObj.Finalize(ctx, &v1alpha1.Account{
		ObjectMeta: v1.ObjectMeta{
			Name:       "example-account",
			Finalizers: []string{"account.core.platform-mesh.io/info", "account.core.platform-mesh.io/abc"},
		},
	})

	// Then
	suite.Nil(err)
	suite.Assert().NotZero(res.RequeueAfter)
}

func (suite *AccountInfoSubroutineTestSuite) mockGetAccountInfoCallNotFound() {
	suite.clientMock.On("Get", mock.Anything, mock.Anything, mock.AnythingOfType("*v1alpha1.AccountInfo")).
		Return(kerrors.NewNotFound(schema.GroupResource{}, ""))
}

func (suite *AccountInfoSubroutineTestSuite) mockGetAccountInfoCallFailed() {
	suite.clientMock.On("Get", mock.Anything, mock.Anything, mock.AnythingOfType("*v1alpha1.AccountInfo")).
		Return(kerrors.NewInternalError(fmt.Errorf("failed")))
}

func (suite *AccountInfoSubroutineTestSuite) mockCreateAccountInfoCall(info v1alpha1.AccountInfo) {
	// Mock Create call for CreateOrUpdate
	suite.clientMock.On("Create", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			obj := args.Get(1).(client.Object)
			actual, _ := obj.(*v1alpha1.AccountInfo)
			if !suite.Equal(info, *actual) {
				suite.log.Info().Msgf("Expected: %+v", actual)
			}
			suite.Assert().Equal(info, *actual)
		}).
		Return(nil).Maybe()

	// Add Update mock for CreateOrUpdate pattern - but it may not be called
	suite.clientMock.On("Update", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			obj := args.Get(1).(client.Object)
			actual, _ := obj.(*v1alpha1.AccountInfo)
			if !suite.Equal(info, *actual) {
				suite.log.Info().Msgf("Expected: %+v", actual)
			}
			suite.Assert().Equal(info, *actual)
		}).
		Return(nil).Maybe() // Make this optional
}

func (suite *AccountInfoSubroutineTestSuite) mockGetWorkspaceByName(ready kcptypes.LogicalClusterPhaseType, path string) {
	suite.clientMock.On("Get", mock.Anything, mock.Anything, mock.AnythingOfType("*types.Workspace")).
		Run(func(args mock.Arguments) {
			key := args.Get(1).(types.NamespacedName)
			obj := args.Get(2).(*kcptypes.Workspace)
			wsPath := ""
			if path != "" {
				wsPath = "https://example.com/" + path
			}
			obj.Name = key.Name
			obj.Spec = kcptypes.WorkspaceSpec{
				Cluster: "some-cluster-id-" + key.Name,
				URL:     wsPath,
			}
			obj.Status.Phase = ready
		}).
		Return(nil)
}

func (suite *AccountInfoSubroutineTestSuite) mockGetWorkspaceByWrongPath(ready kcptypes.LogicalClusterPhaseType) {
	suite.clientMock.On("Get", mock.Anything, mock.Anything, mock.AnythingOfType("*types.Workspace")).
		Run(func(args mock.Arguments) {
			key := args.Get(1).(types.NamespacedName)
			obj := args.Get(2).(*kcptypes.Workspace)
			obj.Name = key.Name
			obj.Spec = kcptypes.WorkspaceSpec{
				Cluster: "some-cluster-id-" + key.Name,
				URL:     "asd",
			}
			obj.Status.Phase = ready
		}).
		Return(nil)
}

func (suite *AccountInfoSubroutineTestSuite) mockGetWorkspaceNotFound() {
	suite.clientMock.On("Get", mock.Anything, mock.Anything, mock.AnythingOfType("*types.Workspace")).
		Return(kerrors.NewNotFound(schema.GroupResource{}, ""))
}

func (suite *AccountInfoSubroutineTestSuite) mockGetAccountInfo(spec v1alpha1.AccountInfoSpec) {
	suite.clientMock.On("Get", mock.Anything, mock.Anything, mock.AnythingOfType("*v1alpha1.AccountInfo")).
		Run(func(args mock.Arguments) {
			key := args.Get(1).(types.NamespacedName)
			obj := args.Get(2).(*v1alpha1.AccountInfo)
			obj.Name = key.Name
			obj.Spec = spec
		}).
		Return(nil)
}
