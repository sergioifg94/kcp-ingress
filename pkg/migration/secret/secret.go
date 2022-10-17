package secret

import (
	"context"
	"strings"

	"github.com/kcp-dev/logicalcluster/v2"
	corev1 "k8s.io/api/core/v1"

	"github.com/kuadrant/kcp-glbc/pkg/_internal/metadata"
	"github.com/kuadrant/kcp-glbc/pkg/migration/workload"
)

func (c *Controller) reconcile(_ context.Context, secret *corev1.Secret) error {
	c.Logger.V(3).Info("starting reconcile of secret ", "name", secret.Name, "namespace", secret.Namespace, "cluster", logicalcluster.From(secret))
	c.migrationHandler(secret, c.Queue, c.Logger)
	if secret.DeletionTimestamp != nil && !secret.DeletionTimestamp.IsZero() {
		//in 0.5.0 these are never cleaned up properly
		for _, f := range secret.Finalizers {
			if strings.Contains(f, workload.SyncerFinalizer) {
				metadata.RemoveFinalizer(secret, f)
			}
		}
	}
	return nil
}
