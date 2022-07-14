package workloadMigration

import (
	"github.com/kuadrant/kcp-glbc/pkg/util/metadata"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"strconv"
	"strings"
	"time"
)

const (
	WorkloadTargetLabel      = "state.internal.workload.kcp.dev"
	SyncerFinalizer          = "workload.kcp.dev/syncer-"
	WorkloadClusterFinalizer = "finalizers.workload.kcp.dev"
	WorkloadStatusAnnotation = "experimental.status.workload.kcp.dev/"
	SoftFinalizer            = "kuadrant.dev/glbc-migration"
	DeleteAtAnnotation       = "kuadrant.dev/glbc-delete-at"
	TTL                      = 60
)

func Process(obj metav1.Object, queue workqueue.RateLimitingInterface) {
	ensureSoftFinalizers(obj)
	gracefulRemoveSoftFinalizers(obj, queue)
}

//ensureSoftFinalizers ensure all active workload clusters have a soft finalizer set
func ensureSoftFinalizers(obj metav1.Object) {
	for label := range obj.GetLabels() {
		if strings.Contains(label, WorkloadTargetLabel) {
			labelParts := strings.Split(label, "/")
			softFinalizer := WorkloadClusterFinalizer + "/" + labelParts[1]
			metadata.AddAnnotation(obj, softFinalizer, SoftFinalizer)
			//if delayed delete is active on this object, remove it
			metadata.RemoveAnnotation(obj, DeleteAtAnnotation+"-"+labelParts[1])
		}
	}
}

//gracefulRemoveSoftFinalizers any soft finalizers with no active workload cluster should trigger a delayed delete
func gracefulRemoveSoftFinalizers(obj metav1.Object, queue workqueue.RateLimitingInterface) {
	at := time.Now()
	at = at.Add((TTL * time.Second) * 2)
	for annotation := range obj.GetAnnotations() {
		if strings.Contains(annotation, WorkloadClusterFinalizer) {
			finalizerParts := strings.Split(annotation, "/")
			//no label for this finalizer, set up graceful delete
			if _, ok := obj.GetLabels()[WorkloadTargetLabel+"/"+finalizerParts[1]]; !ok {
				clusterDeleteAtAnnotation := DeleteAtAnnotation + "-" + finalizerParts[1]
				//delete delay annotation not yet set, set it
				if v, ok := obj.GetAnnotations()[clusterDeleteAtAnnotation]; !ok {
					metadata.AddAnnotation(obj, clusterDeleteAtAnnotation, strconv.FormatInt(at.Unix(), 10))
					// requeue object
					key, err := cache.MetaNamespaceKeyFunc(obj)
					if err != nil {
						return
					}
					queue.AddAfter(key, TTL*2*time.Second)
				} else {
					deleteAt, err := strconv.Atoi(v)
					if err != nil {
						//badly formed deleteAt annotation, remove it, so it will be regenerated
						metadata.RemoveAnnotation(obj, clusterDeleteAtAnnotation)
					}

					if int64(deleteAt) <= time.Now().Unix() {
						metadata.RemoveAnnotation(obj, WorkloadClusterFinalizer+"/"+finalizerParts[1])
						metadata.RemoveAnnotation(obj, clusterDeleteAtAnnotation)
					} else {
						//requeue object
						queueFor := int64(deleteAt) - time.Now().Unix()
						key, err := cache.MetaNamespaceKeyFunc(obj)
						if err != nil {
							return
						}
						queue.AddAfter(key, time.Duration(queueFor)*time.Second)
					}
				}
			}
		}
	}
}
