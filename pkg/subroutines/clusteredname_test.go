package subroutines

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	corev1alpha1 "github.com/platform-mesh/account-operator/api/v1alpha1"
)

// Use real Account CR to satisfy runtimeobject.RuntimeObject interface

func TestGetClusteredName_NoClusterInContext(t *testing.T) {
	obj := &corev1alpha1.Account{}
	obj.Name = "a"
	cn, ok := GetClusteredName(context.Background(), obj)
	require.False(t, ok)
	require.Equal(t, "a", cn.Name)
}
