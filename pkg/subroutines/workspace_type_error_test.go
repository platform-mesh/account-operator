package subroutines

import (
	"context"
	"errors"
	"testing"
	"time"

	kcptenancyv1alpha "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"
	"github.com/platform-mesh/golang-commons/logger"
	"github.com/stretchr/testify/suite"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	corev1alpha1 "github.com/platform-mesh/account-operator/api/v1alpha1"
)

// failingClient wraps a real client but forces error on first Get call to simulate create/update failure.
type failingClient struct{ client.Client }

func (f *failingClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	// Simulate underlying storage error for workspace type retrieval
	if _, ok := obj.(*kcptenancyv1alpha.WorkspaceType); ok {
		return errors.New("injected get error")
	}
	return f.Client.Get(ctx, key, obj, opts...)
}

type WorkspaceTypeErrorSuite struct {
	suite.Suite
	scheme *runtime.Scheme
	ctx    context.Context
	log    *logger.Logger
}

func TestWorkspaceTypeErrorSuite(t *testing.T) { suite.Run(t, new(WorkspaceTypeErrorSuite)) }

func (s *WorkspaceTypeErrorSuite) SetupSuite() {
	s.scheme = runtime.NewScheme()
	s.Require().NoError(corev1alpha1.AddToScheme(s.scheme))
	s.Require().NoError(kcptenancyv1alpha.AddToScheme(s.scheme))
	var err error
	s.log, err = logger.New(logger.DefaultConfig())
	s.Require().NoError(err)
	s.ctx = context.Background()
}

func (s *WorkspaceTypeErrorSuite) TestProcessErrorPath() {
	base := fake.NewClientBuilder().WithScheme(s.scheme).Build()
	fc := &failingClient{Client: base}
	sub := NewWorkspaceTypeSubroutine(fc)
	acc := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "err-org"}, Spec: corev1alpha1.AccountSpec{Type: corev1alpha1.AccountTypeOrg}}
	_, opErr := sub.Process(s.ctx, acc)
	s.NotNil(opErr)
	s.True(opErr.Retry())
}

func (s *WorkspaceTypeErrorSuite) TestFinalizeNotFoundIsIgnored() {
	base := fake.NewClientBuilder().WithScheme(s.scheme).Build()
	sub := NewWorkspaceTypeSubroutine(base)
	acc := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "final-org"}, Spec: corev1alpha1.AccountSpec{Type: corev1alpha1.AccountTypeOrg}}
	res, opErr := sub.Finalize(s.ctx, acc)
	s.Nil(opErr)
	s.Equal(time.Duration(0), res.RequeueAfter)
}

func (s *WorkspaceTypeErrorSuite) TestFinalizeDeletionErrorsAreRetried() {
	// Fails on any delete with a generic error; should return retryable error
	base := fake.NewClientBuilder().WithScheme(s.scheme).Build()
	fd := &failingDeleteClient{Client: base}
	sub := NewWorkspaceTypeSubroutine(fd)
	acc := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "final-err"}, Spec: corev1alpha1.AccountSpec{Type: corev1alpha1.AccountTypeOrg}}
	_, opErr := sub.Finalize(s.ctx, acc)
	s.NotNil(opErr)
	s.True(opErr.Retry())
}

func (s *WorkspaceTypeErrorSuite) TestProcessCreatesAndUpdates() {
	// Ensure both create and subsequent update path work
	cl := fake.NewClientBuilder().WithScheme(s.scheme).Build()
	sub := NewWorkspaceTypeSubroutine(cl)
	acc := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "org-create"}, Spec: corev1alpha1.AccountSpec{Type: corev1alpha1.AccountTypeOrg}}
	_, opErr := sub.Process(s.ctx, acc)
	s.Nil(opErr)
	// Run again to hit update path
	_, opErr = sub.Process(s.ctx, acc)
	s.Nil(opErr)
}

// conditionalDeleteClient returns NotFound on the first delete (org type), and generic error on the second (account type)
type conditionalDeleteClient struct{ client.Client }

var _ client.Client = (*conditionalDeleteClient)(nil)

func (c *conditionalDeleteClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	switch obj.GetName() {
	case "final-org-org":
		return kerrors.NewNotFound(kcptenancyv1alpha.Resource("workspacetype"), obj.GetName())
	default:
		return errors.New("boom")
	}
}

// failingDeleteClient always fails Delete to exercise finalize error path
type failingDeleteClient struct{ client.Client }

func (f *failingDeleteClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	return errors.New("delete failed")
}

func (s *WorkspaceTypeErrorSuite) TestFinalizeSecondDeleteErrorIsRetried() {
	cd := &conditionalDeleteClient{Client: fake.NewClientBuilder().WithScheme(s.scheme).Build()}
	sub := NewWorkspaceTypeSubroutine(cd)
	acc := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "final-org"}, Spec: corev1alpha1.AccountSpec{Type: corev1alpha1.AccountTypeOrg}}
	_, opErr := sub.Finalize(s.ctx, acc)
	s.NotNil(opErr)
	s.True(opErr.Retry())
}

// selectiveFailGetClient fails only on the second WorkspaceType Get to exercise the account type error branch in Process
type selectiveFailGetClient struct {
	client.Client
	count int
}

func (sfc *selectiveFailGetClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if _, ok := obj.(*kcptenancyv1alpha.WorkspaceType); ok {
		sfc.count++
		if sfc.count == 2 {
			return errors.New("second get failed")
		}
	}
	return sfc.Client.Get(ctx, key, obj, opts...)
}

func (s *WorkspaceTypeErrorSuite) TestProcessErrorOnAccountWorkspaceType() {
	base := fake.NewClientBuilder().WithScheme(s.scheme).Build()
	sfc := &selectiveFailGetClient{Client: base}
	sub := NewWorkspaceTypeSubroutine(sfc)
	acc := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "err-branch"}, Spec: corev1alpha1.AccountSpec{Type: corev1alpha1.AccountTypeOrg}}
	_, opErr := sub.Process(s.ctx, acc)
	s.NotNil(opErr)
	s.True(opErr.Retry())
}
