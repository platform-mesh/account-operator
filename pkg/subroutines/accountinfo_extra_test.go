package subroutines

import (
	"context"
	"errors"
	"testing"
	"time"

	kcptenancyv1alpha "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"
	"github.com/platform-mesh/golang-commons/logger"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"

	corev1alpha1 "github.com/platform-mesh/account-operator/api/v1alpha1"
)

func TestAccountInfoGetName(t *testing.T) {
	got := (&AccountInfoSubroutine{}).GetName()
	if got != AccountInfoSubroutineName {
		t.Fatalf("expected %s, got %s", AccountInfoSubroutineName, got)
	}
}

func TestAccountInfoFinalizeNilRuntimeObject(t *testing.T) {
	sub := &AccountInfoSubroutine{}
	_, opErr := sub.Finalize(context.Background(), nil)
	if opErr == nil {
		t.Fatalf("expected error for nil runtimeobject")
	}
	if !opErr.Retry() {
		t.Fatalf("expected retryable error")
	}
}

func TestAccountInfoFinalizeMultipleFinalizersNoLimiter(t *testing.T) {
	acc := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "acc", Finalizers: []string{"f1", "f2"}}}
	sub := &AccountInfoSubroutine{} // limiter is nil
	ctx := mccontext.WithCluster(context.Background(), "cluster-test")
	res, opErr := sub.Finalize(ctx, acc)
	if opErr != nil {
		t.Fatalf("unexpected error: %v", opErr)
	}
	if res.RequeueAfter != 1*time.Second {
		t.Fatalf("expected requeue after 1s, got %v", res.RequeueAfter)
	}
}

// client that returns a non-NotFound error from Get to exercise retrieveWorkspace error branch
type errGetClient struct{ client.Client }

func (e *errGetClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if _, ok := obj.(*kcptenancyv1alpha.Workspace); ok {
		return errors.New("boom")
	}
	return e.Client.Get(ctx, key, obj, opts...)
}

func TestRetrieveWorkspaceNonNotFoundError(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1alpha1.AddToScheme(scheme)
	_ = kcptenancyv1alpha.AddToScheme(scheme)
	base := fake.NewClientBuilder().WithScheme(scheme).Build()
	cl := &errGetClient{Client: base}
	log, _ := logger.New(logger.DefaultConfig())

	acc := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "acc"}}
	_, err := retrieveWorkspace(context.Background(), acc, cl, log)
	if err == nil {
		t.Fatalf("expected error from retrieveWorkspace on generic get error")
	}
}

// notFoundClient returns IsNotFound error to cover the NotFound logging/branch
type notFoundClient struct{ client.Client }

func (n *notFoundClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if _, ok := obj.(*kcptenancyv1alpha.Workspace); ok {
		return kerrors.NewNotFound(kcptenancyv1alpha.Resource("workspacetype"), key.Name)
	}
	return n.Client.Get(ctx, key, obj, opts...)
}

func TestRetrieveWorkspaceNotFound(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1alpha1.AddToScheme(scheme)
	_ = kcptenancyv1alpha.AddToScheme(scheme)
	base := fake.NewClientBuilder().WithScheme(scheme).Build()
	cl := &notFoundClient{Client: base}
	log, _ := logger.New(logger.DefaultConfig())
	acc := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "missing"}}
	_, err := retrieveWorkspace(context.Background(), acc, cl, log)
	if err == nil {
		t.Fatalf("expected error for not found workspace")
	}
}
