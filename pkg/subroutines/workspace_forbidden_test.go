package subroutines

import (
	"context"
	"errors"
	"testing"

	kcpcorev1alpha "github.com/kcp-dev/kcp/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"
	conditionsv1alpha1 "github.com/kcp-dev/kcp/sdk/apis/third_party/conditions/apis/conditions/v1alpha1"
	corev1alpha1 "github.com/platform-mesh/account-operator/api/v1alpha1"
	operatorconfig "github.com/platform-mesh/account-operator/internal/config"
	commonconfig "github.com/platform-mesh/golang-commons/config"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// fakeClient wraps controller-runtime fake client to force Forbidden on CreateOrUpdate via Get
type fakeForbiddenClient struct{ client.Client }

func (f *fakeForbiddenClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	// Simulate Forbidden when checking WorkspaceType readiness (after WT ready); we only trigger on Workspace
	switch obj.(type) {
	case *kcptenancyv1alpha.Workspace:
		return kerrors.NewForbidden(schema.GroupResource{Group: "tenancy.kcp.io", Resource: "workspaces"}, key.Name, errors.New("forbidden"))
	default:
		return f.Client.Get(ctx, key, obj, opts...)
	}
}

func TestWorkspace_Process_Forbidden_RelaxedMode(t *testing.T) {
	s := runtime.NewScheme()
	_ = corev1alpha1.AddToScheme(s)
	_ = kcptenancyv1alpha.AddToScheme(s)
	_ = kcpcorev1alpha.AddToScheme(s)
	// Make WorkspaceType ready so path reaches CreateOrUpdate which will hit Forbidden via fake client
	wt := &kcptenancyv1alpha.WorkspaceType{ObjectMeta: metav1.ObjectMeta{Name: "account"}}
	wt.Status.Conditions = conditionsv1alpha1.Conditions{{Type: "Ready", Status: "True"}}
	baseCl := fake.NewClientBuilder().WithScheme(s).WithObjects(wt).Build()
	cl := &fakeForbiddenClient{Client: baseCl}
	r := NewWorkspaceSubroutine(cl)

	// Config: relaxed forbidden enabled
	cfg := operatorconfig.OperatorConfig{}
	cfg.Subroutines.Workspace.ForbiddenRequeueDelay = "1s"
	cfg.Kcp.ProviderWorkspace = "root"
	cfg.Kcp.RelaxForbiddenWorkspaceCreation = true

	// Inject config into ctx
	ctx := commonconfig.SetConfigInContext(ctxTODO, cfg)

	acct := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "acc"}}
	acct.Spec.Type = corev1alpha1.AccountTypeAccount
	res, opErr := r.Process(ctx, acct)
	if opErr != nil || res.RequeueAfter == 0 {
		t.Fatalf("expected relaxed requeue on forbidden, got res=%v err=%v", res, opErr)
	}
}

func TestWorkspace_Process_Forbidden_StrictMode(t *testing.T) {
	s := runtime.NewScheme()
	_ = corev1alpha1.AddToScheme(s)
	_ = kcptenancyv1alpha.AddToScheme(s)
	_ = kcpcorev1alpha.AddToScheme(s)
	wt := &kcptenancyv1alpha.WorkspaceType{ObjectMeta: metav1.ObjectMeta{Name: "account"}}
	wt.Status.Conditions = conditionsv1alpha1.Conditions{{Type: "Ready", Status: "True"}}
	baseCl := fake.NewClientBuilder().WithScheme(s).WithObjects(wt).Build()
	cl := &fakeForbiddenClient{Client: baseCl}
	r := NewWorkspaceSubroutine(cl)

	// Strict (default) config
	cfg := operatorconfig.OperatorConfig{}
	cfg.Kcp.ProviderWorkspace = "root"

	ctx := commonconfig.SetConfigInContext(ctxTODO, cfg)
	acct := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "acc"}}
	acct.Spec.Type = corev1alpha1.AccountTypeAccount
	if _, opErr := r.Process(ctx, acct); opErr == nil {
		t.Fatalf("expected operator error in strict mode on forbidden")
	}
}
