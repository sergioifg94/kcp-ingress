package ingress

import (
	"context"
	"encoding/json"
	"fmt"
	v1 "github.com/kuadrant/kcp-glbc/pkg/apis/kuadrant/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

func TestProcessCustomHostValidation(t *testing.T) {
	testCases := []struct {
		name                 string
		ingress              *networkingv1.Ingress
		domainVerifications  *v1.DomainVerificationList
		expectedPendingRules Pending
		expectedRules        []networkingv1.IngressRule
		expectedTLS          []networkingv1.IngressTLS
	}{
		{
			name: "Empty host",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name: "ingress",
					Annotations: map[string]string{
						ANNOTATION_HCG_HOST: "generated.host.net",
					},
				},
				Spec: networkingv1.IngressSpec{
					Rules: []networkingv1.IngressRule{
						{
							Host: "",
							IngressRuleValue: networkingv1.IngressRuleValue{
								HTTP: &networkingv1.HTTPIngressRuleValue{
									Paths: []networkingv1.HTTPIngressPath{
										{
											Path: "/",
										},
									},
								},
							},
						},
					},
				},
			},
			domainVerifications:  &v1.DomainVerificationList{},
			expectedPendingRules: Pending{},
			expectedRules: []networkingv1.IngressRule{
				{
					Host: "",
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path: "/",
								},
							},
						},
					},
				},
				{
					Host: "generated.host.net",
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path: "/",
								},
							},
						},
					},
				},
			},
		},
		{
			name: "Custom host verified",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name: "ingress",
					Annotations: map[string]string{
						ANNOTATION_HCG_HOST: "generated.host.net",
					},
				},
				Spec: networkingv1.IngressSpec{
					Rules: []networkingv1.IngressRule{
						{
							Host: "test.pb-custom.hcpapps.net",
							IngressRuleValue: networkingv1.IngressRuleValue{
								HTTP: &networkingv1.HTTPIngressRuleValue{
									Paths: []networkingv1.HTTPIngressPath{
										{
											Path: "/path",
										},
									},
								},
							},
						},
					},
				},
			},
			domainVerifications: &v1.DomainVerificationList{
				Items: []v1.DomainVerification{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "pb-custom.hcpapps.net",
						},
						Spec: v1.DomainVerificationSpec{
							Domain: "pb-custom.hcpapps.net",
						},
						Status: v1.DomainVerificationStatus{
							Verified: true,
						},
					},
				},
			},
			expectedPendingRules: Pending{},
			expectedRules: []networkingv1.IngressRule{
				{
					Host: "test.pb-custom.hcpapps.net",
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path: "/path",
								},
							},
						},
					},
				},
				{
					Host: "generated.host.net",
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path: "/path",
								},
							},
						},
					},
				},
			},
		},
		{
			name: "subdomain of verifiied custom host",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name: "ingress",
					Annotations: map[string]string{
						ANNOTATION_HCG_HOST: "generated.host.net",
					},
				},
				Spec: networkingv1.IngressSpec{
					Rules: []networkingv1.IngressRule{
						{
							Host: "sub.test.pb-custom.hcpapps.net",
							IngressRuleValue: networkingv1.IngressRuleValue{
								HTTP: &networkingv1.HTTPIngressRuleValue{
									Paths: []networkingv1.HTTPIngressPath{
										{
											Path: "/path",
										},
									},
								},
							},
						},
					},
				},
			},
			domainVerifications: &v1.DomainVerificationList{
				Items: []v1.DomainVerification{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "pb-custom.hcpapps.net",
						},
						Spec: v1.DomainVerificationSpec{
							Domain: "pb-custom.hcpapps.net",
						},
						Status: v1.DomainVerificationStatus{
							Verified: true,
						},
					},
				},
			},
			expectedPendingRules: Pending{},
			expectedRules: []networkingv1.IngressRule{
				{
					Host: "sub.test.pb-custom.hcpapps.net",
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path: "/path",
								},
							},
						},
					},
				},
				{
					Host: "generated.host.net",
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path: "/path",
								},
							},
						},
					},
				},
			},
		},
		{
			name: "Custom host unverified",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name: "ingress",
					Annotations: map[string]string{
						ANNOTATION_HCG_HOST: "generated.host.net",
					},
				},
				Spec: networkingv1.IngressSpec{
					Rules: []networkingv1.IngressRule{
						{
							Host: "test.pb-custom.hcpapps.net",
							IngressRuleValue: networkingv1.IngressRuleValue{
								HTTP: &networkingv1.HTTPIngressRuleValue{
									Paths: []networkingv1.HTTPIngressPath{
										{
											Path: "/path",
										},
									},
								},
							},
						},
					},
				},
			},
			domainVerifications: &v1.DomainVerificationList{
				Items: []v1.DomainVerification{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "pb-custom.hcpapps.net",
						},
						Spec: v1.DomainVerificationSpec{
							Domain: "pb-custom.hcpapps.net",
						},
						Status: v1.DomainVerificationStatus{
							Verified: false,
						},
					},
				},
			},
			expectedPendingRules: Pending{
				Rules: []networkingv1.IngressRule{
					{
						Host: "test.pb-custom.hcpapps.net",
						IngressRuleValue: networkingv1.IngressRuleValue{
							HTTP: &networkingv1.HTTPIngressRuleValue{
								Paths: []networkingv1.HTTPIngressPath{
									{
										Path: "/path",
									},
								},
							},
						},
					},
				},
			},
			expectedRules: []networkingv1.IngressRule{
				{
					Host: "generated.host.net",
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path: "/path",
								},
							},
						},
					},
				},
			},
		},
		{
			name: "TLS section is preserved",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name: "ingress",
					Annotations: map[string]string{
						ANNOTATION_HCG_HOST: "generated.host.net",
					},
				},
				Spec: networkingv1.IngressSpec{
					TLS: []networkingv1.IngressTLS{
						{
							Hosts: []string{
								"test.pb-custom.hcpapps.net",
							},
							SecretName: "tls-secret",
						},
					},
					Rules: []networkingv1.IngressRule{
						{
							Host: "test.pb-custom.hcpapps.net",
							IngressRuleValue: networkingv1.IngressRuleValue{
								HTTP: &networkingv1.HTTPIngressRuleValue{
									Paths: []networkingv1.HTTPIngressPath{
										{
											Path: "/path",
										},
									},
								},
							},
						},
					},
				},
			},
			domainVerifications: &v1.DomainVerificationList{
				Items: []v1.DomainVerification{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "pb-custom.hcpapps.net",
						},
						Spec: v1.DomainVerificationSpec{
							Domain: "pb-custom.hcpapps.net",
						},
						Status: v1.DomainVerificationStatus{
							Verified: false,
						},
					},
				},
			},
			expectedPendingRules: Pending{
				Rules: []networkingv1.IngressRule{
					{
						Host: "test.pb-custom.hcpapps.net",
						IngressRuleValue: networkingv1.IngressRuleValue{
							HTTP: &networkingv1.HTTPIngressRuleValue{
								Paths: []networkingv1.HTTPIngressPath{
									{
										Path: "/path",
									},
								},
							},
						},
					},
				},
			},
			expectedRules: []networkingv1.IngressRule{
				{
					Host: "generated.host.net",
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path: "/path",
								},
							},
						},
					},
				},
			},
			expectedTLS: []networkingv1.IngressTLS{
				{
					Hosts: []string{
						"test.pb-custom.hcpapps.net",
					},
					SecretName: "tls-secret",
				},
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			ingress := testCase.ingress.DeepCopy()

			if _, err := doProcessCustomHosts(
				ingress,
				testCase.domainVerifications,
			); err != nil {
				t.Fatal(err)
			}

			// Assert the expected generated rules matches the
			// annotation
			if testCase.expectedPendingRules.Rules != nil {
				annotation, ok := ingress.Annotations[PendingCustomHostsAnnotation]
				if !ok {
					t.Fatalf("expected GeneratedRulesAnnotation to be set")
				}

				pendingRules := Pending{}
				if err := json.Unmarshal(
					[]byte(annotation),
					&pendingRules,
				); err != nil {
					t.Fatalf("invalid format on PendingRules: %v", err)
				}
			}

			// Assert the reconciled rules match the expected rules
			for _, expectedRule := range testCase.expectedRules {
				foundExpectedRule := false
				for _, rule := range ingress.Spec.Rules {
					if equality.Semantic.DeepEqual(expectedRule, rule) {
						foundExpectedRule = true
						break
					}
				}
				if !foundExpectedRule {
					t.Fatalf("Expected rule not found: %+v", expectedRule)
				}
			}

			for _, expectedTLS := range testCase.expectedTLS {
				foundExpectedTLS := false
				for _, tls := range ingress.Spec.TLS {
					if equality.Semantic.DeepEqual(expectedTLS, tls) {
						foundExpectedTLS = true
						break
					}
				}

				if !foundExpectedTLS {
					t.Fatalf("Expected TLS not found: %+v", expectedTLS)
				}
			}
		})
	}
}
