package subroutines

import (
	"context"
	"net/http"

	metameta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
)

// fakeCluster implements cluster.Cluster for testing purposes.
type fakeCluster struct {
	client client.Client
}

func (f *fakeCluster) GetHTTPClient() *http.Client          { return nil }
func (f *fakeCluster) GetConfig() *rest.Config              { return &rest.Config{} }
func (f *fakeCluster) GetCache() cache.Cache                { return nil }
func (f *fakeCluster) GetScheme() *runtime.Scheme           { return nil }
func (f *fakeCluster) GetClient() client.Client             { return f.client }
func (f *fakeCluster) GetFieldIndexer() client.FieldIndexer { return nil }
func (f *fakeCluster) GetEventRecorderFor(string) record.EventRecorder {
	return record.NewFakeRecorder(1)
}
func (f *fakeCluster) GetRESTMapper() metameta.RESTMapper { return nil }
func (f *fakeCluster) GetAPIReader() client.Reader        { return nil }
func (f *fakeCluster) Start(context.Context) error        { return nil }

// fakeClusterGetter returns a preconfigured cluster and error.
type fakeClusterGetter struct {
	cluster cluster.Cluster
	err     error
}

func (g fakeClusterGetter) GetCluster(_ context.Context, _ string) (cluster.Cluster, error) {
	if g.err != nil {
		return nil, g.err
	}
	return g.cluster, nil
}
