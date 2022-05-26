package ingress

import (
	"time"

	"github.com/kuadrant/kcp-glbc/pkg/metrics"
	"github.com/prometheus/client_golang/prometheus"
	networkingv1 "k8s.io/api/networking/v1"
)

const (
	ingressWorkspace     = "workspace"
	ingressNamespace     = "namespace"
	ingressName          = "name"
	resultLabel          = "result"
	resultLabelSucceeded = "succeeded"
	resultLabelFailed    = "failed"
)

var (
	ingressObjectTimeToAdmission = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "glbc_ingress_managed_object_time_to_admission",
			Help: "Duration of the ingress object admission",
			Buckets: []float64{
				1 * time.Second.Seconds(),
				5 * time.Second.Seconds(),
				10 * time.Second.Seconds(),
				15 * time.Second.Seconds(),
				30 * time.Second.Seconds(),
				45 * time.Second.Seconds(),
				1 * time.Minute.Seconds(),
				2 * time.Minute.Seconds(),
				5 * time.Minute.Seconds(),
			},
		},
		[]string{
			ingressWorkspace,
			ingressNamespace,
		})

	ingressObjectTotal = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "glbc_ingress_managed_object_total",
			Help: "Total number of managed ingress object",
		}, []string{
			ingressWorkspace,
			ingressNamespace,
		})

	ingressObjectReconcilationTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "glbc_ingress_managed_object_reconciliation",
			Help: "Number of managed ingress object reconciliation",
		}, []string{
			ingressWorkspace,
			ingressNamespace,
			ingressName,
			resultLabel,
		})
)

func init() {
	// Register metrics with the global prometheus registry
	metrics.Registry.MustRegister(
		ingressObjectTimeToAdmission,
		ingressObjectTotal,
		ingressObjectReconcilationTotal,
	)
}

func initMetrics(ingress *networkingv1.Ingress) {
	ingressObjectReconcilationTotal.WithLabelValues(ingress.ClusterName, ingress.Namespace, ingress.Name, resultLabelSucceeded).Add(0)
	ingressObjectReconcilationTotal.WithLabelValues(ingress.ClusterName, ingress.Namespace, ingress.Name, resultLabelFailed).Add(0)
}
