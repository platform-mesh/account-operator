package subroutines

import (
	"context"
	"fmt"
	"testing"

	kcptenancyv1alpha "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"
	"github.com/platform-mesh/golang-commons/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	corev1alpha1 "github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/account-operator/pkg/subroutines/mocks"
)

// Test suite for common.go functions
type CommonTestSuite struct {
	suite.Suite
	clientMock *mocks.Client
	log        *logger.Logger
	ctx        context.Context
}

func (suite *CommonTestSuite) SetupTest() {
	suite.clientMock = new(mocks.Client)
	var err error
	suite.log, err = logger.New(logger.DefaultConfig())
	suite.Require().NoError(err)
	suite.ctx = context.Background()
}

func TestCommonTestSuite(t *testing.T) {
	suite.Run(t, new(CommonTestSuite))
}

// Test generateAccountWorkspaceTypeName function
func TestGenerateAccountWorkspaceTypeName(t *testing.T) {
	tests := []struct {
		name         string
		orgName      string
		expectedName string
	}{
		{
			name:         "normal organization name",
			orgName:      "test-org",
			expectedName: "test-org-acc",
		},
		{
			name:         "empty organization name",
			orgName:      "",
			expectedName: "-acc",
		},
		{
			name:         "organization name with special characters",
			orgName:      "org-with-dashes",
			expectedName: "org-with-dashes-acc",
		},
		{
			name:         "single character organization name",
			orgName:      "a",
			expectedName: "a-acc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := generateAccountWorkspaceTypeName(tt.orgName)
			assert.Equal(t, tt.expectedName, result)
		})
	}
}

// Test generateOrganizationWorkspaceTypeName function
func TestGenerateOrganizationWorkspaceTypeName(t *testing.T) {
	tests := []struct {
		name         string
		orgName      string
		expectedName string
	}{
		{
			name:         "normal organization name",
			orgName:      "test-org",
			expectedName: "test-org-org",
		},
		{
			name:         "empty organization name",
			orgName:      "",
			expectedName: "-org",
		},
		{
			name:         "organization name with special characters",
			orgName:      "org-with-dashes",
			expectedName: "org-with-dashes-org",
		},
		{
			name:         "single character organization name",
			orgName:      "a",
			expectedName: "a-org",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := generateOrganizationWorkspaceTypeName(tt.orgName)
			assert.Equal(t, tt.expectedName, result)
		})
	}
}

// Test retrieveWorkspace function
func (suite *CommonTestSuite) TestRetrieveWorkspace_Success() {
	// Given
	account := &corev1alpha1.Account{
		ObjectMeta: metav1.ObjectMeta{Name: "test-account"},
	}

	expectedWorkspace := &kcptenancyv1alpha.Workspace{
		ObjectMeta: metav1.ObjectMeta{Name: "test-account"},
		Spec: kcptenancyv1alpha.WorkspaceSpec{
			Cluster: "test-cluster",
		},
	}

	suite.clientMock.EXPECT().
		Get(suite.ctx, client.ObjectKey{Name: "test-account"}, mock.Anything).
		Run(func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) {
			workspace := obj.(*kcptenancyv1alpha.Workspace)
			workspace.Name = expectedWorkspace.Name
			workspace.Spec = expectedWorkspace.Spec
		}).
		Return(nil)

	// When
	result, err := retrieveWorkspace(suite.ctx, account, suite.clientMock, suite.log)

	// Then
	suite.NoError(err)
	suite.NotNil(result)
	suite.Equal("test-account", result.Name)
	suite.Equal("test-cluster", result.Spec.Cluster)
	suite.clientMock.AssertExpectations(suite.T())
}

func (suite *CommonTestSuite) TestRetrieveWorkspace_NotFound() {
	// Given
	account := &corev1alpha1.Account{
		ObjectMeta: metav1.ObjectMeta{Name: "nonexistent-account"},
	}

	suite.clientMock.EXPECT().
		Get(suite.ctx, client.ObjectKey{Name: "nonexistent-account"}, mock.Anything).
		Return(kerrors.NewNotFound(schema.GroupResource{Group: "tenancy.kcp.io", Resource: "workspaces"}, "nonexistent-account"))

	// When
	result, err := retrieveWorkspace(suite.ctx, account, suite.clientMock, suite.log)

	// Then
	suite.Error(err)
	suite.Nil(result)
	suite.Contains(err.Error(), "workspace does not exist")
	suite.clientMock.AssertExpectations(suite.T())
}

func (suite *CommonTestSuite) TestRetrieveWorkspace_GetError() {
	// Given
	account := &corev1alpha1.Account{
		ObjectMeta: metav1.ObjectMeta{Name: "test-account"},
	}

	suite.clientMock.EXPECT().
		Get(suite.ctx, client.ObjectKey{Name: "test-account"}, mock.Anything).
		Return(kerrors.NewInternalError(fmt.Errorf("internal server error")))

	// When
	result, err := retrieveWorkspace(suite.ctx, account, suite.clientMock, suite.log)

	// Then
	suite.Error(err)
	suite.Nil(result)
	suite.Contains(err.Error(), "workspace does not exist")
	suite.clientMock.AssertExpectations(suite.T())
}

// Additional coverage: retrieveWorkspace should error on nil inputs
func TestRetrieveWorkspace_NilAccount(t *testing.T) {
	t.Parallel()
	log, err := logger.New(logger.DefaultConfig())
	assert.NoError(t, err)
	// Using a real mocks.Client is unnecessary here, we won't call it
	var cl client.Client = nil
	ws, gotErr := retrieveWorkspace(context.Background(), nil, cl, log)
	assert.Nil(t, ws)
	assert.Error(t, gotErr)
	assert.Contains(t, gotErr.Error(), "account is nil")
}

func TestRetrieveWorkspace_NilClient(t *testing.T) {
	t.Parallel()
	log, err := logger.New(logger.DefaultConfig())
	assert.NoError(t, err)
	acc := &corev1alpha1.Account{}
	ws, gotErr := retrieveWorkspace(context.Background(), acc, nil, log)
	assert.Nil(t, ws)
	assert.Error(t, gotErr)
	assert.Contains(t, gotErr.Error(), "client is nil")
}

func TestMustGetClusteredNamePanicsWithoutCluster(t *testing.T) {
	// Use a minimal runtime object implementing the required interface
	obj := &corev1alpha1.Account{}
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic when cluster missing in context")
		}
	}()
	_ = MustGetClusteredName(context.Background(), obj)
}

// Mock helper functions (existing)
// (previously had helper mockGetWorkspaceByName; removed as unused to satisfy lint)
