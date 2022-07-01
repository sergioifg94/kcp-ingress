package ingress

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/rs/xid"
	networkingv1 "k8s.io/api/networking/v1"
)

type hostReconciler struct {
	managedDomain string
	log           logr.Logger
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
