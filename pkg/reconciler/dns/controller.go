package dns

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"

	v1 "github.com/kuadrant/kcp-ingress/pkg/apis/kuadrant/v1"
	kuadrantv1 "github.com/kuadrant/kcp-ingress/pkg/client/kuadrant/clientset/versioned"
	"github.com/kuadrant/kcp-ingress/pkg/client/kuadrant/informers/externalversions"
	kuadrantv1lister "github.com/kuadrant/kcp-ingress/pkg/client/kuadrant/listers/kuadrant/v1"
)

const resyncPeriod = 10 * time.Hour

// NewController returns a new Controller which reconciles DNSRecord.
func NewController(config *ControllerConfig) *Controller {
	client := kuadrantv1.NewForConfigOrDie(config.Cfg)
	queue := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())
	stopCh := make(chan struct{}) // TODO: hook this up to SIGTERM/SIGINT

	c := &Controller{
		queue:  queue,
		client: client,
		stopCh: stopCh,
	}

	sif := externalversions.NewSharedInformerFactoryWithOptions(c.client, resyncPeriod)

	// Watch for events related to DNSRecords
	sif.Kuadrant().V1().DNSRecords().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { c.enqueue(obj) },
		UpdateFunc: func(_, obj interface{}) { c.enqueue(obj) },
		DeleteFunc: func(obj interface{}) { c.enqueue(obj) },
	})

	sif.Start(stopCh)
	for inf, sync := range sif.WaitForCacheSync(stopCh) {
		if !sync {
			klog.Fatalf("Failed to sync %s", inf)
		}
	}

	c.indexer = sif.Kuadrant().V1().DNSRecords().Informer().GetIndexer()
	c.lister = sif.Kuadrant().V1().DNSRecords().Lister()

	return c
}

type ControllerConfig struct {
	Cfg *rest.Config
}

type Controller struct {
	queue   workqueue.RateLimitingInterface
	client  kuadrantv1.Interface
	stopCh  chan struct{}
	indexer cache.Indexer
	lister  kuadrantv1lister.DNSRecordLister
}

func (c *Controller) enqueue(obj interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		runtime.HandleError(err)
		return
	}
	c.queue.AddRateLimited(key)
}

func (c *Controller) Start(numThreads int) {
	defer c.queue.ShutDown()
	for i := 0; i < numThreads; i++ {
		go wait.Until(c.startWorker, time.Second, c.stopCh)
	}
	klog.Infof("Starting workers")
	<-c.stopCh
	klog.Infof("Stopping workers")
}

func (c *Controller) startWorker() {
	for c.processNextWorkItem() {
	}
}

func (c *Controller) processNextWorkItem() bool {
	// Wait until there is a new item in the working queue
	k, quit := c.queue.Get()
	if quit {
		return false
	}
	key := k.(string)

	// No matter what, tell the queue we're done with this key, to unblock
	// other workers.
	defer c.queue.Done(key)

	err := c.process(key)
	c.handleErr(err, key)
	return true
}

func (c *Controller) handleErr(err error, key string) {
	// Reconcile worked, nothing else to do for this workqueue item.
	if err == nil {
		c.queue.Forget(key)
		return
	}

	// Re-enqueue up to 5 times.
	num := c.queue.NumRequeues(key)
	if num < 5 {
		klog.Errorf("Error reconciling key %q, retrying... (#%d): %v", key, num, err)
		c.queue.AddRateLimited(key)
		return
	}

	// Give up and report error elsewhere.
	c.queue.Forget(key)
	runtime.HandleError(err)
	klog.Infof("Dropping key %q after failed retries: %v", key, err)
}

func (c *Controller) process(key string) error {
	obj, exists, err := c.indexer.GetByKey(key)
	if err != nil {
		return err
	}

	if !exists {
		klog.Infof("Object with key %q was deleted", key)
	}

	current := obj.(*v1.DNSRecord)

	previous := current.DeepCopy()

	ctx := context.TODO()
	if err := c.reconcile(ctx, current); err != nil {
		return err
	}

	// If the object being reconciled changed as a result, update it.
	if !equality.Semantic.DeepEqual(previous, current) {
		_, uerr := c.client.KuadrantV1().DNSRecords(current.Namespace).Update(ctx, current, metav1.UpdateOptions{})
		return uerr
	}

	return err
}
