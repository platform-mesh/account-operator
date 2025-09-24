package metrics

import (
	"strings"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	// WorkspaceForbiddenCreations counts forbidden workspace creation attempts.
	// cluster_scope label is truncated to reduce cardinality: provider root path, the 'orgs' prefix path, or the literal path if neither applies.
	WorkspaceForbiddenCreations = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "account_operator",
			Subsystem: "workspace",
			Name:      "creation_forbidden_total",
			Help:      "Total number of workspace creation attempts that were forbidden (HTTP 403).",
		},
		[]string{"cluster_scope", "account_type"},
	)
	registerWorkspaceMetricsOnce sync.Once
)

// SanitizeClusterLabel reduces label cardinality by truncating the logical cluster path.
// Examples:
//
//	"root:platform-mesh:orgs:acme" -> "root:platform-mesh:orgs"
//	"orgs:root-org" -> "orgs"
//	"" -> "unknown"
func SanitizeClusterLabel(path string) string { // coverage-ignore
	if path == "" {
		return "unknown"
	}
	segs := strings.Split(path, ":")
	for i, s := range segs {
		if s == "orgs" {
			// Return everything up to and including 'orgs'
			return strings.Join(segs[:i+1], ":")
		}
	}
	return path
}

func RegisterWorkspaceMetrics(reg prometheus.Registerer) { // coverage-ignore
	registerWorkspaceMetricsOnce.Do(func() {
		reg.MustRegister(WorkspaceForbiddenCreations)
	})
}
