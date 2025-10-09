package subroutines

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/cluster"
)

// ClusterClientGetter abstracts the ability to retrieve a cluster-scoped client.
type ClusterClientGetter interface {
	GetCluster(ctx context.Context, clusterName string) (cluster.Cluster, error)
}
