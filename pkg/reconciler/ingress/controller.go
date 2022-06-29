package ingress

import (
	"context"
	"strings"

	v1 "github.com/kuadrant/kcp-glbc/pkg/apis/kuadrant/v1"
	"github.com/kuadrant/kcp-glbc/pkg/client/kuadrant/informers/externalversions"
	"k8s.io/apimachinery/pkg/labels"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	networkingv1lister "k8s.io/client-go/listers/networking/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	tenancyv1alpha1 "github.com/kcp-dev/kcp/pkg/apis/tenancy/v1alpha1"
	"github.com/kcp-dev/logicalcluster"

	kuadrantv1 "github.com/kuadrant/kcp-glbc/pkg/client/kuadrant/clientset/versioned"
	"github.com/kuadrant/kcp-glbc/pkg/net"
	"github.com/kuadrant/kcp-glbc/pkg/reconciler"
	"github.com/kuadrant/kcp-glbc/pkg/tls"
)

const controllerName = "kcp-glbc-ingress"

// NewController returns a new Controller which reconciles Ingress.
func NewController(config *ControllerConfig) *Controller {
	queue := workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), controllerName)

	hostResolver := config.HostResolver
	switch impl := hostResolver.(type) {
	case *net.ConfigMapHostResolver:
		impl.Client = config.KubeClient.Cluster(tenancyv1alpha1.RootCluster)
	}
	hostResolver = net.NewSafeHostResolver(hostResolver)

	base := reconciler.NewController(controllerName, queue)
	c := &Controller{
		Controller:                    base,
		kubeClient:                    config.KubeClient,
		certProvider:                  config.CertProvider,
		sharedInformerFactory:         config.SharedInformerFactory,
		kuadrantSharedInformerFactory: config.KuadrantSharedInformerFactory,
		kuadrantClient:                config.KuadrantClient,
		domain:                        config.Domain,
		tracker:                       newTracker(&base.Logger),
		hostResolver:                  hostResolver,
		hostsWatcher:                  net.NewHostsWatcher(&base.Logger, hostResolver, net.DefaultInterval),
		customHostsEnabled:            config.CustomHostsEnabled,
	}
	c.Process = c.process
	c.hostsWatcher.OnChange = c.Enqueue

	// Watch for events related to Ingresses
	c.sharedInformerFactory.Networking().V1().Ingresses().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			ingressObjectTotal.Inc()
			c.Enqueue(obj)
		},
		UpdateFunc: func(old, obj interface{}) {
			if old.(metav1.Object).GetResourceVersion() != obj.(metav1.Object).GetResourceVersion() {
				c.Enqueue(obj)
			}
		},
		DeleteFunc: func(obj interface{}) {
			ingressObjectTotal.Dec()
			c.Enqueue(obj)
		},
	})

	// Watch for events related to Services
	c.sharedInformerFactory.Core().V1().Services().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) { c.ingressesFromService(obj) },
		UpdateFunc: func(old, obj interface{}) {
			if old.(*corev1.Service).ResourceVersion != obj.(*corev1.Service).ResourceVersion {
				c.ingressesFromService(obj)
			}
		},
		DeleteFunc: func(obj interface{}) {
			c.ingressesFromService(obj)
		},
	})

	c.kuadrantSharedInformerFactory.Kuadrant().V1().DomainVerifications().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.ingressesFromDomainVerification,
		UpdateFunc: func(_, obj interface{}) { c.ingressesFromDomainVerification(obj) },
		DeleteFunc: c.ingressesFromDomainVerification,
	})

	c.indexer = c.sharedInformerFactory.Networking().V1().Ingresses().Informer().GetIndexer()
	c.lister = c.sharedInformerFactory.Networking().V1().Ingresses().Lister()

	return c
}

type ControllerConfig struct {
	KubeClient                    kubernetes.ClusterInterface
	KuadrantClient                kuadrantv1.ClusterInterface
	SharedInformerFactory         informers.SharedInformerFactory
	KuadrantSharedInformerFactory externalversions.SharedInformerFactory
	Domain                        string
	CertProvider                  tls.Provider
	HostResolver                  net.HostResolver
	CustomHostsEnabled            bool
}

type Controller struct {
	*reconciler.Controller
	kubeClient                    kubernetes.ClusterInterface
	sharedInformerFactory         informers.SharedInformerFactory
	kuadrantSharedInformerFactory externalversions.SharedInformerFactory
	kuadrantClient                kuadrantv1.ClusterInterface
	indexer                       cache.Indexer
	lister                        networkingv1lister.IngressLister
	certProvider                  tls.Provider
	domain                        string
	tracker                       *tracker
	hostResolver                  net.HostResolver
	hostsWatcher                  *net.HostsWatcher
	customHostsEnabled            bool
}

func (c *Controller) process(ctx context.Context, key string) error {
	object, exists, err := c.indexer.GetByKey(key)
	if err != nil {
		return err
	}

	if !exists {
		c.Logger.Info("Ingress was deleted", "key", key)
		// The Ingress has been deleted, so we remove any Ingress to Service tracking.
		c.tracker.deleteIngress(key)
		return nil
	}

	current := object.(*networkingv1.Ingress)
	target := current.DeepCopy()

	err = c.reconcile(ctx, target)
	if err != nil {
		return err
	}

	if !equality.Semantic.DeepEqual(current, target) {
		_, err := c.kubeClient.Cluster(logicalcluster.From(target)).NetworkingV1().Ingresses(target.Namespace).Update(ctx, target, metav1.UpdateOptions{})
		return err
	}

	return nil
}

// ingressesFromService enqueues all the related Ingresses for a given service when the service is changed.
func (c *Controller) ingressesFromService(obj interface{}) {
	service := obj.(*corev1.Service)

	serviceKey, err := cache.MetaNamespaceKeyFunc(service)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	// Does that Service has any Ingress associated to?
	ingresses := c.tracker.getIngressesForService(serviceKey)

	// One Service can be referenced by 0..N Ingresses, so we need to enqueue all the related Ingresses.
	for _, ingress := range ingresses.List() {
		c.Logger.Info("Enqueuing Ingress reconciliation via tracked Service", "ingress", ingress, "service", service)
		c.Queue.Add(ingress)
	}
}

// ingressesFromService enqueues all the related ingresses for a given service when the service is changed.
func (c *Controller) ingressesFromDomainVerification(obj interface{}) {
	dv := obj.(*v1.DomainVerification)
	domain := strings.ToLower(strings.TrimSpace(dv.Spec.Domain))
	c.Logger.Info("finding ingresses based on dv", "domain", domain)

	// no actions to take on ingresses if domains is still not verified yet
	if !dv.Status.Verified {
		c.Logger.Info("dv not verified, exiting", "verified", dv.Status.Verified)
		return
	}

	// find all ingresses with pending hosts that contain this domains
	ingressList, err := c.lister.Ingresses("").List(labels.Everything())
	if err != nil {
		runtime.HandleError(err)
		return
	}

	for _, ingress := range ingressList {
		ingressNamespaceName := ingress.Namespace + "/" + ingress.Name
		c.Logger.Info("checking for pending  host", "ingress", ingressNamespaceName)

		pending, ok, err := getPendingHosts(ingress)
		if err != nil {
			c.Logger.Info("error retrieving pending hosts. Skipping", err)
			continue
		}
		if !ok || pending.Rules == nil {
			continue
		}

		c.Logger.Info("got pending hosts", "pendingHosts", pending.Rules)
		for _, pendingHost := range pending.Rules {
			if !hostMatches(pendingHost.Host, domain) {
				c.Logger.Info("not enqueueing ingress", "ingress", ingressNamespaceName)
				continue
			}

			c.Logger.Info("Enqueuing Ingress reconciliation via domains verification", "ingress", ingressNamespaceName, "domain", domain)
			ingressKey, err := cache.MetaNamespaceKeyFunc(ingress)
			if err != nil {
				c.Logger.Error(err, "error queueing ingress", "ingress", ingressNamespaceName)
				continue
			}

			c.Queue.Add(ingressKey)
		}
	}
}
