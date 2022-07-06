package deployment

import (
	"context"
	"github.com/kuadrant/kcp-glbc/pkg/util/workloadMigration"

	appsv1 "k8s.io/api/apps/v1"
)

func (c *Controller) reconcile(ctx context.Context, deployment *appsv1.Deployment) error {
	workloadMigration.Process(deployment, c.Queue)
	return nil
}
