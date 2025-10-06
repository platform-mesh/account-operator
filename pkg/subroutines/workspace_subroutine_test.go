package subroutines

import (
	"context"
	"fmt"
	"testing"
	"time"

	kcpcorev1alpha "github.com/kcp-dev/kcp/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"
	conditionsapi "github.com/kcp-dev/kcp/sdk/apis/third_party/conditions/apis/conditions/v1alpha1"
	conditionshelper "github.com/kcp-dev/kcp/sdk/apis/third_party/conditions/util/conditions"
	"github.com/platform-mesh/golang-commons/logger"
	"github.com/stretchr/testify/suite"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	corev1alpha1 "github.com/platform-mesh/account-operator/api/v1alpha1"
)

type WorkspaceSubroutineTestSuite struct {
	suite.Suite
	scheme *runtime.Scheme
	ctx    context.Context
	log    *logger.Logger
}

func TestWorkspaceSubroutineTestSuite(t *testing.T) {
	suite.Run(t, new(WorkspaceSubroutineTestSuite))
}

func (s *WorkspaceSubroutineTestSuite) SetupSuite() {
	s.scheme = runtime.NewScheme()
	s.Require().NoError(corev1alpha1.AddToScheme(s.scheme))
	s.Require().NoError(kcpcorev1alpha.AddToScheme(s.scheme))
	s.Require().NoError(kcptenancyv1alpha.AddToScheme(s.scheme))

	var err error
	s.log, err = logger.New(logger.DefaultConfig())
	s.Require().NoError(err)
	s.ctx = context.Background()
}

func (s *WorkspaceSubroutineTestSuite) newClient(objs ...client.Object) client.Client {
	return fake.NewClientBuilder().WithScheme(s.scheme).WithObjects(objs...).Build()
}

func (s *WorkspaceSubroutineTestSuite) TestProcessCreatesWorkspaceForOrg() {
	acc := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "org-ws", Annotations: map[string]string{"kcp.io/cluster": "root"}}, Spec: corev1alpha1.AccountSpec{Type: corev1alpha1.AccountTypeOrg}}
	orgType := &kcptenancyv1alpha.WorkspaceType{ObjectMeta: metav1.ObjectMeta{Name: "org-ws-org"}}
	conditionshelper.Set(orgType, &conditionsapi.Condition{Type: conditionsapi.ReadyCondition, Status: v1.ConditionTrue, Reason: "Ready", Message: "ready"})
	cl := s.newClient(acc, orgType)
	sub := NewWorkspaceSubroutine(nil, cl, nil, nil)
	res, opErr := sub.Process(s.ctx, acc)
	s.Nil(opErr)
	s.Equal(time.Duration(0), res.RequeueAfter)

	created := &kcptenancyv1alpha.Workspace{}
	s.Require().NoError(cl.Get(s.ctx, client.ObjectKey{Name: "org-ws"}, created))
	s.NotNil(created.Spec.Type)
	s.Equal(kcptenancyv1alpha.WorkspaceTypeName("org-ws-org"), created.Spec.Type.Name)
}

func (s *WorkspaceSubroutineTestSuite) TestProcessRetriesUntilWorkspaceTypeReady() {
	acc := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "org-ws2", Annotations: map[string]string{"kcp.io/cluster": "root"}}, Spec: corev1alpha1.AccountSpec{Type: corev1alpha1.AccountTypeOrg}}
	orgType := &kcptenancyv1alpha.WorkspaceType{ObjectMeta: metav1.ObjectMeta{Name: "org-ws2-org"}} // no Ready condition
	cl := s.newClient(acc, orgType)
	lim := workqueue.NewTypedItemExponentialFailureRateLimiter[ClusteredName](1*time.Millisecond, 1*time.Millisecond)
	// inject orgsClient so readiness logic actually checks conditions (otherwise treated as ready when baseConfig nil)
	sub := &WorkspaceSubroutine{client: cl, limiter: lim, orgsClient: cl}
	res, opErr := sub.Process(s.ctx, acc)
	s.Nil(opErr)
	s.True(res.RequeueAfter > 0)
}

func (s *WorkspaceSubroutineTestSuite) TestProcessRetriesWhenWorkspaceTypeMissing() {
	acc := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "org-miss", Annotations: map[string]string{"kcp.io/cluster": "root"}}, Spec: corev1alpha1.AccountSpec{Type: corev1alpha1.AccountTypeOrg}}
	cl := s.newClient(acc) // no workspace type at all
	lim := workqueue.NewTypedItemExponentialFailureRateLimiter[ClusteredName](1*time.Millisecond, 1*time.Millisecond)
	sub := &WorkspaceSubroutine{client: cl, limiter: lim, orgsClient: cl}
	res, opErr := sub.Process(s.ctx, acc)
	s.Nil(opErr)
	s.True(res.RequeueAfter > 0)
}

func (s *WorkspaceSubroutineTestSuite) TestFinalizeDeletesWorkspace() {
	acc := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "org-del", Finalizers: []string{WorkspaceSubroutineFinalizer}, Annotations: map[string]string{"kcp.io/cluster": "root"}}, Spec: corev1alpha1.AccountSpec{Type: corev1alpha1.AccountTypeOrg}}
	ws := &kcptenancyv1alpha.Workspace{ObjectMeta: metav1.ObjectMeta{Name: "org-del"}}
	cl := s.newClient(acc, ws)
	lim := workqueue.NewTypedItemExponentialFailureRateLimiter[ClusteredName](1*time.Millisecond, 1*time.Millisecond)
	sub := &WorkspaceSubroutine{client: cl, limiter: lim}
	res, opErr := sub.Finalize(s.ctx, acc)
	s.Nil(opErr)
	s.True(res.RequeueAfter > 0)
}

// failingGetClient returns an error when retrieving a WorkspaceType to exercise error branch
type failingGetClient struct{ client.Client }

func (f *failingGetClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if _, ok := obj.(*kcptenancyv1alpha.WorkspaceType); ok {
		return fmt.Errorf("boom-get")
	}
	return f.Client.Get(ctx, key, obj, opts...)
}

func (s *WorkspaceSubroutineTestSuite) TestProcessWorkspaceTypeGetError() {
	acc := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "org-err", Annotations: map[string]string{"kcp.io/cluster": "root"}}, Spec: corev1alpha1.AccountSpec{Type: corev1alpha1.AccountTypeOrg}}
	// workspace type exists but Get will error
	orgType := &kcptenancyv1alpha.WorkspaceType{ObjectMeta: metav1.ObjectMeta{Name: "org-err-org"}}
	cl := s.newClient(acc, orgType)
	failing := &failingGetClient{Client: cl}
	lim := workqueue.NewTypedItemExponentialFailureRateLimiter[ClusteredName](1*time.Millisecond, 1*time.Millisecond)
	sub := &WorkspaceSubroutine{client: cl, limiter: lim, orgsClient: failing}
	_, opErr := sub.Process(s.ctx, acc)
	s.NotNil(opErr)
	s.True(opErr.Retry())
}
