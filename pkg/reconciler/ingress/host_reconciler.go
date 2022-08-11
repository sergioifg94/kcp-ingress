package ingress

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/kcp-dev/logicalcluster/v2"
	v1 "github.com/kuadrant/kcp-glbc/pkg/apis/kuadrant/v1"
	kuadrantclientv1 "github.com/kuadrant/kcp-glbc/pkg/client/kuadrant/clientset/versioned"
	"github.com/kuadrant/kcp-glbc/pkg/util/metadata"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"strings"

	"github.com/go-logr/logr"
	"github.com/rs/xid"
	networkingv1 "k8s.io/api/networking/v1"
)

type hostReconciler struct {
	managedDomain      string
	log                logr.Logger
	customHostsEnabled bool
	kuadrantClient     kuadrantclientv1.ClusterInterface
}

const (
	PendingCustomHostsAnnotation = "pendingCustomHosts"
	PendingCustomHostsSeparator  = ";"
)

type Pending struct {
	Rules []networkingv1.IngressRule `json:"rules"`
}

func (r *hostReconciler) reconcile(ctx context.Context, ingress *networkingv1.Ingress) (reconcileStatus, error) {
	if ingress.Annotations == nil || ingress.Annotations[ANNOTATION_HCG_HOST] == "" {
		// Let's assign it a global hostname if any
		generatedHost := fmt.Sprintf("%s.%s", xid.New(), r.managedDomain)
		if ingress.Annotations == nil {
			ingress.Annotations = map[string]string{}
		}
		ingress.Annotations[ANNOTATION_HCG_HOST] = generatedHost
		//we need this host set and saved on the ingress before we go any further so force an update
		// if this is not saved we end up with a new host and the certificate can have the wrong host
		return reconcileStatusStop, nil
	}
	if r.customHostsEnabled {
		return r.processCustomHosts(ctx, ingress)
	}
	return r.replaceCustomHosts(ingress)
}

func (r *hostReconciler) replaceCustomHosts(ingress *networkingv1.Ingress) (reconcileStatus, error) {
	//once the annotation is definintely saved continue on
	managedHost := ingress.Annotations[ANNOTATION_HCG_HOST]
	var customHosts []string
	for i, rule := range ingress.Spec.Rules {
		if rule.Host != managedHost {
			ingress.Spec.Rules[i].Host = managedHost
			customHosts = append(customHosts, rule.Host)
		}
	}
	// clean up replaced hosts from the tls list
	removeHostsFromTLS(customHosts, ingress)

	if len(customHosts) > 0 {
		ingress.Annotations[ANNOTATION_HCG_CUSTOM_HOST_REPLACED] = fmt.Sprintf(" replaced custom hosts %v to the glbc host due to custom host policy not being allowed",
			customHosts)
	}

	return reconcileStatusContinue, nil
}
func (r *hostReconciler) processCustomHosts(ctx context.Context, ingress *networkingv1.Ingress) (reconcileStatus, error) {
	if ingress.Annotations == nil {
		ingress.Annotations = map[string]string{}
	}

	dvs, err := r.kuadrantClient.Cluster(logicalcluster.From(ingress)).KuadrantV1().DomainVerifications().List(ctx, metav1.ListOptions{})
	if err != nil {
		return reconcileStatusContinue, err
	}
	return doProcessCustomHosts(ingress, dvs)
}

func doProcessCustomHosts(ingress *networkingv1.Ingress, dvs *v1.DomainVerificationList) (reconcileStatus, error) {
	generatedHost, ok := ingress.Annotations[ANNOTATION_HCG_HOST]
	if !ok || generatedHost == "" {
		return reconcileStatusContinue, fmt.Errorf("generated host is empty for ingress: '%v/%v'", ingress.Namespace, ingress.Name)
	}

	var unverifiedRules []networkingv1.IngressRule
	var hosts []string

	var preservedRules []networkingv1.IngressRule
	//find any rules in the spec that are for hosts that are not verified
	for _, rule := range ingress.Spec.Rules {
		//ignore any rules for generated hosts (these are recalculated later)
		if rule.Host == generatedHost {
			continue
		}

		dv, err := findDomainVerification(ingress, rule.Host, dvs.Items)
		if err != nil {
			return reconcileStatusContinue, err
		}
		//check against domainverification status
		if dv != nil && dv.Status.Verified || rule.Host == "" {
			preservedRules = append(preservedRules, rule)
		} else {
			//remove rule from ingress and mark it as awaiting verification
			unverifiedRules = append(unverifiedRules, rule)
			hosts = append(hosts, rule.Host)
		}

		//recalculate the generatedhost rule in the spec
		generatedHostRule := *rule.DeepCopy()
		generatedHostRule.Host = generatedHost
		preservedRules = append(preservedRules, generatedHostRule)
	}
	ingress.Spec.Rules = preservedRules

	//test all the rules in the pending rules annotation to see if they are verified now
	pendingRulesRaw := ingress.Annotations[PendingCustomHostsAnnotation]
	pending := &Pending{}
	if pendingRulesRaw != "" {
		err := json.Unmarshal([]byte(pendingRulesRaw), pending)
		if err != nil {
			return reconcileStatusContinue, err
		}
	}
	var preservedPendingRules []networkingv1.IngressRule
	for _, pendingRule := range pending.Rules {
		//recalculate the generatedhost rule in the spec
		generatedHostRule := *pendingRule.DeepCopy()
		generatedHostRule.Host = generatedHost
		ingress.Spec.Rules = append(ingress.Spec.Rules, generatedHostRule)

		dv, err := findDomainVerification(ingress, pendingRule.Host, dvs.Items)
		if err != nil {
			return reconcileStatusContinue, err
		}

		//check against domainverification status
		if dv != nil && dv.Status.Verified || pendingRule.Host == "" {
			//add the rule to the spec
			ingress.Spec.Rules = append(ingress.Spec.Rules, pendingRule)
		} else {
			preservedPendingRules = append(preservedPendingRules, pendingRule)
		}
	}

	// clean up replaced hosts from the tls list
	removeHostsFromTLS(hosts, ingress)

	//put the new unverified rules in the list of pending rules and update the annotation
	pending.Rules = append(preservedPendingRules, unverifiedRules...)
	if len(pending.Rules) > 0 {
		newAnnotation, err := json.Marshal(pending)
		if err != nil {
			return reconcileStatusContinue, err
		}
		ingress.Annotations[PendingCustomHostsAnnotation] = string(newAnnotation)
	} else {
		metadata.RemoveAnnotation(ingress, PendingCustomHostsAnnotation)
	}
	return reconcileStatusContinue, nil
}

func findDomainVerification(ingress *networkingv1.Ingress, host string, dvs []v1.DomainVerification) (*v1.DomainVerification, error) {
	parentHostParts := strings.SplitN(host, ".", 2)
	//we've run out of sub-domains
	if len(parentHostParts) < 2 {
		return nil, nil
	}

	parentHost := parentHostParts[1]

	for _, dv := range dvs {
		if dv.Spec.Domain == parentHost {
			return &dv, nil
		}
	}

	//recurse up the subdomains
	return findDomainVerification(ingress, parentHost, dvs)
}

func hostMatches(host, domain string) bool {
	parentHostParts := strings.SplitN(host, ".", 2)

	if len(parentHostParts) < 2 {
		return false
	}

	if parentHostParts[1] == domain {
		return true
	}

	return hostMatches(parentHostParts[1], domain)
}
