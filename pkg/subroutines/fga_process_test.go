package subroutines

import (
	"testing"

	kcpcorev1alpha "github.com/kcp-dev/kcp/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"
	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	corev1alpha1 "github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/account-operator/pkg/subroutines/mocks"
	"github.com/stretchr/testify/mock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newFGAScheme(t *testing.T) *runtime.Scheme {
	s := runtime.NewScheme()
	_ = corev1alpha1.AddToScheme(s)
	_ = kcptenancyv1alpha.AddToScheme(s)
	_ = kcpcorev1alpha.AddToScheme(s)
	return s
}

// Helper to create a ready Workspace matching the account name
func readyWorkspace(name, cluster, url string) *kcptenancyv1alpha.Workspace {
	ws := &kcptenancyv1alpha.Workspace{ObjectMeta: metav1.ObjectMeta{Name: name}}
	ws.Spec.Cluster = cluster
	ws.Spec.URL = url
	ws.Status.Phase = kcpcorev1alpha.LogicalClusterPhaseReady
	return ws
}

func TestFGASubroutine_Process_InvalidCreator_NoWrites(t *testing.T) {
	s := newFGAScheme(t)
	// Workspace for org account is ready
	ws := readyWorkspace("org1", "root:orgs:org1", "https://api/clusters/root:orgs:org1")
	// AccountInfo in workspace with required fields set
	ai := &corev1alpha1.AccountInfo{ObjectMeta: metav1.ObjectMeta{Name: DefaultAccountInfoName}}
	ai.Spec.FGA.Store.Id = "store-1"
	ai.Spec.Account.GeneratedClusterId = "root:orgs:org1"
	ai.Spec.Account.OriginClusterId = "root"

	cl := fake.NewClientBuilder().WithScheme(s).WithObjects(ws, ai).Build()
	mockFGA := mocks.NewOpenFGAServiceClient(t)
	// Do not expect any Write calls when creator is invalid

	sub := NewFGASubroutine(cl, mockFGA, "owner", "parent", "core_platform-mesh_io_account")

	acct := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "org1"}}
	acct.Spec.Type = corev1alpha1.AccountTypeOrg
	// Intentionally pass a protected creator prefix (already dotted) to fail validation
	creator := "system.serviceaccount.ns.name"
	acct.Spec.Creator = &creator

	if _, opErr := sub.Process(ctxTODO, acct); opErr == nil {
		t.Fatalf("expected operator error for invalid creator")
	}
}

func TestFGASubroutine_Process_CreatorWrites_Success(t *testing.T) {
	s := newFGAScheme(t)
	ws := readyWorkspace("org1", "root:orgs:org1", "https://api/clusters/root:orgs:org1")
	ai := &corev1alpha1.AccountInfo{ObjectMeta: metav1.ObjectMeta{Name: DefaultAccountInfoName}}
	ai.Spec.FGA.Store.Id = "store-1"
	ai.Spec.Account.GeneratedClusterId = "root:orgs:org1"
	ai.Spec.Account.OriginClusterId = "root"

	cl := fake.NewClientBuilder().WithScheme(s).WithObjects(ws, ai).Build()
	mockFGA := mocks.NewOpenFGAServiceClient(t)
	mockFGA.On("Write", mock.Anything, mock.Anything).Return(&openfgav1.WriteResponse{}, nil).Twice()

	sub := NewFGASubroutine(cl, mockFGA, "owner", "parent", "core_platform-mesh_io_account")

	acct := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "org1"}}
	acct.Spec.Type = corev1alpha1.AccountTypeOrg
	creator := "alice@example.com"
	acct.Spec.Creator = &creator

	if _, opErr := sub.Process(ctxTODO, acct); opErr != nil {
		t.Fatalf("unexpected operator error: %v", opErr)
	}
}

func TestFGASubroutine_Process_Account_ParentRelationWrite(t *testing.T) {
	s := newFGAScheme(t)
	ws := readyWorkspace("acc1", "root:orgs:org1:acc1", "https://api/clusters/root:orgs:org1:acc1")
	ai := &corev1alpha1.AccountInfo{ObjectMeta: metav1.ObjectMeta{Name: DefaultAccountInfoName}}
	ai.Spec.FGA.Store.Id = "store-1"
	ai.Spec.Account.GeneratedClusterId = "root:orgs:org1:acc1"
	ai.Spec.Account.OriginClusterId = "root"
	ai.Spec.ParentAccount = &corev1alpha1.AccountLocation{}
	ai.Spec.ParentAccount.GeneratedClusterId = "root:orgs:org1"
	ai.Spec.ParentAccount.OriginClusterId = "root"
	ai.Spec.ParentAccount.Name = "org1"

	cl := fake.NewClientBuilder().WithScheme(s).WithObjects(ws, ai).Build()
	mockFGA := mocks.NewOpenFGAServiceClient(t)
	mockFGA.On("Write", mock.Anything, mock.Anything).Return(&openfgav1.WriteResponse{}, nil).Once()

	sub := NewFGASubroutine(cl, mockFGA, "owner", "parent", "core_platform-mesh_io_account")

	acct := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "acc1"}}
	acct.Spec.Type = corev1alpha1.AccountTypeAccount

	if _, opErr := sub.Process(ctxTODO, acct); opErr != nil {
		t.Fatalf("unexpected operator error: %v", opErr)
	}
}

func TestFGASubroutine_Finalize_Account_DeletesTuples(t *testing.T) {
	s := newFGAScheme(t)
	ai := &corev1alpha1.AccountInfo{ObjectMeta: metav1.ObjectMeta{Name: DefaultAccountInfoName}}
	ai.Spec.FGA.Store.Id = "store-1"
	ai.Spec.Account.GeneratedClusterId = "root:orgs:org1:acc1"
	ai.Spec.Account.OriginClusterId = "root"
	ai.Spec.ParentAccount = &corev1alpha1.AccountLocation{}
	ai.Spec.ParentAccount.GeneratedClusterId = "root:orgs:org1"
	ai.Spec.ParentAccount.OriginClusterId = "root"
	ai.Spec.ParentAccount.Name = "org1"

	cl := fake.NewClientBuilder().WithScheme(s).WithObjects(ai).Build()
	mockFGA := mocks.NewOpenFGAServiceClient(t)
	// Expect three delete writes when creator present: parent relation + 2 creator-related
	mockFGA.On("Write", mock.Anything, mock.Anything).Return(&openfgav1.WriteResponse{}, nil).Times(3)

	sub := NewFGASubroutine(cl, mockFGA, "owner", "parent", "core_platform-mesh_io_account")

	acct := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "acc1"}}
	acct.Spec.Type = corev1alpha1.AccountTypeAccount
	creator := "alice@example.com"
	acct.Spec.Creator = &creator

	if _, opErr := sub.Finalize(ctxTODO, acct); opErr != nil {
		t.Fatalf("unexpected operator error: %v", opErr)
	}
}

func TestFGASubroutine_Process_Error_EmptyStoreId(t *testing.T) {
	s := newFGAScheme(t)
	ws := readyWorkspace("org1", "root:orgs:org1", "https://api/clusters/root:orgs:org1")
	ai := &corev1alpha1.AccountInfo{ObjectMeta: metav1.ObjectMeta{Name: DefaultAccountInfoName}}
	// leave Store.Id empty
	ai.Spec.Account.GeneratedClusterId = "root:orgs:org1"
	ai.Spec.Account.OriginClusterId = "root"

	cl := fake.NewClientBuilder().WithScheme(s).WithObjects(ws, ai).Build()
	mockFGA := mocks.NewOpenFGAServiceClient(t)
	sub := NewFGASubroutine(cl, mockFGA, "owner", "parent", "core_platform-mesh_io_account")
	acct := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "org1"}}
	acct.Spec.Type = corev1alpha1.AccountTypeOrg
	if _, opErr := sub.Process(ctxTODO, acct); opErr == nil {
		t.Fatalf("expected error for empty store id")
	}
}

func TestFGASubroutine_Process_Error_EmptyAccountClusterId(t *testing.T) {
	s := newFGAScheme(t)
	ws := readyWorkspace("org1", "root:orgs:org1", "https://api/clusters/root:orgs:org1")
	ai := &corev1alpha1.AccountInfo{ObjectMeta: metav1.ObjectMeta{Name: DefaultAccountInfoName}}
	ai.Spec.FGA.Store.Id = "store-1"
	// leave Account.GeneratedClusterId empty to trigger error
	ai.Spec.Account.OriginClusterId = "root"

	cl := fake.NewClientBuilder().WithScheme(s).WithObjects(ws, ai).Build()
	mockFGA := mocks.NewOpenFGAServiceClient(t)
	sub := NewFGASubroutine(cl, mockFGA, "owner", "parent", "core_platform-mesh_io_account")
	acct := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "org1"}}
	acct.Spec.Type = corev1alpha1.AccountTypeOrg
	if _, opErr := sub.Process(ctxTODO, acct); opErr == nil {
		t.Fatalf("expected error for empty account cluster id")
	}
}

func TestFGASubroutine_Process_Error_EmptyParentClusterId(t *testing.T) {
	s := newFGAScheme(t)
	ws := readyWorkspace("acc1", "root:orgs:org1:acc1", "https://api/clusters/root:orgs:org1:acc1")
	ai := &corev1alpha1.AccountInfo{ObjectMeta: metav1.ObjectMeta{Name: DefaultAccountInfoName}}
	ai.Spec.FGA.Store.Id = "store-1"
	ai.Spec.Account.GeneratedClusterId = "root:orgs:org1:acc1"
	ai.Spec.Account.OriginClusterId = "root"
	// ParentAccount exists but missing GeneratedClusterId
	ai.Spec.ParentAccount = &corev1alpha1.AccountLocation{}
	ai.Spec.ParentAccount.OriginClusterId = "root"
	ai.Spec.ParentAccount.Name = "org1"

	cl := fake.NewClientBuilder().WithScheme(s).WithObjects(ws, ai).Build()
	mockFGA := mocks.NewOpenFGAServiceClient(t)
	sub := NewFGASubroutine(cl, mockFGA, "owner", "parent", "core_platform-mesh_io_account")
	acct := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "acc1"}}
	acct.Spec.Type = corev1alpha1.AccountTypeAccount
	if _, opErr := sub.Process(ctxTODO, acct); opErr == nil {
		t.Fatalf("expected error for empty parent account cluster id")
	}
}

func TestFGASubroutine_Process_GetAccountInfoError(t *testing.T) {
	s := newFGAScheme(t)
	// Workspace exists but AccountInfo missing -> getAccountInfo error
	ws := readyWorkspace("org1", "root:orgs:org1", "https://api/clusters/root:orgs:org1")
	cl := fake.NewClientBuilder().WithScheme(s).WithObjects(ws).Build()
	mockFGA := mocks.NewOpenFGAServiceClient(t)

	sub := NewFGASubroutine(cl, mockFGA, "owner", "parent", "core_platform-mesh_io_account")
	acct := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "org1"}}
	acct.Spec.Type = corev1alpha1.AccountTypeOrg

	if _, opErr := sub.Process(ctxTODO, acct); opErr == nil {
		t.Fatalf("expected operator error when AccountInfo missing")
	}
}

func TestFGASubroutine_Process_WorkspaceNotReady_Requeues(t *testing.T) {
	s := newFGAScheme(t)
	ws := &kcptenancyv1alpha.Workspace{ObjectMeta: metav1.ObjectMeta{Name: "org2"}}
	// Not setting Status.Phase -> not ready
	ws.Spec.Cluster = "root:orgs:org2"
	ws.Spec.URL = "https://api/clusters/root:orgs:org2"
	cl := fake.NewClientBuilder().WithScheme(s).WithObjects(ws).Build()
	mockFGA := mocks.NewOpenFGAServiceClient(t)
	sub := NewFGASubroutine(cl, mockFGA, "owner", "parent", "core_platform-mesh_io_account")
	acct := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "org2"}}
	acct.Spec.Type = corev1alpha1.AccountTypeOrg
	res, opErr := sub.Process(ctxTODO, acct)
	if opErr != nil || res.RequeueAfter == 0 {
		t.Fatalf("expected requeue when workspace not ready; res=%v err=%v", res, opErr)
	}
}

func TestFGASubroutine_Process_WorkspaceMissing_Error(t *testing.T) {
	s := newFGAScheme(t)
	cl := fake.NewClientBuilder().WithScheme(s).Build()
	mockFGA := mocks.NewOpenFGAServiceClient(t)
	sub := NewFGASubroutine(cl, mockFGA, "owner", "parent", "core_platform-mesh_io_account")
	acct := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "ghost"}}
	acct.Spec.Type = corev1alpha1.AccountTypeOrg
	if _, opErr := sub.Process(ctxTODO, acct); opErr == nil {
		t.Fatalf("expected error when workspace missing")
	}
}
