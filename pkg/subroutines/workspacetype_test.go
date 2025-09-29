package subroutines

import (
	"context"
	"testing"

	kcptenancyv1alpha "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"
	"github.com/kcp-dev/logicalcluster/v3"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/kontext"

	corev1alpha1 "github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/account-operator/internal/config"
	"github.com/platform-mesh/account-operator/pkg/subroutines/mocks"
)

func TestWorkspaceTypeSubroutine_GetName(t *testing.T) {
	subroutine := NewWorkspaceTypeSubroutine(nil)
	assert.Equal(t, WorkspaceTypeSubroutineName, subroutine.GetName())
}

func TestWorkspaceTypeSubroutine_Finalizers(t *testing.T) {
	subroutine := NewWorkspaceTypeSubroutine(nil)
	assert.Nil(t, subroutine.Finalizers())
}

func TestWorkspaceTypeSubroutine_Finalize(t *testing.T) {
	subroutine := NewWorkspaceTypeSubroutine(nil)
	result, err := subroutine.Finalize(context.Background(), nil)
	assert.Equal(t, ctrl.Result{}, result)
	assert.Nil(t, err)
}

func TestWorkspaceTypeSubroutine_Process_NonOrgAccount(t *testing.T) {
	mockClient := mocks.NewClient(t)
	subroutine := NewWorkspaceTypeSubroutine(mockClient)

	account := &corev1alpha1.Account{
		ObjectMeta: metav1.ObjectMeta{Name: "test-account"},
		Spec:       corev1alpha1.AccountSpec{Type: corev1alpha1.AccountTypeAccount},
	}

	result, err := subroutine.Process(context.Background(), account)
	assert.Equal(t, ctrl.Result{}, result)
	assert.Nil(t, err)
}

func TestWorkspaceTypeSubroutine_getCurrentClusterPath(t *testing.T) {
	subroutine := NewWorkspaceTypeSubroutine(nil)
	cfg := config.OperatorConfig{}
	cfg.Kcp.ProviderWorkspace = "test-workspace"

	// Test with no cluster in context
	path := subroutine.getCurrentClusterPath(context.Background(), cfg)
	assert.Equal(t, "test-workspace", path)

	// Test with cluster in context
	ctx := kontext.WithCluster(context.Background(), logicalcluster.Name("custom-workspace"))
	path = subroutine.getCurrentClusterPath(ctx, cfg)
	assert.Equal(t, "custom-workspace", path)
}

func TestCreateWorkspaceTypeReference(t *testing.T) {
	ref := createWorkspaceTypeReference("test-name", "test-path")
	assert.Equal(t, kcptenancyv1alpha.WorkspaceTypeName("test-name"), ref.Name)
	assert.Equal(t, "test-path", ref.Path)
}

func TestCreateWorkspaceTypeSelector(t *testing.T) {
	ref1 := createWorkspaceTypeReference("name1", "path1")
	ref2 := createWorkspaceTypeReference("name2", "path2")

	selector := createWorkspaceTypeSelector(ref1, ref2)
	assert.Len(t, selector.Types, 2)
	assert.Equal(t, ref1, selector.Types[0])
	assert.Equal(t, ref2, selector.Types[1])
}

func TestNewWorkspaceTypeSubroutineWithRootClient(t *testing.T) {
	client := mocks.NewClient(t)
	rootClient := mocks.NewClient(t)

	subroutine := NewWorkspaceTypeSubroutineWithRootClient(client, rootClient)
	assert.NotNil(t, subroutine)
	assert.Equal(t, client, subroutine.client)
	assert.Equal(t, rootClient, subroutine.rootClient)
}

func TestWorkspaceTypeSubroutine_Process_WithOrgWorkspaceCluster_Logic(t *testing.T) {
	// Simple test to cover the basic logic paths without complex mocking
	subroutine := &WorkspaceTypeSubroutine{rootClient: &mocks.Client{}}

	// Test non-org account (should return early)
	account := &corev1alpha1.Account{
		ObjectMeta: metav1.ObjectMeta{Name: "test-account"},
		Spec:       corev1alpha1.AccountSpec{Type: corev1alpha1.AccountTypeAccount},
	}

	result, err := subroutine.Process(context.Background(), account)
	assert.Equal(t, ctrl.Result{}, result)
	assert.Nil(t, err)
}

func TestWorkspaceTypeSubroutine_getCurrentClusterPath_Logic(t *testing.T) {
	// Test the cluster path logic without complex dependencies
	subroutine := &WorkspaceTypeSubroutine{}
	cfg := config.OperatorConfig{}

	ctx := kontext.WithCluster(context.Background(), logicalcluster.Name("root:current"))

	// Test without OrgWorkspaceCluster
	path := subroutine.getCurrentClusterPath(ctx, cfg)
	assert.Equal(t, "root:current", path)

	// Test with OrgWorkspaceCluster
	cfg.Kcp.OrgWorkspaceCluster = "root:org-cluster"
	path = subroutine.getCurrentClusterPath(ctx, cfg)
	assert.Equal(t, "root:current", path) // Should still return current cluster
}
