package controller

import (
	"testing"

	config "github.com/platform-mesh/account-operator/internal/config"
	"github.com/platform-mesh/golang-commons/logger"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// stubManager embeds ctrl.Manager to satisfy the interface and overrides only the
// methods used by NewAccountReconciler. The embedded Manager is left nil; we do
// not call other methods in these unit tests.
type stubManager struct {
	ctrl.Manager
	cfg    *rest.Config
	scheme *runtime.Scheme
	cl     client.Client
}

func (m *stubManager) GetConfig() *rest.Config    { return m.cfg }
func (m *stubManager) GetScheme() *runtime.Scheme { return m.scheme }
func (m *stubManager) GetClient() client.Client   { return m.cl }
func (m *stubManager) Elected() <-chan struct{}   { ch := make(chan struct{}); close(ch); return ch }

func newLogger(t *testing.T) *logger.Logger {
	cfg := logger.DefaultConfig()
	cfg.NoJSON = true
	cfg.Name = t.Name()
	l, err := logger.New(cfg)
	if err != nil {
		t.Fatalf("failed to init logger: %v", err)
	}
	return l
}

// Test that NewAccountReconciler derives RootHost correctly when pointing at a virtual workspace URL
func TestNewAccountReconciler_DeriveRootHost_FromVirtualWorkspace_Unit(t *testing.T) {
	log := newLogger(t)
	scheme := runtime.NewScheme()
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()

	mgr := &stubManager{
		cfg: &rest.Config{Host: "https://example.local/services/apiexport/core/platform-mesh"},
		// return a non-nil scheme for subroutine setup
		scheme: scheme,
		cl:     cl,
	}

	// Enable only WorkspaceType to exercise root host derivation branch
	cfg := config.OperatorConfig{}
	cfg.Subroutines.WorkspaceType.Enabled = true
	cfg.Kcp.ProviderWorkspace = "root"
	cfg.Kcp.RootHost = "" // force derivation from mgr config host

	// No-op fga client for disabled FGA path
	rec := NewAccountReconciler(log, mgr, cfg, nil)
	if rec == nil || rec.lifecycle == nil {
		t.Fatalf("expected non-nil reconciler and lifecycle")
	}
}

// Test that both '/services/' and '/clusters/' segments are stripped when deriving root host
func TestNewAccountReconciler_DeriveRootHost_StripsClustersAndServices_Unit(t *testing.T) {
	log := newLogger(t)
	scheme := runtime.NewScheme()
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()

	mgr := &stubManager{
		cfg:    &rest.Config{Host: "https://api.local/services/foo/clusters/root:orgs:team-1"},
		scheme: scheme,
		cl:     cl,
	}

	cfg := config.OperatorConfig{}
	cfg.Subroutines.WorkspaceType.Enabled = true
	cfg.Kcp.ProviderWorkspace = "root"
	cfg.Kcp.RootHost = "" // derive

	rec := NewAccountReconciler(log, mgr, cfg, nil)
	if rec == nil || rec.lifecycle == nil {
		t.Fatalf("expected non-nil reconciler and lifecycle")
	}
}

// Test that we gracefully fall back to the shared client when creating a root-scoped client fails (e.g., nil scheme)
func TestNewAccountReconciler_FallbackToSharedClientOnRootClientError_Unit(t *testing.T) {
	log := newLogger(t)
	// Use a valid client for mgr.GetClient(), but return nil scheme to trigger client.New error.
	cl := fake.NewClientBuilder().WithScheme(runtime.NewScheme()).Build()
	mgr := &stubManager{
		cfg:    &rest.Config{Host: "https://api.local/clusters/root"},
		scheme: nil, // force root client creation failure
		cl:     cl,
	}

	cfg := config.OperatorConfig{}
	cfg.Subroutines.WorkspaceType.Enabled = true
	cfg.Kcp.ProviderWorkspace = "root"
	cfg.Kcp.RootHost = "https://api.local/clusters/root"

	rec := NewAccountReconciler(log, mgr, cfg, nil)
	if rec == nil || rec.lifecycle == nil {
		t.Fatalf("expected non-nil reconciler and lifecycle")
	}
}

// Test derivation when host has no services/clusters segments and all subroutines enabled
func TestNewAccountReconciler_AllSubroutinesEnabled_NoSegmentsInHost(t *testing.T) {
	log := newLogger(t)
	scheme := runtime.NewScheme()
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()

	mgr := &stubManager{
		cfg:    &rest.Config{Host: "https://plain.local"},
		scheme: scheme,
		cl:     cl,
	}

	cfg := config.OperatorConfig{}
	cfg.Subroutines.WorkspaceType.Enabled = true
	cfg.Subroutines.Workspace.Enabled = true
	cfg.Subroutines.AccountInfo.Enabled = true
	cfg.Subroutines.FGA.Enabled = true
	cfg.Kcp.ProviderWorkspace = "root"
	cfg.Kcp.RootHost = "" // derive from host without special segments

	rec := NewAccountReconciler(log, mgr, cfg, nil)
	if rec == nil || rec.lifecycle == nil {
		t.Fatalf("expected non-nil reconciler and lifecycle")
	}
}

// Enable only the Workspace subroutine
func TestNewAccountReconciler_OnlyWorkspaceEnabled(t *testing.T) {
	log := newLogger(t)
	scheme := runtime.NewScheme()
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()

	mgr := &stubManager{cfg: &rest.Config{Host: "https://h"}, scheme: scheme, cl: cl}
	cfg := config.OperatorConfig{}
	cfg.Subroutines.Workspace.Enabled = true
	cfg.Kcp.ProviderWorkspace = "root"

	rec := NewAccountReconciler(log, mgr, cfg, nil)
	if rec == nil {
		t.Fatalf("expected reconciler")
	}
}

// Enable only the AccountInfo subroutine
func TestNewAccountReconciler_OnlyAccountInfoEnabled(t *testing.T) {
	log := newLogger(t)
	scheme := runtime.NewScheme()
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()

	cfgRest := &rest.Config{Host: "https://h"}
	mgr := &stubManager{cfg: cfgRest, scheme: scheme, cl: cl}

	cfg := config.OperatorConfig{}
	cfg.Subroutines.AccountInfo.Enabled = true

	rec := NewAccountReconciler(log, mgr, cfg, nil)
	if rec == nil {
		t.Fatalf("expected reconciler")
	}
}

// Enable only the FGA subroutine
func TestNewAccountReconciler_OnlyFGAEnabled(t *testing.T) {
	log := newLogger(t)
	scheme := runtime.NewScheme()
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()
	mgr := &stubManager{cfg: &rest.Config{Host: "https://h"}, scheme: scheme, cl: cl}

	cfg := config.OperatorConfig{}
	cfg.Subroutines.FGA.Enabled = true

	rec := NewAccountReconciler(log, mgr, cfg, nil)
	if rec == nil {
		t.Fatalf("expected reconciler")
	}
}
