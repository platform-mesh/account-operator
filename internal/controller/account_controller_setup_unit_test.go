package controller

import (
	"testing"

	platformmeshconfig "github.com/platform-mesh/golang-commons/config"
	corev1 "k8s.io/api/core/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/rest"
	crconfig "sigs.k8s.io/controller-runtime/pkg/config"
	crmanager "sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	corev1alpha1 "github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/account-operator/internal/config"
)

// buildHostMcManager creates a controller-runtime manager wrapped as a multicluster manager for setup tests.
func buildHostMcManager(t *testing.T) mcmanager.Manager {
	t.Helper()
	scheme := newTestScheme(t)
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(corev1alpha1.AddToScheme(scheme))

	cfg := &rest.Config{Host: "https://example.invalid"}
	skip := true
	host, err := crmanager.New(cfg, crmanager.Options{Scheme: scheme, Metrics: metricsserver.Options{BindAddress: "0"}, Controller: crconfig.Controller{SkipNameValidation: &skip}})
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	mc, err := mcmanager.WithMultiCluster(host, nil)
	if err != nil {
		t.Fatalf("mc manager: %v", err)
	}
	return mc
}

func TestSetupWithManager_NoPredicates(t *testing.T) {
	mc := buildHostMcManager(t)
	r := NewAccountReconciler(newLogger(t), mc, config.OperatorConfig{}, nil)

	// No predicates, zero MaxConcurrentReconciles
	cfg := &platformmeshconfig.CommonServiceConfig{}
	if err := r.SetupWithManager(mc, cfg, newLogger(t)); err != nil {
		t.Fatalf("SetupWithManager returned error: %v", err)
	}
}
