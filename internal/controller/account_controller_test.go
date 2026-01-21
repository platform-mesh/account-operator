package controller_test

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/kcp-dev/logicalcluster/v3"
	apiexport "github.com/kcp-dev/multicluster-provider/apiexport"
	mcc "github.com/kcp-dev/multicluster-provider/client"
	kcpapisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
	"golang.org/x/sync/errgroup"

	kcpcorev1alpha "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	ctrl "sigs.k8s.io/controller-runtime"

	kcptenancyv1alpha "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
	platformmeshconfig "github.com/platform-mesh/golang-commons/config"
	platformmeshcontext "github.com/platform-mesh/golang-commons/context"
	"github.com/platform-mesh/golang-commons/logger"
	"github.com/stretchr/testify/suite"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	"github.com/kcp-dev/multicluster-provider/envtest"
	"github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/account-operator/internal/config"
	"github.com/platform-mesh/account-operator/internal/controller"
	"github.com/platform-mesh/account-operator/pkg/subroutines/accountinfo"
	"github.com/platform-mesh/account-operator/pkg/subroutines/mocks"
	kcpenvtest "github.com/platform-mesh/account-operator/pkg/testing/kcpenvtest"
)

var (
	//go:embed test/setup/workspace-orgs.yaml
	workspaceOrgsYAML []byte
	//go:embed test/setup/workspace-platform-mesh-system.yaml
	workspacePlatformMeshSystemYAML []byte
	//go:embed test/setup/workspace-type-account.yaml
	workspaceTypeAccountYAML []byte
	//go:embed test/setup/workspace-type-orgs.yaml
	workspaceTypeOrgsYAML []byte
	//go:embed test/setup/workspacetype-org.yaml
	workspacetypeOrgYAML []byte

	//go:embed test/setup/01-platform-mesh-system/apiexport-core.platform-mesh.io.yaml
	apiexportCorePlatformMeshIoYAML []byte
	//go:embed test/setup/01-platform-mesh-system/apiexportendpointslice-core.platform-mesh.org.yaml
	apiexportendpointsliceCorePlatformMeshOrgYAML []byte
	//go:embed test/setup/01-platform-mesh-system/apiresourceschema-accountinfos.core.platform-mesh.io.yaml
	apiresourceschemaAccountinfosCorePlatformMeshIoYAML []byte
	//go:embed test/setup/01-platform-mesh-system/apiresourceschema-accounts.core.platform-mesh.io.yaml
	apiresourceschemaAccountsCorePlatformMeshIoYAML []byte

	//go:embed test/setup/02-orgs/account-root-org.yaml
	accountRootOrgYAML []byte
)

var rootWorkspaceTypes = [][]byte{
	workspaceTypeAccountYAML,
	workspaceTypeOrgsYAML,
	workspacetypeOrgYAML, // Why different?
}

var rootWorkspaces = [][]byte{
	workspaceOrgsYAML,
	workspacePlatformMeshSystemYAML,
	workspaceTypeAccountYAML,
}

var platformMeshSystemAPIResourceSchemas = [][]byte{
	apiresourceschemaAccountinfosCorePlatformMeshIoYAML,
	apiresourceschemaAccountsCorePlatformMeshIoYAML,
}

const (
	defaultTestTimeout  = 15 * time.Second
	defaultTickInterval = 250 * time.Millisecond
	defaultNamespace    = "default"
)

type AccountTestSuite struct {
	suite.Suite

	rootOrgsDefaultClient client.Client
	rootOrgsClient        client.Client
	mcc                   mcc.ClusterClient
	multiClusterManager   mcmanager.Manager
	provider              *apiexport.Provider
	testEnv               *envtest.Environment
	log                   *logger.Logger
	ctx                   context.Context
	cancel                context.CancelCauseFunc
	rootConfig            *rest.Config
	scheme                *runtime.Scheme
}

func TestAccountTestSuite(t *testing.T) {
	suite.Run(t, new(AccountTestSuite))
}

func (suite *AccountTestSuite) SetupSuite() {
	logConfig := logger.DefaultConfig()
	logConfig.NoJSON = true
	logConfig.Name = "AccountTestSuite"
	logConfig.Level = "debug"

	log, err := logger.New(logConfig)
	suite.Require().NoError(err)
	suite.log = log
	ctrl.SetLogger(log.Logr())

	cfg := config.OperatorConfig{}
	cfg.Subroutines.FGA.Enabled = false
	cfg.Subroutines.Workspace.Enabled = true
	cfg.Subroutines.AccountInfo.Enabled = true
	cfg.Subroutines.WorkspaceType.Enabled = true
	cfg.Kcp.ProviderWorkspace = "root"

	suite.ctx, suite.cancel, _ = platformmeshcontext.StartContext(log, cfg, 2*time.Minute)
	testEnvLogger := log.ComponentLogger("kcpenvtest")

	suite.scheme = runtime.NewScheme()
	utilruntime.Must(v1alpha1.AddToScheme(suite.scheme))
	utilruntime.Must(v1.AddToScheme(suite.scheme))
	utilruntime.Must(kcpapisv1alpha1.AddToScheme(suite.scheme))
	utilruntime.Must(kcpcorev1alpha.AddToScheme(suite.scheme))
	utilruntime.Must(kcptenancyv1alpha.AddToScheme(suite.scheme))

	suite.testEnv = &envtest.Environment{AttachKcpOutput: false}
	suite.testEnv.BinaryAssetsDirectory = "../../bin"
	suite.testEnv.KcpStartTimeout = 2 * time.Minute
	suite.testEnv.KcpStopTimeout = 30 * time.Second

	kcpConfig, err := suite.testEnv.Start()
	suite.Require().NoError(err)
	suite.mcc, err = mcc.New(kcpConfig, client.Options{
		Scheme: suite.scheme,
	})
	suite.Require().NoError(err)

	// Setup root workspace
	providerCfg := rest.CopyConfig(kcpConfig)
	providerCfg.Host += "/clusters/root:platform-mesh-system"
	suite.Require().NoError(err)
	c := suite.mcc.Cluster(logicalcluster.NewPath("root"))
	suite.Require().NoError(err)
	kcpenvtest.WaitForWorkspaceWithTimeout(c, "root", testEnvLogger, 15*time.Second)

	// Setup root workspace and wait for workspaces to appear
	rootClient := suite.mcc.Cluster(logicalcluster.NewPath("root"))
	for _, data := range rootWorkspaceTypes {
		var wt kcptenancyv1alpha1.WorkspaceType
		err := yaml.Unmarshal(data, &wt)
		suite.Require().NoError(err, "Unmarshalling embedded data")

		err = rootClient.Create(suite.ctx, &wt)
		suite.Require().NoError(err, "Creating unmarshalled object")
	}
	for _, data := range rootWorkspaces {
		var ws kcptenancyv1alpha1.Workspace
		err := yaml.Unmarshal(data, &ws)
		suite.Require().NoError(err, "Unmarshalling embedded data")

		err = rootClient.Create(suite.ctx, &ws)
		suite.Require().NoError(err, "Creating unmarshalled object")
	}

	// Setup platform-mesh-system workspace
	kcpenvtest.WaitForWorkspaceWithTimeout(c, "platform-mesh-system", testEnvLogger, 15*time.Second)
	pmsClient := suite.mcc.Cluster(logicalcluster.NewPath("root:platform-mesh-system"))
	for _, data := range platformMeshSystemAPIResourceSchemas {
		var ars kcpapisv1alpha1.APIResourceSchema
		err := yaml.Unmarshal(data, &ars)
		suite.Require().NoError(err, "Unmarshalling embedded data")

		testEnvLogger.Debug().Msgf("Creating APIResourceSchema %s", ars.Name)
		err = pmsClient.Create(suite.ctx, &ars)
		suite.Require().NoError(err, "Creating unmarshalled object")
	}

	var aePlatformMesh, aeTenancy kcpapisv1alpha1.APIExport
	err = rootClient.Get(suite.ctx, types.NamespacedName{Name: "tenancy.kcp.io"}, &aeTenancy)
	suite.Require().NoError(err)
	err = yaml.Unmarshal(apiexportCorePlatformMeshIoYAML, &aePlatformMesh)
	suite.Require().NoError(err, "Unmarshalling embedded data")
	aePlatformMesh.Spec.PermissionClaims[1].IdentityHash = aeTenancy.Status.IdentityHash
	aePlatformMesh.Spec.PermissionClaims[2].IdentityHash = aeTenancy.Status.IdentityHash
	err = pmsClient.Create(suite.ctx, &aePlatformMesh)
	suite.Require().NoError(err, "Creating unmarshalled object")

	time.Sleep(time.Second * 10) // race, auf was muss man hier warten?
	var axes kcpapisv1alpha1.APIExportEndpointSlice
	err = yaml.Unmarshal(apiexportendpointsliceCorePlatformMeshOrgYAML, &axes)
	suite.Require().NoError(err, "Unmarshalling embedded data")
	err = pmsClient.Create(suite.ctx, &axes)
	suite.Require().NoError(err, "Creating unmarshalled object")

	// Setup orgs workspace with test Account
	kcpenvtest.WaitForWorkspaceWithTimeout(c, "orgs", testEnvLogger, 15*time.Second)
	orgsClient := suite.mcc.Cluster(logicalcluster.NewPath("root:orgs"))
	var acc v1alpha1.Account
	err = yaml.Unmarshal(accountRootOrgYAML, &acc)
	suite.Require().NoError(err, "Unmarshalling embedded data")
	err = orgsClient.Create(suite.ctx, &acc)
	suite.Require().NoError(err, "Creating unmarshalled object")

	axplogr := log.ComponentLogger("apiexport_provider").Logr()
	suite.provider, err = apiexport.New(providerCfg, "core.platform-mesh.io", apiexport.Options{
		Scheme: suite.scheme,
		Log:    &axplogr,
	})
	suite.Require().NoError(err)

	mcOpts := mcmanager.Options{
		Scheme: suite.scheme,
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
		BaseContext: func() context.Context { return suite.ctx },
	}

	suite.multiClusterManager, err = mcmanager.New(providerCfg, suite.provider, mcOpts)
	suite.Require().NoError(err)

	mockClient := mocks.NewOpenFGAServiceClient(suite.T())
	accountReconciler := controller.NewAccountReconciler(log, suite.multiClusterManager, cfg, orgsClient, mockClient)

	managerCtx, cancel := context.WithCancel(suite.ctx)
	eg, egCtx := errgroup.WithContext(managerCtx)
	eg.Go(func() error {
		return suite.multiClusterManager.Start(egCtx)
	})

	suite.T().Cleanup(func() {
		cancel()
		if err := eg.Wait(); err != nil && !errors.Is(err, context.Canceled) {
			suite.T().Logf("controller manager exited with error: %v", err)
		}
	})

	dCfg := &platformmeshconfig.CommonServiceConfig{}
	suite.Require().NoError(accountReconciler.SetupWithManager(suite.multiClusterManager, dCfg, log))

	suite.rootOrgsClient = suite.mcc.Cluster(logicalcluster.NewPath("root:orgs"))
	suite.rootOrgsDefaultClient = suite.mcc.Cluster(logicalcluster.NewPath("root:orgs:default"))
	suite.Require().NoError(kcpenvtest.WaitForWorkspaceWithTimeout(orgsClient, "default", testEnvLogger, time.Minute))
}

func (suite *AccountTestSuite) TearDownSuite() {
	if suite.cancel != nil {
		suite.cancel(fmt.Errorf("tearing down test suite"))
	}
	if suite.testEnv != nil {
		_ = suite.testEnv.Stop()
	}
}

func (suite *AccountTestSuite) TestAddingFinalizer() {
	testContext := context.Background()
	accountName := "test-account-finalizer"

	account := &v1alpha1.Account{
		ObjectMeta: metav1.ObjectMeta{
			Name: accountName,
		},
		Spec: v1alpha1.AccountSpec{
			Type: v1alpha1.AccountTypeAccount,
		},
	}

	suite.Require().NoError(suite.kubernetesClient.Create(testContext, account))

	createdAccount := v1alpha1.Account{}
	suite.Assert().Eventually(func() bool {
		err := suite.kubernetesClient.Get(testContext, types.NamespacedName{Name: accountName, Namespace: defaultNamespace}, &createdAccount)
		return err == nil && len(createdAccount.Finalizers) == 3
	}, defaultTestTimeout, defaultTickInterval)

	suite.ElementsMatch([]string{"workspacetype.core.platform-mesh.io/finalizer", "account.core.platform-mesh.io/finalizer", "account.core.platform-mesh.io/info"}, createdAccount.Finalizers)
}

func (suite *AccountTestSuite) TestWorkspaceCreation() {
	testContext := context.Background()
	accountName := "test-account-ws-creation"
	account := &v1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: accountName}, Spec: v1alpha1.AccountSpec{Type: v1alpha1.AccountTypeAccount}}

	suite.Require().NoError(suite.kubernetesClient.Create(testContext, account))

	createdWorkspace := kcptenancyv1alpha.Workspace{}
	suite.Assert().Eventually(func() bool {
		if err := suite.kubernetesClient.Get(testContext, types.NamespacedName{Name: accountName}, &createdWorkspace); err != nil {
			return false
		}
		return createdWorkspace.Status.Phase == kcpcorev1alpha.LogicalClusterPhaseReady
	}, defaultTestTimeout, defaultTickInterval)

	updatedAccount := &v1alpha1.Account{}
	suite.Assert().Eventually(func() bool {
		if err := suite.kubernetesClient.Get(testContext, types.NamespacedName{Name: accountName}, updatedAccount); err != nil {
			return false
		}
		return meta.IsStatusConditionTrue(updatedAccount.Status.Conditions, "WorkspaceSubroutine_Ready")
	}, defaultTestTimeout, defaultTickInterval)

	suite.verifyWorkspace(testContext, accountName)
	suite.verifyCondition(updatedAccount.Status.Conditions, "WorkspaceSubroutine_Ready", metav1.ConditionTrue, "Complete")
}

func (suite *AccountTestSuite) TestAccountInfoCreationForOrganization() {
	testContext := context.Background()
	accountName := "test-org-account"
	account := &v1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: accountName}, Spec: v1alpha1.AccountSpec{Type: v1alpha1.AccountTypeOrg}}

	suite.Require().NoError(suite.kubernetesClient.Create(testContext, account))

	createdAccount := &v1alpha1.Account{}
	suite.Assert().Eventually(func() bool {
		if err := suite.kubernetesClient.Get(testContext, types.NamespacedName{Name: accountName}, createdAccount); err != nil {
			return false
		}
		return meta.IsStatusConditionTrue(createdAccount.Status.Conditions, "AccountInfoSubroutine_Ready")
	}, defaultTestTimeout, defaultTickInterval)

	accountInfo := &v1alpha1.AccountInfo{}
	suite.Assert().Eventually(func() bool {
		if err := suite.kubernetesClient.Get(testContext, client.ObjectKey{Name: accountinfo.DefaultAccountInfoName}, accountInfo); err != nil {
			return false
		}
		return accountInfo.Spec.Account.Type == v1alpha1.AccountTypeOrg
	}, defaultTestTimeout, defaultTickInterval)
}

func (suite *AccountTestSuite) TestWorkspaceFinalizerRemovesWorkspace() {
	testContext := context.Background()
	accountName := "test-workspace-finalizer"
	account := &v1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: accountName}, Spec: v1alpha1.AccountSpec{Type: v1alpha1.AccountTypeAccount}}

	suite.Require().NoError(suite.kubernetesClient.Create(testContext, account))

	createdWorkspace := kcptenancyv1alpha.Workspace{}
	suite.Assert().Eventually(func() bool {
		return suite.kubernetesClient.Get(testContext, types.NamespacedName{Name: accountName}, &createdWorkspace) == nil
	}, defaultTestTimeout, defaultTickInterval)

	suite.Require().NoError(suite.kubernetesClient.Delete(testContext, account))

	suite.Assert().Eventually(func() bool {
		err := suite.kubernetesClient.Get(testContext, types.NamespacedName{Name: accountName}, &kcptenancyv1alpha.Workspace{})
		return kerrors.IsNotFound(err)
	}, defaultTestTimeout, defaultTickInterval)
}

func (suite *AccountTestSuite) verifyWorkspace(ctx context.Context, accountName string) {
	workspace := &kcptenancyv1alpha.Workspace{}
	suite.Require().NoError(suite.kubernetesClient.Get(ctx, types.NamespacedName{Name: accountName}, workspace))
	suite.Equal(accountName, workspace.Name)
	suite.NotNil(workspace.Spec.Type)
	expectedType := kcptenancyv1alpha.WorkspaceTypeName(fmt.Sprintf("%s-acc", accountName))
	suite.Equal(expectedType, workspace.Spec.Type.Name)
}

func (suite *AccountTestSuite) verifyCondition(conditions []metav1.Condition, conditionType string, status metav1.ConditionStatus, reason string) {
	suite.Assert().True(meta.IsStatusConditionPresentAndEqual(conditions, conditionType, status))
	condition := meta.FindStatusCondition(conditions, conditionType)
	suite.Require().NotNil(condition)
	suite.Equal(reason, condition.Reason)
}
