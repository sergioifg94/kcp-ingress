package ingress

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/go-logr/logr"

	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	k8errors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	updateDNS        func(ctx context.Context, dns *v1.DNSRecord) error
	watchHost        func(ctx context.Context, key interface{}, host string) bool
	forgetHost       func(key interface{}, host string)
	listHostWatchers func(key interface{}) []net.RecordWatcher
	DNSLookup        func(ctx context.Context, host string) ([]net.HostAddress, error)
	log              logr.Logger
}

func (r *dnsReconciler) reconcile(ctx context.Context, ingress *networkingv1.Ingress) (reconcileStatus, error) {
	if ingress.DeletionTimestamp != nil && !ingress.DeletionTimestamp.IsZero() {
		// delete DNSRecord
		if err := r.deleteDNS(ctx, ingress); err != nil && !k8errors.IsNotFound(err) {
			return reconcileStatusStop, err
		}
		return reconcileStatusContinue, nil
	}

	var (
		activeLBHosts   []string
		dnsTargets      = map[string][]string{}
		deletingTargets = map[string][]string{}
	)
	key := ingressKey(ingress)

	// get status blocks. An array is returned to allow for more than one placement
	statuses, err := GetStatus(ingress)
	if err != nil {
		return reconcileStatusStop, err
	}
	fmt.Println("**dns** setting up status targets ", statuses)
	//discover targets and setup watchers
	for cluster, status := range statuses {
		// Start watching for address changes in the LBs hostnames
		statusTargets, err := r.targetsFromIngressStatus(ctx, status)
		if err != nil {
			return reconcileStatusStop, err
		}

		if metadata.HasAnnotation(ingress, workloadMigration.WorkloadDeletingAnnotation+cluster.String()) {
			for host, target := range statusTargets {
				deletingTargets[host] = append(deletingTargets[host], target.value...)
			}
			continue
		}
		for host, target := range statusTargets {
			if target.targetType == targetTypeHost {
				r.watchHost(ctx, key, host)
				activeLBHosts = append(activeLBHosts, host)
			}
			dnsTargets[host] = append(dnsTargets[host], target.value...)
		}
	}

	fmt.Println("**dns** setting up targets ", dnsTargets)

	//no non-deleting hosts have an IP yet, so continue using IPs of "losing" clusters
	if len(dnsTargets) == 0 && len(deletingTargets) > 0 {
		r.log.V(3).Info("setting the dns target to the deleting target as no new dns targets set yet")
		dnsTargets = deletingTargets
	}

	// clean up any watchers no longer needed
	hostRecordWatchers := r.listHostWatchers(key)
	for _, watcher := range hostRecordWatchers {
		if !slice.ContainsString(activeLBHosts, watcher.Host) {
			r.forgetHost(key, watcher.Host)
		}
	}
	// create / update dns

	// Attempt to retrieve the existing DNSRecord for this Ingress
	existing, err := r.getDNS(ctx, ingress)
	// If it doesn't exist, create it
	if err != nil {
		if !k8errors.IsNotFound(err) {
			return reconcileStatusStop, err
		}
		// doesn't exist so Create the DNSRecord object
		record := &v1.DNSRecord{}

		record.TypeMeta = metav1.TypeMeta{
			APIVersion: v1.SchemeGroupVersion.String(),
			Kind:       "DNSRecord",
		}
		record.ObjectMeta = metav1.ObjectMeta{
			Annotations: map[string]string{
				logicalcluster.AnnotationKey: logicalcluster.From(ingress).String(),
			},
			Name:      ingress.Name,
			Namespace: ingress.Namespace,
		}

		// Sets the Ingress as the owner reference
		record.SetOwnerReferences([]metav1.OwnerReference{
			{
				APIVersion:         networkingv1.SchemeGroupVersion.String(),
				Kind:               "Ingress",
				Name:               ingress.Name,
				UID:                ingress.UID,
				Controller:         pointer.Bool(true),
				BlockOwnerDeletion: pointer.Bool(true),
			},
		})
		if _, ok := record.Annotations[annotationIngressKey]; !ok {
			if record.Annotations == nil {
				record.Annotations = map[string]string{}
			}
			record.Annotations[annotationIngressKey] = string(key)
		}
		metadata.CopyAnnotationsPredicate(ingress, record, metadata.KeyPredicate(func(key string) bool {
			return strings.HasPrefix(key, ANNOTATION_HEALTH_CHECK_PREFIX)
		}))

		// for _, t := range dnsTargets {
		// 	r.setEndpointFromTarget()
		// }

		if err := r.setEndpointsFromIngress(ctx, ingress, record); err != nil {
			return reconcileStatusStop, err
		}
		// Create the resource in the cluster
		existing, err = r.createDNS(ctx, record)
		if err != nil {
			return reconcileStatusStop, err
		}

		// metric to observe the ingress admission time
		ingressObjectTimeToAdmission.
			Observe(existing.CreationTimestamp.Time.Sub(ingress.CreationTimestamp.Time).Seconds())
		return reconcileStatusContinue, nil

	}
	// If it does exist, update it
	copyDNS := existing.DeepCopy()
	if err := r.setEndpointsFromIngress(ctx, ingress, existing); err != nil {
		return reconcileStatusStop, err
	}

	if !equality.Semantic.DeepEqual(copyDNS, existing) {
		if err = r.updateDNS(ctx, existing); err != nil {
			return reconcileStatusStop, err
		}
	}

	return reconcileStatusContinue, nil
}

// func (r *dnsReconciler) setDnsRecordFromIngress(ctx context.Context, ingress *networkingv1.Ingress, dnsRecord *v1.DNSRecord) error {
// 	key, err := cache.MetaNamespaceKeyFunc(ingress)
// 	if err != nil {
// 		return fmt.Errorf("failed to get namespace key for ingress %s", err)
// 	}

// 	if _, ok := dnsRecord.Annotations[annotationIngressKey]; !ok {
// 		if dnsRecord.Annotations == nil {
// 			dnsRecord.Annotations = map[string]string{}
// 		}
// 		dnsRecord.Annotations[annotationIngressKey] = key
// 	}
// 	metadata.CopyAnnotationsPredicate(ingress, dnsRecord, metadata.KeyPredicate(func(key string) bool {
// 		return strings.HasPrefix(key, ANNOTATION_HEALTH_CHECK_PREFIX)
// 	}))
// 	return r.setEndpointsFromIngress(ctx, ingress, dnsRecord)
// }

func (r *dnsReconciler) setEndpointFromTargets(dnsName string, dnsTargets map[string][]string, dnsRecord *v1.DNSRecord) {
	currentEndpoints := make(map[string]*v1.Endpoint, len(dnsRecord.Spec.Endpoints))
	for _, endpoint := range dnsRecord.Spec.Endpoints {
		address, ok := endpoint.GetAddress()
		if !ok {
			continue
		}

		currentEndpoints[address] = endpoint
	}
	var newEndpoints []*v1.Endpoint

	var endpoint *v1.Endpoint
	ok := false
	for _, targets := range dnsTargets {
		for _, target := range targets {
			// If the endpoint for this target does not exist, add a new one
			if endpoint, ok = currentEndpoints[target]; !ok {
				endpoint = &v1.Endpoint{
					SetIdentifier: target,
				}
			}

			newEndpoints = append(newEndpoints, endpoint)

			// Update the endpoint fields
			endpoint.DNSName = dnsName
			endpoint.RecordType = "A"
			endpoint.Targets = []string{target}
			endpoint.RecordTTL = 60
			endpoint.SetProviderSpecific(aws.ProviderSpecificWeight, awsEndpointWeight(len(targets)))
		}
	}

	dnsRecord.Spec.Endpoints = newEndpoints
}

func (r *dnsReconciler) setEndpointsFromIngress(ctx context.Context, ingress *networkingv1.Ingress, dnsRecord *v1.DNSRecord) error {
	targets, err := r.targetsFromIngress(ctx, ingress)
	if err != nil {
		return err
	}

	hostname := ingress.Annotations[ANNOTATION_HCG_HOST]

	// Build a map[Address]Endpoint with the current endpoints to assist
	// finding endpoints that match the targets
	currentEndpoints := make(map[string]*v1.Endpoint, len(dnsRecord.Spec.Endpoints))
	for _, endpoint := range dnsRecord.Spec.Endpoints {
		address, ok := endpoint.GetAddress()
		if !ok {
			continue
		}

		currentEndpoints[address] = endpoint
	}

	var newEndpoints []*v1.Endpoint

	for _, ingressTargets := range targets {
		for _, target := range ingressTargets {
			var endpoint *v1.Endpoint
			ok := false

			// If the endpoint for this target does not exist, add a new one
			if endpoint, ok = currentEndpoints[target]; !ok {
				endpoint = &v1.Endpoint{
					SetIdentifier: target,
				}
			}

			newEndpoints = append(newEndpoints, endpoint)

			// Update the endpoint fields
			endpoint.DNSName = hostname
			endpoint.RecordType = "A"
			endpoint.Targets = []string{target}
			endpoint.RecordTTL = 60
			endpoint.SetProviderSpecific(aws.ProviderSpecificWeight, awsEndpointWeight(len(ingressTargets)))
		}
	}

	dnsRecord.Spec.Endpoints = newEndpoints
	return nil
}

// targetsFromIngress returns a map of all the IPs associated with a single ingress(cluster)
func (r *dnsReconciler) targetsFromIngress(ctx context.Context, ingress *networkingv1.Ingress) (map[string][]string, error) {
	targets := map[string][]string{}
	deletingTargets := map[string][]string{}

	ingressStatus := &networkingv1.IngressStatus{}
	//find all annotations of a workload status (indicates a synctarget for this resource)
	has, annotations := metadata.HasAnnotationsContaining(ingress, workloadMigration.WorkloadStatusAnnotation)
	if !has {
		st, err := r.targetsFromIngressStatus(ctx, &ingress.Status)
		if err != nil {
			return targets, err
		}
		for host, statusTarget := range st {
			targets[host] = append(targets[host], statusTarget.value...)
		}
	}
	for k, v := range annotations {
		//get the cluster name
		annotationParts := strings.Split(k, "/")
		if len(annotationParts) < 2 {
			r.log.Error(errors.New("invalid workloadStatus annotation format"), "skipping sync target")
			continue
		}
		clusterName := annotationParts[1]

		err := json.Unmarshal([]byte(v), ingressStatus)
		if err != nil {
			return nil, err
		}
		statusTargets, err := r.targetsFromIngressStatus(ctx, ingressStatus)
		if err != nil {
			return nil, err
		}

		if metadata.HasAnnotation(ingress, workloadMigration.WorkloadDeletingAnnotation+clusterName) {
			for host, st := range statusTargets {
				deletingTargets[host] = append(deletingTargets[host], st.value...)
			}
			continue
		}
		for host, st := range statusTargets {
			targets[host] = append(targets[host], st.value...)
		}
	}
	//no non-deleting hosts have an IP yet, so continue using IPs of "losing" clusters
	if len(targets) == 0 && len(deletingTargets) > 0 {
		return deletingTargets, nil
	}

	return targets, nil
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

func (c *Controller) updateDNS(ctx context.Context, dns *v1.DNSRecord) error {
	if _, err := c.kuadrantClient.Cluster(logicalcluster.From(dns)).KuadrantV1().DNSRecords(dns.Namespace).Update(ctx, dns, metav1.UpdateOptions{}); err != nil {
		return err
	}
	return nil
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
