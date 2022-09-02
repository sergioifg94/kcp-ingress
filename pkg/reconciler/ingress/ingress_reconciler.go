package ingress

import (
	"context"
	"strconv"
	"strings"

	networkingv1 "k8s.io/api/networking/v1"

	"k8s.io/apimachinery/pkg/runtime"
	utilserrors "k8s.io/apimachinery/pkg/util/errors"
	apiRuntime "k8s.io/apimachinery/pkg/util/runtime"

	"k8s.io/client-go/tools/cache"

	"github.com/kcp-dev/logicalcluster/v2"

	"github.com/kuadrant/kcp-glbc/pkg/util/metadata"
	"github.com/kuadrant/kcp-glbc/pkg/util/workloadMigration"
)

type reconcileStatus int

const (
	reconcileStatusStop reconcileStatus = iota
	reconcileStatusContinue
	cascadeCleanupFinalizer  = "kuadrant.dev/cascade-cleanup"
	GeneratedRulesAnnotation = "kuadrant.dev/custom-hosts.generated"
)

type reconciler interface {
	reconcile(ctx context.Context, ingress *networkingv1.Ingress) (reconcileStatus, error)
}

func (c *Controller) reconcile(ctx context.Context, ingress *networkingv1.Ingress) error {
	c.Logger.V(3).Info("starting reconcile of ingress ", "name", ingress.Name, "namespace", ingress.Namespace, "cluster", logicalcluster.From(ingress))
	if ingress.DeletionTimestamp == nil {
		metadata.AddFinalizer(ingress, cascadeCleanupFinalizer)
	}
	//TODO evaluate where this actually belongs
	if c.advancedSchedulingEnabled {
		workloadMigration.Process(ingress, c.Queue, c.Logger)
	}

	reconcilers := []reconciler{
		//hostReconciler is first as the others depends on it for the host to be set on the ingress
		&hostReconciler{
			managedDomain:          c.domain,
			log:                    c.Logger,
			customHostsEnabled:     c.customHostsEnabled,
			GetDomainVerifications: c.getDomainVerifications,
		},
		&certificateReconciler{
			createCertificate:    c.certProvider.Create,
			deleteCertificate:    c.certProvider.Delete,
			getCertificateSecret: c.certProvider.GetCertificateSecret,
			updateCertificate:    c.certProvider.Update,
			getCertificateStatus: c.certProvider.GetCertificateStatus,
			copySecret:           c.copySecret,
			deleteSecret:         c.deleteTLSSecret,
			log:                  c.Logger,
		},
		&dnsReconciler{
			deleteDNS:        c.deleteDNS,
			DNSLookup:        c.hostResolver.LookupIPAddr,
			getDNS:           c.getDNS,
			createDNS:        c.createDNS,
			updateDNS:        c.updateDNS,
			watchHost:        c.hostsWatcher.StartWatching,
			forgetHost:       c.hostsWatcher.StopWatching,
			listHostWatchers: c.hostsWatcher.ListHostRecordWatchers,
			log:              c.Logger,
		},
	}
	var errs []error

	for _, r := range reconcilers {
		status, err := r.reconcile(ctx, ingress)
		if err != nil {
			errs = append(errs, err)
		}
		if status == reconcileStatusStop {
			break
		}
	}

	if len(errs) == 0 {
		if ingress.DeletionTimestamp != nil && !ingress.DeletionTimestamp.IsZero() {
			metadata.RemoveFinalizer(ingress, cascadeCleanupFinalizer)
			c.hostsWatcher.StopWatching(objectKey(ingress), "")
			//in 0.5.0 these are never cleaned up properly
			for _, f := range ingress.Finalizers {
				if strings.Contains(f, workloadMigration.SyncerFinalizer) {
					metadata.RemoveFinalizer(ingress, f)
				}
			}
		}
	}
	c.Logger.V(3).Info("ingress reconcile complete", "errors", strconv.Itoa(len(errs)), "name", ingress.Name, "namespace", ingress.Namespace, "cluster", logicalcluster.From(ingress))
	return utilserrors.NewAggregate(errs)
}

func objectKey(obj runtime.Object) cache.ExplicitKey {
	key, _ := cache.MetaNamespaceKeyFunc(obj)
	return cache.ExplicitKey(key)
}

// enqueueIngresses creates an event handler function given a function that
// returns a list of ingresses to enqueue, or an error. If an error is returned,
// no ingresses are enqueued.
//
// This allows to easierly unit test the logic of the function that returns
// the ingresses to enqueue
func (c *Controller) enqueueIngresses(getIngresses func(obj interface{}) ([]*networkingv1.Ingress, error)) func(obj interface{}) {
	return func(obj interface{}) {
		ingresses, err := getIngresses(obj)
		if err != nil {
			apiRuntime.HandleError(err)
			return
		}

		for _, ingress := range ingresses {
			ingressKey, err := cache.MetaNamespaceKeyFunc(ingress)
			if err != nil {
				apiRuntime.HandleError(err)
				continue
			}

			c.Queue.Add(ingressKey)
		}
	}
}

func (c *Controller) enqueueIngressesFromUpdate(getIngresses func(obj interface{}) ([]*networkingv1.Ingress, error)) func(oldObj, newObj interface{}) {
	return func(oldObj, newObj interface{}) {
		c.enqueueIngresses(getIngresses)(newObj)
	}
}
