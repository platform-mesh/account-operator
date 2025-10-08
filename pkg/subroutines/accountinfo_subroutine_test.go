package subroutines

import (
	"context"
	"testing"
	"time"

	kcpcorev1alpha "github.com/kcp-dev/kcp/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"

	corev1alpha1 "github.com/platform-mesh/account-operator/api/v1alpha1"
)

func newSchemeForAccountInfo(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := corev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := kcpcorev1alpha.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := kcptenancyv1alpha.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return scheme
}

func TestAccountInfoProcessOrgCreatesInfo(t *testing.T) {
	scheme := newSchemeForAccountInfo(t)
	acc := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "org-ai", Annotations: map[string]string{"kcp.io/cluster": "root"}}, Spec: corev1alpha1.AccountSpec{Type: corev1alpha1.AccountTypeOrg}}
	ws := &kcptenancyv1alpha.Workspace{ObjectMeta: metav1.ObjectMeta{Name: "org-ai"}}
	ws.Spec.Cluster = "cluster-org-ai"
	ws.Spec.URL = "https://host/root:orgs/org-ai"
	ws.Status.Phase = kcpcorev1alpha.LogicalClusterPhaseReady
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(acc, ws).Build()
	sub := &AccountInfoSubroutine{
		client:        cl,
		clusterGetter: fakeClusterGetter{cluster: &fakeCluster{client: cl}},
		serverCA:      "ca",
		limiter:       workqueue.NewTypedItemExponentialFailureRateLimiter[ClusteredName](1, 1),
	}
	ctx := mccontext.WithCluster(context.Background(), "cluster-test")
	_, opErr := sub.Process(ctx, acc)
	if opErr != nil {
		t.Fatalf("unexpected error: %v", opErr)
	}
	// Verify AccountInfo created with expected fields
	info := &corev1alpha1.AccountInfo{}
	if err := cl.Get(context.Background(), client.ObjectKey{Name: DefaultAccountInfoName}, info); err != nil {
		t.Fatalf("expected accountinfo to exist: %v", err)
	}
	if info.Spec.Account.Name != "org-ai" || info.Spec.Organization.Name != "org-ai" {
		t.Fatalf("unexpected accountinfo fields: %+v", info.Spec)
	}
	if info.Spec.ClusterInfo.CA != "ca" {
		t.Fatalf("expected CA to be set")
	}
}

func TestAccountInfoProcessAccountCreatesInfoFromParent(t *testing.T) {
	scheme := newSchemeForAccountInfo(t)
	acc := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "acc-ai", Annotations: map[string]string{"kcp.io/cluster": "root"}}, Spec: corev1alpha1.AccountSpec{Type: corev1alpha1.AccountTypeAccount}}
	ws := &kcptenancyv1alpha.Workspace{ObjectMeta: metav1.ObjectMeta{Name: "acc-ai"}}
	ws.Spec.Cluster = "cluster-acc-ai"
	ws.Spec.URL = "https://host/root:orgs/org-x/acc-ai"
	ws.Status.Phase = kcpcorev1alpha.LogicalClusterPhaseReady
	parent := &corev1alpha1.AccountInfo{ObjectMeta: metav1.ObjectMeta{Name: DefaultAccountInfoName}}
	parent.Spec.Account = corev1alpha1.AccountLocation{Name: "org-x", GeneratedClusterId: "cluster-org-x", OriginClusterId: "root", Type: corev1alpha1.AccountTypeOrg, Path: "org-x", URL: "https://host/root:orgs/org-x"}
	parent.Spec.Organization = parent.Spec.Account
	parent.Spec.FGA.Store.Id = "store-parent"
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(acc, ws, parent).Build()
	sub := &AccountInfoSubroutine{
		client:        cl,
		clusterGetter: fakeClusterGetter{cluster: &fakeCluster{client: cl}},
		serverCA:      "cacert",
		limiter:       workqueue.NewTypedItemExponentialFailureRateLimiter[ClusteredName](1, 1),
	}
	ctx := mccontext.WithCluster(context.Background(), "cluster-test")
	_, opErr := sub.Process(ctx, acc)
	if opErr != nil {
		t.Fatalf("unexpected error: %v", opErr)
	}
	info := &corev1alpha1.AccountInfo{}
	if err := cl.Get(context.Background(), client.ObjectKey{Name: DefaultAccountInfoName}, info); err != nil {
		t.Fatalf("expected accountinfo to exist: %v", err)
	}
	if info.Spec.ParentAccount == nil || info.Spec.ParentAccount.Name != "org-x" {
		t.Fatalf("expected parent account to be set: %+v", info.Spec.ParentAccount)
	}
	if info.Spec.FGA.Store.Id != "store-parent" || info.Spec.ClusterInfo.CA != "cacert" {
		t.Fatalf("expected inherited store id and CA; got %+v", info.Spec)
	}
}

func TestAccountInfoProcessWorkspaceNotReadyStaticLimiter(t *testing.T) {
	scheme := newSchemeForAccountInfo(t)
	acc := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "org-nr", Annotations: map[string]string{"kcp.io/cluster": "root"}}, Spec: corev1alpha1.AccountSpec{Type: corev1alpha1.AccountTypeOrg}}
	ws := &kcptenancyv1alpha.Workspace{ObjectMeta: metav1.ObjectMeta{Name: "org-nr"}}
	ws.Spec.Cluster = "cluster-org-nr"
	ws.Spec.URL = "https://host/root:orgs/org-nr"
	ws.Status.Phase = kcpcorev1alpha.LogicalClusterPhaseInitializing
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(acc, ws).Build()
	sub := &AccountInfoSubroutine{
		client:        cl,
		clusterGetter: fakeClusterGetter{cluster: &fakeCluster{client: cl}},
		serverCA:      "ca",
		limiter:       staticLimiter{delay: time.Second},
	}
	ctx := mccontext.WithCluster(context.Background(), "cluster-test")
	res, opErr := sub.Process(ctx, acc)
	if opErr != nil {
		t.Fatalf("unexpected error: %v", opErr)
	}
	if res.RequeueAfter <= 0 {
		t.Fatalf("expected positive requeue, got %v", res.RequeueAfter)
	}
}

func TestAccountInfoProcessMissingOriginClusterAnnotation(t *testing.T) {
	scheme := newSchemeForAccountInfo(t)
	acc := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "org-noann"}, Spec: corev1alpha1.AccountSpec{Type: corev1alpha1.AccountTypeOrg}}
	ws := &kcptenancyv1alpha.Workspace{ObjectMeta: metav1.ObjectMeta{Name: "org-noann"}}
	ws.Spec.Cluster = "cluster-org-noann"
	ws.Spec.URL = "https://host/root:orgs/org-noann"
	ws.Status.Phase = kcpcorev1alpha.LogicalClusterPhaseReady
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(acc, ws).Build()
	sub := &AccountInfoSubroutine{
		client:        cl,
		clusterGetter: fakeClusterGetter{cluster: &fakeCluster{client: cl}},
		serverCA:      "ca",
		limiter:       staticLimiter{delay: time.Second},
	}
	ctx := mccontext.WithCluster(context.Background(), "cluster-test")
	_, opErr := sub.Process(ctx, acc)
	if opErr == nil || !opErr.Retry() {
		t.Fatalf("expected retryable error for missing origin cluster annotation")
	}
}
