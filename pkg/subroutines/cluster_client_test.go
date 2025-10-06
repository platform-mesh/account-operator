package subroutines

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
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

func TestClientForContextFallsBackToLocalClient(t *testing.T) {
	scheme := runtime.NewScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	got, err := clientForContext(context.Background(), nil, fakeClient)
	require.NoError(t, err)
	require.Equal(t, fakeClient, got)
}

func TestClientForContextErrorsWithoutFallback(t *testing.T) {
	_, err := clientForContext(context.Background(), nil, nil)
	require.Error(t, err)
}

func TestClientForContextGetterPresentButNoClusterUsesFallback(t *testing.T) {
	scheme := runtime.NewScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	// Provide a getter, but don’t set cluster in context; should ignore getter and use fallback
	getter := &recordingGetter{}
	got, err := clientForContext(context.Background(), getter, fakeClient)
	require.NoError(t, err)
	require.Equal(t, fakeClient, got)
	// Getter should not be called since no cluster in context
	require.Equal(t, 0, getter.calls)
}
