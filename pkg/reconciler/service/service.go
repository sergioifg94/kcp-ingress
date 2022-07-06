package service

import (
	"context"
	"github.com/kuadrant/kcp-glbc/pkg/util/workloadMigration"
	corev1 "k8s.io/api/core/v1"
)

func (c *Controller) reconcile(ctx context.Context, service *corev1.Service) error {
	workloadMigration.Process(service, c.Queue)
	return nil
}
