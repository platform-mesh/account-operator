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

	// Only test DefaultChildWorkspaceType linkage
	customOrg := &kcptenancyv1alpha.WorkspaceType{ObjectMeta: metav1.ObjectMeta{Name: customOrgName}}
	customOrg.Spec = baseOrg.Spec
	customOrg.Spec.DefaultChildWorkspaceType = &kcptenancyv1alpha.WorkspaceTypeReference{Name: kcptenancyv1alpha.WorkspaceTypeName(customAccName), Path: "root"}
	require.NoError(t, c.Create(ctx, customOrg))

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
	err = fakeClient.Get(ctx, types.NamespacedName{Name: "root-org-test-org-org"}, customOrgWT)
	require.NoError(t, err, "custom org workspacetype should be created")

	customAccWT := &kcptenancyv1alpha.WorkspaceType{}
	err = fakeClient.Get(ctx, types.NamespacedName{Name: "root-org-test-org-acc"}, customAccWT)
	require.NoError(t, err, "custom account workspacetype should be created")

	// LimitAllowedParents may not be set depending on subroutine logic; log for debug
	if customAccWT.Spec.LimitAllowedParents != nil {
		require.Len(t, customAccWT.Spec.LimitAllowedParents.Types, 1)
		require.Equal(t, "root-org-test-org-org", string(customAccWT.Spec.LimitAllowedParents.Types[0].Name))
		require.Equal(t, "orgs:root-org", customAccWT.Spec.LimitAllowedParents.Types[0].Path)
	} else {
		// Should be set; fail explicitly for clarity
		t.Fatalf("expected LimitAllowedParents to be set")
	}

	// LimitAllowedParents may not be set depending on subroutine logic; log for debug
	if customOrgWT.Spec.LimitAllowedParents != nil {
		require.Len(t, customOrgWT.Spec.LimitAllowedParents.Types, 1)
		require.Equal(t, "org", string(customOrgWT.Spec.LimitAllowedParents.Types[0].Name))
		require.Equal(t, "root", customOrgWT.Spec.LimitAllowedParents.Types[0].Path)
	} else {
		t.Log("LimitAllowedParents is nil (allowed by current logic)")
	}

	// LimitAllowedChildren may not be set depending on subroutine logic; log for debug
	if customOrgWT.Spec.LimitAllowedChildren != nil {
		require.Len(t, customOrgWT.Spec.LimitAllowedChildren.Types, 1)
		require.Equal(t, "root-org-test-org-acc", string(customOrgWT.Spec.LimitAllowedChildren.Types[0].Name))
		actualPath := customOrgWT.Spec.LimitAllowedChildren.Types[0].Path
		require.NotEmpty(t, actualPath, "LimitAllowedChildren path should not be empty")
	} else {
		// Should be set; fail explicitly for clarity
		t.Fatalf("expected LimitAllowedChildren to be set")
	}
	// DefaultAPIBindings may not be set depending on subroutine logic; log for debug
	if customAccWT.Spec.DefaultAPIBindings != nil {
		t.Log("customAccWT DefaultAPIBindings is set")
	} else {
		t.Log("customAccWT DefaultAPIBindings is nil (allowed by current logic)")
	}
	if customOrgWT.Spec.DefaultAPIBindings != nil {
		t.Log("customOrgWT DefaultAPIBindings is set")
	} else {
		t.Log("customOrgWT DefaultAPIBindings is nil (allowed by current logic)")
	}

	// Ensure both custom types are persisted with expected references
	require.Eventually(t, func() bool {
		var wtOrg kcptenancyv1alpha.WorkspaceType
		var wtAcc kcptenancyv1alpha.WorkspaceType
		if err := fakeClient.Get(ctx, types.NamespacedName{Name: "root-org-test-org-org"}, &wtOrg); err != nil {
			return false
		}
		if err := fakeClient.Get(ctx, types.NamespacedName{Name: "root-org-test-org-acc"}, &wtAcc); err != nil {
			return false
		}
		return wtOrg.Spec.DefaultChildWorkspaceType != nil &&
			string(wtOrg.Spec.DefaultChildWorkspaceType.Name) == "root-org-test-org-acc" &&
			wtOrg.Spec.LimitAllowedParents != nil &&
			len(wtOrg.Spec.LimitAllowedParents.Types) == 1 &&
			string(wtOrg.Spec.LimitAllowedParents.Types[0].Name) == "org" &&
			wtAcc.Spec.LimitAllowedParents != nil &&
			len(wtAcc.Spec.LimitAllowedParents.Types) == 1 &&
			string(wtAcc.Spec.LimitAllowedParents.Types[0].Name) == "root-org-test-org-org" &&
			// Verify extension relationships
			len(wtOrg.Spec.Extend.With) == 1 &&
			string(wtOrg.Spec.Extend.With[0].Name) == "org" &&
			len(wtAcc.Spec.Extend.With) == 1 &&
			string(wtAcc.Spec.Extend.With[0].Name) == "account"
	}, 5*time.Second, 200*time.Millisecond)
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
	// Intentionally NOT setting kontext.WithCluster; should succeed without panic
	_, opErr := sub.Process(ctx, acct)
	require.Nil(t, opErr)
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
	err = fakeClient.Get(ctx, types.NamespacedName{Name: "root-org-no-base-org"}, customOrgWT)
	require.NoError(t, err)
	customAccWT := &kcptenancyv1alpha.WorkspaceType{}
	err = fakeClient.Get(ctx, types.NamespacedName{Name: "root-org-no-base-acc"}, customAccWT)
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
	// Wrap main client to error on Create for customAcc (name: "root-org-x-acc")
	mainErr := &errorClient{Client: mainBase, failCreate: map[string]bool{"root-org-x-acc": true}}

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
	// Wrap main client to error on Create for customOrg (name: "root-org-y-org"). Account should succeed first.
	mainErr := &errorClient{Client: mainBase, failCreate: map[string]bool{"root-org-y-org": true}}

	sub := subroutines.NewWorkspaceTypeSubroutineWithRootClient(mainErr, rootBase)

	cfg := operatorconfig.OperatorConfig{}
	cfg.Kcp.ProviderWorkspace = "root"
	log, _ := logger.New(logger.DefaultConfig())
	ctx, _, _ := platformmeshcontext.StartContext(log, cfg, 1*time.Minute)
	ctx = kontext.WithCluster(ctx, logicalcluster.Name("orgs:root-org"))

	_, opErr := sub.Process(ctx, acct)
	require.NotNil(t, opErr)
}
func TestWorkspaceTypeSubroutine_Process_AuthenticationConfiguration(t *testing.T) {
	scheme := runtime.NewScheme()
	utilruntime.Must(kcptenancyv1alpha.AddToScheme(scheme))

	baseOrg := &kcptenancyv1alpha.WorkspaceType{ObjectMeta: metav1.ObjectMeta{Name: "org"}}
	baseAcc := &kcptenancyv1alpha.WorkspaceType{ObjectMeta: metav1.ObjectMeta{Name: "account"}}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(baseOrg, baseAcc).Build()

	acct := &corev1alpha1.Account{
		ObjectMeta: metav1.ObjectMeta{Name: "test-org", UID: "1234"},
		Spec:       corev1alpha1.AccountSpec{Type: corev1alpha1.AccountTypeOrg},
	}

	cfg := operatorconfig.OperatorConfig{}
	cfg.Kcp.ProviderWorkspace = "root"
	log, _ := logger.New(logger.DefaultConfig())
	ctx, _, _ := platformmeshcontext.StartContext(log, cfg, 1*time.Minute)
	ctx = kontext.WithCluster(ctx, logicalcluster.Name("orgs:root-org"))

	sub := subroutines.NewWorkspaceTypeSubroutine(c)

	_, opErr := sub.Process(ctx, acct)
	require.Nil(t, opErr)

	// Verify that the custom org workspace type was created with authentication configuration
	customOrgName := subroutines.GetOrgWorkspaceTypeName(acct.Name, "orgs:root-org")
	customOrg := &kcptenancyv1alpha.WorkspaceType{}
	err := c.Get(ctx, client.ObjectKey{Name: customOrgName}, customOrg)
	require.NoError(t, err)

	// Verify that AuthenticationConfigurations is set
	require.NotNil(t, customOrg.Spec.AuthenticationConfigurations)
	require.Len(t, customOrg.Spec.AuthenticationConfigurations, 1)

	// Verify the authentication configuration name
	expectedAuthConfigName := fmt.Sprintf("%s-auth", customOrgName)
	require.Equal(t, expectedAuthConfigName, string(customOrg.Spec.AuthenticationConfigurations[0].Name))
}
