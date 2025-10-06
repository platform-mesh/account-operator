package subroutines

import (
	"context"
	"testing"
	"time"

	kcpcorev1alpha "github.com/kcp-dev/kcp/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"
	corev1alpha1 "github.com/platform-mesh/account-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// Basic sanity tests to bump coverage on simple getters/finalizers.
func TestWorkspaceTypeSubroutine_Getters(t *testing.T) {
	sub := NewWorkspaceTypeSubroutine(nil, nil)
	if sub.GetName() != "WorkspaceTypeSubroutine" {
		t.Fatalf("unexpected name: %s", sub.GetName())
	}
	fins := sub.Finalizers(&corev1alpha1.Account{})
	if len(fins) != 1 || fins[0] == "" {
		t.Fatalf("unexpected finalizers: %v", fins)
	}
}

func TestWorkspaceSubroutine_Getters(t *testing.T) {
	sub := NewWorkspaceSubroutineForTesting(nil, nil)
	if sub.GetName() != "WorkspaceSubroutine" {
		t.Fatalf("unexpected name: %s", sub.GetName())
	}
	fins := sub.Finalizers(&corev1alpha1.Account{})
	if len(fins) != 1 || fins[0] == "" {
		t.Fatalf("unexpected finalizers: %v", fins)
	}
}

func TestAccountInfoSubroutine_Finalizers(t *testing.T) {
	sub := NewAccountInfoSubroutine(nil, nil, "")
	fins := sub.Finalizers(&corev1alpha1.Account{})
	if len(fins) != 1 || fins[0] == "" {
		t.Fatalf("unexpected finalizers: %v", fins)
	}
}

func TestFGASubroutine_GetName(t *testing.T) {
	sub := NewFGASubroutine(nil, nil, nil, "creator", "parent", "account")
	if sub.GetName() != "FGASubroutine" {
		t.Fatalf("unexpected name: %s", sub.GetName())
	}
	fins := sub.Finalizers(&corev1alpha1.Account{})
	if len(fins) != 1 || fins[0] == "" {
		t.Fatalf("unexpected finalizers: %v", fins)
	}
}

// Light integration of WorkspaceTypeSubroutine createOrUpdate with an in-memory client
func TestWorkspaceTypeSubroutine_ProcessCreates(t *testing.T) {
	scheme := runtime.NewScheme()
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(corev1alpha1.AddToScheme(scheme))
	utilruntime.Must(kcpcorev1alpha.AddToScheme(scheme))
	utilruntime.Must(kcptenancyv1alpha.AddToScheme(scheme))
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()
	sub := NewWorkspaceTypeSubroutineWithClient(cl)
	acc := &corev1alpha1.Account{}
	acc.Name = "mini"
	acc.Spec.Type = corev1alpha1.AccountTypeOrg
	if _, err := sub.Process(context.Background(), acc); err != nil {
		t.Fatalf("process error: %v", err)
	}
}

// Exercise AccountInfo finalize fast path
func TestAccountInfoSubroutine_FinalizeFastPath(t *testing.T) {
	sub := &AccountInfoSubroutine{limiter: nil}
	acc := &corev1alpha1.Account{}
	acc.Name = "org-final"
	acc.Spec.Type = corev1alpha1.AccountTypeOrg
	// Only our finalizer present -> immediate return
	acc.Finalizers = []string{"account.core.platform-mesh.io/info"}
	res, err := sub.Finalize(context.Background(), acc)
	if err != nil {
		t.Fatalf("unexpected op error: %v", err)
	}
	if res.RequeueAfter != 0 {
		t.Fatalf("expected no requeue, got %v", res.RequeueAfter)
	}
}

// Validate exponential limiter path triggers requeue (simulate >1 finalizer)
func TestAccountInfoSubroutine_FinalizeDelayed(t *testing.T) {
	acc := &corev1alpha1.Account{}
	acc.Name = "org-final-delay"
	acc.Spec.Type = corev1alpha1.AccountTypeOrg
	acc.Finalizers = []string{"x", "account.core.platform-mesh.io/info"}
	sub := &AccountInfoSubroutine{limiter: workqueueNoop{}}
	res, err := sub.Finalize(context.Background(), acc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.RequeueAfter == 0 {
		t.Fatalf("expected requeue delay")
	}
}

// workqueueNoop provides deterministic small delay
type workqueueNoop struct{}

func (w workqueueNoop) When(_ ClusteredName) time.Duration { return time.Millisecond }
func (w workqueueNoop) Forget(_ ClusteredName)             {}
func (w workqueueNoop) NumRequeues(_ ClusteredName) int    { return 0 }
