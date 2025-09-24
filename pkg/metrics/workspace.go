package metrics

import "github.com/prometheus/client_golang/prometheus"

var (
	WorkspaceForbiddenCreations = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "account_operator",
			Subsystem: "workspace",
			Name:      "creation_forbidden_total",
			Help:      "Total number of workspace creation attempts that were forbidden (HTTP 403).",
		},
		[]string{"cluster", "account_type"},
	)
)

func RegisterWorkspaceMetrics(reg prometheus.Registerer) {
	reg.MustRegister(WorkspaceForbiddenCreations)
}
