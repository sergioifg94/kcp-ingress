package dns

import (
	"context"

	v1 "github.com/kuadrant/kcp-glbc/pkg/apis/kuadrant/v1"
)

type HealthCheckReconciler interface {
	Reconcile(ctx context.Context, spec HealthCheckSpec, endpoint *v1.Endpoint) error

	Delete(ctx context.Context, endpoint *v1.Endpoint) error
}

type HealthCheckSpec struct {
	Id               string
	Name             string
	Port             *int64
	FailureThreshold *int64
	Protocol         *HealthCheckProtocol

	Path string
}

type HealthCheckProtocol string

const HealthCheckProtocolHTTP HealthCheckProtocol = "HTTP"
const HealthCheckProtocolHTTPS HealthCheckProtocol = "HTTPS"

type FakeHealthCheckReconciler struct{}

func (*FakeHealthCheckReconciler) Reconcile(ctx context.Context, _ HealthCheckSpec, _ *v1.Endpoint) error {
	return nil
}

func (*FakeHealthCheckReconciler) Delete(ctx context.Context, _ *v1.Endpoint) error {
	return nil
}

var _ HealthCheckReconciler = &FakeHealthCheckReconciler{}
