package subroutines_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	kcpcorev1alpha1 "github.com/kcp-dev/kcp/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"
	conditionsv1alpha1 "github.com/kcp-dev/kcp/sdk/apis/third_party/conditions/apis/conditions/v1alpha1"
	"github.com/kcp-dev/logicalcluster/v3"
	commonconfig "github.com/platform-mesh/golang-commons/config"
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
	clientMock *mocks.Client

	context context.Context
	log     *logger.Logger
}

func (suite *WorkspaceSubroutineTestSuite) SetupTest() {
	// Setup Mocks
	suite.clientMock = new(mocks.Client)

	// Initialize Tested Object(s)
	suite.testObj = subroutines.NewWorkspaceSubroutine(suite.clientMock)

	utilruntime.Must(corev1alpha1.AddToScheme(scheme.Scheme))
	utilruntime.Must(corev1.AddToScheme(scheme.Scheme))

	cfg := config.OperatorConfig{}
	var err error
	suite.log, err = logger.New(logger.DefaultConfig())
	suite.Require().NoError(err)
	suite.context, _, _ = platformmeshcontext.StartContext(suite.log, cfg, 1*time.Minute)
	// Generic Scheme expectation (CreateOrUpdate may call it multiple times)
	suite.clientMock.On("Scheme").Return(scheme.Scheme).Maybe()
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
	mockGetWorkspaceByName(suite.clientMock, kcpcorev1alpha1.LogicalClusterPhaseReady, "https://example.com/")
	mockDeleteWorkspaceCall(suite)
	ctx := kontext.WithCluster(suite.context, "some-cluster-id")

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
	mockGetWorkspaceByName(suite.clientMock, kcpcorev1alpha1.LogicalClusterPhaseReady, "https://example.com/")
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
	suite.clientMock.On("Scheme").Return(scheme.Scheme).Maybe()
	mockGetWorkspaceCallNotFound(suite)
	mockNewWorkspaceCreateCall(suite, defaultExpectedTestNamespace)

	// When
	_, err := suite.testObj.Process(suite.context, testAccount)

	// Then
	suite.Nil(err)
	suite.clientMock.AssertExpectations(suite.T())
}

func (suite *WorkspaceSubroutineTestSuite) TestProcessing_Error_On_Get() {
	// Given
	testAccount := &corev1alpha1.Account{}
	mockGetWorkspaceFailed(suite)

	// When
	_, err := suite.testObj.Process(suite.context, testAccount)

	// Then
	suite.Require().NotNil(err)
	suite.Error(err.Err())
	suite.True(err.Sentry())
	suite.True(err.Retry())
	suite.clientMock.AssertExpectations(suite.T())
}

func (suite *WorkspaceSubroutineTestSuite) TestProcessing_CreateError() {
	// Given
	testAccount := &corev1alpha1.Account{}
	suite.clientMock.On("Scheme").Return(scheme.Scheme).Maybe()
	mockGetWorkspaceCallNotFound(suite)
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
}

func (suite *WorkspaceSubroutineTestSuite) TestProcessing_AccountType_MultiSegmentPath() {
	// Given
	testAccount := &corev1alpha1.Account{
		Spec: corev1alpha1.AccountSpec{
			Type: corev1alpha1.AccountTypeAccount,
		},
	}
	testAccount.Name = "test-account"

	// Setup context with multi-segment cluster path: "root:platform-mesh:orgs:acme"
	// This tests the fixed parsing logic that should extract "acme" as the org name
	// and "root:platform-mesh:orgs" as the workspace path
	clusterPath := logicalcluster.Name("root:platform-mesh:orgs:acme")
	ctx := kontext.WithCluster(suite.context, clusterPath)

	// Mock all Get/Create calls in expected order
	suite.clientMock.On("Scheme").Return(scheme.Scheme).Maybe() // Allow multiple calls

	// 1. waitForWorkspaceType Get call - workspace type should be ready
	suite.clientMock.On("Get", mock.Anything, types.NamespacedName{Name: "orgs-acme-acc"}, mock.Anything).
		Run(func(args mock.Arguments) {
			obj := args[2].(*kcptenancyv1alpha.WorkspaceType)
			obj.Name = "orgs-acme-acc"
			obj.Status.Conditions = conditionsv1alpha1.Conditions{
				{
					Type:   "Ready",
					Status: "True",
				},
			}
		}).Return(nil)

	// 2. CreateOrUpdate Get call for Workspace - not found (will trigger creation)
	suite.clientMock.On("Get", mock.Anything, types.NamespacedName{Name: "test-account"}, mock.Anything).
		Return(kerrors.NewNotFound(schema.GroupResource{}, "test-account"))

	// 3. CreateOrUpdate Create call for Workspace
	suite.clientMock.On("Create", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			obj := args[1].(*kcptenancyv1alpha.Workspace)
			obj.Name = "test-account"
		}).Return(nil)

	// When
	_, err := suite.testObj.Process(ctx, testAccount)

	// Then
	suite.Nil(err)
	suite.clientMock.AssertExpectations(suite.T())
}

func (suite *WorkspaceSubroutineTestSuite) TestProcessing_ForbiddenError_RelaxEnabled() {
	// Given - Account with organization type
	testAccount := &corev1alpha1.Account{
		Spec: corev1alpha1.AccountSpec{
			Type: corev1alpha1.AccountTypeOrg,
		},
	}
	testAccount.Name = "test-org"

	// Set up context with cluster path for organization
	clusterPath := logicalcluster.Name("root:orgs:test-org")
	ctx := kontext.WithCluster(suite.context, clusterPath)

	// Create context with RelaxForbiddenWorkspaceCreation enabled and custom delay
	configWithRelaxEnabled := config.OperatorConfig{}
	configWithRelaxEnabled.Kcp.RelaxForbiddenWorkspaceCreation = true
	configWithRelaxEnabled.Subroutines.Workspace.ForbiddenRequeueDelay = "45s" // Custom delay
	ctxWithConfig := commonconfig.SetConfigInContext(ctx, configWithRelaxEnabled)

	// Mock workspace type as ready
	suite.clientMock.On("Get", mock.Anything, types.NamespacedName{Name: "test-org-org"}, mock.Anything).
		Run(func(args mock.Arguments) {
			obj := args[2].(*kcptenancyv1alpha.WorkspaceType)
			obj.Name = "test-org-org"
			obj.Status.Conditions = conditionsv1alpha1.Conditions{
				{
					Type:   "Ready",
					Status: "True",
				},
			}
		}).Return(nil)

	// Mock workspace not found (will trigger creation)
	mockGetWorkspaceCallNotFound(suite)

	// Mock workspace creation returning Forbidden error
	suite.clientMock.EXPECT().
		Create(mock.Anything, mock.Anything).
		Return(kerrors.NewForbidden(schema.GroupResource{}, "", fmt.Errorf("workspace creation forbidden")))

	// When
	result, err := suite.testObj.Process(ctxWithConfig, testAccount)

	// Then
	suite.Nil(err, "Should not return error when RelaxForbiddenWorkspaceCreation is enabled")
	suite.False(result.Requeue, "Should not set Requeue=true")
	suite.Equal(45*time.Second, result.RequeueAfter, "Should use configured ForbiddenRequeueDelay")
	suite.clientMock.AssertExpectations(suite.T())
}

func (suite *WorkspaceSubroutineTestSuite) TestProcessing_ForbiddenError_RelaxDisabled() {
	// Given - Account with organization type
	testAccount := &corev1alpha1.Account{
		Spec: corev1alpha1.AccountSpec{
			Type: corev1alpha1.AccountTypeOrg,
		},
	}
	testAccount.Name = "test-org"

	// Set up context with cluster path for organization
	clusterPath := logicalcluster.Name("root:orgs:test-org")
	ctx := kontext.WithCluster(suite.context, clusterPath)

	// Create context with RelaxForbiddenWorkspaceCreation disabled (default behavior)
	configWithRelaxDisabled := config.OperatorConfig{}
	configWithRelaxDisabled.Kcp.RelaxForbiddenWorkspaceCreation = false
	ctxWithConfig := commonconfig.SetConfigInContext(ctx, configWithRelaxDisabled)

	// Mock workspace type as ready
	suite.clientMock.On("Get", mock.Anything, types.NamespacedName{Name: "test-org-org"}, mock.Anything).
		Run(func(args mock.Arguments) {
			obj := args[2].(*kcptenancyv1alpha.WorkspaceType)
			obj.Name = "test-org-org"
			obj.Status.Conditions = conditionsv1alpha1.Conditions{
				{
					Type:   "Ready",
					Status: "True",
				},
			}
		}).Return(nil)

	// Mock workspace not found (will trigger creation)
	mockGetWorkspaceCallNotFound(suite)

	// Mock workspace creation returning Forbidden error
	suite.clientMock.EXPECT().
		Create(mock.Anything, mock.Anything).
		Return(kerrors.NewForbidden(schema.GroupResource{}, "", fmt.Errorf("workspace creation forbidden")))

	// When
	result, err := suite.testObj.Process(ctxWithConfig, testAccount)

	// Then
	suite.NotNil(err, "Should return error when RelaxForbiddenWorkspaceCreation is disabled")
	suite.True(err.Retry(), "Error should be retryable")
	suite.False(result.Requeue, "Should not set Requeue=true when error is returned")
	suite.Zero(result.RequeueAfter, "Should not set RequeueAfter when error is returned")
	suite.clientMock.AssertExpectations(suite.T())
}

func (suite *WorkspaceSubroutineTestSuite) TestProcessing_ForbiddenError_DefaultDelay() {
	// Given - Account with organization type
	testAccount := &corev1alpha1.Account{
		Spec: corev1alpha1.AccountSpec{
			Type: corev1alpha1.AccountTypeOrg,
		},
	}
	testAccount.Name = "test-org"

	// Set up context with cluster path for organization
	clusterPath := logicalcluster.Name("root:orgs:test-org")
	ctx := kontext.WithCluster(suite.context, clusterPath)

	// Create context with RelaxForbiddenWorkspaceCreation enabled but no custom delay
	configWithDefaultDelay := config.OperatorConfig{}
	configWithDefaultDelay.Kcp.RelaxForbiddenWorkspaceCreation = true
	// ForbiddenRequeueDelay not set - should use default 30s
	ctxWithConfig := commonconfig.SetConfigInContext(ctx, configWithDefaultDelay)

	// Mock workspace type as ready
	suite.clientMock.On("Get", mock.Anything, types.NamespacedName{Name: "test-org-org"}, mock.Anything).
		Run(func(args mock.Arguments) {
			obj := args[2].(*kcptenancyv1alpha.WorkspaceType)
			obj.Name = "test-org-org"
			obj.Status.Conditions = conditionsv1alpha1.Conditions{
				{
					Type:   "Ready",
					Status: "True",
				},
			}
		}).Return(nil)

	// Mock workspace not found (will trigger creation)
	mockGetWorkspaceCallNotFound(suite)

	// Mock workspace creation returning Forbidden error
	suite.clientMock.EXPECT().
		Create(mock.Anything, mock.Anything).
		Return(kerrors.NewForbidden(schema.GroupResource{}, "", fmt.Errorf("workspace creation forbidden")))

	// When
	result, err := suite.testObj.Process(ctxWithConfig, testAccount)

	// Then
	suite.Nil(err, "Should not return error when RelaxForbiddenWorkspaceCreation is enabled")
	suite.False(result.Requeue, "Should not set Requeue=true")
	suite.Equal(30*time.Second, result.RequeueAfter, "Should use default 30s delay")
	suite.clientMock.AssertExpectations(suite.T())
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
		Get(mock.Anything, types.NamespacedName{}, mock.Anything).
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
