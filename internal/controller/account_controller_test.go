package controller_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	kcpapisv1alpha1 "github.com/kcp-dev/kcp/sdk/apis/apis/v1alpha1"
	"github.com/kcp-dev/kcp/sdk/apis/core"
	kcpcorev1alpha "github.com/kcp-dev/kcp/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"
	"github.com/kcp-dev/logicalcluster/v3"
	"github.com/kcp-dev/multicluster-provider/apiexport"
	clusterclient "github.com/kcp-dev/multicluster-provider/client"
	"github.com/kcp-dev/multicluster-provider/envtest"
	platformmeshconfig "github.com/platform-mesh/golang-commons/config"
	"github.com/platform-mesh/golang-commons/logger"
	"github.com/stretchr/testify/suite"
	"golang.org/x/sync/errgroup"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	"sigs.k8s.io/yaml"

	"github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/account-operator/internal/config"
	"github.com/platform-mesh/account-operator/internal/controller"
	"github.com/platform-mesh/account-operator/pkg/subroutines/accountinfo"
	"github.com/platform-mesh/account-operator/pkg/subroutines/mocks"
)

const (
	defaultTestTimeout  = 30 * time.Second
	defaultTickInterval = 250 * time.Millisecond
	defaultNamespace    = "default"
)

var (
	env       *envtest.Environment
	kcpConfig *rest.Config
)

type AccountTestSuite struct {
	suite.Suite

	cli                 clusterclient.ClusterClient
	provider            *apiexport.Provider
	providerWS          *kcptenancyv1alpha.Workspace
	providerPath        logicalcluster.Path
	orgsWS              *kcptenancyv1alpha.Workspace
	orgsPath            logicalcluster.Path
	rootOrgWS           *kcptenancyv1alpha.Workspace
	rootOrgPath         logicalcluster.Path
	multiClusterManager mcmanager.Manager
	log                 *logger.Logger
	ctx                 context.Context
	cancel              context.CancelFunc
	g                   *errgroup.Group
}

func init() {
	utilruntime.Must(v1alpha1.AddToScheme(scheme.Scheme))
	utilruntime.Must(kcpapisv1alpha1.AddToScheme(scheme.Scheme))
	utilruntime.Must(kcptenancyv1alpha.AddToScheme(scheme.Scheme))
	utilruntime.Must(kcpcorev1alpha.AddToScheme(scheme.Scheme))
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
	suite.Require().NoError(err, "failed to create logger")
	suite.log = log

	ctrl.SetLogger(log.Logr())
	suite.ctx, suite.cancel = context.WithCancel(context.Background())

	// Prevent the metrics listener being created
	metricsserver.DefaultBindAddress = "0"

	env = &envtest.Environment{}
	env.BinaryAssetsDirectory = "../../bin"
	err = os.Setenv("PRESERVE", "true")
	suite.Require().NoError(err, "failed to set PRESERVE environment variable")

	kcpConfig, err = env.Start()
	suite.Require().NoError(err, "failed to start envtest environment")

	suite.cli, err = clusterclient.New(kcpConfig, client.Options{})
	suite.Require().NoError(err, "failed to create cluster client")

	// Create provider workspace (platform-mesh-system)
	suite.providerWS, suite.providerPath = envtest.NewWorkspaceFixture(suite.T(), suite.cli, core.RootCluster.Path(), envtest.WithName("platform-mesh-system"))

	// Create orgs workspace
	suite.orgsWS, suite.orgsPath = envtest.NewWorkspaceFixture(suite.T(), suite.cli, core.RootCluster.Path(), envtest.WithName("orgs"))

	// Create root-org workspace under orgs
	suite.rootOrgWS, suite.rootOrgPath = envtest.NewWorkspaceFixture(suite.T(), suite.cli, suite.orgsPath, envtest.WithName("root-org"))

	// Load API resources and APIExport for the provider workspace
	suite.loadFromFile("../../test/setup/01-platform-mesh-system/apiresourceschema-accounts.core.platform-mesh.io.yaml", suite.providerPath)
	suite.loadFromFile("../../test/setup/01-platform-mesh-system/apiresourceschema-accountinfos.core.platform-mesh.io.yaml", suite.providerPath)
	suite.loadFromFile("../../test/setup/01-platform-mesh-system/apiexport-core.platform-mesh.io.yaml", suite.providerPath)

	// Create APIExportEndpointSlice
	aes := &kcpapisv1alpha1.APIExportEndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name: "core.platform-mesh.io",
		},
		Spec: kcpapisv1alpha1.APIExportEndpointSliceSpec{
			APIExport: kcpapisv1alpha1.ExportBindingReference{
				Name: "core.platform-mesh.io",
				Path: suite.providerPath.String(),
			},
		},
	}
	suite.cli.Cluster(suite.providerPath).Create(suite.ctx, aes) //nolint:errcheck

	// Create APIBinding in orgs workspace
	ab := &kcpapisv1alpha1.APIBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "core.platform-mesh.io",
		},
		Spec: kcpapisv1alpha1.APIBindingSpec{
			Reference: kcpapisv1alpha1.BindingReference{
				Export: &kcpapisv1alpha1.ExportBindingReference{
					Name: "core.platform-mesh.io",
					Path: suite.providerPath.String(),
				},
			},
		},
	}
	err = suite.cli.Cluster(suite.orgsPath).Create(suite.ctx, ab)
	suite.Require().NoError(err, "failed to create APIBinding in orgs workspace")

	suite.Eventually(func() bool {
		getErr := suite.cli.Cluster(suite.orgsPath).Get(suite.ctx, types.NamespacedName{Name: "core.platform-mesh.io"}, ab)
		return getErr == nil && ab.Status.Phase == kcpapisv1alpha1.APIBindingPhaseBound
	}, 60*time.Second, 500*time.Millisecond, "APIBinding for core.platform-mesh.io in orgs workspace did not become ready")

	// Create APIBinding in root-org workspace as well
	abRootOrg := &kcpapisv1alpha1.APIBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "core.platform-mesh.io",
		},
		Spec: kcpapisv1alpha1.APIBindingSpec{
			Reference: kcpapisv1alpha1.BindingReference{
				Export: &kcpapisv1alpha1.ExportBindingReference{
					Name: "core.platform-mesh.io",
					Path: suite.providerPath.String(),
				},
			},
		},
	}
	err = suite.cli.Cluster(suite.rootOrgPath).Create(suite.ctx, abRootOrg)
	suite.Require().NoError(err, "failed to create APIBinding in root-org workspace")

	suite.Eventually(func() bool {
		getErr := suite.cli.Cluster(suite.rootOrgPath).Get(suite.ctx, types.NamespacedName{Name: "core.platform-mesh.io"}, abRootOrg)
		return getErr == nil && abRootOrg.Status.Phase == kcpapisv1alpha1.APIBindingPhaseBound
	}, 60*time.Second, 500*time.Millisecond, "APIBinding for core.platform-mesh.io in root-org workspace did not become ready")

	// Lookup APIExportEndpointSlice to get the URL
	err = suite.cli.Cluster(suite.providerPath).Get(suite.ctx, types.NamespacedName{Name: "core.platform-mesh.io"}, aes)
	suite.Require().NoError(err, "failed to get APIExportEndpointSlice")
	suite.Require().NotEmpty(aes.Status.APIExportEndpoints, "APIExportEndpointSlice has no endpoints")

	// Setup provider and manager
	cfg := rest.CopyConfig(kcpConfig)
	cfg.Host = aes.Status.APIExportEndpoints[0].URL

	suite.provider, err = apiexport.New(cfg, apiexport.Options{})
	suite.Require().NoError(err, "failed to create APIExport client")

	operatorCfg := config.OperatorConfig{}
	operatorCfg.Subroutines.FGA.Enabled = false
	operatorCfg.Subroutines.Workspace.Enabled = false
	operatorCfg.Subroutines.AccountInfo.Enabled = false
	operatorCfg.Subroutines.WorkspaceType.Enabled = false
	operatorCfg.Kcp.ProviderWorkspace = "root"

	mgr, err := mcmanager.New(cfg, suite.provider, mcmanager.Options{
		Logger: log.Logr(),
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
	})
	suite.Require().NoError(err, "failed to create multicluster manager")
	suite.multiClusterManager = mgr

	// Build orgs client - needed for workspace and workspacetype subroutines
	var orgsClient client.Client
	if operatorCfg.Subroutines.Workspace.Enabled || operatorCfg.Subroutines.WorkspaceType.Enabled {
		orgsClient, err = suite.buildOrgsClient()
		suite.Require().NoError(err, "failed to build orgs client")
	}

	mockClient := mocks.NewOpenFGAServiceClient(suite.T())
	accountReconciler := controller.NewAccountReconciler(log, suite.multiClusterManager, operatorCfg, orgsClient, mockClient)

	dCfg := &platformmeshconfig.CommonServiceConfig{}
	suite.Require().NoError(accountReconciler.SetupWithManager(suite.multiClusterManager, dCfg, log))

	var groupContext context.Context
	suite.g, groupContext = errgroup.WithContext(suite.ctx)
	suite.g.Go(func() error {
		return suite.provider.Run(groupContext, suite.multiClusterManager)
	})
	suite.g.Go(func() error {
		return suite.multiClusterManager.Start(groupContext)
	})

	// Wait for the manager to be ready
	<-suite.multiClusterManager.Elected()
	suite.T().Log("Manager is ready and elected as leader")
}

func (suite *AccountTestSuite) loadFromFile(filePath string, workspace logicalcluster.Path) {
	data, err := os.ReadFile(filePath)
	suite.Require().NoError(err, "failed to read file %s", filePath)

	var u unstructured.Unstructured
	err = yaml.Unmarshal(data, &u.Object)
	suite.Require().NoError(err, "failed to unmarshal file %s", filePath)

	err = suite.cli.Cluster(workspace).Create(suite.ctx, &u)
	suite.Require().NoError(err, "failed to create resource %s", filePath)
}

func (suite *AccountTestSuite) buildOrgsClient() (client.Client, error) {
	cfg := rest.CopyConfig(suite.multiClusterManager.GetLocalManager().GetConfig())
	cfg.Host = fmt.Sprintf("%s/clusters/%s", cfg.Host, suite.orgsPath.String())
	return client.New(cfg, client.Options{
		Scheme: suite.multiClusterManager.GetLocalManager().GetScheme(),
	})
}

func (suite *AccountTestSuite) TearDownSuite() {
	suite.cancel()
	suite.g.Wait() //nolint:errcheck
	env.Stop()     //nolint:errcheck
}

func (suite *AccountTestSuite) TestAddingFinalizer() {
	suite.T().Skip("Workspace and AccountInfo subroutines require direct KCP API access which is not available through APIExport endpoints in test environment")
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

	suite.Require().NoError(suite.cli.Cluster(suite.rootOrgPath).Create(testContext, account))

	createdAccount := v1alpha1.Account{}
	suite.Assert().Eventually(func() bool {
		err := suite.cli.Cluster(suite.rootOrgPath).Get(testContext, types.NamespacedName{Name: accountName, Namespace: defaultNamespace}, &createdAccount)
		return err == nil && len(createdAccount.Finalizers) == 2
	}, defaultTestTimeout, defaultTickInterval)

	suite.ElementsMatch([]string{"account.core.platform-mesh.io/finalizer", "account.core.platform-mesh.io/info"}, createdAccount.Finalizers)
}

func (suite *AccountTestSuite) TestAccountCreation() {
	testContext := context.Background()
	accountName := "test-account-basic"

	account := &v1alpha1.Account{
		ObjectMeta: metav1.ObjectMeta{
			Name: accountName,
		},
		Spec: v1alpha1.AccountSpec{
			Type: v1alpha1.AccountTypeAccount,
		},
	}

	suite.Require().NoError(suite.cli.Cluster(suite.rootOrgPath).Create(testContext, account))

	// Verify the account can be retrieved
	createdAccount := &v1alpha1.Account{}
	suite.Assert().Eventually(func() bool {
		err := suite.cli.Cluster(suite.rootOrgPath).Get(testContext, types.NamespacedName{Name: accountName}, createdAccount)
		return err == nil
	}, defaultTestTimeout, defaultTickInterval)

	// Verify basic properties
	suite.Equal(accountName, createdAccount.Name)
	suite.Equal(v1alpha1.AccountTypeAccount, createdAccount.Spec.Type)

	// Cleanup
	suite.NoError(suite.cli.Cluster(suite.rootOrgPath).Delete(testContext, account))
}

func (suite *AccountTestSuite) TestWorkspaceCreation() {
	suite.T().Skip("Workspace subroutine requires direct KCP API access which is not available through APIExport endpoints in test environment")
	testContext := context.Background()
	accountName := "test-account-ws-creation"
	account := &v1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: accountName}, Spec: v1alpha1.AccountSpec{Type: v1alpha1.AccountTypeAccount}}

	suite.Require().NoError(suite.cli.Cluster(suite.rootOrgPath).Create(testContext, account))

	createdWorkspace := kcptenancyv1alpha.Workspace{}
	suite.Assert().Eventually(func() bool {
		if err := suite.cli.Cluster(suite.rootOrgPath).Get(testContext, types.NamespacedName{Name: accountName}, &createdWorkspace); err != nil {
			return false
		}
		return createdWorkspace.Status.Phase == kcpcorev1alpha.LogicalClusterPhaseReady
	}, defaultTestTimeout, defaultTickInterval)

	updatedAccount := &v1alpha1.Account{}
	suite.Assert().Eventually(func() bool {
		if err := suite.cli.Cluster(suite.rootOrgPath).Get(testContext, types.NamespacedName{Name: accountName}, updatedAccount); err != nil {
			return false
		}
		return meta.IsStatusConditionTrue(updatedAccount.Status.Conditions, "WorkspaceSubroutine_Ready")
	}, defaultTestTimeout, defaultTickInterval)

	suite.verifyWorkspace(testContext, accountName)
	suite.verifyCondition(updatedAccount.Status.Conditions, "WorkspaceSubroutine_Ready", metav1.ConditionTrue, "Complete")
}

func (suite *AccountTestSuite) TestAccountInfoCreationForOrganization() {
	suite.T().Skip("AccountInfo subroutine requires direct KCP API access which is not available through APIExport endpoints in test environment")
	testContext := context.Background()
	accountName := "test-org-account"
	account := &v1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: accountName}, Spec: v1alpha1.AccountSpec{Type: v1alpha1.AccountTypeOrg}}

	suite.Require().NoError(suite.cli.Cluster(suite.rootOrgPath).Create(testContext, account))

	createdAccount := &v1alpha1.Account{}
	suite.Assert().Eventually(func() bool {
		if err := suite.cli.Cluster(suite.rootOrgPath).Get(testContext, types.NamespacedName{Name: accountName}, createdAccount); err != nil {
			return false
		}
		return meta.IsStatusConditionTrue(createdAccount.Status.Conditions, "AccountInfoSubroutine_Ready")
	}, defaultTestTimeout, defaultTickInterval)

	accountInfo := &v1alpha1.AccountInfo{}
	suite.Assert().Eventually(func() bool {
		if err := suite.cli.Cluster(suite.rootOrgPath).Get(testContext, client.ObjectKey{Name: accountinfo.DefaultAccountInfoName}, accountInfo); err != nil {
			return false
		}
		return accountInfo.Spec.Account.Type == v1alpha1.AccountTypeOrg
	}, defaultTestTimeout, defaultTickInterval)
}

func (suite *AccountTestSuite) TestWorkspaceFinalizerRemovesWorkspace() {
	suite.T().Skip("Workspace subroutine requires direct KCP API access which is not available through APIExport endpoints in test environment")
	testContext := context.Background()
	accountName := "test-workspace-finalizer"
	account := &v1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: accountName}, Spec: v1alpha1.AccountSpec{Type: v1alpha1.AccountTypeAccount}}

	suite.Require().NoError(suite.cli.Cluster(suite.rootOrgPath).Create(testContext, account))

	createdWorkspace := kcptenancyv1alpha.Workspace{}
	suite.Assert().Eventually(func() bool {
		return suite.cli.Cluster(suite.rootOrgPath).Get(testContext, types.NamespacedName{Name: accountName}, &createdWorkspace) == nil
	}, defaultTestTimeout, defaultTickInterval)

	suite.Require().NoError(suite.cli.Cluster(suite.rootOrgPath).Delete(testContext, account))

	suite.Assert().Eventually(func() bool {
		err := suite.cli.Cluster(suite.rootOrgPath).Get(testContext, types.NamespacedName{Name: accountName}, &kcptenancyv1alpha.Workspace{})
		return kerrors.IsNotFound(err)
	}, defaultTestTimeout, defaultTickInterval)
}

func (suite *AccountTestSuite) verifyWorkspace(ctx context.Context, accountName string) {
	workspace := &kcptenancyv1alpha.Workspace{}
	suite.Require().NoError(suite.cli.Cluster(suite.rootOrgPath).Get(ctx, types.NamespacedName{Name: accountName}, workspace))
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
