package subroutines

import (
	"testing"

	kcptenancyv1alpha "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	corev1alpha1 "github.com/platform-mesh/account-operator/api/v1alpha1"
)

func newScheme(t *testing.T) *runtime.Scheme {
	s := runtime.NewScheme()
	if err := corev1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	if err := kcptenancyv1alpha.AddToScheme(s); err != nil {
		t.Fatalf("add kcp tenancy: %v", err)
	}
	return s
}

func TestWaitForWorkspaceType_NotFound(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	r := &WorkspaceSubroutine{client: cl}
	ready, opErr := r.waitForWorkspaceType(ctxTODO, "missing")
	if opErr != nil || ready {
		t.Fatalf("expected not ready and no error, got ready=%v err=%v", ready, opErr)
	}
}

func TestWaitForWorkspaceType_FoundNotReady(t *testing.T) {
	s := newScheme(t)
	// WorkspaceType exists but has no Ready condition -> treated as not ready
	wt := &kcptenancyv1alpha.WorkspaceType{ObjectMeta: metav1.ObjectMeta{Name: "acc"}}
	cl := fake.NewClientBuilder().WithScheme(s).WithObjects(wt).Build()
	r := &WorkspaceSubroutine{client: cl}
	ready, opErr := r.waitForWorkspaceType(ctxTODO, "acc")
	if opErr != nil || ready {
		t.Fatalf("expected not ready and no error")
	}
}

func TestWorkspace_Process_Account_WaitsWhenWTNotReady(t *testing.T) {
	// No WorkspaceType present -> should requeue after default delay
	cl := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	r := NewWorkspaceSubroutine(cl)

	acct := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "acc1"}}
	acct.Spec.Type = corev1alpha1.AccountTypeAccount

	res, opErr := r.Process(ctxTODO, acct)
	if opErr != nil {
		t.Fatalf("unexpected error: %v", opErr)
	}
	if res.RequeueAfter != defaultWorkspaceTypeRequeueDelay {
		t.Fatalf("expected requeue=%v, got %v", defaultWorkspaceTypeRequeueDelay, res.RequeueAfter)
	}
}

func TestWorkspace_Finalize_NotFound(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	r := NewWorkspaceSubroutine(cl)
	acct := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "acc1"}}
	acct.Spec.Type = corev1alpha1.AccountTypeAccount
	res, opErr := r.Finalize(ctxTODO, acct)
	if opErr != nil || res.RequeueAfter != 0 {
		t.Fatalf("expected no error and no requeue, got %v %v", res, opErr)
	}
}

// Note: avoiding a test for deletionTimestamp behavior with fake client because
// controller-runtime fake client refuses objects with deletionTimestamp but no finalizers.
