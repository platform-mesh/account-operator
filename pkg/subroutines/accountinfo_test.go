package subroutines

import (
	"context"
	"testing"
	"time"

	kcpcorev1alpha "github.com/kcp-dev/kcp/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"
	"github.com/platform-mesh/golang-commons/logger"
	"github.com/stretchr/testify/suite"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	corev1alpha1 "github.com/platform-mesh/account-operator/api/v1alpha1"
)

type AccountInfoSubroutineTestSuite struct {
	suite.Suite
	scheme *runtime.Scheme
	ctx    context.Context
	log    *logger.Logger
}

func TestAccountInfoSubroutineTestSuite(t *testing.T) {
	suite.Run(t, new(AccountInfoSubroutineTestSuite))
}

func (s *AccountInfoSubroutineTestSuite) SetupSuite() {
	s.scheme = runtime.NewScheme()
	s.Require().NoError(corev1alpha1.AddToScheme(s.scheme))
	s.Require().NoError(kcpcorev1alpha.AddToScheme(s.scheme))
	s.Require().NoError(kcptenancyv1alpha.AddToScheme(s.scheme))

	var err error
	s.log, err = logger.New(logger.DefaultConfig())
	s.Require().NoError(err)
	s.ctx = context.Background()
}

func (s *AccountInfoSubroutineTestSuite) newClient(objs ...client.Object) client.Client {
	return fake.NewClientBuilder().WithScheme(s.scheme).WithObjects(objs...).Build()
}

// helper to create workspace
func newWorkspace(name, phase, cluster, url string) *kcptenancyv1alpha.Workspace {
	ws := &kcptenancyv1alpha.Workspace{ObjectMeta: metav1.ObjectMeta{Name: name}}
	ws.Spec.Cluster = cluster
	ws.Spec.URL = url
	if phase != "" {
		ws.Status.Phase = kcpcorev1alpha.LogicalClusterPhaseType(phase)
	}
	return ws
}

// Test organization account info creation path
func (s *AccountInfoSubroutineTestSuite) TestProcessOrganizationCreatesAccountInfo() {
	acc := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "org-a", Annotations: map[string]string{"kcp.io/cluster": "root"}}, Spec: corev1alpha1.AccountSpec{Type: corev1alpha1.AccountTypeOrg}}
	ws := newWorkspace("org-a", string(kcpcorev1alpha.LogicalClusterPhaseReady), "cluster-org-a", "https://host/root:orgs/org-a")
	cl := s.newClient(acc, ws)

	sub := NewAccountInfoSubroutine(cl, "FAKE-CA")
	res, opErr := sub.Process(s.ctx, acc)
	// should succeed immediately for org
	s.Nil(opErr)
	s.Equal(time.Duration(0), res.RequeueAfter)

	created := &corev1alpha1.AccountInfo{}
	s.Require().NoError(cl.Get(s.ctx, client.ObjectKey{Name: DefaultAccountInfoName}, created))
	s.Equal("org-a", created.Spec.Account.Name)
	s.Equal(corev1alpha1.AccountTypeOrg, created.Spec.Account.Type)
	s.Equal("FAKE-CA", created.Spec.ClusterInfo.CA)
}

// Test retry when workspace not ready
func (s *AccountInfoSubroutineTestSuite) TestProcessWorkspaceNotReadyRetries() {
	acc := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "org-b", Annotations: map[string]string{"kcp.io/cluster": "root"}}, Spec: corev1alpha1.AccountSpec{Type: corev1alpha1.AccountTypeOrg}}
	ws := newWorkspace("org-b", string(kcpcorev1alpha.LogicalClusterPhaseScheduling), "cluster-org-b", "https://host/root:orgs/org-b")
	cl := s.newClient(acc, ws)
	sub := NewAccountInfoSubroutine(cl, "CA")
	res, opErr := sub.Process(s.ctx, acc)
	s.Nil(opErr)
	s.True(res.RequeueAfter > 0)
}

// Test account type inherits parent info
func (s *AccountInfoSubroutineTestSuite) TestProcessAccountInheritsParent() {
	// parent org and its workspace + existing AccountInfo in workspace
	orgAcc := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "org-c", Annotations: map[string]string{"kcp.io/cluster": "root"}}, Spec: corev1alpha1.AccountSpec{Type: corev1alpha1.AccountTypeOrg}}
	orgWs := newWorkspace("org-c", string(kcpcorev1alpha.LogicalClusterPhaseReady), "cluster-org-c", "https://host/root:orgs/org-c")
	parentInfo := &corev1alpha1.AccountInfo{ObjectMeta: metav1.ObjectMeta{Name: DefaultAccountInfoName}}
	parentInfo.Spec.Account = corev1alpha1.AccountLocation{Name: "org-c", GeneratedClusterId: "cluster-org-c", OriginClusterId: "root", Type: corev1alpha1.AccountTypeOrg, Path: "org-c", URL: "https://host/root:orgs/org-c"}
	parentInfo.Spec.Organization = parentInfo.Spec.Account
	parentInfo.Spec.FGA.Store.Id = "store-123"
	parentInfo.Spec.ClusterInfo.CA = "CA"

	acc := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "acc-x", Annotations: map[string]string{"kcp.io/cluster": "root"}}, Spec: corev1alpha1.AccountSpec{Type: corev1alpha1.AccountTypeAccount}}
	accWs := newWorkspace("acc-x", string(kcpcorev1alpha.LogicalClusterPhaseReady), "cluster-acc-x", "https://host/root:orgs/org-c/acc-x")

	cl := s.newClient(orgAcc, orgWs, parentInfo, acc, accWs)
	sub := NewAccountInfoSubroutine(cl, "CA")

	res, opErr := sub.Process(s.ctx, acc)
	s.Nil(opErr)
	s.Equal(time.Duration(0), res.RequeueAfter)

	created := &corev1alpha1.AccountInfo{}
	s.Require().NoError(cl.Get(s.ctx, client.ObjectKey{Name: DefaultAccountInfoName}, created))
	s.Equal("acc-x", created.Spec.Account.Name)
	s.NotNil(created.Spec.ParentAccount)
	s.Equal("org-c", created.Spec.ParentAccount.Name)
	s.Equal("store-123", created.Spec.FGA.Store.Id)
}

// Test account path when parent AccountInfo missing -> expect operator error
func (s *AccountInfoSubroutineTestSuite) TestProcessAccountParentMissing() {
	orgAcc := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "org-missing", Annotations: map[string]string{"kcp.io/cluster": "root"}}, Spec: corev1alpha1.AccountSpec{Type: corev1alpha1.AccountTypeOrg}}
	orgWs := newWorkspace("org-missing", string(kcpcorev1alpha.LogicalClusterPhaseReady), "cluster-org-missing", "https://host/root:orgs/org-missing")
	acc := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "acc-missing", Annotations: map[string]string{"kcp.io/cluster": "root"}}, Spec: corev1alpha1.AccountSpec{Type: corev1alpha1.AccountTypeAccount}}
	accWs := newWorkspace("acc-missing", string(kcpcorev1alpha.LogicalClusterPhaseReady), "cluster-acc-missing", "https://host/root:orgs/org-missing/acc-missing")
	cl := s.newClient(orgAcc, orgWs, acc, accWs) // intentionally no AccountInfo object
	sub := NewAccountInfoSubroutine(cl, "CA")
	_, opErr := sub.Process(s.ctx, acc)
	s.NotNil(opErr)
}

// Test Finalize behavior waiting for other finalizers
func (s *AccountInfoSubroutineTestSuite) TestFinalizeDelaysUntilLast() {
	acc := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "org-d", Finalizers: []string{"x", "y"}, Annotations: map[string]string{"kcp.io/cluster": "root"}}, Spec: corev1alpha1.AccountSpec{Type: corev1alpha1.AccountTypeOrg}}
	ws := newWorkspace("org-d", string(kcpcorev1alpha.LogicalClusterPhaseReady), "cluster-org-d", "https://host/root:orgs/org-d")
	cl := s.newClient(acc, ws)

	// custom limiter to make deterministic timing
	lim := workqueue.NewTypedItemExponentialFailureRateLimiter[ClusteredName](1*time.Millisecond, 1*time.Millisecond)
	sub := &AccountInfoSubroutine{client: cl, serverCA: "CA", limiter: lim}

	res, opErr := sub.Finalize(s.ctx, acc)
	s.Nil(opErr)
	s.True(res.RequeueAfter > 0)

	// now only our finalizer
	acc.Finalizers = []string{"account.core.platform-mesh.io/info"}
	res, opErr = sub.Finalize(s.ctx, acc)
	s.Nil(opErr)
	s.Equal(time.Duration(0), res.RequeueAfter)
}

// Test retrieveCurrentWorkspacePath error paths
func (s *AccountInfoSubroutineTestSuite) TestRetrieveCurrentWorkspacePathErrors() {
	sub := &AccountInfoSubroutine{}
	_, _, err := sub.retrieveCurrentWorkspacePath(&kcptenancyv1alpha.Workspace{})
	s.Error(err)

	ws := &kcptenancyv1alpha.Workspace{}
	ws.Spec.URL = "https://host/root:orgs/" // trailing slash -> last segment empty
	_, _, err = sub.retrieveCurrentWorkspacePath(ws)
	s.Error(err)
}

func (s *AccountInfoSubroutineTestSuite) TestRetrieveCurrentWorkspacePathSuccess() {
	sub := &AccountInfoSubroutine{}
	ws := &kcptenancyv1alpha.Workspace{}
	ws.Spec.URL = "https://host/root:orgs/my-org"
	last, full, err := sub.retrieveCurrentWorkspacePath(ws)
	s.NoError(err)
	s.Equal("my-org", last)
	s.Equal("https://host/root:orgs/my-org", full)
}
