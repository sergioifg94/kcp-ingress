package service

import (
	"context"
	"strings"

	"github.com/kcp-dev/logicalcluster/v2"
	corev1 "k8s.io/api/core/v1"

	"github.com/kuadrant/kcp-glbc/pkg/_internal/metadata"
	"github.com/kuadrant/kcp-glbc/pkg/migration/workload"
)

func (c *Controller) reconcile(_ context.Context, service *corev1.Service) error {
	c.Logger.V(3).Info("starting reconcile of service ", "name", service.Name, "namespace", service.Namespace, "cluster", logicalcluster.From(service))
	c.migrationHandler(service, c.Queue, c.Logger)
	if service.DeletionTimestamp != nil && !service.DeletionTimestamp.IsZero() {
		//in 0.5.0 these are never cleaned up properly
		for _, f := range service.Finalizers {
			if strings.Contains(f, workload.SyncerFinalizer) {
				metadata.RemoveFinalizer(service, f)
			}
		}
	}
	return nil
}
