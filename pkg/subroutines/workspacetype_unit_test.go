package subroutines

import "testing"

func TestCreateWorkspaceTypeReference(t *testing.T) {
	ref := createWorkspaceTypeReference("name", "root")
	if string(ref.Name) != "name" || ref.Path != "root" {
		t.Fatalf("unexpected: %+v", ref)
	}
}

func TestCreateWorkspaceTypeSelector(t *testing.T) {
	a := createWorkspaceTypeReference("a", "root")
	b := createWorkspaceTypeReference("b", "root")
	sel := createWorkspaceTypeSelector(a, b)
	if len(sel.Types) != 2 {
		t.Fatalf("expected 2, got %d", len(sel.Types))
	}
}

func TestGetAuthConfigName(t *testing.T) {
	got := getAuthConfigName("alice", "root:orgs:team")
	if got == "" {
		t.Fatalf("expected non-empty")
	}
}
