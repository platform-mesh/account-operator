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
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"

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

func (s *FGASubroutineTestSuite) TestFinalizeDeletesTuplesForAccount_NoCreator() {
	acc := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "acc-fga-nc", Annotations: map[string]string{"kcp.io/cluster": "root"}}, Spec: corev1alpha1.AccountSpec{Type: corev1alpha1.AccountTypeAccount}}
	ws := newReadyWorkspace("acc-fga-nc", "cluster-acc-fga-nc", "https://host/root:orgs/org-x/acc-fga-nc")
	info := &corev1alpha1.AccountInfo{ObjectMeta: metav1.ObjectMeta{Name: DefaultAccountInfoName}}
	info.Spec.Account = corev1alpha1.AccountLocation{Name: "acc-fga-nc", GeneratedClusterId: "cluster-acc-fga-nc", OriginClusterId: "root", Type: corev1alpha1.AccountTypeAccount, Path: "org-x/acc-fga-nc", URL: "https://host/root:orgs/org-x/acc-fga-nc"}
	info.Spec.ParentAccount = &corev1alpha1.AccountLocation{Name: "org-x", GeneratedClusterId: "cluster-org-x", OriginClusterId: "root", Type: corev1alpha1.AccountTypeOrg, Path: "org-x", URL: "https://host/root:orgs/org-x"}
	info.Spec.FGA.Store.Id = "store-1"
	cl := s.newClient(acc, ws, info)
	mockFGA := mocks.NewOpenFGAServiceClient(s.T())
	// Only one delete expected (parent relation)
	mockFGA.EXPECT().Write(mock.Anything, mock.Anything).Return(&openfgav1.WriteResponse{}, nil).Once()
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

func (s *FGASubroutineTestSuite) TestProcessSkipsCreatorWhenAlreadyWritten() {
	// If the Account condition marks FGASubroutine_Ready true, creator tuples should be skipped.
	acc := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "org-fga5", Annotations: map[string]string{"kcp.io/cluster": "root"}}, Spec: corev1alpha1.AccountSpec{Type: corev1alpha1.AccountTypeOrg, Creator: strPtr("user@example.com")}}
	acc.Status.Conditions = append(acc.Status.Conditions, metav1.Condition{Type: "FGASubroutine_Ready", Status: metav1.ConditionTrue})
	ws := newReadyWorkspace("org-fga5", "cluster-org-fga5", "https://host/root:orgs/org-fga5")
	info := &corev1alpha1.AccountInfo{ObjectMeta: metav1.ObjectMeta{Name: DefaultAccountInfoName}}
	info.Spec.Account = corev1alpha1.AccountLocation{Name: "org-fga5", GeneratedClusterId: "cluster-org-fga5", OriginClusterId: "root", Type: corev1alpha1.AccountTypeOrg, Path: "org-fga5", URL: "https://host/root:orgs/org-fga5"}
	info.Spec.Organization = info.Spec.Account
	info.Spec.FGA.Store.Id = "store-1"
	cl := s.newClient(acc, ws, info)
	mockFGA := mocks.NewOpenFGAServiceClient(s.T())
	// No writes expected because creatorTuplesWritten is true
	sub := NewFGASubroutine(nil, cl, mockFGA, "member", "parent", "account")
	res, opErr := sub.Process(s.ctx, acc)
	s.Nil(opErr)
	s.Equal(time.Duration(0), res.RequeueAfter)
	mockFGA.AssertExpectations(s.T())
}

func (s *FGASubroutineTestSuite) TestProcessCreatorValidationRejectsServiceAccountLike() {
	acc := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "org-fga6", Annotations: map[string]string{"kcp.io/cluster": "root"}}, Spec: corev1alpha1.AccountSpec{Type: corev1alpha1.AccountTypeOrg, Creator: strPtr("system.serviceaccount:ns:sa")}}
	ws := newReadyWorkspace("org-fga6", "cluster-org-fga6", "https://host/root:orgs/org-fga6")
	info := &corev1alpha1.AccountInfo{ObjectMeta: metav1.ObjectMeta{Name: DefaultAccountInfoName}}
	info.Spec.Account = corev1alpha1.AccountLocation{Name: "org-fga6", GeneratedClusterId: "cluster-org-fga6", OriginClusterId: "root", Type: corev1alpha1.AccountTypeOrg, Path: "org-fga6", URL: "https://host/root:orgs/org-fga6"}
	info.Spec.Organization = info.Spec.Account
	info.Spec.FGA.Store.Id = "store-1"
	cl := s.newClient(acc, ws, info)
	mockFGA := mocks.NewOpenFGAServiceClient(s.T())
	sub := NewFGASubroutine(nil, cl, mockFGA, "member", "parent", "account")
	_, opErr := sub.Process(s.ctx, acc)
	s.NotNil(opErr)
}

func (s *FGASubroutineTestSuite) TestFinalizeSkipsForOrgType() {
	acc := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "org-fga-finalize", Annotations: map[string]string{"kcp.io/cluster": "root"}}, Spec: corev1alpha1.AccountSpec{Type: corev1alpha1.AccountTypeOrg}}
	sub := &FGASubroutine{}
	res, opErr := sub.Finalize(s.ctx, acc)
	s.Nil(opErr)
	s.Equal(time.Duration(0), res.RequeueAfter)
}

func (s *FGASubroutineTestSuite) TestFormatUserAndValidateCreatorHelpers() {
	s.True(validateCreator("user@example.com"))
	s.False(validateCreator("system.serviceaccount:ns:sa"))
	s.Equal("system.serviceaccount.ns.sa", formatUser("system:serviceaccount:ns:sa"))
	s.Equal("bob", formatUser("bob"))
}

func (s *FGASubroutineTestSuite) TestProcessUsesClusterGetter() {
	acc := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "org-cluster", Annotations: map[string]string{"kcp.io/cluster": "root"}}, Spec: corev1alpha1.AccountSpec{Type: corev1alpha1.AccountTypeOrg, Creator: strPtr("user@example.com")}}
	ws := newReadyWorkspace("org-cluster", "cluster-org", "https://host/root:orgs/org-cluster")
	info := &corev1alpha1.AccountInfo{ObjectMeta: metav1.ObjectMeta{Name: DefaultAccountInfoName}}
	info.Spec.Account = corev1alpha1.AccountLocation{Name: "org-cluster", GeneratedClusterId: "cluster-org", OriginClusterId: "root", Type: corev1alpha1.AccountTypeOrg, Path: "org-cluster", URL: "https://host/root:orgs/org-cluster"}
	info.Spec.Organization = info.Spec.Account
	info.Spec.FGA.Store.Id = "store-xyz"
	cl := s.newClient(acc, ws, info)
	getter := fakeClusterGetter{cluster: &fakeCluster{client: cl}}
	mockFGA := mocks.NewOpenFGAServiceClient(s.T())
	// Org with creator triggers two writes (role assignee + creator relation)
	mockFGA.EXPECT().Write(mock.Anything, mock.Anything).Return(&openfgav1.WriteResponse{}, nil).Twice()
	sub := NewFGASubroutine(getter, nil, mockFGA, "member", "parent", "account")
	ctx := mccontext.WithCluster(s.ctx, "cluster-org")
	res, opErr := sub.Process(ctx, acc)
	s.Nil(opErr)
	s.Equal(time.Duration(0), res.RequeueAfter)
	mockFGA.AssertExpectations(s.T())
}

func (s *FGASubroutineTestSuite) TestProcessClusterGetterError() {
	acc := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "org-cluster-err", Annotations: map[string]string{"kcp.io/cluster": "root"}}, Spec: corev1alpha1.AccountSpec{Type: corev1alpha1.AccountTypeOrg}}
	getter := fakeClusterGetter{err: errors.New("boom")}
	sub := NewFGASubroutine(getter, nil, mocks.NewOpenFGAServiceClient(s.T()), "member", "parent", "account")
	ctx := mccontext.WithCluster(s.ctx, "cluster")
	_, opErr := sub.Process(ctx, acc)
	s.NotNil(opErr)
}

func (s *FGASubroutineTestSuite) TestFinalizeClusterGetterError() {
	acc := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "acc-fga-err", Annotations: map[string]string{"kcp.io/cluster": "root"}}, Spec: corev1alpha1.AccountSpec{Type: corev1alpha1.AccountTypeAccount}}
	getter := fakeClusterGetter{err: errors.New("boom")}
	sub := NewFGASubroutine(getter, nil, mocks.NewOpenFGAServiceClient(s.T()), "member", "parent", "account")
	ctx := mccontext.WithCluster(s.ctx, "cluster")
	_, opErr := sub.Finalize(ctx, acc)
	s.NotNil(opErr)
}

func (s *FGASubroutineTestSuite) TestFinalizeErrorsOnMissingStoreId() {
	// For non-org accounts, when store id is empty, finalize should error
	acc := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "acc-finalize-empty", Annotations: map[string]string{"kcp.io/cluster": "root"}}, Spec: corev1alpha1.AccountSpec{Type: corev1alpha1.AccountTypeAccount}}
	ws := newReadyWorkspace("acc-finalize-empty", "cluster-acc", "https://host/root:orgs/org-x/acc-finalize-empty")
	info := &corev1alpha1.AccountInfo{ObjectMeta: metav1.ObjectMeta{Name: DefaultAccountInfoName}}
	info.Spec.Account = corev1alpha1.AccountLocation{Name: "acc-finalize-empty", GeneratedClusterId: "cluster-acc", OriginClusterId: "root", Type: corev1alpha1.AccountTypeAccount, Path: "org-x/acc-finalize-empty"}
	// store id remains empty
	cl := s.newClient(acc, ws, info)
	sub := NewFGASubroutine(nil, cl, mocks.NewOpenFGAServiceClient(s.T()), "member", "parent", "account")
	_, opErr := sub.Finalize(s.ctx, acc)
	s.NotNil(opErr)
}

func (s *FGASubroutineTestSuite) TestProcessWritesParentForAccountType() {
	// For account type, parent relation should be written
	acc := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "acc-parent", Annotations: map[string]string{"kcp.io/cluster": "root"}}, Spec: corev1alpha1.AccountSpec{Type: corev1alpha1.AccountTypeAccount}}
	ws := newReadyWorkspace("acc-parent", "cluster-acc-parent", "https://host/root:orgs/org-y/acc-parent")
	info := &corev1alpha1.AccountInfo{ObjectMeta: metav1.ObjectMeta{Name: DefaultAccountInfoName}}
	info.Spec.Account = corev1alpha1.AccountLocation{Name: "acc-parent", GeneratedClusterId: "cluster-acc-parent", OriginClusterId: "root", Type: corev1alpha1.AccountTypeAccount, Path: "org-y/acc-parent", URL: "https://host/root:orgs/org-y/acc-parent"}
	info.Spec.ParentAccount = &corev1alpha1.AccountLocation{Name: "org-y", GeneratedClusterId: "cluster-org-y", OriginClusterId: "root", Type: corev1alpha1.AccountTypeOrg, Path: "org-y", URL: "https://host/root:orgs/org-y"}
	info.Spec.FGA.Store.Id = "store-2"
	cl := s.newClient(acc, ws, info)
	mockFGA := mocks.NewOpenFGAServiceClient(s.T())
	// Expect exactly one write for parent relation (no creator)
	mockFGA.EXPECT().Write(mock.Anything, mock.Anything).Return(&openfgav1.WriteResponse{}, nil).Once()
	sub := NewFGASubroutine(nil, cl, mockFGA, "member", "parent", "account")
	res, opErr := sub.Process(s.ctx, acc)
	s.Nil(opErr)
	s.Equal(time.Duration(0), res.RequeueAfter)
	mockFGA.AssertExpectations(s.T())
}

func (s *FGASubroutineTestSuite) TestProcessErrorsOnMissingAccountClusterId() {
	acc := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "org-missacc", Annotations: map[string]string{"kcp.io/cluster": "root"}}, Spec: corev1alpha1.AccountSpec{Type: corev1alpha1.AccountTypeOrg}}
	ws := newReadyWorkspace("org-missacc", "cluster-org", "https://host/root:orgs/org-missacc")
	info := &corev1alpha1.AccountInfo{ObjectMeta: metav1.ObjectMeta{Name: DefaultAccountInfoName}}
	info.Spec.Account = corev1alpha1.AccountLocation{Name: "org-missacc", GeneratedClusterId: "", OriginClusterId: "root", Type: corev1alpha1.AccountTypeOrg, Path: "org-missacc"}
	info.Spec.Organization = info.Spec.Account
	info.Spec.FGA.Store.Id = "store-1"
	cl := s.newClient(acc, ws, info)
	sub := NewFGASubroutine(nil, cl, mocks.NewOpenFGAServiceClient(s.T()), "member", "parent", "account")
	_, opErr := sub.Process(s.ctx, acc)
	s.NotNil(opErr)
}

func (s *FGASubroutineTestSuite) TestProcessErrorsOnMissingParentClusterIdForAccount() {
	acc := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "acc-missparent", Annotations: map[string]string{"kcp.io/cluster": "root"}}, Spec: corev1alpha1.AccountSpec{Type: corev1alpha1.AccountTypeAccount}}
	ws := newReadyWorkspace("acc-missparent", "cluster-acc", "https://host/root:orgs/org-x/acc-missparent")
	info := &corev1alpha1.AccountInfo{ObjectMeta: metav1.ObjectMeta{Name: DefaultAccountInfoName}}
	info.Spec.Account = corev1alpha1.AccountLocation{Name: "acc-missparent", GeneratedClusterId: "cluster-acc", OriginClusterId: "root", Type: corev1alpha1.AccountTypeAccount, Path: "org-x/acc-missparent"}
	info.Spec.ParentAccount = &corev1alpha1.AccountLocation{Name: "org-x", GeneratedClusterId: "", OriginClusterId: "root", Type: corev1alpha1.AccountTypeOrg, Path: "org-x"}
	info.Spec.FGA.Store.Id = "store-1"
	cl := s.newClient(acc, ws, info)
	sub := NewFGASubroutine(nil, cl, mocks.NewOpenFGAServiceClient(s.T()), "member", "parent", "account")
	_, opErr := sub.Process(s.ctx, acc)
	s.NotNil(opErr)
}

func strPtr(s string) *string { return &s }
