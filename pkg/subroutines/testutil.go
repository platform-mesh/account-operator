package subroutines

import (
	"context"

	"github.com/kcp-dev/logicalcluster/v3"
	"sigs.k8s.io/controller-runtime/pkg/kontext"
)

// ctxTODO is a reusable context for unit tests in this package with a dummy cluster set.
var ctxTODO = kontext.WithCluster(context.TODO(), logicalcluster.Name("root"))
