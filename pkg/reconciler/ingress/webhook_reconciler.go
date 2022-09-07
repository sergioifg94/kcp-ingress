package ingress

import (
	"context"

	"github.com/kcp-dev/logicalcluster/v2"
	"github.com/kuadrant/kcp-glbc/pkg/admission/domainverification"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type webhookReconciler struct {
	// managedDomain string
	glbcWorkspace string
	kubeClient    kubernetes.Interface
}

func (r *webhookReconciler) reconcile(ctx context.Context, ingress *networkingv1.Ingress) (reconcileStatus, error) {
	// Only create webhooks when we're reconciling the Ingress that exposes
	// the controller
	if !r.isOwnIngress(ingress) {
		return reconcileStatusContinue, nil
	}

	if _, ok := ingress.Annotations[ANNOTATION_HCG_HOST]; !ok {
		return reconcileStatusContinue, nil
	}

	// Get DomainVerification ValidatingWebhookConfiguration with the
	// configuration for the webhook
	vwc := domainverification.GetValidatingWebhookConfiguration()

	// Retrieve the ValidatingWebhookConfiguration from the cluster
	client := r.kubeClient.AdmissionregistrationV1().ValidatingWebhookConfigurations()
	existing, err := client.Get(ctx, vwc.Name, v1.GetOptions{})
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return reconcileStatusContinue, err
		}

		// If it doesn't exist, inject the clientConfig into the webhook
		// and create it
		r.injectClientConfig(ctx, ingress, vwc)
		_, err := client.Create(ctx, vwc, v1.CreateOptions{})
		return reconcileStatusContinue, err
	}

	// If it does exist, copy the expected webhook configuration into the
	// existing one and update it
	for i, webhook := range vwc.Webhooks {
		existingWebhook := existing.Webhooks[i]
		webhook.DeepCopyInto(&existingWebhook)
	}

	r.injectClientConfig(ctx, ingress, existing)
	_, err = client.Update(ctx, existing, v1.UpdateOptions{})
	return reconcileStatusContinue, err
}

// isOwnIngress checks if the given ingress is the special ingress that exposes
// the GLBC
func (r *webhookReconciler) isOwnIngress(ingress *networkingv1.Ingress) bool {
	workspace, ok := ingress.Annotations[logicalcluster.AnnotationKey]
	if !ok {
		return false
	}

	return workspace == r.glbcWorkspace
}

func (r *webhookReconciler) injectClientConfig(ctx context.Context, ingress *networkingv1.Ingress, vwc *admissionv1.ValidatingWebhookConfiguration) error {
	webhookUrl := domainverification.MakeURL(ingress.Annotations[ANNOTATION_HCG_HOST])
	caBundle, err := r.getCABundle(ctx, ingress)
	if err != nil {
		return err
	}

	for i := range vwc.Webhooks {
		webhook := &vwc.Webhooks[i]
		webhook.ClientConfig.URL = &webhookUrl

		if caBundle != nil {
			webhook.ClientConfig.CABundle = caBundle
		}
	}

	return nil
}

// getCABundle retrieves the CA bundle for ingress by obtaining the certificates
// from the secret referenced in the .spec.TLS field
//
// If no TLS secret is referrenced, returns nil
func (r *webhookReconciler) getCABundle(ctx context.Context, ingress *networkingv1.Ingress) ([]byte, error) {
	for _, tls := range ingress.Spec.TLS {
		if !r.matchesHost(ingress, &tls) {
			continue
		}

		secret, err := r.kubeClient.
			CoreV1().Secrets(ingress.Namespace).
			Get(ctx, tls.SecretName, v1.GetOptions{})

		if err != nil {
			return nil, err
		}

		bundle := []byte{}

		for _, cert := range secret.Data {
			bundle = append(bundle, cert...)
		}

		return bundle, nil
	}

	return nil, nil
}

func (r *webhookReconciler) matchesHost(ingress *networkingv1.Ingress, tls *networkingv1.IngressTLS) bool {
	for _, host := range tls.Hosts {
		if ingress.Annotations[ANNOTATION_HCG_HOST] == host {
			return true
		}
	}

	return false
}
