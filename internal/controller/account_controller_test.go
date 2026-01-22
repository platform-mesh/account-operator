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
	"k8s.io/apimachinery/pkg/util/wait"
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

func (s *AccountTestSuite) SetupSuite() {
	logConfig := logger.DefaultConfig()
	logConfig.NoJSON = true
	logConfig.Name = "AccountTestSuite"
	logConfig.Level = "debug"

	log, err := logger.New(logConfig)
	s.Require().NoError(err)
	s.log = log
	ctrl.SetLogger(log.Logr())

	cfg := config.OperatorConfig{}
	cfg.Subroutines.FGA.Enabled = false
	cfg.Subroutines.Workspace.Enabled = true
	cfg.Subroutines.AccountInfo.Enabled = true
	cfg.Subroutines.WorkspaceType.Enabled = true
	cfg.Kcp.ProviderWorkspace = "root"

	s.ctx, s.cancel, _ = platformmeshcontext.StartContext(log, cfg, 2*time.Minute)
	testEnvLogger := log.ComponentLogger("kcpenvtest")

	s.scheme = runtime.NewScheme()
	utilruntime.Must(v1alpha1.AddToScheme(s.scheme))
	utilruntime.Must(v1.AddToScheme(s.scheme))
	utilruntime.Must(kcpapisv1alpha1.AddToScheme(s.scheme))
	utilruntime.Must(kcpcorev1alpha.AddToScheme(s.scheme))
	utilruntime.Must(kcptenancyv1alpha.AddToScheme(s.scheme))

	s.testEnv = &envtest.Environment{AttachKcpOutput: false}
	s.testEnv.BinaryAssetsDirectory = "../../bin"
	s.testEnv.KcpStartTimeout = 2 * time.Minute
	s.testEnv.KcpStopTimeout = 30 * time.Second

	kcpConfig, err := s.testEnv.Start()
	s.Require().NoError(err)
	s.mcc, err = mcc.New(kcpConfig, client.Options{
		Scheme: s.scheme,
	})
	s.Require().NoError(err)

	// Setup root workspace
	providerCfg := rest.CopyConfig(kcpConfig)
	providerCfg.Host += "/clusters/root:platform-mesh-system"
	s.Require().NoError(err)
	c := s.mcc.Cluster(logicalcluster.NewPath("root"))
	s.Require().NoError(err)
	WaitForWorkspaceWithTimeout(c, "root", testEnvLogger, 15*time.Second)

	// Setup root workspace and wait for workspaces to appear
	rootClient := s.mcc.Cluster(logicalcluster.NewPath("root"))
	for _, data := range rootWorkspaceTypes {
		var wt kcptenancyv1alpha1.WorkspaceType
		err := yaml.Unmarshal(data, &wt)
		s.Require().NoError(err, "Unmarshalling embedded data")

		err = rootClient.Create(s.ctx, &wt)
		s.Require().NoError(err, "Creating unmarshalled object")
	}
	for _, data := range rootWorkspaces {
		var ws kcptenancyv1alpha1.Workspace
		err := yaml.Unmarshal(data, &ws)
		s.Require().NoError(err, "Unmarshalling embedded data")

		err = rootClient.Create(s.ctx, &ws)
		s.Require().NoError(err, "Creating unmarshalled object")
	}

	// Setup platform-mesh-system workspace
	WaitForWorkspaceWithTimeout(c, "platform-mesh-system", testEnvLogger, 15*time.Second)
	pmsClient := s.mcc.Cluster(logicalcluster.NewPath("root:platform-mesh-system"))
	for _, data := range platformMeshSystemAPIResourceSchemas {
		var ars kcpapisv1alpha1.APIResourceSchema
		err := yaml.Unmarshal(data, &ars)
		s.Require().NoError(err, "Unmarshalling embedded data")

		testEnvLogger.Debug().Msgf("Creating APIResourceSchema %s", ars.Name)
		err = pmsClient.Create(s.ctx, &ars)
		s.Require().NoError(err, "Creating unmarshalled object")
	}

	var aePlatformMesh, aeTenancy kcpapisv1alpha1.APIExport
	err = rootClient.Get(s.ctx, types.NamespacedName{Name: "tenancy.kcp.io"}, &aeTenancy)
	s.Require().NoError(err)
	err = yaml.Unmarshal(apiexportCorePlatformMeshIoYAML, &aePlatformMesh)
	s.Require().NoError(err, "Unmarshalling embedded data")
	aePlatformMesh.Spec.PermissionClaims[1].IdentityHash = aeTenancy.Status.IdentityHash
	aePlatformMesh.Spec.PermissionClaims[2].IdentityHash = aeTenancy.Status.IdentityHash
	err = pmsClient.Create(s.ctx, &aePlatformMesh)
	s.Require().NoError(err, "Creating unmarshalled object")

	time.Sleep(time.Second * 10) // race, auf was muss man hier warten?
	var axes kcpapisv1alpha1.APIExportEndpointSlice
	err = yaml.Unmarshal(apiexportendpointsliceCorePlatformMeshOrgYAML, &axes)
	s.Require().NoError(err, "Unmarshalling embedded data")
	err = pmsClient.Create(s.ctx, &axes)
	s.Require().NoError(err, "Creating unmarshalled object")

	// Setup orgs workspace with test Account
	WaitForWorkspaceWithTimeout(c, "orgs", testEnvLogger, 15*time.Second)
	orgsClient := s.mcc.Cluster(logicalcluster.NewPath("root:orgs"))
	var acc v1alpha1.Account
	err = yaml.Unmarshal(accountRootOrgYAML, &acc)
	s.Require().NoError(err, "Unmarshalling embedded data")
	err = orgsClient.Create(s.ctx, &acc)
	s.Require().NoError(err, "Creating unmarshalled object")

	axplogr := log.ComponentLogger("apiexport_provider").Logr()
	s.provider, err = apiexport.New(providerCfg, "core.platform-mesh.io", apiexport.Options{
		Scheme: s.scheme,
		Log:    &axplogr,
	})
	s.Require().NoError(err)

	mcOpts := mcmanager.Options{
		Scheme: s.scheme,
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
		BaseContext: func() context.Context { return s.ctx },
	}

	s.multiClusterManager, err = mcmanager.New(providerCfg, s.provider, mcOpts)
	s.Require().NoError(err)

	mockClient := mocks.NewOpenFGAServiceClient(s.T())
	accountReconciler := controller.NewAccountReconciler(log, s.multiClusterManager, cfg, orgsClient, mockClient)

	managerCtx, cancel := context.WithCancel(s.ctx)
	eg, egCtx := errgroup.WithContext(managerCtx)
	eg.Go(func() error {
		return s.multiClusterManager.Start(egCtx)
	})

	s.T().Cleanup(func() {
		cancel()
		if err := eg.Wait(); err != nil && !errors.Is(err, context.Canceled) {
			s.T().Logf("controller manager exited with error: %v", err)
		}
	})

	dCfg := &platformmeshconfig.CommonServiceConfig{}
	s.Require().NoError(accountReconciler.SetupWithManager(s.multiClusterManager, dCfg, log))

	s.rootOrgsClient = s.mcc.Cluster(logicalcluster.NewPath("root:orgs"))
	s.rootOrgsDefaultClient = s.mcc.Cluster(logicalcluster.NewPath("root:orgs:default"))
	s.Require().NoError(WaitForWorkspaceWithTimeout(orgsClient, "default", testEnvLogger, time.Minute))
}

func (s *AccountTestSuite) TearDownSuite() {
	if s.cancel != nil {
		s.cancel(fmt.Errorf("tearing down test suite"))
	}
	if s.testEnv != nil {
		_ = s.testEnv.Stop()
	}
}

func (s *AccountTestSuite) TestAddingFinalizer() {
	testContext := context.Background()
	accountName := "test-account-finalizer"

	account := &v1alpha1.Account{
		ObjectMeta: metav1.ObjectMeta{
			Name: accountName,
		},
		Spec: v1alpha1.AccountSpec{
			Type: v1alpha1.AccountTypeOrg,
		},
	}

	s.Require().NoError(s.rootOrgsDefaultClient.Create(testContext, account))

	createdAccount := v1alpha1.Account{}
	s.Assert().Eventually(func() bool {
		err := s.rootOrgsDefaultClient.Get(testContext, types.NamespacedName{Name: accountName, Namespace: defaultNamespace}, &createdAccount)

		return err == nil && len(createdAccount.Finalizers) == 3
	}, defaultTestTimeout*2, defaultTickInterval)

	s.ElementsMatch([]string{"workspacetype.core.platform-mesh.io/finalizer", "account.core.platform-mesh.io/finalizer", "account.core.platform-mesh.io/info"}, createdAccount.Finalizers)
}

func (s *AccountTestSuite) TestWorkspaceCreation() {
	testContext := context.Background()
	accountName := "test-account-ws-creation"
	account := &v1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: accountName}, Spec: v1alpha1.AccountSpec{Type: v1alpha1.AccountTypeAccount}}

	s.Require().NoError(s.rootOrgsDefaultClient.Create(testContext, account))

	createdWorkspace := kcptenancyv1alpha.Workspace{}
	s.Assert().Eventually(func() bool {
		if err := s.rootOrgsDefaultClient.Get(testContext, types.NamespacedName{Name: accountName}, &createdWorkspace); err != nil {
			return false
		}
		return createdWorkspace.Status.Phase == kcpcorev1alpha.LogicalClusterPhaseReady
	}, defaultTestTimeout, defaultTickInterval)

	updatedAccount := &v1alpha1.Account{}
	s.Assert().Eventually(func() bool {
		if err := s.rootOrgsDefaultClient.Get(testContext, types.NamespacedName{Name: accountName}, updatedAccount); err != nil {
			return false
		}
		return meta.IsStatusConditionTrue(updatedAccount.Status.Conditions, "WorkspaceSubroutine_Ready")
	}, defaultTestTimeout, defaultTickInterval)

	s.verifyWorkspace(testContext, "default", accountName)
	s.verifyCondition(updatedAccount.Status.Conditions, "WorkspaceSubroutine_Ready", metav1.ConditionTrue, "Complete")
}

func (s *AccountTestSuite) TestAccountInfoCreationForOrganization() {
	testContext := context.Background()
	accountName := "test-org-account"
	account := &v1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: accountName}, Spec: v1alpha1.AccountSpec{Type: v1alpha1.AccountTypeOrg}}

	s.Require().NoError(s.rootOrgsClient.Create(testContext, account))

	createdAccount := &v1alpha1.Account{}
	s.Assert().Eventually(func() bool {
		if err := s.rootOrgsClient.Get(testContext, types.NamespacedName{Name: accountName}, createdAccount); err != nil {
			return false
		}
		return meta.IsStatusConditionTrue(createdAccount.Status.Conditions, "AccountInfoSubroutine_Ready")
	}, defaultTestTimeout, defaultTickInterval)

	accountInfo := &v1alpha1.AccountInfo{}
	s.Assert().Eventually(func() bool {
		if err := s.rootOrgsDefaultClient.Get(testContext, client.ObjectKey{Name: accountinfo.DefaultAccountInfoName}, accountInfo); err != nil {
			return false
		}
		return accountInfo.Spec.Account.Type == v1alpha1.AccountTypeOrg
	}, defaultTestTimeout, defaultTickInterval)
}

func (s *AccountTestSuite) TestWorkspaceFinalizerRemovesWorkspace() {
	testContext := context.Background()
	accountName := "test-workspace-finalizer"
	account := &v1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: accountName}, Spec: v1alpha1.AccountSpec{Type: v1alpha1.AccountTypeAccount}}

	s.Require().NoError(s.rootOrgsDefaultClient.Create(testContext, account))

	createdWorkspace := kcptenancyv1alpha.Workspace{}
	s.Assert().Eventually(func() bool {
		return s.rootOrgsDefaultClient.Get(testContext, types.NamespacedName{Name: accountName}, &createdWorkspace) == nil
	}, defaultTestTimeout, defaultTickInterval)

	s.Require().NoError(s.rootOrgsDefaultClient.Delete(testContext, account))

	s.Assert().Eventually(func() bool {
		err := s.rootOrgsDefaultClient.Get(testContext, types.NamespacedName{Name: accountName}, &kcptenancyv1alpha.Workspace{})
		return kerrors.IsNotFound(err)
	}, defaultTestTimeout, defaultTickInterval)
}

func (s *AccountTestSuite) verifyWorkspace(ctx context.Context, orgName, accountName string) {
	workspace := &kcptenancyv1alpha.Workspace{}
	s.Require().NoError(s.rootOrgsDefaultClient.Get(ctx, types.NamespacedName{Name: accountName}, workspace))
	s.Equal(accountName, workspace.Name)
	s.NotNil(workspace.Spec.Type)
	expectedType := kcptenancyv1alpha.WorkspaceTypeName(fmt.Sprintf("%s-acc", orgName))
	s.Equal(expectedType, workspace.Spec.Type.Name)
}

func (s *AccountTestSuite) verifyCondition(conditions []metav1.Condition, conditionType string, status metav1.ConditionStatus, reason string) {
	s.Assert().True(meta.IsStatusConditionPresentAndEqual(conditions, conditionType, status))
	condition := meta.FindStatusCondition(conditions, conditionType)
	s.Require().NotNil(condition)
	s.Equal(reason, condition.Reason)
}

func WaitForWorkspaceWithTimeout(client client.Client, name string, log *logger.Logger, timeout time.Duration) error {
	// It shouldn't take longer than 5s for the default namespace to be brought up in etcd
	err := wait.PollUntilContextTimeout(context.TODO(), time.Millisecond*500, timeout, true, func(ctx context.Context) (bool, error) {
		ws := &kcptenancyv1alpha.Workspace{}
		if err := client.Get(ctx, types.NamespacedName{Name: name}, ws); err != nil {
			return false, nil //nolint:nilerr
		}
		ready := ws.Status.Phase == "Ready"
		log.Info().Str("workspace", name).Bool("ready", ready).Msg("waiting for workspace to be ready")
		return ready, nil
	})

	if err != nil {
		return fmt.Errorf("workspace %s did not become ready: %w", name, err)
	}
	return err
}
