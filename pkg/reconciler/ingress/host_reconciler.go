package ingress

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kcp-dev/logicalcluster/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/go-logr/logr"
	"github.com/rs/xid"
	networkingv1 "k8s.io/api/networking/v1"

	v1 "github.com/kuadrant/kcp-glbc/pkg/apis/kuadrant/v1"
	"github.com/kuadrant/kcp-glbc/pkg/util/metadata"
)

type hostReconciler struct {
	managedDomain          string
	log                    logr.Logger
	customHostsEnabled     bool
	GetDomainVerifications func(ctx context.Context, ingress *networkingv1.Ingress) (*v1.DomainVerificationList, error)
}

const (
	ANNOTATION_PENDING_CUSTOM_HOSTS = "pendingCustomHosts"
	LABEL_HAS_PENDING_CUSTOM_HOSTS  = "hasPendingCustomHosts"
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
	dvs, err := r.GetDomainVerifications(ctx, ingress)
	if err != nil {
		return reconcileStatusContinue, err
	}
	return doProcessCustomHosts(ingress, dvs)
}

func doProcessCustomHosts(ingress *networkingv1.Ingress, dvs *v1.DomainVerificationList) (reconcileStatus, error) {
	generatedHost, ok := ingress.Annotations[ANNOTATION_HCG_HOST]
	if !ok || generatedHost == "" {
		return reconcileStatusStop, fmt.Errorf("generated host is empty for ingress: '%v/%v'", ingress.Namespace, ingress.Name)
	}

	var unverifiedRules []networkingv1.IngressRule

	var verifiedRules []networkingv1.IngressRule
	//find any rules in the spec that are for unverifiedHosts that are not verified
	for _, rule := range ingress.Spec.Rules {
		//ignore any rules for generated unverifiedHosts (these are recalculated later)
		if rule.Host == generatedHost {
			continue
		}

		verified, err := IsDomainVerified(rule.Host, dvs.Items)
		if err != nil {
			return reconcileStatusContinue, err
		}
		//check against domainverification status
		if verified || rule.Host == "" {
			verifiedRules = append(verifiedRules, rule)
		} else {
			//remove rule from ingress and mark it as awaiting verification
			unverifiedRules = append(unverifiedRules, rule)
		}

		//recalculate the generatedhost rule in the spec
		generatedHostRule := *rule.DeepCopy()
		generatedHostRule.Host = generatedHost
		verifiedRules = append(verifiedRules, generatedHostRule)
	}
	ingress.Spec.Rules = verifiedRules

	//test all the rules in the pending rules annotation to see if they are verified now
	pendingRulesRaw := ingress.Annotations[ANNOTATION_PENDING_CUSTOM_HOSTS]
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

		verified, err := IsDomainVerified(pendingRule.Host, dvs.Items)
		if err != nil {
			return reconcileStatusContinue, err
		}

		//check against domainverification status
		if verified || pendingRule.Host == "" {
			//add the rule to the spec
			ingress.Spec.Rules = append(ingress.Spec.Rules, pendingRule)
		} else {
			preservedPendingRules = append(preservedPendingRules, pendingRule)
		}
	}

	//put the new unverified rules in the list of pending rules and update the annotation
	pending.Rules = append(preservedPendingRules, unverifiedRules...)
	if len(pending.Rules) > 0 {
		metadata.AddLabel(ingress, LABEL_HAS_PENDING_CUSTOM_HOSTS, "true")
		newAnnotation, err := json.Marshal(pending)
		if err != nil {
			return reconcileStatusContinue, err
		}
		ingress.Annotations[ANNOTATION_PENDING_CUSTOM_HOSTS] = string(newAnnotation)
		return reconcileStatusContinue, nil
	}
	metadata.RemoveLabel(ingress, LABEL_HAS_PENDING_CUSTOM_HOSTS)
	metadata.RemoveAnnotation(ingress, ANNOTATION_PENDING_CUSTOM_HOSTS)
	return reconcileStatusContinue, nil
}

// IsDomainVerified will take the host and recursively remove subdomains searching for a matching domainverification
// that is verified. Until either a match is found, or the subdomains run out.
func IsDomainVerified(host string, dvs []v1.DomainVerification) (bool, error) {
	if len(dvs) == 0 {
		return false, nil
	}
	for _, dv := range dvs {
		if dv.Spec.Domain == host && dv.Status.Verified {
			return true, nil
		}
	}

	parentHostParts := strings.SplitN(host, ".", 2)
	//we've run out of sub-domains
	if len(parentHostParts) < 2 {
		return false, nil
	}

	//recurse up the subdomains
	return IsDomainVerified(parentHostParts[1], dvs)
}

func hostMatches(host, domain string) bool {
	if host == domain {
		return true
	}

	parentHostParts := strings.SplitN(host, ".", 2)

	if len(parentHostParts) < 2 {
		return false
	}

	return hostMatches(parentHostParts[1], domain)
}

func (c *Controller) getDomainVerifications(ctx context.Context, ingress *networkingv1.Ingress) (*v1.DomainVerificationList, error) {
	return c.kuadrantClient.Cluster(logicalcluster.From(ingress)).KuadrantV1().DomainVerifications().List(ctx, metav1.ListOptions{})
}
