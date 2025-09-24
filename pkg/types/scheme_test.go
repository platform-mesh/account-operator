package types

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
)

// TestAddKnownTypes tests the addKnownTypes function to ensure coverage
func TestAddKnownTypes(t *testing.T) {
	scheme := runtime.NewScheme()

	err := addKnownTypes(scheme)
	if err != nil {
		t.Errorf("addKnownTypes returned error: %v", err)
	}

	// Verify that the types were added to the scheme
	gvk := GroupVersion.WithKind("Workspace")
	if !scheme.Recognizes(gvk) {
		t.Error("Workspace type not recognized by scheme")
	}

	gvk = GroupVersion.WithKind("WorkspaceList")
	if !scheme.Recognizes(gvk) {
		t.Error("WorkspaceList type not recognized by scheme")
	}

	gvk = GroupVersion.WithKind("LogicalCluster")
	if !scheme.Recognizes(gvk) {
		t.Error("LogicalCluster type not recognized by scheme")
	}

	gvk = GroupVersion.WithKind("LogicalClusterList")
	if !scheme.Recognizes(gvk) {
		t.Error("LogicalClusterList type not recognized by scheme")
	}
}
