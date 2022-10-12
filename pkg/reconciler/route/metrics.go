package route

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/kuadrant/kcp-glbc/pkg/metrics"
)

var (
	routeObjectTotal = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "glbc_route_managed_object_total",
			Help: "Total number of managed route object",
		})
)

func init() {
	// Register metrics with the global prometheus registry
	metrics.Registry.MustRegister(
		routeObjectTotal,
	)
}

func InitMetrics() {
}
