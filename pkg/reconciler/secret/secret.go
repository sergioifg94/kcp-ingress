package secret

import (
	"context"
	"strings"

	"github.com/kuadrant/kcp-glbc/pkg/util/metadata"
	"github.com/kuadrant/kcp-glbc/pkg/util/workloadMigration"

	corev1 "k8s.io/api/core/v1"
)

func (c *Controller) reconcile(ctx context.Context, secret *corev1.Secret) error {
	workloadMigration.Process(secret, c.Queue, c.Logger)
	if secret.DeletionTimestamp != nil && !secret.DeletionTimestamp.IsZero() {
		//in 0.5.0 these are never cleaned up properly
		for _, f := range secret.Finalizers {
			if strings.Contains(f, workloadMigration.SyncerFinalizer) {
				metadata.RemoveFinalizer(secret, f)
			}
		}
	}
	return nil
}
