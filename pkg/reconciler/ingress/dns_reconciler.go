package ingress

import (
	"context"
	"strconv"
	"strings"

	"github.com/go-logr/logr"

	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	k8errors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/pointer"

	"github.com/kcp-dev/logicalcluster/v2"

	v1 "github.com/kuadrant/kcp-glbc/pkg/apis/kuadrant/v1"
	"github.com/kuadrant/kcp-glbc/pkg/dns/aws"
	"github.com/kuadrant/kcp-glbc/pkg/net"
	"github.com/kuadrant/kcp-glbc/pkg/util/metadata"
	"github.com/kuadrant/kcp-glbc/pkg/util/slice"
	"github.com/kuadrant/kcp-glbc/pkg/util/workloadMigration"
)

const (
	targetTypeHost = "HOST"
	targetTypeIP   = "IP"
)

type dnsReconciler struct {
	deleteDNS        func(ctx context.Context, ingress *networkingv1.Ingress) error
	getDNS           func(ctx context.Context, ingress *networkingv1.Ingress) (*v1.DNSRecord, error)
	createDNS        func(ctx context.Context, dns *v1.DNSRecord) (*v1.DNSRecord, error)
	updateDNS        func(ctx context.Context, dns *v1.DNSRecord) (*v1.DNSRecord, error)
	watchHost        func(ctx context.Context, key interface{}, host string) bool
	forgetHost       func(key interface{}, host string)
	listHostWatchers func(key interface{}) []net.RecordWatcher
	DNSLookup        func(ctx context.Context, host string) ([]net.HostAddress, error)
	log              logr.Logger
}

func (r *dnsReconciler) reconcile(ctx context.Context, ingress *networkingv1.Ingress) (reconcileStatus, error) {
	if ingress.DeletionTimestamp != nil && !ingress.DeletionTimestamp.IsZero() {
		if err := r.deleteDNS(ctx, ingress); err != nil && !k8errors.IsNotFound(err) {
			return reconcileStatusStop, err
		}
		return reconcileStatusContinue, nil
	}

	key := objectKey(ingress)
	managedHost := ingress.Annotations[ANNOTATION_HCG_HOST]
	activeLBHosts := []string{}
	activeDNSTargetIPs := map[string][]string{}
	deletingTargetIPs := map[string][]string{}

	// get status blocks. An array is returned to allow for more than one placement
	statuses, err := GetStatus(ingress)
	if err != nil {
		return reconcileStatusContinue, err
	}
	//discover active and deleting targets and setup watchers for non ip load balancer results
	for cluster, status := range statuses {
		statusTargets, err := r.targetsFromIngressStatus(ctx, status)
		if err != nil {
			return reconcileStatusContinue, err
		}
		deleteAnnotation := workloadMigration.WorkloadDeletingAnnotation + cluster.String()
		if metadata.HasAnnotation(ingress, deleteAnnotation) {
			for host, target := range statusTargets {
				deletingTargetIPs[host] = append(deletingTargetIPs[host], target.value...)
			}
			continue
		}
		for host, target := range statusTargets {
			if metadata.HasAnnotation(ingress, deleteAnnotation) {
				continue
			}
			if target.targetType == targetTypeHost {
				r.watchHost(ctx, key, host)
				activeLBHosts = append(activeLBHosts, host)
			}
			activeDNSTargetIPs[host] = append(activeDNSTargetIPs[host], target.value...)
		}
	}
	//no non-deleting hosts have an IP yet, so continue using IPs of "losing" clusters
	if len(activeDNSTargetIPs) == 0 && len(deletingTargetIPs) > 0 {
		r.log.V(3).Info("setting the dns target to the deleting target as no new dns targets set yet")
		activeDNSTargetIPs = deletingTargetIPs
	}
	// clean up any watchers no longer needed
	hostRecordWatchers := r.listHostWatchers(key)
	for _, watcher := range hostRecordWatchers {
		if !slice.ContainsString(activeLBHosts, watcher.Host) {
			r.forgetHost(key, watcher.Host)
		}
	}
	// Attempt to retrieve the existing DNSRecord for this Ingress
	existing, err := r.getDNS(ctx, ingress)
	// If it doesn't exist, create it
	if err != nil {
		if !k8errors.IsNotFound(err) {
			return reconcileStatusStop, err
		}
		record, err := newDNSRecordForObject(ingress)
		if err != nil {
			return reconcileStatusContinue, err
		}
		r.setEndpointFromTargets(managedHost, activeDNSTargetIPs, record)
		// Create the resource in the cluster
		if len(record.Spec.Endpoints) > 0 {
			r.log.V(3).Info("creating DNSRecord ", "record", record.Name, "endpoints for DNSRecord", "endpoints", record.Spec.Endpoints)
			existing, err = r.createDNS(ctx, record)
			if err != nil {
				return reconcileStatusContinue, err
			}
			// metric to observe the ingress admission time
			ingressObjectTimeToAdmission.
				Observe(existing.CreationTimestamp.Time.Sub(ingress.CreationTimestamp.Time).Seconds())
		}
		return reconcileStatusContinue, nil
	}
	// If it does exist, update it
	copyDNS := existing.DeepCopy()
	r.setEndpointFromTargets(managedHost, activeDNSTargetIPs, copyDNS)
	if !equality.Semantic.DeepEqual(copyDNS, existing) {
		r.log.V(3).Info("updating DNSRecord ", "record", copyDNS.Name, "endpoints for DNSRecord", "endpoints", copyDNS.Spec.Endpoints)
		if _, err = r.updateDNS(ctx, copyDNS); err != nil {
			return reconcileStatusStop, err
		}
	}

	return reconcileStatusContinue, nil
}

func newDNSRecordForObject(obj runtime.Object) (*v1.DNSRecord, error) {
	objMeta, err := meta.Accessor(obj)
	if err != nil {
		return nil, err
	}
	record := &v1.DNSRecord{}

	record.TypeMeta = metav1.TypeMeta{
		APIVersion: v1.SchemeGroupVersion.String(),
		Kind:       "DNSRecord",
	}
	objGroupVersion := schema.GroupVersion{Group: obj.GetObjectKind().GroupVersionKind().Group, Version: obj.GetObjectKind().GroupVersionKind().Version}
	// Sets the Ingress as the owner reference
	record.SetOwnerReferences([]metav1.OwnerReference{
		{
			APIVersion:         objGroupVersion.String(),
			Kind:               obj.GetObjectKind().GroupVersionKind().Kind,
			Name:               objMeta.GetName(),
			UID:                objMeta.GetUID(),
			Controller:         pointer.Bool(true),
			BlockOwnerDeletion: pointer.Bool(true),
		},
	})
	record.ObjectMeta = metav1.ObjectMeta{
		Annotations: map[string]string{
			logicalcluster.AnnotationKey: logicalcluster.From(objMeta).String(),
		},
		Name:      objMeta.GetName(),
		Namespace: objMeta.GetNamespace(),
	}
	if _, ok := record.Annotations[annotationIngressKey]; !ok {
		if record.Annotations == nil {
			record.Annotations = map[string]string{}
		}
		record.Annotations[annotationIngressKey] = string(objectKey(obj))
	}

	metadata.CopyAnnotationsPredicate(objMeta, record, metadata.KeyPredicate(func(key string) bool {
		return strings.HasPrefix(key, ANNOTATION_HEALTH_CHECK_PREFIX)
	}))
	return record, nil

}

func (r *dnsReconciler) setEndpointFromTargets(dnsName string, dnsTargets map[string][]string, dnsRecord *v1.DNSRecord) {
	currentEndpoints := make(map[string]*v1.Endpoint, len(dnsRecord.Spec.Endpoints))
	for _, endpoint := range dnsRecord.Spec.Endpoints {
		address, ok := endpoint.GetAddress()
		if !ok {
			continue
		}
		currentEndpoints[address] = endpoint
	}
	var (
		newEndpoints []*v1.Endpoint
		endpoint     *v1.Endpoint
	)
	ok := false
	for _, targets := range dnsTargets {
		for _, target := range targets {
			// If the endpoint for this target does not exist, add a new one
			if endpoint, ok = currentEndpoints[target]; !ok {
				endpoint = &v1.Endpoint{
					SetIdentifier: target,
				}
			}
			// Update the endpoint fields
			endpoint.DNSName = dnsName
			endpoint.RecordType = "A"
			endpoint.Targets = []string{target}
			endpoint.RecordTTL = 60
			endpoint.SetProviderSpecific(aws.ProviderSpecificWeight, awsEndpointWeight(len(targets)))
			newEndpoints = append(newEndpoints, endpoint)
		}
	}

	dnsRecord.Spec.Endpoints = newEndpoints
}

type target struct {
	targetType string
	value      []string
}

func (r *dnsReconciler) targetsFromIngressStatus(ctx context.Context, ingressStatus *networkingv1.IngressStatus) (map[string]target, error) {
	targets := map[string]target{}
	for _, lb := range ingressStatus.LoadBalancer.Ingress {
		if lb.IP != "" {
			targets[lb.IP] = target{value: []string{lb.IP}, targetType: targetTypeIP}
		}
		if lb.Hostname != "" {
			ips, err := r.DNSLookup(ctx, lb.Hostname)
			if err != nil {
				return nil, err
			}
			targets[lb.Hostname] = target{value: []string{}, targetType: targetTypeHost}
			for _, ip := range ips {
				t := targets[lb.Hostname]
				t.value = append(targets[lb.Hostname].value, ip.IP.String())
				targets[lb.Hostname] = t
			}
		}
	}
	return targets, nil
}

// awsEndpointWeight returns the weight value for a single AWS record in a set of records where the traffic is split
// evenly between a number of clusters/ingresses, each splitting traffic evenly to a number of IPs (numIPs)
//
// Divides the number of IPs by a known weight allowance for a cluster/ingress, note that this means:
// * Will always return 1 after a certain number of ips is reached, 60 in the current case (maxWeight / 2)
// * Will return values that don't add up to the total maxWeight when the number of ingresses is not divisible by numIPs
//
// The aws weight value must be an integer between 0 and 255.
// https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/resource-record-sets-values-weighted.html#rrsets-values-weighted-weight
func awsEndpointWeight(numIPs int) string {
	maxWeight := 120
	if numIPs > maxWeight {
		numIPs = maxWeight
	}
	return strconv.Itoa(maxWeight / numIPs)
}

func (c *Controller) updateDNS(ctx context.Context, dns *v1.DNSRecord) (*v1.DNSRecord, error) {
	updated, err := c.kuadrantClient.Cluster(logicalcluster.From(dns)).KuadrantV1().DNSRecords(dns.Namespace).Update(ctx, dns, metav1.UpdateOptions{})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

func (c *Controller) deleteDNS(ctx context.Context, ingress *networkingv1.Ingress) error {
	return c.kuadrantClient.Cluster(logicalcluster.From(ingress)).KuadrantV1().DNSRecords(ingress.Namespace).Delete(ctx, ingress.Name, metav1.DeleteOptions{})
}

func (c *Controller) getDNS(ctx context.Context, ingress *networkingv1.Ingress) (*v1.DNSRecord, error) {
	return c.kuadrantClient.Cluster(logicalcluster.From(ingress)).KuadrantV1().DNSRecords(ingress.Namespace).Get(ctx, ingress.Name, metav1.GetOptions{})
}

func (c *Controller) createDNS(ctx context.Context, dnsRecord *v1.DNSRecord) (*v1.DNSRecord, error) {
	return c.kuadrantClient.Cluster(logicalcluster.From(dnsRecord)).KuadrantV1().DNSRecords(dnsRecord.Namespace).Create(ctx, dnsRecord, metav1.CreateOptions{})
}
