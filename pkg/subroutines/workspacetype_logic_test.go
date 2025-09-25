package subroutines

import (
	"testing"

	kcptenancyv1alpha "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"
	operatorconfig "github.com/platform-mesh/account-operator/internal/config"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newWTClient(t *testing.T) client.Client {
	s := runtime.NewScheme()
	_ = kcptenancyv1alpha.AddToScheme(s)
	return fake.NewClientBuilder().WithScheme(s).Build()
}

func TestCreateCustomAccountAndOrgWorkspaceTypes(t *testing.T) {
	cl := newWTClient(t)
	r := &WorkspaceTypeSubroutine{client: cl, rootClient: cl}
	ctx := ctxTODO

	// base account type to copy from
	baseAcc := &kcptenancyv1alpha.WorkspaceType{}
	if err := r.createCustomAccountWorkspaceType(ctx, "alice", "root:orgs:team", "team-alice-org", baseAcc); err != nil {
		t.Fatalf("create custom account WT: %v", err)
	}
	if err := r.createCustomOrgWorkspaceType(ctx, "alice", "root:orgs:team", nil); err != nil {
		t.Fatalf("create custom org WT: %v", err)
	}

	// Verify they exist
	acc := &kcptenancyv1alpha.WorkspaceType{}
	if err := cl.Get(ctx, client.ObjectKey{Name: GetAccWorkspaceTypeName("alice", "root:orgs:team")}, acc); err != nil {
		t.Fatalf("get custom acc: %v", err)
	}
	org := &kcptenancyv1alpha.WorkspaceType{}
	if err := cl.Get(ctx, client.ObjectKey{Name: GetOrgWorkspaceTypeName("alice", "root:orgs:team")}, org); err != nil {
		t.Fatalf("get custom org: %v", err)
	}
	if org.Spec.DefaultChildWorkspaceType == nil {
		t.Fatalf("expected default child set")
	}
}

func TestUpdateBaseOrgWorkspaceType_AddsChildRef(t *testing.T) {
	cl := newWTClient(t)
	r := &WorkspaceTypeSubroutine{client: cl, rootClient: cl}
	ctx := ctxTODO

	baseOrg := &kcptenancyv1alpha.WorkspaceType{}
	// set name and create object in fake client by creating then getting
	baseOrg.Name = "org"
	if err := cl.Create(ctx, baseOrg); err != nil {
		t.Fatalf("create base org: %v", err)
	}

	err := r.updateBaseOrgWorkspaceType(ctx, operatorconfig.OperatorConfig{Kcp: struct {
		ApiExportEndpointSliceName      string `mapstructure:"kcp-api-export-endpoint-slice-name"`
		ProviderWorkspace               string `mapstructure:"kcp-provider-workspace" default:"root"`
		RootHost                        string `mapstructure:"kcp-root-host" default:""`
		OrgWorkspaceCluster             string `mapstructure:"kcp-org-workspace-cluster" default:""`
		RelaxForbiddenWorkspaceCreation bool   `mapstructure:"kcp-relax-forbidden-workspace-creation" default:"false"`
	}{ProviderWorkspace: "root"}}, baseOrg, "team-alice-org", "root:orgs:team", nil)
	if err != nil {
		t.Fatalf("update base org: %v", err)
	}
}
