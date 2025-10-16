package clusteredname_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	corev1alpha1 "github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/account-operator/pkg/clusteredname"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
)

func TestGetClusteredName_NoClusterInContext(t *testing.T) {
	obj := &corev1alpha1.Account{
		ObjectMeta: metav1.ObjectMeta{
			Name: "a",
		},
	}

	cn, ok := clusteredname.GetClusteredName(t.Context(), obj)

	require.False(t, ok)
	require.Equal(t, "a", cn.Name)
}

func TestGetClusteredName_WithClusterInContext(t *testing.T) {
	obj := &corev1alpha1.Account{}
	obj.Name = "b"

	ctx := mccontext.WithCluster(context.Background(), "root:orgs")
	cn, ok := clusteredname.GetClusteredName(ctx, obj)

	require.True(t, ok)
	require.Equal(t, "b", cn.Name)
	require.Equal(t, "root:orgs", cn.ClusterID.String())
}

func TestMustGetClusteredName_PanicsWithoutCluster(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic when cluster missing")
		}
	}()
	obj := &corev1alpha1.Account{}
	_ = clusteredname.MustGetClusteredName(context.Background(), obj)
}
