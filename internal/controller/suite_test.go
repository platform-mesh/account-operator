package controller_test

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"time"

	"github.com/kcp-dev/logicalcluster/v3"
	apiexport "github.com/kcp-dev/multicluster-provider/apiexport"
	mcc "github.com/kcp-dev/multicluster-provider/client"
	"github.com/kcp-dev/multicluster-provider/envtest"
	kcpapisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	kcptenancyv1alpha "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
	"github.com/platform-mesh/account-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"

	"sigs.k8s.io/controller-runtime/pkg/client"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	"sigs.k8s.io/yaml"
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
	rootWorkspace               = "root"
	platformMeshSystemWorkspace = "platform-mesh-system"
	orgsWorkspace               = "orgs"
	defaultWorkspace            = "default"
)

const relativeBinaryAssetsDirectory = "../../bin"

// setupKCP starts KCP and sets up the basic platform-mesh workspace structure
// and configuration.
func (s *AccountTestSuite) setupKCP() {
	s.env = &envtest.Environment{AttachKcpOutput: false}
	s.env.BinaryAssetsDirectory = relativeBinaryAssetsDirectory
	s.env.KcpStartTimeout = 2 * time.Minute
	s.env.KcpStopTimeout = 30 * time.Second

	var err error
	s.kcpConfig, err = s.env.Start()
	s.Require().NoError(err)

	s.kcpClient, err = mcc.New(s.kcpConfig, client.Options{
		Scheme: s.scheme,
	})
	s.Require().NoError(err)

	// Setup root workspace and wait for children to be ready
	s.rootClient = s.kcpClient.Cluster(logicalcluster.NewPath(rootWorkspace))
	for _, data := range rootWorkspaceTypes {
		var wt kcptenancyv1alpha1.WorkspaceType
		err := yaml.Unmarshal(data, &wt)
		s.Require().NoError(err, "Unmarshalling embedded data")

		err = s.rootClient.Create(s.ctx, &wt)
		s.Require().NoError(err, "Creating unmarshalled object")
	}
	for _, data := range rootWorkspaces {
		var ws kcptenancyv1alpha1.Workspace
		err := yaml.Unmarshal(data, &ws)
		s.Require().NoError(err, "Unmarshalling embedded data")

		err = s.rootClient.Create(s.ctx, &ws)
		s.Require().NoError(err, "Creating unmarshalled object")
	}
	s.rootOrgsClient = s.kcpClient.Cluster(logicalcluster.NewPath(rootWorkspace).Join(orgsWorkspace))

	// Create APIResourceSchemas of platform-mesh-system
	s.waitForWorkspace(platformMeshSystemWorkspace, s.rootClient)
	pmsClient := s.kcpClient.Cluster(logicalcluster.NewPath(rootWorkspace).Join(platformMeshSystemWorkspace))
	for _, data := range platformMeshSystemAPIResourceSchemas {
		var ars kcpapisv1alpha1.APIResourceSchema
		err := yaml.Unmarshal(data, &ars)
		s.Require().NoError(err, "Unmarshalling embedded data")

		err = pmsClient.Create(s.ctx, &ars)
		s.Require().NoError(err, "Creating unmarshalled object")
	}

	// Fetch identity hash and create "core.platform-mesh.io" APIExport in
	// platform-mesh-system
	var aePlatformMesh, aeTenancy kcpapisv1alpha1.APIExport
	err = s.rootClient.Get(s.ctx, types.NamespacedName{Name: "tenancy.kcp.io"}, &aeTenancy)
	s.Require().NoError(err)
	err = yaml.Unmarshal(apiexportCorePlatformMeshIoYAML, &aePlatformMesh)
	s.Require().NoError(err, "Unmarshalling embedded data")
	aePlatformMesh.Spec.PermissionClaims[1].IdentityHash = aeTenancy.Status.IdentityHash
	aePlatformMesh.Spec.PermissionClaims[2].IdentityHash = aeTenancy.Status.IdentityHash
	err = pmsClient.Create(s.ctx, &aePlatformMesh)
	s.Require().NoError(err, "Creating unmarshalled object")

	// Create APIExportEndpointSlice for exported API
	// TODO(simontesar): is this really still necessary, should it get populated?
	time.Sleep(time.Second * 10) // TODO(simontesar): race, auf was muss man hier warten?
	var axes kcpapisv1alpha1.APIExportEndpointSlice
	err = yaml.Unmarshal(apiexportendpointsliceCorePlatformMeshOrgYAML, &axes)
	s.Require().NoError(err, "Unmarshalling embedded data")
	err = pmsClient.Create(s.ctx, &axes)
	s.Require().NoError(err, "Creating unmarshalled object")
}

func (s *AccountTestSuite) waitForWorkspace(name string, c client.Client) {
	err := wait.PollUntilContextTimeout(s.ctx, time.Millisecond*500, 15*time.Second, true, func(ctx context.Context) (bool, error) {
		s.logger.Info().Str("workspace", name).Msg("Waiting for workspace to be ready")
		ws := &kcptenancyv1alpha.Workspace{}
		if err := c.Get(ctx, types.NamespacedName{Name: name}, ws); err != nil {
			return false, nil //nolint:nilerr
		}

		if ws.Status.Phase == "Ready" {
			s.logger.Info().Str("workspace", name).Msg("Workspace is ready")
			return true, nil
		}
		return false, nil
	})

	if err != nil {
		s.T().Fatalf("Workspace %s did not get ready", name)
	}
}

func (s *AccountTestSuite) setupManager() {
	// Setup root workspace
	providerCfg := rest.CopyConfig(s.kcpConfig)
	providerCfg.Host += fmt.Sprintf("/clusters/%s", logicalcluster.NewPath(rootWorkspace).Join(platformMeshSystemWorkspace))

	axplogr := s.logger.ComponentLogger("apiexport_provider").Logr()
	provider, err := apiexport.New(providerCfg, "core.platform-mesh.io", apiexport.Options{
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

	s.mgr, err = mcmanager.New(providerCfg, provider, mcOpts)
	s.Require().NoError(err)

	s.mgrCtx, s.mgrCancel = context.WithCancel(s.ctx)
}

func (s *AccountTestSuite) startManager() {
	go func() {
		if err := s.mgr.Start(s.mgrCtx); err != nil && !errors.Is(err, context.Canceled) {
			s.T().Fatalf("Failed to start manager: %v", err)
		}
	}()
}

func (s *AccountTestSuite) setupDefaultOrg() {
	// Setup orgs workspace with test "default" organisation
	s.waitForWorkspace(orgsWorkspace, s.rootClient)
	var acc v1alpha1.Account
	err := yaml.Unmarshal(accountRootOrgYAML, &acc)
	s.Require().NoError(err, "Unmarshalling embedded data")
	err = s.rootOrgsClient.Create(s.ctx, &acc)
	s.Require().NoError(err, "Creating unmarshalled object")
	s.waitForWorkspace(defaultWorkspace, s.rootOrgsClient)
	s.rootOrgsDefaultClient = s.kcpClient.Cluster(logicalcluster.NewPath(rootWorkspace).Join(orgsWorkspace).Join(defaultWorkspace))
}
