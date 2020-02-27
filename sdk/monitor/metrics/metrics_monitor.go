package metrics

import "github.com/networkservicemesh/cmd-nsmgr/pkg/api/crossconnect"

type MetricsMonitor interface {
	HandleMetrics(statistics map[string]*crossconnect.Metrics)
}
