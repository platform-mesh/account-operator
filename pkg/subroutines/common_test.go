package subroutines_test

import (
	kcptypes "github.com/platform-mesh/account-operator/pkg/types"
	"github.com/stretchr/testify/mock"
	"k8s.io/apimachinery/pkg/types"

	"github.com/platform-mesh/account-operator/pkg/subroutines/mocks"
)

func mockGetWorkspaceByName(clientMock *mocks.Client, ready kcptypes.LogicalClusterPhaseType, path string) {
	clientMock.On("Get", mock.Anything, mock.Anything, mock.AnythingOfType("*types.Workspace")).
		Run(func(args mock.Arguments) {
			key := args.Get(1).(types.NamespacedName)
			obj := args.Get(2).(*kcptypes.Workspace)
			wsPath := ""
			if path != "" {
				wsPath = "https://example.com/" + path
			}
			obj.Name = key.Name
			obj.Spec = kcptypes.WorkspaceSpec{
				URL: wsPath,
			}
			obj.Status.Phase = ready
		}).
		Return(nil)
}
