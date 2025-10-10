package subroutines

import (
	"fmt"
)

func generateAccountWorkspaceTypeName(organizationName string) string {
	return fmt.Sprintf("%s-%s", organizationName, "acc")
}

func generateOrganizationWorkspaceTypeName(organizationName string) string {
	return fmt.Sprintf("%s-%s", organizationName, "org")
}
