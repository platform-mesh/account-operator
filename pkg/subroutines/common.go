package subroutines

import (
	"context"
	"crypto/sha256"
	"fmt"
	"regexp"
	"strings"

	kcptenancyv1alpha "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"
	"github.com/platform-mesh/golang-commons/errors"
	"github.com/platform-mesh/golang-commons/logger"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/platform-mesh/account-operator/api/v1alpha1"
)

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

func extractWorkspaceNameFromPath(path string) string {
	if path == "" {
		return ""
	}
	parts := strings.Split(path, ":")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return ""
}

func sanitizeForKubernetes(name string) string {
	if name == "" {
		return ""
	}

	validChars := regexp.MustCompile(`[^a-z0-9-]`)
	sanitized := validChars.ReplaceAllString(strings.ToLower(name), "-")

	if len(sanitized) > 0 && !regexp.MustCompile(`^[a-z]`).MatchString(sanitized) {
		sanitized = "w-" + sanitized
	}

	if len(sanitized) > 0 && !regexp.MustCompile(`[a-z0-9]$`).MatchString(sanitized) {
		sanitized = sanitized + "w"
	}

	if len(sanitized) > 63 {
		hash := fmt.Sprintf("%x", sha256.Sum256([]byte(name)))[:8]
		maxPrefix := 63 - len(hash) - 1 // -1 for the hyphen
		if maxPrefix > 0 {
			sanitized = sanitized[:maxPrefix] + "-" + hash
		} else {
			sanitized = "w-" + hash
		}
	}

	return sanitized
}

func GetOrgWorkspaceTypeName(accountName, workspacePath string) string {
	workspaceName := extractWorkspaceNameFromPath(workspacePath)
	var name string
	if workspaceName != "" {
		name = fmt.Sprintf("%s-%s-org", workspaceName, accountName)
	} else {
		name = fmt.Sprintf("%s-org", accountName)
	}
	return sanitizeForKubernetes(name)
}

func GetAccWorkspaceTypeName(accountName, workspacePath string) string {
	workspaceName := extractWorkspaceNameFromPath(workspacePath)
	var name string
	if workspaceName != "" {
		name = fmt.Sprintf("%s-%s-acc", workspaceName, accountName)
	} else {
		name = fmt.Sprintf("%s-acc", accountName)
	}
	return sanitizeForKubernetes(name)
}
