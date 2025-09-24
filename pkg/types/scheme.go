package types

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	// GroupVersion is group version used to register these objects
	GroupVersion = schema.GroupVersion{Group: "tenancy.kcp.io", Version: "v1alpha1"}

	// SchemeBuilder is used to add go types to the GroupVersionKind scheme
	SchemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)

	// AddToScheme adds the types in this group-version to the given scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)

func addKnownTypes(scheme *runtime.Scheme) error { // coverage-ignore
	scheme.AddKnownTypes(GroupVersion,
		&Workspace{},
		&WorkspaceList{},
		&LogicalCluster{},
		&LogicalClusterList{},
	)

	// Add APIExportEndpointSlice types
	apiGroupVersion := schema.GroupVersion{Group: "apis.kcp.io", Version: "v1alpha1"}
	scheme.AddKnownTypes(apiGroupVersion,
		&APIExport{},
		&APIExportEndpointSlice{},
		&APIExportEndpointSliceList{},
	)

	return nil
}
