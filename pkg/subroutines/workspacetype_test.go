package subroutines_test

import (
	"context"
	"testing"
	"time"

	kcptenancyv1alpha "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"
	"github.com/kcp-dev/logicalcluster/v3"
	corev1alpha1 "github.com/platform-mesh/account-operator/api/v1alpha1"
	operatorconfig "github.com/platform-mesh/account-operator/internal/config"
	"github.com/platform-mesh/account-operator/pkg/subroutines"
	platformmeshcontext "github.com/platform-mesh/golang-commons/context"
	"github.com/platform-mesh/golang-commons/logger"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/kontext"
)

func TestWorkspaceTypeSpecCopy(t *testing.T) {
	scheme := runtime.NewScheme()
	utilruntime.Must(kcptenancyv1alpha.AddToScheme(scheme))

	baseOrg := &kcptenancyv1alpha.WorkspaceType{ObjectMeta: metav1.ObjectMeta{Name: "org"}}
	baseAcc := &kcptenancyv1alpha.WorkspaceType{ObjectMeta: metav1.ObjectMeta{Name: "account"}}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(baseOrg, baseAcc).Build()

	customAccName := "demo-acc"
	customOrgName := "demo-org"

	ctx := context.Background()

	customAcc := &kcptenancyv1alpha.WorkspaceType{ObjectMeta: metav1.ObjectMeta{Name: customAccName}}
	require.NoError(t, c.Create(ctx, customAcc))
	customAcc.Spec = baseAcc.Spec
	require.NoError(t, c.Update(ctx, customAcc))

	customOrg := &kcptenancyv1alpha.WorkspaceType{ObjectMeta: metav1.ObjectMeta{Name: customOrgName}}
	require.NoError(t, c.Create(ctx, customOrg))
	customOrg.Spec = baseOrg.Spec
	customOrg.Spec.DefaultChildWorkspaceType = &kcptenancyv1alpha.WorkspaceTypeReference{Name: kcptenancyv1alpha.WorkspaceTypeName(customAccName), Path: "root"}
	require.NoError(t, c.Update(ctx, customOrg))

	fetchedOrg := &kcptenancyv1alpha.WorkspaceType{}
	require.Eventually(t, func() bool {
		if err := c.Get(ctx, client.ObjectKey{Name: customOrgName}, fetchedOrg); err != nil {
			return false
		}
		return fetchedOrg.Spec.DefaultChildWorkspaceType != nil && string(fetchedOrg.Spec.DefaultChildWorkspaceType.Name) == customAccName
	}, 2*time.Second, 100*time.Millisecond)
}

func TestWorkspaceTypeSubroutine_Process_FallbackClient(t *testing.T) {
	// Arrange
	scheme := runtime.NewScheme()
	utilruntime.Must(corev1alpha1.AddToScheme(scheme))
	utilruntime.Must(kcptenancyv1alpha.AddToScheme(scheme))

	acct := &corev1alpha1.Account{
		ObjectMeta: metav1.ObjectMeta{Name: "test-org", UID: "1234"},
		Spec:       corev1alpha1.AccountSpec{Type: corev1alpha1.AccountTypeOrg},
	}

	// Create a fake client that will act as both the regular and root client
	baseOrgWT := &kcptenancyv1alpha.WorkspaceType{ObjectMeta: metav1.ObjectMeta{Name: "org"}}
	baseAccWT := &kcptenancyv1alpha.WorkspaceType{ObjectMeta: metav1.ObjectMeta{Name: "account"}}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(baseOrgWT, baseAccWT).Build()

	// Use the constructor WITHOUT the root client to test the fallback logic
	sub := subroutines.NewWorkspaceTypeSubroutine(fakeClient)

	cfg := operatorconfig.OperatorConfig{}
	cfg.Kcp.ProviderWorkspace = "root"

	log, err := logger.New(logger.DefaultConfig())
	require.NoError(t, err)
	ctx, _, _ := platformmeshcontext.StartContext(log, cfg, 1*time.Minute)

	// Set a logical cluster on the context, as the real controller would
	ctx = kontext.WithCluster(ctx, logicalcluster.Name("orgs:root-org"))

	// Act
	_, opErr := sub.Process(ctx, acct)

	// Assert
	require.Nil(t, opErr)

	// Check that the custom WorkspaceTypes were created correctly
	customOrgWT := &kcptenancyv1alpha.WorkspaceType{}
	err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-org-org"}, customOrgWT)
	require.NoError(t, err, "custom org workspacetype should be created")

	customAccWT := &kcptenancyv1alpha.WorkspaceType{}
	err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-org-acc"}, customAccWT)
	require.NoError(t, err, "custom account workspacetype should be created")
}

func TestWorkspaceTypeSubroutine_Process_BaseTypesNotFound(t *testing.T) {
	// Arrange: no base org/account WorkspaceTypes present in the fake root client
	scheme := runtime.NewScheme()
	utilruntime.Must(corev1alpha1.AddToScheme(scheme))
	utilruntime.Must(kcptenancyv1alpha.AddToScheme(scheme))

	acct := &corev1alpha1.Account{
		ObjectMeta: metav1.ObjectMeta{Name: "no-base", UID: "1234"},
		Spec:       corev1alpha1.AccountSpec{Type: corev1alpha1.AccountTypeOrg},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	sub := subroutines.NewWorkspaceTypeSubroutine(fakeClient)

	cfg := operatorconfig.OperatorConfig{}
	cfg.Kcp.ProviderWorkspace = "root"

	log, err := logger.New(logger.DefaultConfig())
	require.NoError(t, err)
	ctx, _, _ := platformmeshcontext.StartContext(log, cfg, 1*time.Minute)
	ctx = kontext.WithCluster(ctx, logicalcluster.Name("orgs:root-org"))

	// Act
	_, opErr := sub.Process(ctx, acct)

	// Assert
	require.Nil(t, opErr)

	// Custom types should be created even if base types are not found
	customOrgWT := &kcptenancyv1alpha.WorkspaceType{}
	err = fakeClient.Get(ctx, types.NamespacedName{Name: "no-base-org"}, customOrgWT)
	require.NoError(t, err)
	customAccWT := &kcptenancyv1alpha.WorkspaceType{}
	err = fakeClient.Get(ctx, types.NamespacedName{Name: "no-base-acc"}, customAccWT)
	require.NoError(t, err)
}
