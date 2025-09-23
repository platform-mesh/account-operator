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

var (
	validCharsRegex           = regexp.MustCompile(`[^a-z0-9-]`)
	startsWithAlphaRegex      = regexp.MustCompile(`^[a-z]`)
	endsWithAlphanumericRegex = regexp.MustCompile(`[a-z0-9]$`)
)

// retrieveWorkspace fetches the KCP Workspace resource whose name matches the provided Account's Name.
// It returns the Workspace on success. If the client Get fails, the function logs the failure and
// returns an error that wraps the underlying client error with the message "workspace does not exist".
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

// extractWorkspaceNameFromPath extracts and returns the workspace name from a colon-separated path.
// It returns the substring after the last ':'; if the input is empty or contains no segments, it returns an empty string.
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

// sanitizeForKubernetes returns a Kubernetes-safe resource name derived from the
// provided string.
//
// It lowercases the input, replaces characters outside [a-z0-9-] with '-', ensures
// the name starts with a lowercase letter (prefixing "w-" if necessary) and ends
// with an alphanumeric character (appending "w" if necessary). If the resulting
// name exceeds 63 characters it is truncated and suffixed with an 8-hex-digit
// SHA-256 hash of the original input (separated by a hyphen). An empty input
// yields an empty string.
func sanitizeForKubernetes(name string) string {
	if name == "" {
		return ""
	}

	sanitized := validCharsRegex.ReplaceAllString(strings.ToLower(name), "-")

	if len(sanitized) > 0 && !startsWithAlphaRegex.MatchString(sanitized) {
		sanitized = "w-" + sanitized
	}

	if len(sanitized) > 0 && !endsWithAlphanumericRegex.MatchString(sanitized) {
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

// GetOrgWorkspaceTypeName returns a Kubernetes-safe workspace type name for an organization workspace.
// If workspacePath contains a workspace component (the segment after the last ':'), that component is
// combined with accountName and the "org" suffix; otherwise the name is based on accountName and the
// "org" suffix. The final string is sanitized to conform to Kubernetes resource naming rules.
func GetOrgWorkspaceTypeName(accountName, workspacePath string) string {
	return getWorkspaceTypeName(accountName, workspacePath, "org")
}

// GetAccWorkspaceTypeName returns a Kubernetes-safe workspace type name for an account.
// If workspacePath contains a workspace segment (the portion after the last ':'), the
// resulting base name is "<workspaceName>-<accountName>-acc"; otherwise it's
// "<accountName>-acc". The returned string is sanitized to meet Kubernetes resource
// naming rules (lowercased, invalid characters replaced, must start with a letter,
// end with an alphanumeric character, and truncated/hashed if it exceeds length limits).
func GetAccWorkspaceTypeName(accountName, workspacePath string) string {
	return getWorkspaceTypeName(accountName, workspacePath, "acc")
}

// getWorkspaceTypeName constructs a workspace type identifier from the account name,
// workspace path, and suffix, then returns a Kubernetes-safe version of that identifier.
// If workspacePath contains a colon-separated path, the last segment is used as the
// workspace name; when absent, the name is formed as "<accountName>-<suffix>".
// The resulting string is sanitized to meet Kubernetes resource name requirements.
func getWorkspaceTypeName(accountName, workspacePath, suffix string) string {
	workspaceName := extractWorkspaceNameFromPath(workspacePath)
	var name string
	if workspaceName != "" {
		name = fmt.Sprintf("%s-%s-%s", workspaceName, accountName, suffix)
	} else {
		name = fmt.Sprintf("%s-%s", accountName, suffix)
	}
	return sanitizeForKubernetes(name)
}
