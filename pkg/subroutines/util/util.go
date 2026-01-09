package util

import (
	"fmt"

	"github.com/platform-mesh/account-operator/api/v1alpha1"
)

func GetAccountWorkspaceTypeName(accountName string) string {
	return fmt.Sprintf("%s-acc", accountName)
}

func GetWorkspaceTypeName(accountName string, accountType v1alpha1.AccountType) string {
	return fmt.Sprintf("%s-%s", accountName, accountType)
}
