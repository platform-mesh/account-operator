package subroutines

import "testing"

func TestWorkspaceTypeNameHelpers(t *testing.T) {
	if GetOrgWorkspaceTypeName("acct", "root:orgs:team") == "" {
		t.Fatal("org name")
	}
	if GetAccWorkspaceTypeName("acct", "root:orgs:team") == "" {
		t.Fatal("acc name")
	}
}
