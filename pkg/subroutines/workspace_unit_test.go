package subroutines

import (
	"testing"
	"time"

	"github.com/platform-mesh/account-operator/internal/config"
)

func TestWorkspaceSubroutine_getWorkspaceTypeRequeueDelay_Default(t *testing.T) {
	r := NewWorkspaceSubroutine(nil)
	// zero value config -> default
	d := r.getWorkspaceTypeRequeueDelay(config.OperatorConfig{})
	if d != defaultWorkspaceTypeRequeueDelay {
		t.Fatalf("expected default %v, got %v", defaultWorkspaceTypeRequeueDelay, d)
	}
}

func TestWorkspaceSubroutine_getWorkspaceTypeRequeueDelay_Custom(t *testing.T) {
	r := NewWorkspaceSubroutine(nil)
	cfg := config.OperatorConfig{}
	cfg.Subroutines.Workspace.WorkspaceTypeRequeueDelay = "17s"
	d := r.getWorkspaceTypeRequeueDelay(cfg)
	if d != 17*time.Second {
		t.Fatalf("expected 17s, got %v", d)
	}
}

func TestWorkspaceSubroutine_getForbiddenRequeueDelay_Default(t *testing.T) {
	r := NewWorkspaceSubroutine(nil)
	d := r.getForbiddenRequeueDelay(config.OperatorConfig{})
	if d != defaultForbiddenRequeueDelay {
		t.Fatalf("expected default %v, got %v", defaultForbiddenRequeueDelay, d)
	}
}

func TestWorkspaceSubroutine_getForbiddenRequeueDelay_Custom(t *testing.T) {
	r := NewWorkspaceSubroutine(nil)
	cfg := config.OperatorConfig{}
	cfg.Subroutines.Workspace.ForbiddenRequeueDelay = "45s"
	d := r.getForbiddenRequeueDelay(cfg)
	if d != 45*time.Second {
		t.Fatalf("expected 45s, got %v", d)
	}
}
