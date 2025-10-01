package subroutines

import (
	"context"

	"github.com/kcp-dev/logicalcluster/v3"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/runtimeobject"
	"k8s.io/apimachinery/pkg/types"
)

type ClusteredName struct {
	types.NamespacedName
	ClusterID logicalcluster.Name
}

// GetClusteredName creates a ClusteredName without cluster context (single cluster mode)
func GetClusteredName(ctx context.Context, instance runtimeobject.RuntimeObject) (ClusteredName, bool) {
	// In single cluster mode, we use an empty cluster ID
	cn := ClusteredName{
		NamespacedName: types.NamespacedName{
			Name:      instance.GetName(), 
			Namespace: instance.GetNamespace(),
		}, 
		ClusterID: "",
	}
	return cn, true
}

// MustGetClusteredName creates a ClusteredName without cluster context (single cluster mode)
func MustGetClusteredName(ctx context.Context, instance runtimeobject.RuntimeObject) ClusteredName {
	cn := ClusteredName{
		NamespacedName: types.NamespacedName{
			Name:      instance.GetName(), 
			Namespace: instance.GetNamespace(),
		}, 
		ClusterID: "",
	}
	return cn
}