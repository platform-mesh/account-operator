package subroutines

import (
    "context"
    "errors"
    "testing"
    "time"

    kcptenancyv1alpha "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"
    "github.com/platform-mesh/golang-commons/logger"
    "github.com/stretchr/testify/suite"
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
    sub := NewWorkspaceTypeSubroutineWithClient(fc)
    acc := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "err-org"}, Spec: corev1alpha1.AccountSpec{Type: corev1alpha1.AccountTypeOrg}}
    _, opErr := sub.Process(s.ctx, acc)
    s.NotNil(opErr)
    s.True(opErr.Retry())
}

func (s *WorkspaceTypeErrorSuite) TestFinalizeNotFoundIsIgnored() {
    base := fake.NewClientBuilder().WithScheme(s.scheme).Build()
    sub := NewWorkspaceTypeSubroutineWithClient(base)
    acc := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "final-org"}, Spec: corev1alpha1.AccountSpec{Type: corev1alpha1.AccountTypeOrg}}
    res, opErr := sub.Finalize(s.ctx, acc)
    s.Nil(opErr)
    s.Equal(time.Duration(0), res.RequeueAfter)
}