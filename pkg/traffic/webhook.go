package traffic

import (
	"context"

	"github.com/kcp-dev/logicalcluster/v2"
	"github.com/kuadrant/kcp-glbc/pkg/admission/domainverification"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type WebhookReconciler struct {
	GLBCWorkspace logicalcluster.Name
	KubeClient    kubernetes.Interface
}

var _ Reconciler = &WebhookReconciler{}

func (r *WebhookReconciler) GetName() string {
	return "Webhook Reconciler"
}

func (r *WebhookReconciler) Reconcile(ctx context.Context, accessor Interface) (ReconcileStatus, error) {
	// Only create webhooks when we're reconciling the Ingress that exposes
	// the controller
	if !r.isOwnIngress(accessor) {
		return ReconcileStatusContinue, nil
	}

	if _, ok := accessor.GetAnnotations()[ANNOTATION_HCG_HOST]; !ok {
		return ReconcileStatusContinue, nil
	}

	// Get DomainVerification ValidatingWebhookConfiguration with the
	// configuration for the webhook
	vwc := domainverification.GetValidatingWebhookConfiguration()

	// Retrieve the ValidatingWebhookConfiguration from the cluster
	client := r.KubeClient.AdmissionregistrationV1().ValidatingWebhookConfigurations()
	existing, err := client.Get(ctx, vwc.Name, v1.GetOptions{})
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return ReconcileStatusContinue, err
		}

		// If it doesn't exist, inject the clientConfig into the webhook
		// and create it
		if err := r.injectClientConfig(ctx, accessor, vwc); err != nil {
			return ReconcileStatusContinue, err
		}

		_, err := client.Create(ctx, vwc, v1.CreateOptions{})
		return ReconcileStatusContinue, err
	}

	// If it does exist, copy the expected webhook configuration into the
	// existing one and update it
	for i, webhook := range vwc.Webhooks {
		existingWebhook := existing.Webhooks[i]
		webhook.DeepCopyInto(&existingWebhook)
	}

	if err := r.injectClientConfig(ctx, accessor, existing); err != nil {
		return ReconcileStatusContinue, err
	}

	_, err = client.Update(ctx, existing, v1.UpdateOptions{})
	return ReconcileStatusContinue, err
}

// isOwnIngress checks if the given ingress is the special ingress that exposes
// the GLBC
func (r *WebhookReconciler) isOwnIngress(accessor Interface) bool {
	workspace, ok := accessor.GetAnnotations()[logicalcluster.AnnotationKey]
	if !ok {
		return false
	}

	return workspace == r.GLBCWorkspace.String()
}

func (r *WebhookReconciler) injectClientConfig(ctx context.Context, accessor Interface, vwc *admissionv1.ValidatingWebhookConfiguration) error {
	host, ok := accessor.GetAnnotations()[ANNOTATION_HCG_HOST]
	if !ok {
		return nil
	}

	webhookUrl := domainverification.MakeURL(host)
	caBundle, err := r.getCABundle(ctx, accessor)
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
func (r *WebhookReconciler) getCABundle(ctx context.Context, accessor Interface) ([]byte, error) {
	tlss, err := accessor.GetTLS(func(secretName string) (*corev1.Secret, error) {
		return r.KubeClient.
			CoreV1().Secrets(accessor.GetNamespace()).
			Get(ctx, secretName, v1.GetOptions{})
	})
	if err != nil {
		return nil, err
	}

	for _, tls := range tlss {
		if !r.matchesHost(accessor, tls) {
			continue
		}

		bundle := []byte{}

		for _, cert := range tls.Bundle {
			bundle = append(bundle, cert...)
		}

		return bundle, nil
	}

	return nil, nil
}

func (r *WebhookReconciler) matchesHost(accessor Interface, tls *TLS) bool {
	accessorHost, ok := accessor.GetAnnotations()[ANNOTATION_HCG_HOST]
	if !ok {
		return false
	}

	for _, host := range tls.Hosts {
		if host == accessorHost {
			return true
		}
	}

	return false
}
