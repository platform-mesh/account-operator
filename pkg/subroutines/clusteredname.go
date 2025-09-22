package subroutines

import (
	"context"

	"github.com/platform-mesh/golang-commons/controller/lifecycle/runtimeobject"
	"k8s.io/apimachinery/pkg/types"
)

// LogicalClusterName represents a logical cluster name
type LogicalClusterName string

type ClusteredName struct {
	types.NamespacedName
	ClusterID LogicalClusterName
}

func GetClusteredName(ctx context.Context, instance runtimeobject.RuntimeObject) (ClusteredName, bool) {
	var cn ClusteredName
	// Upstream controller-runtime does not inject KCP cluster into context.
	// Without KCP's fork, we cannot extract a logical cluster from context.
	// Return false to indicate absence; callers should handle single-cluster behavior.
	_ = ctx
	cn = ClusteredName{NamespacedName: types.NamespacedName{Name: instance.GetName(), Namespace: instance.GetNamespace()}}
	return cn, false
}

func MustGetClusteredName(ctx context.Context, instance runtimeobject.RuntimeObject) ClusteredName {
	// No cluster in context when not using KCP controller-runtime; panic to preserve contract
	_ = ctx
	panic("cluster not found in context, cannot requeue")
}
