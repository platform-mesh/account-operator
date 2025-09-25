package subroutines

import (
	"testing"

	kcpcorev1alpha "github.com/kcp-dev/kcp/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"
	corev1alpha1 "github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/golang-commons/logger"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestAccountInfo_retrieveCurrentWorkspacePath(t *testing.T) {
	r := NewAccountInfoSubroutine(nil, "")
	// empty URL
	_, _, err := r.retrieveCurrentWorkspacePath(&kcptenancyv1alpha.Workspace{Spec: kcptenancyv1alpha.WorkspaceSpec{URL: ""}})
	if err == nil {
		t.Fatal("expected error for empty URL")
	}
	// last segment empty
	_, _, err = r.retrieveCurrentWorkspacePath(&kcptenancyv1alpha.Workspace{Spec: kcptenancyv1alpha.WorkspaceSpec{URL: "http://x/y/"}})
	if err == nil {
		t.Fatal("expected error for empty segment")
	}
	// valid
	p, u, err := r.retrieveCurrentWorkspacePath(&kcptenancyv1alpha.Workspace{Spec: kcptenancyv1alpha.WorkspaceSpec{URL: "https://api.local/clusters/root:orgs:acme"}})
	if err != nil || p != "root:orgs:acme" || u == "" {
		t.Fatalf("unexpected: p=%s u=%s err=%v", p, u, err)
	}
}

func TestAccountInfo_Finalize_RequeueWhenOtherFinalizersPresent(t *testing.T) {
	r := NewAccountInfoSubroutine(nil, "")
	acct := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "a", Finalizers: []string{"f1", "f2"}}}
	res, opErr := r.Finalize(ctxTODO, acct)
	if opErr != nil || res.RequeueAfter == 0 {
		t.Fatalf("expected requeue, err=%v res=%v", opErr, res)
	}
}

func TestRetrieveWorkspace_ErrorPath(t *testing.T) {
	lcfg := logger.DefaultConfig()
	lcfg.NoJSON = true
	l, _ := logger.New(lcfg)
	acct := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "missing"}}
	s := runtime.NewScheme()
	_ = corev1alpha1.AddToScheme(s)
	_ = kcptenancyv1alpha.AddToScheme(s)
	cl := fake.NewClientBuilder().WithScheme(s).Build()
	if ws, err := retrieveWorkspace(ctxTODO, acct, cl, l); err == nil || ws != nil {
		t.Fatalf("expected error")
	}
}

func TestAccountInfo_Process_Org_CreatesAccountInfo(t *testing.T) {
	s := runtime.NewScheme()
	_ = corev1alpha1.AddToScheme(s)
	_ = kcptenancyv1alpha.AddToScheme(s)
	_ = kcpcorev1alpha.AddToScheme(s)
	// Workspace for org is ready
	ws := &kcptenancyv1alpha.Workspace{ObjectMeta: metav1.ObjectMeta{Name: "org1"}}
	ws.Status.Phase = kcpcorev1alpha.LogicalClusterPhaseReady
	ws.Spec.Cluster = "root:orgs:org1"
	ws.Spec.URL = "https://api/clusters/root:orgs:org1"
	cl := fake.NewClientBuilder().WithScheme(s).WithObjects(ws).Build()
	r := NewAccountInfoSubroutine(cl, "CA")

	acct := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "org1", Annotations: map[string]string{"kcp.io/cluster": "root"}}}
	acct.Spec.Type = corev1alpha1.AccountTypeOrg

	if _, opErr := r.Process(ctxTODO, acct); opErr != nil {
		t.Fatalf("unexpected err: %v", opErr)
	}
	// Validate AccountInfo created
	ai := &corev1alpha1.AccountInfo{}
	if err := cl.Get(ctxTODO, client.ObjectKey{Name: DefaultAccountInfoName}, ai); err != nil {
		t.Fatalf("expected accountinfo: %v", err)
	}
	if ai.Spec.ClusterInfo.CA != "CA" || ai.Spec.Account.Name != "org1" || ai.Spec.ParentAccount != nil {
		t.Fatalf("unexpected accountinfo: %+v", ai.Spec)
	}
}

func TestAccountInfo_Process_Account_ParentMissing(t *testing.T) {
	s := runtime.NewScheme()
	_ = corev1alpha1.AddToScheme(s)
	_ = kcptenancyv1alpha.AddToScheme(s)
	_ = kcpcorev1alpha.AddToScheme(s)
	ws := &kcptenancyv1alpha.Workspace{ObjectMeta: metav1.ObjectMeta{Name: "acc1"}}
	ws.Status.Phase = kcpcorev1alpha.LogicalClusterPhaseReady
	ws.Spec.Cluster = "root:orgs:org1:acc1"
	ws.Spec.URL = "https://api/clusters/root:orgs:org1:acc1"
	cl := fake.NewClientBuilder().WithScheme(s).WithObjects(ws).Build()
	r := NewAccountInfoSubroutine(cl, "CA")
	acct := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "acc1", Annotations: map[string]string{"kcp.io/cluster": "root"}}}
	acct.Spec.Type = corev1alpha1.AccountTypeAccount
	if _, opErr := r.Process(ctxTODO, acct); opErr == nil {
		t.Fatalf("expected operator error when parent accountinfo missing")
	}
}

func TestAccountInfo_Process_WorkspaceNotReady(t *testing.T) {
	s := runtime.NewScheme()
	_ = corev1alpha1.AddToScheme(s)
	_ = kcptenancyv1alpha.AddToScheme(s)
	_ = kcpcorev1alpha.AddToScheme(s)
	ws := &kcptenancyv1alpha.Workspace{ObjectMeta: metav1.ObjectMeta{Name: "acc2"}}
	// Phase empty -> not ready
	ws.Spec.Cluster = "root:orgs:org1:acc2"
	ws.Spec.URL = "https://api/clusters/root:orgs:org1:acc2"
	cl := fake.NewClientBuilder().WithScheme(s).WithObjects(ws).Build()
	r := NewAccountInfoSubroutine(cl, "CA")
	acct := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "acc2", Annotations: map[string]string{"kcp.io/cluster": "root"}}}
	acct.Spec.Type = corev1alpha1.AccountTypeAccount
	res, opErr := r.Process(ctxTODO, acct)
	if opErr != nil || res.RequeueAfter == 0 {
		t.Fatalf("expected requeue when workspace not ready; res=%v err=%v", res, opErr)
	}
}

func TestAccountInfo_Process_MissingOriginAnnotation(t *testing.T) {
	s := runtime.NewScheme()
	_ = corev1alpha1.AddToScheme(s)
	_ = kcptenancyv1alpha.AddToScheme(s)
	_ = kcpcorev1alpha.AddToScheme(s)
	ws := &kcptenancyv1alpha.Workspace{ObjectMeta: metav1.ObjectMeta{Name: "org2"}}
	ws.Status.Phase = kcpcorev1alpha.LogicalClusterPhaseReady
	ws.Spec.Cluster = "root:orgs:org2"
	ws.Spec.URL = "https://api/clusters/root:orgs:org2"
	cl := fake.NewClientBuilder().WithScheme(s).WithObjects(ws).Build()
	r := NewAccountInfoSubroutine(cl, "CA")
	acct := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "org2"}}
	acct.Spec.Type = corev1alpha1.AccountTypeOrg
	if _, opErr := r.Process(ctxTODO, acct); opErr == nil {
		t.Fatalf("expected error for missing origin annotation")
	}
}

func TestAccountInfo_retrieveAccountInfo_NotFound(t *testing.T) {
	s := runtime.NewScheme()
	_ = corev1alpha1.AddToScheme(s)
	cl := fake.NewClientBuilder().WithScheme(s).Build()
	r := NewAccountInfoSubroutine(cl, "")
	lcfg := logger.DefaultConfig()
	lcfg.NoJSON = true
	l, _ := logger.New(lcfg)
	if ai, ok, err := r.retrieveAccountInfo(ctxTODO, l); err != nil || ok || ai != nil {
		t.Fatalf("expected not found -> (nil,false,nil), got (%v,%v,%v)", ai, ok, err)
	}
}

func TestAccountInfo_Finalize_SingleFinalizer_NoRequeue(t *testing.T) {
	r := NewAccountInfoSubroutine(nil, "")
	acct := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "a", Finalizers: []string{"account.core.platform-mesh.io/info"}}}
	res, opErr := r.Finalize(ctxTODO, acct)
	if opErr != nil || res.RequeueAfter != 0 {
		t.Fatalf("expected immediate return without requeue; res=%v err=%v", res, opErr)
	}
}

func TestAccountInfo_Process_Account_WithParent(t *testing.T) {
	s := runtime.NewScheme()
	_ = corev1alpha1.AddToScheme(s)
	_ = kcptenancyv1alpha.AddToScheme(s)
	_ = kcpcorev1alpha.AddToScheme(s)
	// Child workspace ready
	ws := &kcptenancyv1alpha.Workspace{ObjectMeta: metav1.ObjectMeta{Name: "acc3"}}
	ws.Status.Phase = kcpcorev1alpha.LogicalClusterPhaseReady
	ws.Spec.Cluster = "root:orgs:org1:acc3"
	ws.Spec.URL = "https://api/clusters/root:orgs:org1:acc3"

	// Parent AccountInfo exists with store and organization populated
	parent := &corev1alpha1.AccountInfo{ObjectMeta: metav1.ObjectMeta{Name: DefaultAccountInfoName}}
	parent.Spec.FGA.Store.Id = "store-1"
	parent.Spec.Account.Name = "org1"
	parent.Spec.Account.OriginClusterId = "root"
	parent.Spec.Organization.Name = "org1"
	parent.Spec.Organization.OriginClusterId = "root"

	cl := fake.NewClientBuilder().WithScheme(s).WithObjects(ws, parent).Build()
	r := NewAccountInfoSubroutine(cl, "CA")
	acct := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "acc3", Annotations: map[string]string{"kcp.io/cluster": "root"}}}
	acct.Spec.Type = corev1alpha1.AccountTypeAccount

	if _, opErr := r.Process(ctxTODO, acct); opErr != nil {
		t.Fatalf("unexpected err: %v", opErr)
	}
	ai := &corev1alpha1.AccountInfo{}
	if err := cl.Get(ctxTODO, client.ObjectKey{Name: DefaultAccountInfoName}, ai); err != nil {
		t.Fatalf("expected child accountinfo created: %v", err)
	}
	if ai.Spec.ParentAccount == nil || ai.Spec.FGA.Store.Id != "store-1" {
		t.Fatalf("expected parent linkage and store id copy, got: %+v", ai.Spec)
	}
}
