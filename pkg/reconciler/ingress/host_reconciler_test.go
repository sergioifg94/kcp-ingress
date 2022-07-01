package ingress

import (
	"context"
	"fmt"
	"testing"

	networkingv1 "k8s.io/api/networking/v1"
)

type hostResult struct {
	Status  reconcileStatus
	Err     error
	Ingress *networkingv1.Ingress
}

func TestReconcileHost(t *testing.T) {
	ingress := func(rules []networkingv1.IngressRule, tls []networkingv1.IngressTLS) *networkingv1.Ingress {
		i := &networkingv1.Ingress{
			Spec: networkingv1.IngressSpec{
				Rules: rules,
			},
		}
		i.Spec.TLS = tls

		return i
	}

	var buildResult = func(r reconciler, i *networkingv1.Ingress) hostResult {
		status, err := r.reconcile(context.TODO(), i)
		return hostResult{
			Status:  status,
			Err:     err,
			Ingress: i,
		}
	}
	var mangedDomain = "test.com"

	var commonValidation = func(hr hostResult, expectedStatus reconcileStatus) error {
		if hr.Status != expectedStatus {
			return fmt.Errorf("unexpected status ")
		}
		if hr.Err != nil {
			return fmt.Errorf("unexpected error from reconcile : %s", hr.Err)
		}
		_, ok := hr.Ingress.Annotations[ANNOTATION_HCG_HOST]
		if !ok {
			return fmt.Errorf("expected annotation %s to be set", ANNOTATION_HCG_HOST)
		}

		return nil
	}

	cases := []struct {
		Name     string
		Ingress  func() *networkingv1.Ingress
		Validate func(hr hostResult) error
	}{
		{
			Name: "test managed host generated for empty host field",
			Ingress: func() *networkingv1.Ingress {
				return ingress([]networkingv1.IngressRule{{
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{},
					},
				}, {
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{},
					},
				}}, []networkingv1.IngressTLS{})
			},
			Validate: func(hr hostResult) error {
				return commonValidation(hr, reconcileStatusStop)
			},
		},
		{
			Name: "test custom host replaced with generated managed host",
			Ingress: func() *networkingv1.Ingress {
				i := ingress([]networkingv1.IngressRule{{
					Host: "api.example.com",
				}}, []networkingv1.IngressTLS{})
				i.Annotations = map[string]string{ANNOTATION_HCG_HOST: "123.test.com"}
				return i
			},
			Validate: func(hr hostResult) error {
				err := commonValidation(hr, reconcileStatusContinue)
				if err != nil {
					return err
				}
				if _, ok := hr.Ingress.Annotations[ANNOTATION_HCG_CUSTOM_HOST_REPLACED]; !ok {
					return fmt.Errorf("expected the custom host annotation to be present")
				}
				for _, r := range hr.Ingress.Spec.Rules {
					if r.Host != hr.Ingress.Annotations[ANNOTATION_HCG_HOST] {
						return fmt.Errorf("expected the host to be set to %s", hr.Ingress.Annotations[ANNOTATION_HCG_HOST])
					}
				}
				return nil
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			reconciler := &hostReconciler{
				managedDomain: mangedDomain,
			}

			if err := tc.Validate(buildResult(reconciler, tc.Ingress())); err != nil {
				t.Fatalf("fail: %s", err)
			}

		})
	}
}
