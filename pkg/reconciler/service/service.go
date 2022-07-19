package service

import (
	"context"
	"github.com/kuadrant/kcp-glbc/pkg/util/metadata"
	"github.com/kuadrant/kcp-glbc/pkg/util/workloadMigration"
	corev1 "k8s.io/api/core/v1"
	"strings"
)

func (c *Controller) reconcile(ctx context.Context, service *corev1.Service) error {
	workloadMigration.Process(service, c.Queue, c.Logger)
	if service.DeletionTimestamp != nil && !service.DeletionTimestamp.IsZero() {
		//in 0.5.0 these are never cleaned up properly
		for _, f := range service.Finalizers {
			if strings.Contains(f, workloadMigration.SyncerFinalizer) {
				metadata.RemoveFinalizer(service, f)
			}
		}
	}
	return nil
}
