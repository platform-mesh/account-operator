package subroutines

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
)

// ClusterClientGetter abstracts the ability to retrieve a cluster-scoped client.
type ClusterClientGetter interface {
	GetCluster(ctx context.Context, clusterName string) (cluster.Cluster, error)
}

func clientForContext(ctx context.Context, getter ClusterClientGetter, fallback client.Client) (client.Client, error) {
	if getter != nil {
		if clusterName, ok := mccontext.ClusterFrom(ctx); ok {
			cl, err := getter.GetCluster(ctx, clusterName)
			if err != nil {
				return nil, err
			}
			return cl.GetClient(), nil
		}
	}
	if fallback != nil {
		return fallback, nil
	}
	return nil, fmt.Errorf("cluster client not available: ensure context carries cluster information")
}
