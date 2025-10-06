package subroutines

import (
	"context"
	"errors"
	"testing"
	"time"

	kcpcorev1alpha "github.com/kcp-dev/kcp/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"
	"github.com/platform-mesh/golang-commons/logger"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	corev1alpha1 "github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/account-operator/pkg/subroutines/mocks"
)

type FGASubroutineTestSuite struct {
	suite.Suite
	scheme *runtime.Scheme
	ctx    context.Context
	log    *logger.Logger
}

func TestFGASubroutineTestSuite(t *testing.T) {
	suite.Run(t, new(FGASubroutineTestSuite))
}

func (s *FGASubroutineTestSuite) SetupSuite() {
	s.scheme = runtime.NewScheme()
	s.Require().NoError(corev1alpha1.AddToScheme(s.scheme))
	s.Require().NoError(kcpcorev1alpha.AddToScheme(s.scheme))
	s.Require().NoError(kcptenancyv1alpha.AddToScheme(s.scheme))

	var err error
	s.log, err = logger.New(logger.DefaultConfig())
	s.Require().NoError(err)
	s.ctx = context.Background()
}

func (s *FGASubroutineTestSuite) newClient(objs ...client.Object) client.Client {
	return fake.NewClientBuilder().WithScheme(s.scheme).WithObjects(objs...).Build()
}

func newReadyWorkspace(name, cluster, url string) *kcptenancyv1alpha.Workspace {
	ws := &kcptenancyv1alpha.Workspace{ObjectMeta: metav1.ObjectMeta{Name: name}}
	ws.Spec.Cluster = cluster
	ws.Spec.URL = url
	ws.Status.Phase = kcpcorev1alpha.LogicalClusterPhaseReady
	return ws
}

func (s *FGASubroutineTestSuite) TestProcessWritesTuplesForOrgCreator() {
	acc := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "org-fga", Annotations: map[string]string{"kcp.io/cluster": "root"}}, Spec: corev1alpha1.AccountSpec{Type: corev1alpha1.AccountTypeOrg, Creator: strPtr("user@example.com")}}
	ws := newReadyWorkspace("org-fga", "cluster-org-fga", "https://host/root:orgs/org-fga")
	info := &corev1alpha1.AccountInfo{ObjectMeta: metav1.ObjectMeta{Name: DefaultAccountInfoName}}
	info.Spec.Account = corev1alpha1.AccountLocation{Name: "org-fga", GeneratedClusterId: "cluster-org-fga", OriginClusterId: "root", Type: corev1alpha1.AccountTypeOrg, Path: "org-fga", URL: "https://host/root:orgs/org-fga"}
	info.Spec.Organization = info.Spec.Account
	info.Spec.FGA.Store.Id = "store-1"

	cl := s.newClient(acc, ws, info)

	mockFGA := mocks.NewOpenFGAServiceClient(s.T())
	mockFGA.EXPECT().Write(mock.Anything, mock.Anything).Return(&openfgav1.WriteResponse{}, nil).Twice()

	sub := NewFGASubroutine(nil, cl, mockFGA, "member", "parent", "account")
	res, opErr := sub.Process(s.ctx, acc)
	s.Nil(opErr)
	s.Equal(time.Duration(0), res.RequeueAfter)
	mockFGA.AssertExpectations(s.T())
}

func (s *FGASubroutineTestSuite) TestProcessRetriesUntilWorkspaceReady() {
	acc := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "org-fga2", Annotations: map[string]string{"kcp.io/cluster": "root"}}, Spec: corev1alpha1.AccountSpec{Type: corev1alpha1.AccountTypeOrg}}
	ws := &kcptenancyv1alpha.Workspace{ObjectMeta: metav1.ObjectMeta{Name: "org-fga2"}}
	ws.Spec.Cluster = "cluster-org-fga2"
	ws.Spec.URL = "https://host/root:orgs/org-fga2"
	ws.Status.Phase = kcpcorev1alpha.LogicalClusterPhaseInitializing
	info := &corev1alpha1.AccountInfo{ObjectMeta: metav1.ObjectMeta{Name: DefaultAccountInfoName}}
	s.Require().NoError(nil)
	cl := s.newClient(acc, ws, info)
	mockFGA := mocks.NewOpenFGAServiceClient(s.T())
	lim := workqueue.NewTypedItemExponentialFailureRateLimiter[ClusteredName](1*time.Millisecond, 1*time.Millisecond)
	sub := &FGASubroutine{client: cl, fgaClient: mockFGA, creatorRelation: "member", parentRelation: "parent", objectType: "account", limiter: lim}
	res, opErr := sub.Process(s.ctx, acc)
	s.Nil(opErr)
	s.True(res.RequeueAfter > 0)
	mockFGA.AssertExpectations(s.T())
}

func (s *FGASubroutineTestSuite) TestFinalizeDeletesTuplesForAccount() {
	acc := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "acc-fga", Annotations: map[string]string{"kcp.io/cluster": "root"}}, Spec: corev1alpha1.AccountSpec{Type: corev1alpha1.AccountTypeAccount, Creator: strPtr("user@example.com")}}
	ws := newReadyWorkspace("acc-fga", "cluster-acc-fga", "https://host/root:orgs/org-x/acc-fga")
	info := &corev1alpha1.AccountInfo{ObjectMeta: metav1.ObjectMeta{Name: DefaultAccountInfoName}}
	info.Spec.Account = corev1alpha1.AccountLocation{Name: "acc-fga", GeneratedClusterId: "cluster-acc-fga", OriginClusterId: "root", Type: corev1alpha1.AccountTypeAccount, Path: "org-x/acc-fga", URL: "https://host/root:orgs/org-x/acc-fga"}
	info.Spec.ParentAccount = &corev1alpha1.AccountLocation{Name: "org-x", GeneratedClusterId: "cluster-org-x", OriginClusterId: "root", Type: corev1alpha1.AccountTypeOrg, Path: "org-x", URL: "https://host/root:orgs/org-x"}
	info.Spec.FGA.Store.Id = "store-1"
	cl := s.newClient(acc, ws, info)
	mockFGA := mocks.NewOpenFGAServiceClient(s.T())
	// Expect three deletes when creator present: parent relation + role assignee + creator relation
	mockFGA.EXPECT().Write(mock.Anything, mock.Anything).Return(&openfgav1.WriteResponse{}, nil).Times(3)
	sub := NewFGASubroutine(nil, cl, mockFGA, "member", "parent", "account")
	res, opErr := sub.Finalize(s.ctx, acc)
	s.Nil(opErr)
	s.Equal(time.Duration(0), res.RequeueAfter)
	mockFGA.AssertExpectations(s.T())
}

func (s *FGASubroutineTestSuite) TestProcessErrorsOnMissingStore() {
	acc := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "org-fga3", Annotations: map[string]string{"kcp.io/cluster": "root"}}, Spec: corev1alpha1.AccountSpec{Type: corev1alpha1.AccountTypeOrg, Creator: strPtr("user@example.com")}}
	ws := newReadyWorkspace("org-fga3", "cluster-org-fga3", "https://host/root:orgs/org-fga3")
	info := &corev1alpha1.AccountInfo{ObjectMeta: metav1.ObjectMeta{Name: DefaultAccountInfoName}}
	info.Spec.Account = corev1alpha1.AccountLocation{Name: "org-fga3", GeneratedClusterId: "cluster-org-fga3", OriginClusterId: "root", Type: corev1alpha1.AccountTypeOrg, Path: "org-fga3", URL: "https://host/root:orgs/org-fga3"}
	// store id empty
	cl := s.newClient(acc, ws, info)
	mockFGA := mocks.NewOpenFGAServiceClient(s.T())
	sub := NewFGASubroutine(nil, cl, mockFGA, "member", "parent", "account")
	_, opErr := sub.Process(s.ctx, acc)
	s.NotNil(opErr)
}

func (s *FGASubroutineTestSuite) TestProcessErrorOnWriterFailure() {
	acc := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "org-fga4", Annotations: map[string]string{"kcp.io/cluster": "root"}}, Spec: corev1alpha1.AccountSpec{Type: corev1alpha1.AccountTypeOrg, Creator: strPtr("user@example.com")}}
	ws := newReadyWorkspace("org-fga4", "cluster-org-fga4", "https://host/root:orgs/org-fga4")
	info := &corev1alpha1.AccountInfo{ObjectMeta: metav1.ObjectMeta{Name: DefaultAccountInfoName}}
	info.Spec.Account = corev1alpha1.AccountLocation{Name: "org-fga4", GeneratedClusterId: "cluster-org-fga4", OriginClusterId: "root", Type: corev1alpha1.AccountTypeOrg, Path: "org-fga4", URL: "https://host/root:orgs/org-fga4"}
	info.Spec.FGA.Store.Id = "store-err"
	cl := s.newClient(acc, ws, info)
	mockFGA := mocks.NewOpenFGAServiceClient(s.T())
	mockFGA.EXPECT().Write(mock.Anything, mock.Anything).Return(nil, errors.New("boom"))
	lim := workqueue.NewTypedItemExponentialFailureRateLimiter[ClusteredName](1*time.Millisecond, 1*time.Millisecond)
	sub := &FGASubroutine{client: cl, fgaClient: mockFGA, creatorRelation: "member", parentRelation: "parent", objectType: "account", limiter: lim}
	_, opErr := sub.Process(s.ctx, acc)
	s.NotNil(opErr)
	mockFGA.AssertExpectations(s.T())
}

func strPtr(s string) *string { return &s }
