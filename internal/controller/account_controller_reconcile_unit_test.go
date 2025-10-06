package controller

import (
	"context"
	"errors"
	"testing"

	kcpcorev1alpha "github.com/kcp-dev/kcp/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"
	"github.com/platform-mesh/golang-commons/logger"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	crmanager "sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	corev1alpha1 "github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/account-operator/internal/config"
)

// errProvider always returns the provided error from Get.
type errProvider struct{ err error }

func (p *errProvider) Get(_ context.Context, _ string) (cluster.Cluster, error) { return nil, p.err }
func (p *errProvider) IndexField(_ context.Context, _ crclient.Object, _ string, _ crclient.IndexerFunc) error {
	return nil
}

func newTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	utilruntime.Must(corev1.AddToScheme(s))
	utilruntime.Must(corev1alpha1.AddToScheme(s))
	utilruntime.Must(kcpcorev1alpha.AddToScheme(s))
	utilruntime.Must(kcptenancyv1alpha.AddToScheme(s))
	return s
}

func newHostManager(t *testing.T, scheme *runtime.Scheme) crmanager.Manager {
	t.Helper()
	cfg := &rest.Config{Host: "https://example.invalid"}
	m, err := crmanager.New(cfg, crmanager.Options{
		Scheme:  scheme,
		Metrics: metricsserver.Options{BindAddress: "0"},
	})
	if err != nil {
		t.Fatalf("failed to construct manager: %v", err)
	}
	return m
}

func newLogger(t *testing.T) *logger.Logger {
	t.Helper()
	log, err := logger.New(logger.DefaultConfig())
	if err != nil {
		t.Fatalf("logger init: %v", err)
	}
	return log
}

func TestReconcile_ClusterNotFoundReturnsError(t *testing.T) {
	scheme := newTestScheme(t)
	host := newHostManager(t, scheme)
	mcMgr, err := mcmanager.WithMultiCluster(host, &errProvider{err: multicluster.ErrClusterNotFound})
	if err != nil {
		t.Fatalf("failed to setup mc manager: %v", err)
	}

	var cfg config.OperatorConfig // defaults; subroutines disabled so we don't build anything if reached
	r := NewAccountReconciler(newLogger(t), mcMgr, cfg, nil)

	// Call reconcile with a non-existent cluster. Commons lifecycle returns an error in this case.
	_, recErr := r.Reconcile(context.Background(), mcreconcile.Request{Request: ctrl.Request{NamespacedName: types.NamespacedName{Name: "dummy"}}, ClusterName: "missing"})
	if recErr == nil {
		t.Fatalf("expected error for ErrClusterNotFound")
	}
}

func TestReconcile_GetClusterOtherErrorIsReturned(t *testing.T) {
	scheme := newTestScheme(t)
	host := newHostManager(t, scheme)
	mcMgr, err := mcmanager.WithMultiCluster(host, &errProvider{err: errors.New("boom")})
	if err != nil {
		t.Fatalf("failed to setup mc manager: %v", err)
	}

	r := NewAccountReconciler(newLogger(t), mcMgr, config.OperatorConfig{}, nil)
	_, recErr := r.Reconcile(context.Background(), mcreconcile.Request{Request: ctrl.Request{NamespacedName: types.NamespacedName{Name: "dummy"}}, ClusterName: "any"})
	if recErr == nil {
		t.Fatalf("expected error to be returned")
	}
}
