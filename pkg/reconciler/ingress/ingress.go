package ingress

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rs/xid"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/api/networking/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"k8s.io/utils/pointer"

	"github.com/kcp-dev/apimachinery/pkg/logicalcluster"
	kcp "github.com/kcp-dev/kcp/pkg/reconciler/workload/namespace"

	v1 "github.com/kuadrant/kcp-glbc/pkg/apis/kuadrant/v1"
	"github.com/kuadrant/kcp-glbc/pkg/cluster"
	"github.com/kuadrant/kcp-glbc/pkg/dns/aws"
	svc "github.com/kuadrant/kcp-glbc/pkg/reconciler/service"
	"github.com/kuadrant/kcp-glbc/pkg/util/deleteDelay"
	"github.com/kuadrant/kcp-glbc/pkg/util/metadata"
	"github.com/kuadrant/kcp-glbc/pkg/util/slice"
)

const (
	ownedByLabel = "kcp.dev/owned-by"

	manager                 = "kcp-ingress"
	cascadeCleanupFinalizer = "kcp.dev/cascade-cleanup"
)

func (c *Controller) reconcileRoot(ctx context.Context, ingress *networkingv1.Ingress) error {
	klog.Infof("reconciling root ingress: %v", ingress.Name)
	// is deleting
	if ingress.DeletionTimestamp != nil && !ingress.DeletionTimestamp.IsZero() {
		klog.Infof("deleting root ingress '%v'", ingress.Name)
		sel, err := labels.Parse(fmt.Sprintf("%s=%s", ownedByLabel, ingress.Name))
		if err != nil {
			return err
		}
		currentLeaves, err := c.lister.Ingresses(ingress.Namespace).List(sel)
		if err != nil {
			return err
		}
		klog.Infof("found %v leaf ingresses", len(currentLeaves))
		for _, leaf := range currentLeaves {
			deleteDelay.CleanForDeletion(leaf)
			leaf, err = c.kubeClient.Cluster(logicalcluster.From(leaf)).NetworkingV1().Ingresses(leaf.Namespace).Update(ctx, leaf, metav1.UpdateOptions{})
			if err != nil {
				return err
			}

			if err := c.kubeClient.Cluster(logicalcluster.From(leaf)).NetworkingV1().Ingresses(leaf.Namespace).Delete(ctx, leaf.Name, metav1.DeleteOptions{}); err != nil {
				return err
			}
			// delete copied leaf secret
			host := ingress.Annotations[cluster.ANNOTATION_HCG_HOST]
			leafSecretName := getTLSSecretName(host, leaf)
			if leafSecretName != "" {
				if err := c.kubeClient.Cluster(logicalcluster.From(leaf)).CoreV1().Secrets(leaf.Namespace).Delete(ctx, leafSecretName, metav1.DeleteOptions{}); err != nil {
					return err
				}
			}
		}
		// delete any DNS records
		if err := c.ensureDNS(ctx, ingress); err != nil {
			return err
		}
		// delete any certificates
		if err := c.ensureCertificate(ctx, ingress); err != nil {
			return err
		}
		// all leaves removed, remove finalizer
		klog.Infof("'%v' ingress leaves cleaned up - removing finalizer", ingress.Name)
		metadata.RemoveFinalizer(ingress, cascadeCleanupFinalizer)

		c.hostsWatcher.StopWatching(ingressKey(ingress))
		for _, leaf := range currentLeaves {
			c.hostsWatcher.StopWatching(ingressKey(leaf))
		}

		return nil
	}

	metadata.AddFinalizer(ingress, cascadeCleanupFinalizer)

	if ingress.Annotations == nil || ingress.Annotations[cluster.ANNOTATION_HCG_HOST] == "" {
		// Let's assign it a global hostname if any
		generatedHost := fmt.Sprintf("%s.%s", xid.New(), *c.domain)
		patch := fmt.Sprintf(`{"metadata":{"annotations":{%q:%q}}}`, cluster.ANNOTATION_HCG_HOST, generatedHost)
		i, err := c.patchIngress(ctx, ingress, []byte(patch))
		if err != nil {
			return err
		}
		ingress = i
	}

	// if custom hosts are not enabled all the hosts in the ingress
	// will be replaced to the generated host
	if !*c.customHostsEnabled {
		err := c.replaceCustomHosts(ctx, ingress)
		if err != nil {
			return err
		}
	}

	// Get the current leaves
	sel, err := labels.Parse(fmt.Sprintf("%s=%s", ownedByLabel, ingress.Name))
	if err != nil {
		return err
	}
	currentLeaves, err := c.lister.Ingresses(ingress.Namespace).List(sel)
	if err != nil {
		return err
	}

	if err := c.ensurePlacement(ctx, ingress); err != nil {
		return err
	}

	// setup certificates
	if err := c.ensureCertificate(ctx, ingress); err != nil {
		return err
	}

	// Generate the desired leaves
	desiredLeaves, err := c.desiredLeaves(ctx, ingress)
	if err != nil {
		return err
	}
	klog.Infof("desired leaves count: %v", len(desiredLeaves))

	// Delete the leaves that are not desired anymore
	for _, leftover := range findUndesiredLeaves(currentLeaves, desiredLeaves) {
		if err := c.delayDeleteLeaf(ctx, leftover); err != nil {
			return err
		}
	}

	// TODO(jmprusi): ugly. fix. use indexer, etc.
	// Create and/or update the desired leaves
	for _, leaf := range desiredLeaves {
		// before we create if tls is enabled we need to wait for the tls secret to be present
		if c.tlsEnabled {
			// copy root tls secret
			klog.Info("TLS is enabled copy tls secret for leaf ingress ", leaf.Name)
			if err := c.copyRootTLSSecretForLeafs(ctx, ingress, leaf); err != nil {
				return err
			}
		}
		if _, err := c.kubeClient.Cluster(logicalcluster.From(leaf)).NetworkingV1().Ingresses(leaf.Namespace).Create(ctx, leaf, metav1.CreateOptions{}); err != nil {
			if errors.IsAlreadyExists(err) {
				existingLeaf, err := c.kubeClient.Cluster(logicalcluster.From(leaf)).NetworkingV1().Ingresses(leaf.Namespace).Get(ctx, leaf.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}

				// Set the resourceVersion and UID to update the desired leaf.
				leaf.ResourceVersion = existingLeaf.ResourceVersion
				leaf.UID = existingLeaf.UID

				if _, err := c.kubeClient.Cluster(logicalcluster.From(leaf)).NetworkingV1().Ingresses(leaf.Namespace).Update(ctx, leaf, metav1.UpdateOptions{}); err != nil {
					return err
				}
			} else {
				return err
			}
		}
	}

	// update DNS
	if err := c.ensureDNS(ctx, ingress); err != nil {
		return err
	}

	return nil
}

// ensureCertificate creates a certificate request for the root ingress into the control cluster
func (c *Controller) ensureCertificate(ctx context.Context, rootIngress *networkingv1.Ingress) error {
	if !c.tlsEnabled {
		klog.Info("tls support not enabled. not creating certificates")
		return nil
	}
	controlClusterContext, err := cluster.NewControlObjectMapper(rootIngress)
	if err != nil {
		return err
	}
	if rootIngress.DeletionTimestamp != nil && !rootIngress.DeletionTimestamp.IsZero() {
		if err := c.certProvider.Delete(ctx, controlClusterContext); err != nil {
			return err
		}
		return nil
	}
	err = c.certProvider.Create(ctx, controlClusterContext)
	if err != nil && !errors.IsAlreadyExists(err) {
		return err
	}

	klog.Info("Patching Ingress With TLS ", rootIngress.Name)
	patch := fmt.Sprintf(`{"spec":{"tls":[{"hosts":[%q],"secretName":%q}]}}`, controlClusterContext.Host(), controlClusterContext.Name())
	if _, err := c.patchIngress(ctx, rootIngress, []byte(patch)); err != nil {
		klog.Info("failed to patch ingress *** ", err)
		return err
	}

	return nil
}

func getTLSSecretName(host string, ingress *networkingv1.Ingress) string {
	for _, tls := range ingress.Spec.TLS {
		for _, tlsHost := range tls.Hosts {
			if tlsHost == host {
				return tls.SecretName
			}
		}
	}
	return ""
}

func (c *Controller) copyRootTLSSecretForLeafs(ctx context.Context, root *networkingv1.Ingress, leaf *networkingv1.Ingress) error {
	host := root.Annotations[cluster.ANNOTATION_HCG_HOST]
	if host == "" {
		return fmt.Errorf("no host set yet cannot set up TLS")
	}
	var rootSecretName = getTLSSecretName(host, root)
	var leafSecretName = getTLSSecretName(host, leaf)

	if leafSecretName == "" || rootSecretName == "" {
		return fmt.Errorf("cannot copy secrets yet as secrets names not present")
	}
	secretClient := c.kubeClient.Cluster(logicalcluster.From(root)).CoreV1().Secrets(root.Namespace)
	// get the root secret
	rootSecret, err := secretClient.Get(ctx, rootSecretName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	leafSecret := rootSecret.DeepCopy()
	leafSecret.Name = leafSecretName
	leafSecret.Labels = map[string]string{}
	leafSecret.Labels[kcp.ClusterLabel] = leaf.Labels[kcp.ClusterLabel]
	leafSecret.Labels[ownedByLabel] = root.Name

	// Cleanup finalizers
	leafSecret.Finalizers = []string{}
	// Cleanup owner references
	leafSecret.OwnerReferences = []metav1.OwnerReference{}
	leafSecret.SetResourceVersion("")

	_, err = secretClient.Create(ctx, leafSecret, metav1.CreateOptions{})
	if err != nil && errors.IsAlreadyExists(err) {
		ls, err := secretClient.Get(ctx, leafSecretName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		ls.Data = leafSecret.Data
		if _, err := secretClient.Update(ctx, ls, metav1.UpdateOptions{}); err != nil {
			return err
		}
		return nil
	}
	if err != nil {
		return err
	}
	return nil
}

func (c *Controller) ensureDNS(ctx context.Context, ingress *networkingv1.Ingress) error {

	if ingress.DeletionTimestamp != nil && !ingress.DeletionTimestamp.IsZero() {
		// delete DNSRecord
		err := c.dnsRecordClient.Cluster(logicalcluster.From(ingress)).KuadrantV1().DNSRecords(ingress.Namespace).Delete(ctx, ingress.Name, metav1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			return err
		}
		return nil
	}

	if len(ingress.Status.LoadBalancer.Ingress) > 0 {
		var activeHosts []string
		// Start watching for address changes in the LBs hostnames
		for _, lbs := range ingress.Status.LoadBalancer.Ingress {
			if lbs.Hostname != "" {
				c.hostsWatcher.StartWatching(ctx, ingressKey(ingress), lbs.Hostname)
				activeHosts = append(activeHosts, lbs.Hostname)
			}
		}

		hostRecordWatchers := c.hostsWatcher.ListHostRecordWatchers(ingressKey(ingress))
		for _, watcher := range hostRecordWatchers {
			if !slice.ContainsString(activeHosts, watcher.Host) {
				c.hostsWatcher.StopWatching(ingressKey(ingress))
			}
		}

		// Attempt to retrieve the existing DNSRecord for this Ingress
		existing, err := c.dnsRecordClient.Cluster(logicalcluster.From(ingress)).KuadrantV1().DNSRecords(ingress.Namespace).Get(ctx, ingress.Name, metav1.GetOptions{})
		// If it doesn't exist, create it
		if err != nil && errors.IsNotFound(err) {
			// Create the DNSRecord object
			record := &v1.DNSRecord{}
			if err := c.setDnsRecordFromIngress(ctx, ingress, record); err != nil {
				return err
			}
			// Create the resource in the cluster
			_, err = c.dnsRecordClient.Cluster(logicalcluster.From(record)).KuadrantV1().DNSRecords(record.Namespace).Create(ctx, record, metav1.CreateOptions{})
			if err != nil {
				return err
			}
		} else if err == nil {
			// If it does exist, update it
			if err := c.setDnsRecordFromIngress(ctx, ingress, existing); err != nil {
				return err
			}

			data, err := json.Marshal(existing)
			if err != nil {
				return err
			}
			_, err = c.dnsRecordClient.Cluster(logicalcluster.From(existing)).KuadrantV1().DNSRecords(existing.Namespace).Patch(ctx, existing.Name, types.ApplyPatchType, data, metav1.PatchOptions{FieldManager: manager, Force: pointer.Bool(true)})
			if err != nil {
				return err
			}
		} else {
			return err
		}
	}

	return nil
}

func (c *Controller) delayDeleteLeaf(ctx context.Context, leaf *networkingv1.Ingress) error {
	klog.Infof("marking non desired leaf for delayed delete: %q", leaf.Name)
	obj, err := deleteDelay.SetDefaultDeleteAt(leaf, c.queue)
	if err != nil {
		return err
	}
	leaf = obj.(*networkingv1.Ingress)
	if _, err := c.kubeClient.Cluster(logicalcluster.From(leaf)).NetworkingV1().Ingresses(leaf.Namespace).Update(ctx, leaf, metav1.UpdateOptions{}); err != nil {
		return err
	}
	// mark for deletion
	if err = c.kubeClient.Cluster(logicalcluster.From(leaf)).NetworkingV1().Ingresses(leaf.Namespace).Delete(ctx, leaf.Name, metav1.DeleteOptions{}); err != nil {
		return err
	}
	return nil
}

func (c *Controller) setDnsRecordFromIngress(ctx context.Context, ingress *networkingv1.Ingress, dnsRecord *v1.DNSRecord) error {
	dnsRecord.TypeMeta = metav1.TypeMeta{
		APIVersion: v1.SchemeGroupVersion.String(),
		Kind:       "DNSRecord",
	}
	dnsRecord.ObjectMeta = metav1.ObjectMeta{
		Name:        ingress.Name,
		Namespace:   ingress.Namespace,
		ClusterName: ingress.ClusterName,
	}

	metadata.CopyAnnotationsPredicate(ingress, dnsRecord, metadata.KeyPredicate(func(key string) bool {
		return strings.HasPrefix(key, cluster.ANNOTATION_HEALTH_CHECK_PREFIX)
	}))

	// Sets the Ingress as the owner reference
	dnsRecord.SetOwnerReferences([]metav1.OwnerReference{
		{
			APIVersion:         networkingv1.SchemeGroupVersion.String(),
			Kind:               "Ingress",
			Name:               ingress.Name,
			UID:                ingress.UID,
			Controller:         pointer.Bool(true),
			BlockOwnerDeletion: pointer.Bool(true),
		},
	})

	return c.setEndpointsFromIngress(ctx, ingress, dnsRecord)
}

func (c *Controller) setEndpointsFromIngress(ctx context.Context, ingress *networkingv1.Ingress, dnsRecord *v1.DNSRecord) error {
	targets, err := c.targetsFromIngressStatus(ctx, ingress.Status)
	if err != nil {
		return err
	}

	hostname := ingress.Annotations[cluster.ANNOTATION_HCG_HOST]

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

	newEndpoints := make([]*v1.Endpoint, len(targets))

	for i, target := range targets {
		var endpoint *v1.Endpoint
		ok := false

		// If the endpoint for this target does not exist, add a new one
		if endpoint, ok = currentEndpoints[target]; !ok {
			endpoint = &v1.Endpoint{
				SetIdentifier: target,
			}
		}

		newEndpoints[i] = endpoint

		// Update the endpoint fields
		endpoint.DNSName = hostname
		endpoint.RecordType = "A"
		endpoint.Targets = []string{target}
		endpoint.RecordTTL = 60
		endpoint.SetProviderSpecific(aws.ProviderSpecificWeight, "100")
	}

	dnsRecord.Spec.Endpoints = newEndpoints
	return nil
}

func (c *Controller) targetsFromIngressStatus(ctx context.Context, status networkingv1.IngressStatus) ([]string, error) {
	var targets []string

	for _, lb := range status.LoadBalancer.Ingress {
		if lb.IP != "" {
			targets = append(targets, lb.IP)
		}
		if lb.Hostname != "" {
			ips, err := c.hostResolver.LookupIPAddr(ctx, lb.Hostname)
			if err != nil {
				return nil, err
			}
			for _, ip := range ips {
				targets = append(targets, ip.IP.String())
			}
		}
	}
	return targets, nil
}

func (c *Controller) reconcileLeaf(ctx context.Context, rootName string, ingress *networkingv1.Ingress) error {
	// The leaf Ingress was updated, get others and aggregate status.
	sel, err := labels.Parse(fmt.Sprintf("%s=%s", ownedByLabel, rootName))
	if err != nil {
		return err
	}
	others, err := c.lister.Ingresses(ingress.Namespace).List(sel)
	if err != nil {
		return err
	}

	// Get the rootIngress based on the labels.
	var rootIngress *networkingv1.Ingress

	rootIf, exists, err := c.indexer.Get(&v1beta1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			ClusterName: ingress.ClusterName,
			Namespace:   ingress.Namespace,
			Name:        rootName,
		},
	})
	if err != nil {
		return err
	}

	if ingress.DeletionTimestamp != nil && !ingress.DeletionTimestamp.IsZero() {
		klog.Infof("processing delete for: %v", ingress.Name)
		if deleteDelay.CanDelete(ingress) {
			ingress := deleteDelay.CleanForDeletion(ingress).(*networkingv1.Ingress)
			_, err = c.kubeClient.Cluster(logicalcluster.From(ingress)).NetworkingV1().Ingresses(ingress.Namespace).Update(ctx, ingress, metav1.UpdateOptions{})
			return err
		}
		// not ready to delete yet, requeue
		err = deleteDelay.Requeue(ingress, c.queue)
		if err != nil {
			return err
		}
		return nil
	}

	if !exists {
		klog.Infof("deleting orphaned leaf ingress '%v' of missing root ingress '%v'", ingress.Name, rootName)
		return c.kubeClient.Cluster(logicalcluster.From(ingress)).NetworkingV1().Ingresses(ingress.Namespace).Delete(ctx, ingress.Name, metav1.DeleteOptions{})
	}

	// Clean the current status, and then recreate if from the other leafs.
	rootIngress = rootIf.(*networkingv1.Ingress).DeepCopy()
	rootIngress.Status.LoadBalancer.Ingress = []corev1.LoadBalancerIngress{}
	for _, o := range others {
		klog.Infof("updating root ingress %v from leaf: %v, with deletionTimestamp: %v", rootIngress.Name, o.Name, o.DeletionTimestamp)
		// don't include leaves that are deleting
		if metadata.HasFinalizer(o, deleteDelay.DeleteAtFinalizer) {
			klog.Infof("%v is deleting, skipping...", o.Name)
			continue
		}

		klog.Infof("adding %v to root ingress", o.Name)
		// Should the root Ingress status be updated only once the DNS record is successfully created / updated?
		rootIngress.Status.LoadBalancer.Ingress = append(rootIngress.Status.LoadBalancer.Ingress, o.Status.LoadBalancer.Ingress...)
	}

	// Update the root Ingress status with our desired LB.
	if _, err := c.kubeClient.Cluster(logicalcluster.From(rootIngress)).NetworkingV1().Ingresses(rootIngress.Namespace).UpdateStatus(ctx, rootIngress, metav1.UpdateOptions{}); err != nil {
		if errors.IsConflict(err) {
			key, err := cache.MetaNamespaceKeyFunc(ingress)
			if err != nil {
				return err
			}
			c.queue.AddRateLimited(key)
			return nil
		}
		return err
	}

	return nil
}

func (c *Controller) desiredLeaves(ctx context.Context, root *networkingv1.Ingress) ([]*networkingv1.Ingress, error) {
	// This will parse the ingresses and extract all the destination services,
	// then create a new ingress leaf for each of them.
	services, err := c.getServices(ctx, root)
	if err != nil {
		return nil, err
	}

	klog.Infof("found services count: %v", len(services))

	var locations []string
	var serviceNames []string
	for _, service := range services {
		serviceLocations, empty := svc.GetLocations(service)
		if empty {
			continue
		}
		serviceNames = append(serviceNames, service.Name)
		locations = append(locations, serviceLocations...)

		// Trigger reconciliation of the root ingress when this service changes.
		c.tracker.add(root, service)
	}

	desiredLeaves := make([]*networkingv1.Ingress, 0, len(locations))
	for _, cl := range locations {
		vd := root.DeepCopy()
		// TODO: munge cluster name
		vd.Name = fmt.Sprintf("%s--%s", root.Name, cl)

		vd.Labels = map[string]string{}
		vd.Labels[kcp.ClusterLabel] = cl
		vd.Labels[ownedByLabel] = root.Name

		// Cleanup finalizers
		vd.Finalizers = []string{}
		// Cleanup owner references
		vd.OwnerReferences = []metav1.OwnerReference{}
		vd.SetResourceVersion("")

		// update ingress paths that point to shadowed services, to point to the shadow for it's cluster
		for i, rule := range vd.Spec.Rules {
			for j, path := range rule.HTTP.Paths {
				if slice.ContainsString(serviceNames, path.Backend.Service.Name) {
					vd.Spec.Rules[i].HTTP.Paths[j].Backend.Service.Name = fmt.Sprintf("%v--%v", path.Backend.Service.Name, cl)
				}
			}
		}

		if hostname, ok := root.Annotations[cluster.ANNOTATION_HCG_HOST]; ok {
			// Duplicate the existing rules for the global hostname
			if c.tlsEnabled {
				klog.Info("tls is enabled updating leaf ingress with secret name")
				for tlsIndex, tls := range root.Spec.TLS {
					// find the RH host
					for _, th := range tls.Hosts {
						if hostname == th {
							// set the tls section on the leaf at the right index. The secret will be created when the leaf is created
							vd.Spec.TLS[tlsIndex].SecretName = fmt.Sprintf("%s-tls", vd.Name)
						}
					}

				}
			}
			globalRules := make([]networkingv1.IngressRule, len(vd.Spec.Rules))
			for i, rule := range vd.Spec.Rules {
				r := rule.DeepCopy()
				r.Host = hostname
				globalRules[i] = *r
			}
			vd.Spec.Rules = append(vd.Spec.Rules, globalRules...)
		}

		desiredLeaves = append(desiredLeaves, vd)
	}
	klog.Infof("desired leaves generated %v ", len(desiredLeaves))
	return desiredLeaves, nil
}

// getServices will parse the ingress object and return a list of the services.
func (c *Controller) getServices(ctx context.Context, ingress *networkingv1.Ingress) ([]*corev1.Service, error) {
	var services []*corev1.Service
	for _, rule := range ingress.Spec.Rules {
		for _, path := range rule.HTTP.Paths {
			klog.Infof("getting service: %v", path.Backend.Service.Name)
			service, err := c.kubeClient.Cluster(logicalcluster.From(ingress)).CoreV1().Services(ingress.Namespace).Get(ctx, path.Backend.Service.Name, metav1.GetOptions{})
			if err == nil {
				c.tracker.add(ingress, service)
				services = append(services, service)
			} else if !errors.IsNotFound(err) {
				return nil, err
			} else {
				// ignore service not found errors
				continue
			}
		}
	}
	return services, nil
}

func (c *Controller) ensurePlacement(ctx context.Context, ingress *networkingv1.Ingress) error {
	svcs, err := c.getServices(ctx, ingress)
	if err != nil {
		return err
	}
	if err := c.ingressPlacer.PlaceRoutingObj(svcs, ingress); err != nil {
		return err
	}
	if _, err := c.kubeClient.Cluster(logicalcluster.From(ingress)).NetworkingV1().Ingresses(ingress.Namespace).Update(ctx, ingress, metav1.UpdateOptions{}); err != nil {
		return err
	}
	return nil

}

func (c *Controller) patchIngress(ctx context.Context, ingress *networkingv1.Ingress, data []byte) (*networkingv1.Ingress, error) {
	return c.kubeClient.Cluster(logicalcluster.From(ingress)).NetworkingV1().Ingresses(ingress.Namespace).
		Patch(ctx, ingress.Name, types.MergePatchType, data, metav1.PatchOptions{FieldManager: manager})
}

func findUndesiredLeaves(current, desired []*networkingv1.Ingress) []*networkingv1.Ingress {
	var missing []*networkingv1.Ingress

	for _, c := range current {
		found := false
		for _, d := range desired {
			if c.Name == d.Name {
				found = true
			}
		}
		if !found {
			missing = append(missing, c)
		}
	}

	return missing
}

func getRootName(ingress *networkingv1.Ingress) (rootName string, isLeaf bool) {
	if ingress.Labels != nil {
		rootName, isLeaf = ingress.Labels[ownedByLabel]
	}

	return
}

func ingressKey(ingress *networkingv1.Ingress) interface{} {
	key, _ := cache.MetaNamespaceKeyFunc(ingress)
	return cache.ExplicitKey(key)
}

func (c *Controller) replaceCustomHosts(ctx context.Context, ingress *networkingv1.Ingress) error {
	generatedHost := ingress.Annotations[cluster.ANNOTATION_HCG_HOST]
	hosts := []string{}
	for i, rule := range ingress.Spec.Rules {
		if rule.Host != generatedHost {
			ingress.Spec.Rules[i].Host = generatedHost
			hosts = append(hosts, rule.Host)
		}
	}

	// clean up replaced hosts from the tls list
	removeHostsFromTLS(hosts, ingress)

	if len(hosts) > 0 {
		ingress.Annotations[cluster.ANNOTATION_HCG_CUSTOM_HOST_REPLACED] = fmt.Sprintf(" replaced custom hosts %v to the glbc host due to custom host policy not being allowed",
			hosts)
		if _, err := c.kubeClient.Cluster(logicalcluster.From(ingress)).NetworkingV1().Ingresses(ingress.Namespace).Update(ctx, ingress, metav1.UpdateOptions{}); err != nil {
			return err
		}
	}

	return nil
}

func removeHostsFromTLS(hostsToRemove []string, ingress *networkingv1.Ingress) {
	for _, host := range hostsToRemove {
		for i, tls := range ingress.Spec.TLS {
			hosts := tls.Hosts
			for j, ingressHost := range tls.Hosts {
				if ingressHost == host {
					hosts = append(hosts[:j], hosts[j+1:]...)
				}
			}
			// if there are no hosts remaining remove the entry for TLS
			if len(hosts) == 0 {
				ingress.Spec.TLS[i] = networkingv1.IngressTLS{}
			} else {
				ingress.Spec.TLS[i].Hosts = hosts
			}
		}
	}
}
