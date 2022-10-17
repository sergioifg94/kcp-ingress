package dns

import (
	"context"

	v1 "github.com/kuadrant/kcp-glbc/pkg/apis/kuadrant/v1"
)

// TODO once we have a specific Health Check API this should have its own controller rather than piggy backing on the DNSRecord
type HealthCheckReconciler interface {
	ReconcileHealthCheck(ctx context.Context, hc v1.HealthCheck, endpoint *v1.Endpoint) error

	DeleteHealthCheck(ctx context.Context, endpoint *v1.Endpoint) error
}

type fakeHealthCheckReconciler struct{}

func (*fakeHealthCheckReconciler) ReconcileHealthCheck(ctx context.Context, _ v1.HealthCheck, _ *v1.Endpoint) error {
	return nil
}

func (*fakeHealthCheckReconciler) DeleteHealthCheck(ctx context.Context, _ *v1.Endpoint) error {
	return nil
}

var _ HealthCheckReconciler = &fakeHealthCheckReconciler{}
