package deployment

import (
	"context"
	"strings"

	"github.com/kcp-dev/logicalcluster/v2"
	appsv1 "k8s.io/api/apps/v1"

	"github.com/kuadrant/kcp-glbc/pkg/_internal/metadata"
	"github.com/kuadrant/kcp-glbc/pkg/migration/workload"
)

func (c *Controller) reconcile(_ context.Context, deployment *appsv1.Deployment) error {
	c.Logger.V(3).Info("starting reconcile of deployment ", "name", deployment.Name, "namespace", deployment.Namespace, "cluster", logicalcluster.From(deployment))
	c.migrationHandler(deployment, c.Queue, c.Logger)
	if deployment.DeletionTimestamp != nil && !deployment.DeletionTimestamp.IsZero() {
		//in 0.5.0 these are never cleaned up properly
		for _, f := range deployment.Finalizers {
			if strings.Contains(f, workload.SyncerFinalizer) {
				metadata.RemoveFinalizer(deployment, f)
			}
		}
	}
	return nil
}
