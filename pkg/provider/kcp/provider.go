/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package kcp provides a KCP cluster provider that discovers logical clusters
// through KCP APIExport endpoints and makes them available for multi-cluster controllers.
package kcp

import (
	"context"
	"fmt"
	"sync"

	apisv1alpha1 "github.com/kcp-dev/kcp/sdk/apis/apis/v1alpha1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"

	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"
)

var _ multicluster.Provider = &Provider{}

// Provider is a KCP cluster provider that discovers logical clusters through KCP APIExport endpoints.
type Provider struct {
	config                     *rest.Config
	apiExportEndpointSliceName string
	clusters                   map[string]cluster.Cluster
	mu                         sync.RWMutex
}

// Options contains configuration for the KCP provider.
type Options struct {
	// APIExportEndpointSliceName is the name of the APIExportEndpointSlice to watch
	APIExportEndpointSliceName string
}

// New creates a new KCP provider.
func New(config *rest.Config, opts Options) *Provider {
	return &Provider{
		config:                     config,
		apiExportEndpointSliceName: opts.APIExportEndpointSliceName,
		clusters:                   make(map[string]cluster.Cluster),
	}
}

// Run starts the provider and discovers KCP logical clusters.
func (p *Provider) Run(ctx context.Context, mgr mcmanager.Manager) error {
	if p.apiExportEndpointSliceName == "" {
		// Single cluster mode - create a default cluster
		defaultCluster, err := cluster.New(p.config, func(o *cluster.Options) {
			o.Scheme = mgr.GetLocalManager().GetScheme()
		})
		if err != nil {
			return fmt.Errorf("failed to create default cluster: %w", err)
		}

		if err := mgr.Engage(ctx, "default", defaultCluster); err != nil {
			return fmt.Errorf("failed to engage default cluster: %w", err)
		}

		p.mu.Lock()
		p.clusters["default"] = defaultCluster
		p.mu.Unlock()
	} else {
		// KCP multi-cluster mode - discover APIExport endpoints
		if err := p.discoverAPIExportEndpoints(ctx, mgr); err != nil {
			return fmt.Errorf("failed to discover APIExport endpoints: %w", err)
		}
	}

	// Block until context is done
	<-ctx.Done()
	return nil
} // Get returns the cluster with the given name.
func (p *Provider) Get(ctx context.Context, clusterName string) (cluster.Cluster, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	cl, exists := p.clusters[clusterName]
	if !exists {
		return nil, multicluster.ErrClusterNotFound
	}
	return cl, nil
}

// IndexField calls IndexField on all managed clusters.
func (p *Provider) IndexField(ctx context.Context, obj client.Object, field string, extractValue client.IndexerFunc) error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	for _, cl := range p.clusters {
		if err := cl.GetFieldIndexer().IndexField(ctx, obj, field, extractValue); err != nil {
			return err
		}
	}
	return nil
}

// discoverAPIExportEndpoints discovers KCP logical clusters through APIExport endpoints.
func (p *Provider) discoverAPIExportEndpoints(ctx context.Context, mgr mcmanager.Manager) error {
	// Create a client to lookup APIExportEndpointSlice
	kclient, err := client.New(p.config, client.Options{
		Scheme: mgr.GetLocalManager().GetScheme(),
	})
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	// Get APIExportEndpointSlice
	es := &apisv1alpha1.APIExportEndpointSlice{}
	err = kclient.Get(ctx, client.ObjectKey{Name: p.apiExportEndpointSliceName}, es)
	if err != nil {
		return fmt.Errorf("failed to get APIExportEndpointSlice %s: %w", p.apiExportEndpointSliceName, err)
	}

	if len(es.Status.APIExportEndpoints) == 0 {
		return fmt.Errorf("no APIExportEndpoints found in APIExportEndpointSlice %s", p.apiExportEndpointSliceName)
	}

	// Create cluster for the virtual workspace endpoint
	virtualConfig := rest.CopyConfig(p.config)
	virtualConfig.Host = es.Status.APIExportEndpoints[0].URL

	virtualCluster, err := cluster.New(virtualConfig, func(o *cluster.Options) {
		o.Scheme = mgr.GetLocalManager().GetScheme()
	})
	if err != nil {
		return fmt.Errorf("failed to create virtual workspace cluster: %w", err)
	}

	// Engage the virtual workspace cluster
	clusterName := "kcp-virtual"
	if err := mgr.Engage(ctx, clusterName, virtualCluster); err != nil {
		return fmt.Errorf("failed to engage virtual workspace cluster: %w", err)
	}

	p.mu.Lock()
	p.clusters[clusterName] = virtualCluster
	p.mu.Unlock()

	return nil
}
