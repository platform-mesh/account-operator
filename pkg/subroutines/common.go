package subroutines

import (
	"context"
	"fmt"
	"net/url"

	kcptenancyv1alpha "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"
	"github.com/platform-mesh/golang-commons/errors"
	"github.com/platform-mesh/golang-commons/logger"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/platform-mesh/account-operator/api/v1alpha1"
)

const (
	orgsWorkspacePath = "root:orgs"
)

func generateAccountWorkspaceTypeName(organizationName string) string {
	return fmt.Sprintf("%s-%s", organizationName, "acc")
}

func generateOrganizationWorkspaceTypeName(organizationName string) string {
	return fmt.Sprintf("%s-%s", organizationName, "org")
}

func retrieveWorkspace(ctx context.Context, instance *v1alpha1.Account, c client.Client, log *logger.Logger) (*kcptenancyv1alpha.Workspace, error) {
	if instance == nil {
		return nil, fmt.Errorf("account is nil")
	}
	if c == nil {
		return nil, fmt.Errorf("client is nil")
	}

	ws := &kcptenancyv1alpha.Workspace{}
	err := c.Get(ctx, client.ObjectKey{Name: instance.Name}, ws)
	if err != nil {
		// Keep the wrapped message stable for tests, but log with more context
		const msg = "workspace does not exist"
		if kerrors.IsNotFound(err) {
			if log != nil {
				log.Info().Str("account", instance.Name).Msg(msg)
			}
		} else {
			if log != nil {
				log.Error().Err(err).Str("account", instance.Name).Msg("failed to get workspace")
			}
		}
		return nil, errors.Wrap(err, msg)
	}
	return ws, nil
}

func createOrganizationRestConfig(cfg *rest.Config) (*rest.Config, error) {
	parsedUrl, err := url.Parse(cfg.Host)
	if err != nil {
		return nil, err
	}
	copyConfig := rest.CopyConfig(cfg)
	protocolHost := fmt.Sprintf("%s://%s", parsedUrl.Scheme, parsedUrl.Host)
	copyConfig.Host = fmt.Sprintf("%s/clusters/%s", protocolHost, orgsWorkspacePath)
	// Remove cluster aware round tripper to avoid redirect issues
	copyConfig.WrapTransport = nil
	return copyConfig, nil
}
