package subroutines

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"

	corev1alpha1 "github.com/platform-mesh/account-operator/api/v1alpha1"
)

// recordingGetter records the last cluster name requested and always returns an error.
type recordingGetter struct {
	last  string
	calls int
}

func (g *recordingGetter) GetCluster(_ context.Context, clusterName string) (cluster.Cluster, error) {
	g.last = clusterName
	g.calls++
	return nil, errors.New("no cluster available in test")
}

func newScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	utilruntime.Must(corev1.AddToScheme(s))
	utilruntime.Must(corev1alpha1.AddToScheme(s))
	return s
}

func TestWorkspaceSubroutine_UsesClusterClientFromContext(t *testing.T) {
	t.Parallel()

	getter := &recordingGetter{}
	scheme := newScheme(t)

	// localClient is nil on purpose so the subroutine must use the getter path.
	sub := NewWorkspaceSubroutine(getter, nil, nil, scheme)

	acc := &corev1alpha1.Account{}

	// First call with cluster "alpha".
	ctxAlpha := mccontext.WithCluster(context.Background(), "alpha")
	_, opErr := sub.Process(ctxAlpha, acc)
	if opErr == nil {
		t.Fatalf("expected an operator error due to missing test cluster, got nil")
	}
	require.Equal(t, "alpha", getter.last, "expected getter to be called with cluster 'alpha'")

	// Second call with cluster "beta".
	ctxBeta := mccontext.WithCluster(context.Background(), "beta")
	_, opErr = sub.Process(ctxBeta, acc)
	if opErr == nil {
		t.Fatalf("expected an operator error due to missing test cluster, got nil")
	}
	require.Equal(t, "beta", getter.last, "expected getter to be called with cluster 'beta'")

	require.Equal(t, 2, getter.calls)
}
