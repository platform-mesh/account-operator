package subroutines

import "testing"

func TestFormatUser_ServiceAccountTransforms(t *testing.T) {
	in := "system:serviceaccount:ns:name"
	out := formatUser(in)
	if out != "system.serviceaccount.ns.name" {
		t.Fatalf("unexpected transform: %q", out)
	}
}

func TestFGASubroutine_Finalizers_NonEmpty(t *testing.T) {
	sub := NewFGASubroutine(nil, nil, "owner", "parent", "obj")
	if len(sub.Finalizers()) == 0 {
		t.Fatal("expected at least one finalizer")
	}
}

func TestFormatUser_OtherUnchanged(t *testing.T) {
	in := "alice@example.com"
	if formatUser(in) != in {
		t.Fatalf("should not change non-SA user")
	}
}

func TestValidateCreator(t *testing.T) {
	if !validateCreator("alice") {
		t.Fatal("alice should be valid")
	}
	if validateCreator("system.serviceaccount.ns.name") {
		t.Fatal("service account prefix should be invalid")
	}
}
