package subroutines

import (
	"context"

	kcptypes "github.com/platform-mesh/account-operator/pkg/types"
	"github.com/platform-mesh/golang-commons/errors"
	"github.com/platform-mesh/golang-commons/logger"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/platform-mesh/account-operator/api/v1alpha1"
)

func retrieveWorkspace(ctx context.Context, instance *v1alpha1.Account, c client.Client, log *logger.Logger) (*kcptypes.Workspace, error) {
	ws := &kcptypes.Workspace{}
	err := c.Get(ctx, client.ObjectKey{Name: instance.Name}, ws)
	if err != nil {
		const msg = "workspace does not exist"
		log.Error().Msg(msg)
		return nil, errors.Wrap(err, msg)
	}
	return ws, nil
}
