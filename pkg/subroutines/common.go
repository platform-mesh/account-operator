package subroutines

import (
	"context"
	"fmt"

	kcptenancyv1alpha "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"
	"github.com/platform-mesh/golang-commons/errors"
	"github.com/platform-mesh/golang-commons/logger"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/platform-mesh/account-operator/api/v1alpha1"
)

const (
	orgsWorkspacePath = "root:orgs"
)

func generateAccountWorkspaceTypeName(instance *v1alpha1.Account) string {
	return fmt.Sprintf("%s-%s", instance.Name, "acc")
}

func generateOrganizationWorkspaceTypeName(instance *v1alpha1.Account) string {
	return fmt.Sprintf("%s-%s", instance.Name, "org")
}

func retrieveWorkspace(ctx context.Context, instance *v1alpha1.Account, c client.Client, log *logger.Logger) (*kcptenancyv1alpha.Workspace, error) {
	ws := &kcptenancyv1alpha.Workspace{}
	err := c.Get(ctx, client.ObjectKey{Name: instance.Name}, ws)
	if err != nil {
		const msg = "workspace does not exist"
		log.Error().Msg(msg)
		return nil, errors.Wrap(err, msg)
	}
	return ws, nil
}
