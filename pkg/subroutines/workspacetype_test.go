package subroutines_test

import (
	"context"
	"fmt"
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

// errorClient wraps a controller-runtime client and injects errors for specified operations.
type errorClient struct {
	client.Client
	failGet    map[string]bool
	failCreate map[string]bool
	failPatch  map[string]bool
}

func (e *errorClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if e.failGet != nil && e.failGet[key.Name] {
		return fmt.Errorf("injected get error for %s", key.Name)
	}
	return e.Client.Get(ctx, key, obj, opts...)
}

func (e *errorClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	if e.failCreate != nil && e.failCreate[obj.GetName()] {
		return fmt.Errorf("injected create error for %s", obj.GetName())
	}
	return e.Client.Create(ctx, obj, opts...)
}

func (e *errorClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	if e.failPatch != nil && e.failPatch[obj.GetName()] {
		return fmt.Errorf("injected patch error for %s", obj.GetName())
	}
	return e.Client.Patch(ctx, obj, patch, opts...)
}

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

	// Validate LimitAllowedParents set on custom account type (org + acc in current cluster)
	require.NotNil(t, customAccWT.Spec.LimitAllowedParents)
	names := map[string]bool{}
	paths := map[string]bool{}
	for _, r := range customAccWT.Spec.LimitAllowedParents.Types {
		names[string(r.Name)] = true
		paths[r.Path] = true
	}
	require.True(t, names["test-org-org"])  // custom org
	require.True(t, names["test-org-acc"])  // self
	require.True(t, paths["orgs:root-org"]) // current path used

	// Validate LimitAllowedParents on custom org type (org@root)
	require.NotNil(t, customOrgWT.Spec.LimitAllowedParents)
	require.Len(t, customOrgWT.Spec.LimitAllowedParents.Types, 1)
	require.Equal(t, "org", string(customOrgWT.Spec.LimitAllowedParents.Types[0].Name))
	require.Equal(t, "root", customOrgWT.Spec.LimitAllowedParents.Types[0].Path)
}

func TestWorkspaceTypeSubroutine_MetadataHelpers(t *testing.T) {
	scheme := runtime.NewScheme()
	utilruntime.Must(corev1alpha1.AddToScheme(scheme))
	utilruntime.Must(kcptenancyv1alpha.AddToScheme(scheme))

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	sub := subroutines.NewWorkspaceTypeSubroutine(fakeClient)

	// GetName
	require.Equal(t, subroutines.WorkspaceTypeSubroutineName, sub.GetName())

	// Finalize should no-op without error
	acct := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "x"}}
	log, err := logger.New(logger.DefaultConfig())
	require.NoError(t, err)
	ctx, _, _ := platformmeshcontext.StartContext(log, operatorconfig.OperatorConfig{}, 1*time.Minute)
	ctx = kontext.WithCluster(ctx, logicalcluster.Name("orgs:root-org"))
	_, opErr := sub.Finalize(ctx, acct)
	require.Nil(t, opErr)
}

func TestWorkspaceTypeSubroutine_Process_MissingClusterInContext(t *testing.T) {
	scheme := runtime.NewScheme()
	utilruntime.Must(corev1alpha1.AddToScheme(scheme))
	utilruntime.Must(kcptenancyv1alpha.AddToScheme(scheme))

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	sub := subroutines.NewWorkspaceTypeSubroutine(fakeClient)

	acct := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "x"}, Spec: corev1alpha1.AccountSpec{Type: corev1alpha1.AccountTypeOrg}}
	log, err := logger.New(logger.DefaultConfig())
	require.NoError(t, err)
	ctx, _, _ := platformmeshcontext.StartContext(log, operatorconfig.OperatorConfig{}, 1*time.Minute)
	// Intentionally NOT setting kontext.WithCluster; expect an operator error, not panic
	_, opErr := sub.Process(ctx, acct)
	require.NotNil(t, opErr)
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

func TestWorkspaceTypeSubroutine_Process_BaseOrgGetError(t *testing.T) {
	scheme := runtime.NewScheme()
	utilruntime.Must(corev1alpha1.AddToScheme(scheme))
	utilruntime.Must(kcptenancyv1alpha.AddToScheme(scheme))

	acct := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "err-org"}, Spec: corev1alpha1.AccountSpec{Type: corev1alpha1.AccountTypeOrg}}

	// Underlying clients
	mainClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	rootBase := fake.NewClientBuilder().WithScheme(scheme).Build()
	// Wrap root client to error on Get("org")
	rootErr := &errorClient{Client: rootBase, failGet: map[string]bool{"org": true}}

	sub := subroutines.NewWorkspaceTypeSubroutineWithRootClient(mainClient, rootErr)

	cfg := operatorconfig.OperatorConfig{}
	cfg.Kcp.ProviderWorkspace = "root"
	log, _ := logger.New(logger.DefaultConfig())
	ctx, _, _ := platformmeshcontext.StartContext(log, cfg, 1*time.Minute)
	ctx = kontext.WithCluster(ctx, logicalcluster.Name("orgs:root-org"))

	_, opErr := sub.Process(ctx, acct)
	require.NotNil(t, opErr)
}

func TestWorkspaceTypeSubroutine_Process_BaseAccGetError(t *testing.T) {
	scheme := runtime.NewScheme()
	utilruntime.Must(corev1alpha1.AddToScheme(scheme))
	utilruntime.Must(kcptenancyv1alpha.AddToScheme(scheme))

	acct := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "err-acc"}, Spec: corev1alpha1.AccountSpec{Type: corev1alpha1.AccountTypeOrg}}

	mainClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	rootBase := fake.NewClientBuilder().WithScheme(scheme).Build()
	// Wrap root client to error on Get("account")
	rootErr := &errorClient{Client: rootBase, failGet: map[string]bool{"account": true}}

	sub := subroutines.NewWorkspaceTypeSubroutineWithRootClient(mainClient, rootErr)

	cfg := operatorconfig.OperatorConfig{}
	cfg.Kcp.ProviderWorkspace = "root"
	log, _ := logger.New(logger.DefaultConfig())
	ctx, _, _ := platformmeshcontext.StartContext(log, cfg, 1*time.Minute)
	ctx = kontext.WithCluster(ctx, logicalcluster.Name("orgs:root-org"))

	_, opErr := sub.Process(ctx, acct)
	require.NotNil(t, opErr)
}

func TestWorkspaceTypeSubroutine_Process_CustomAccCreateError(t *testing.T) {
	scheme := runtime.NewScheme()
	utilruntime.Must(corev1alpha1.AddToScheme(scheme))
	utilruntime.Must(kcptenancyv1alpha.AddToScheme(scheme))

	acct := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "x"}, Spec: corev1alpha1.AccountSpec{Type: corev1alpha1.AccountTypeOrg}}

	// Preload base types so earlier gets succeed
	rootBaseOrg := &kcptenancyv1alpha.WorkspaceType{ObjectMeta: metav1.ObjectMeta{Name: "org"}}
	rootBaseAcc := &kcptenancyv1alpha.WorkspaceType{ObjectMeta: metav1.ObjectMeta{Name: "account"}}
	mainBase := fake.NewClientBuilder().WithScheme(scheme).Build()
	rootBase := fake.NewClientBuilder().WithScheme(scheme).WithObjects(rootBaseOrg, rootBaseAcc).Build()
	// Wrap main client to error on Create for customAcc (name: "x-acc")
	mainErr := &errorClient{Client: mainBase, failCreate: map[string]bool{"x-acc": true}}

	sub := subroutines.NewWorkspaceTypeSubroutineWithRootClient(mainErr, rootBase)

	cfg := operatorconfig.OperatorConfig{}
	cfg.Kcp.ProviderWorkspace = "root"
	log, _ := logger.New(logger.DefaultConfig())
	ctx, _, _ := platformmeshcontext.StartContext(log, cfg, 1*time.Minute)
	ctx = kontext.WithCluster(ctx, logicalcluster.Name("orgs:root-org"))

	_, opErr := sub.Process(ctx, acct)
	require.NotNil(t, opErr)
}

func TestWorkspaceTypeSubroutine_Process_CustomOrgCreateError(t *testing.T) {
	scheme := runtime.NewScheme()
	utilruntime.Must(corev1alpha1.AddToScheme(scheme))
	utilruntime.Must(kcptenancyv1alpha.AddToScheme(scheme))

	acct := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "y"}, Spec: corev1alpha1.AccountSpec{Type: corev1alpha1.AccountTypeOrg}}

	rootBaseOrg := &kcptenancyv1alpha.WorkspaceType{ObjectMeta: metav1.ObjectMeta{Name: "org"}}
	rootBaseAcc := &kcptenancyv1alpha.WorkspaceType{ObjectMeta: metav1.ObjectMeta{Name: "account"}}
	mainBase := fake.NewClientBuilder().WithScheme(scheme).Build()
	rootBase := fake.NewClientBuilder().WithScheme(scheme).WithObjects(rootBaseOrg, rootBaseAcc).Build()
	// Wrap main client to error on Create for customOrg (name: "y-org"). Account should succeed first.
	mainErr := &errorClient{Client: mainBase, failCreate: map[string]bool{"y-org": true}}

	sub := subroutines.NewWorkspaceTypeSubroutineWithRootClient(mainErr, rootBase)

	cfg := operatorconfig.OperatorConfig{}
	cfg.Kcp.ProviderWorkspace = "root"
	log, _ := logger.New(logger.DefaultConfig())
	ctx, _, _ := platformmeshcontext.StartContext(log, cfg, 1*time.Minute)
	ctx = kontext.WithCluster(ctx, logicalcluster.Name("orgs:root-org"))

	_, opErr := sub.Process(ctx, acct)
	require.NotNil(t, opErr)
}
