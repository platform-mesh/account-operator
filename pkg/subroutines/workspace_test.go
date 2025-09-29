package subroutines_test

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"
	"unsafe"

	kcpcorev1alpha1 "github.com/kcp-dev/kcp/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"
	conditionsapi "github.com/kcp-dev/kcp/sdk/apis/third_party/conditions/apis/conditions/v1alpha1"
	conditionshelper "github.com/kcp-dev/kcp/sdk/apis/third_party/conditions/util/conditions"
	platformmeshcontext "github.com/platform-mesh/golang-commons/context"
	"github.com/platform-mesh/golang-commons/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/kontext"

	corev1alpha1 "github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/account-operator/internal/config"
	"github.com/platform-mesh/account-operator/pkg/subroutines"
	"github.com/platform-mesh/account-operator/pkg/subroutines/mocks"
)

const defaultExpectedTestNamespace = "account-test"

type WorkspaceSubroutineTestSuite struct {
	suite.Suite

	// Tested Object(s)
	testObj *subroutines.WorkspaceSubroutine

	// Mocks
	clientMock    *mocks.Client
	orgClientMock *mocks.Client

	context context.Context
	log     *logger.Logger
}

func (suite *WorkspaceSubroutineTestSuite) SetupTest() {
	// Setup Mocks
	suite.clientMock = new(mocks.Client)
	suite.orgClientMock = new(mocks.Client)

	// Initialize Tested Object(s) with proper field injection using unsafe
	suite.testObj = &subroutines.WorkspaceSubroutine{}

	// Use unsafe to set private fields
	v := reflect.ValueOf(suite.testObj).Elem()

	// Set client field
	clientField := v.FieldByName("client")
	if clientField.IsValid() {
		clientPtr := (*client.Client)(unsafe.Pointer(clientField.UnsafeAddr()))
		*clientPtr = suite.clientMock
	}

	// Set organizationsClient field
	orgClientField := v.FieldByName("organizationsClient")
	if orgClientField.IsValid() {
		orgClientPtr := (*client.Client)(unsafe.Pointer(orgClientField.UnsafeAddr()))
		*orgClientPtr = suite.orgClientMock
	}

	// Set limiter field to prevent nil pointer issues
	limiterField := v.FieldByName("limiter")
	if limiterField.IsValid() {
		exp := workqueue.NewTypedItemExponentialFailureRateLimiter[subroutines.ClusteredName](1*time.Second, 120*time.Second)
		limiterPtr := (*workqueue.TypedRateLimiter[subroutines.ClusteredName])(unsafe.Pointer(limiterField.UnsafeAddr()))
		*limiterPtr = exp
	}

	utilruntime.Must(corev1alpha1.AddToScheme(scheme.Scheme))
	utilruntime.Must(corev1.AddToScheme(scheme.Scheme))

	cfg := config.OperatorConfig{}
	var err error
	suite.log, err = logger.New(logger.DefaultConfig())
	suite.Require().NoError(err)
	suite.context, _, _ = platformmeshcontext.StartContext(suite.log, cfg, 1*time.Minute)
}

func (suite *WorkspaceSubroutineTestSuite) TestGetName_OK() {
	// When
	result := suite.testObj.GetName()

	// Then
	suite.Equal(subroutines.WorkspaceSubroutineName, result)
}

func (suite *WorkspaceSubroutineTestSuite) TestGetFinalizerName() {
	// When
	finalizers := suite.testObj.Finalizers()

	// Then
	suite.Contains(finalizers, subroutines.WorkspaceSubroutineFinalizer)
}

func (suite *WorkspaceSubroutineTestSuite) TestFinalize_OK_Workspace_NotExisting() {
	// Given
	testAccount := &corev1alpha1.Account{}
	mockGetWorkspaceCallNotFound(suite)
	ctx := kontext.WithCluster(suite.context, "some-cluster-id")
	// When
	res, err := suite.testObj.Finalize(ctx, testAccount)

	// Then
	suite.False(res.Requeue)
	suite.Assert().Zero(res.RequeueAfter)
	suite.Nil(err)
	suite.clientMock.AssertExpectations(suite.T())
}

func (suite *WorkspaceSubroutineTestSuite) TestFinalize_Error_No_Cluster() {
	// Given
	testAccount := &corev1alpha1.Account{}

	ctx := suite.context
	// When
	assert.Panics(suite.T(), func() {
		suite.testObj.Finalize(ctx, testAccount)
	})

	suite.clientMock.AssertExpectations(suite.T())
}

func (suite *WorkspaceSubroutineTestSuite) TestFinalize_OK_Workspace_ExistingButInDeletion() {
	// Given
	testAccount := &corev1alpha1.Account{}
	mockGetWorkspaceByNameInDeletion(suite)
	ctx := kontext.WithCluster(suite.context, "some-cluster-id")

	// When
	res, err := suite.testObj.Finalize(ctx, testAccount)

	// Then
	suite.Assert().NotZero(res.RequeueAfter)
	suite.Nil(err)
	suite.clientMock.AssertExpectations(suite.T())
}

func (suite *WorkspaceSubroutineTestSuite) TestFinalize_OK_Workspace_Existing() {
	// Given
	testAccount := &corev1alpha1.Account{}
	mockGetWorkspaceByName(suite.clientMock, kcpcorev1alpha1.LogicalClusterPhaseReady, "example.com/")
	mockDeleteWorkspaceCall(suite)
	ctx := context.Background()
	ctx = kontext.WithCluster(ctx, "some-cluster-id")

	// When
	res, err := suite.testObj.Finalize(ctx, testAccount)

	// Then
	suite.Assert().NotZero(res.RequeueAfter)
	suite.Nil(err)
	suite.clientMock.AssertExpectations(suite.T())
}

func (suite *WorkspaceSubroutineTestSuite) TestFinalize_Error_On_Deletion() {
	// Given
	testAccount := &corev1alpha1.Account{}
	mockGetWorkspaceByName(suite.clientMock, kcpcorev1alpha1.LogicalClusterPhaseReady, "example.com/")
	mockDeleteWorkspaceCallFailed(suite)
	ctx := kontext.WithCluster(suite.context, "some-cluster-id")
	// When
	_, err := suite.testObj.Finalize(ctx, testAccount)

	// Then
	suite.Require().NotNil(err)
	suite.Error(err.Err())

	suite.True(err.Sentry())
	suite.True(err.Retry())
	suite.clientMock.AssertExpectations(suite.T())
}

func (suite *WorkspaceSubroutineTestSuite) TestFinalize_Error_On_Get() {
	// Given
	testAccount := &corev1alpha1.Account{}
	mockGetWorkspaceFailed(suite)
	ctx := kontext.WithCluster(suite.context, "some-cluster-id")
	// When
	_, err := suite.testObj.Finalize(ctx, testAccount)

	// Then
	suite.Require().NotNil(err)
	suite.Error(err.Err())

	suite.True(err.Sentry())
	suite.True(err.Retry())
	suite.clientMock.AssertExpectations(suite.T())
}

func (suite *WorkspaceSubroutineTestSuite) TestProcessing_OK() {
	// Given
	testAccount := &corev1alpha1.Account{}
	suite.clientMock.On("Scheme").Return(scheme.Scheme)
	mockGetWorkspaceCallNotFound(suite)
	mockGetWorkspaceTypeReady(suite.orgClientMock)
	mockNewWorkspaceCreateCall(suite, defaultExpectedTestNamespace)

	// When
	_, err := suite.testObj.Process(suite.context, testAccount)

	// Then
	suite.Nil(err)
	suite.clientMock.AssertExpectations(suite.T())
	suite.orgClientMock.AssertExpectations(suite.T())
}

func (suite *WorkspaceSubroutineTestSuite) TestProcessing_Error_On_Get() {
	// Given
	testAccount := &corev1alpha1.Account{}
	// First the workspace type check happens and succeeds
	mockGetWorkspaceTypeReady(suite.orgClientMock)
	// Then CreateOrUpdate internally does a Get which fails
	mockGetWorkspaceFailed(suite)

	// When
	_, err := suite.testObj.Process(suite.context, testAccount)

	// Then
	suite.Require().NotNil(err)
	suite.Error(err.Err())
	suite.True(err.Sentry())
	suite.True(err.Retry())
	suite.clientMock.AssertExpectations(suite.T())
	suite.orgClientMock.AssertExpectations(suite.T())
}

func (suite *WorkspaceSubroutineTestSuite) TestProcessing_CreateError() {
	// Given
	testAccount := &corev1alpha1.Account{}
	suite.clientMock.On("Scheme").Return(scheme.Scheme)
	mockGetWorkspaceCallNotFound(suite)
	mockGetWorkspaceTypeReady(suite.orgClientMock)
	suite.clientMock.EXPECT().
		Create(mock.Anything, mock.Anything).
		Return(kerrors.NewBadRequest(""))

	// When
	_, err := suite.testObj.Process(suite.context, testAccount)

	// Then
	suite.NotNil(err)
	suite.True(err.Retry())
	suite.True(err.Sentry())
	suite.clientMock.AssertExpectations(suite.T())
	suite.orgClientMock.AssertExpectations(suite.T())
}

func TestWorkspaceSubroutineTestSuite(t *testing.T) {
	suite.Run(t, new(WorkspaceSubroutineTestSuite))
}

//nolint:golint,unparam
func mockNewWorkspaceCreateCall(suite *WorkspaceSubroutineTestSuite, name string) *mocks.Client_Create_Call {
	return suite.clientMock.EXPECT().
		Create(mock.Anything, mock.Anything).
		Run(func(ctx context.Context, obj client.Object, opts ...client.CreateOption) {
			actual, _ := obj.(*kcptenancyv1alpha.Workspace)
			actual.Name = name
		}).
		Return(nil)
}

//nolint:golint,unparam
func mockGetWorkspaceCallNotFound(suite *WorkspaceSubroutineTestSuite) *mocks.Client_Get_Call {
	return suite.clientMock.EXPECT().
		Get(mock.Anything, mock.Anything, mock.Anything).
		Return(kerrors.NewNotFound(schema.GroupResource{}, ""))
}

func mockGetWorkspaceFailed(suite *WorkspaceSubroutineTestSuite) *mocks.Client_Get_Call {
	return suite.clientMock.EXPECT().
		Get(mock.Anything, mock.Anything, mock.Anything).
		Return(kerrors.NewInternalError(fmt.Errorf("failed")))
}

func mockGetWorkspaceByNameInDeletion(suite *WorkspaceSubroutineTestSuite) *mocks.Client_Get_Call {
	return suite.clientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{}, mock.Anything).
		Run(func(ctx context.Context, key types.NamespacedName, obj client.Object, opts ...client.GetOption) {
			actual, _ := obj.(*kcptenancyv1alpha.Workspace)
			actual.Name = key.Name
			actual.DeletionTimestamp = &metav1.Time{}
		}).
		Return(nil)
}

//nolint:golint,unparam
func mockDeleteWorkspaceCall(suite *WorkspaceSubroutineTestSuite) *mocks.Client_Delete_Call {
	return suite.clientMock.EXPECT().
		Delete(mock.Anything, mock.Anything).
		Return(nil)
}

func mockDeleteWorkspaceCallFailed(suite *WorkspaceSubroutineTestSuite) *mocks.Client_Delete_Call {
	return suite.clientMock.EXPECT().
		Delete(mock.Anything, mock.Anything).
		Return(kerrors.NewInternalError(fmt.Errorf("failed")))
}

// WorkspaceType mock helpers
func mockGetWorkspaceTypeReady(clientMock *mocks.Client) *mocks.Client_Get_Call {
	return clientMock.EXPECT().
		Get(mock.Anything, mock.Anything, mock.AnythingOfType("*v1alpha1.WorkspaceType")).
		Run(func(ctx context.Context, key types.NamespacedName, obj client.Object, opts ...client.GetOption) {
			workspaceType, _ := obj.(*kcptenancyv1alpha.WorkspaceType)
			workspaceType.Name = key.Name
			// Set up ready condition - the code checks for conditionsapi.ReadyCondition
			conditionshelper.Set(workspaceType, &conditionsapi.Condition{
				Type:   conditionsapi.ReadyCondition,
				Status: corev1.ConditionTrue,
			})
		}).
		Return(nil)
}
