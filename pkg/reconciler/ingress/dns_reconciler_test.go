package ingress

import (
	"context"
	"encoding/json"
	"fmt"

	"testing"

	v1 "github.com/kuadrant/kcp-glbc/pkg/apis/kuadrant/v1"
	"github.com/kuadrant/kcp-glbc/pkg/log"
	"github.com/kuadrant/kcp-glbc/pkg/net"
	"github.com/kuadrant/kcp-glbc/pkg/util/slice"
	"github.com/kuadrant/kcp-glbc/pkg/util/workloadMigration"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type validatedDNSClient struct {
	createCalled int
	updateCalled int
	getCalled    int
}

func (ve *validatedDNSClient) create(validate func(dns *v1.DNSRecord) error) func(ctx context.Context, dns *v1.DNSRecord) (*v1.DNSRecord, error) {
	return func(ctx context.Context, dns *v1.DNSRecord) (*v1.DNSRecord, error) {
		ve.createCalled++
		err := validate(dns)
		return dns, err
	}

}

func (ve *validatedDNSClient) update(validate func(dns *v1.DNSRecord) error) func(ctx context.Context, dns *v1.DNSRecord) (*v1.DNSRecord, error) {
	return func(ctx context.Context, dns *v1.DNSRecord) (*v1.DNSRecord, error) {
		ve.updateCalled++
		err := validate(dns)
		return dns, err
	}

}

func (ve *validatedDNSClient) get(getfunc func(ctx context.Context, ing *networkingv1.Ingress) (*v1.DNSRecord, error)) func(ctx context.Context, ing *networkingv1.Ingress) (*v1.DNSRecord, error) {
	ve.getCalled++
	return getfunc
}

func TestDNSReconciler(t *testing.T) {

	// sets up 2 ingresses one with status in the advanced scheduling annotation and one in the regular status block
	setupIngresses := func(managedHost string, ingressStatus networkingv1.IngressStatus) []*networkingv1.Ingress {
		var ingresses = []*networkingv1.Ingress{}
		for i := 0; i <= 1; i++ {
			ing := networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("ingress-%d", i),
					Annotations: map[string]string{
						ANNOTATION_HCG_HOST: managedHost,
						"kcp.dev/cluster":   "somecluster",
					},
				},
			}
			rule := networkingv1.IngressRule{
				Host: managedHost,
				IngressRuleValue: networkingv1.IngressRuleValue{
					HTTP: &networkingv1.HTTPIngressRuleValue{},
				},
			}
			ing.Spec.Rules = append(ing.Spec.Rules, rule)
			if i == 0 {
				// add to annotation
				status, _ := json.Marshal(ingressStatus)
				ing.Annotations[workloadMigration.WorkloadStatusAnnotation+"/somecluster"] = string(status)
			} else {
				ing.Status = ingressStatus
			}
			ingresses = append(ingresses, &ing)
		}
		return ingresses
	}

	fakewatcher := func(k interface{}) []net.RecordWatcher {
		return []net.RecordWatcher{}
	}

	commonDNSValidate := func(expectedIPs []string) func(dns *v1.DNSRecord) error {
		return func(dns *v1.DNSRecord) error {
			if dns == nil {
				return fmt.Errorf("did not expect a nil dns record")
			}
			if len(dns.Spec.Endpoints) != 1 {
				return fmt.Errorf("expected only 1 dns endpoint but got %d", len(dns.Spec.Endpoints))
			}
			for _, ep := range dns.Spec.Endpoints {
				if len(ep.Targets) != len(expectedIPs) {
					return fmt.Errorf("expected only 1 dns target but got %d", len(ep.Targets))
				}
				if ep.RecordType != "A" {
					return fmt.Errorf("expected an A record but got %s", ep.RecordType)
				}
				for _, ip := range expectedIPs {
					if !slice.ContainsString(ep.Targets, ip) {
						return fmt.Errorf("ip %s not in targets ", ip)
					}
				}
			}
			return nil
		}
	}

	cases := []struct {
		Name           string
		getDNS         func(ctx context.Context, ingress *networkingv1.Ingress) (*v1.DNSRecord, error)
		validateResult func(status reconcileStatus, dnsClient *validatedDNSClient, err error) error
		ingresses      []*networkingv1.Ingress
		expectedIPs    []string
	}{{
		Name: "test DNSRecord is created with correct values when it doesn't exist",
		getDNS: func(ctx context.Context, ingress *networkingv1.Ingress) (*v1.DNSRecord, error) {
			return nil, errors.NewNotFound(v1.Resource("dnsrecord"), ingress.Name)
		},
		ingresses: setupIngresses("test.cb.example.com", networkingv1.IngressStatus{
			LoadBalancer: corev1.LoadBalancerStatus{
				Ingress: []corev1.LoadBalancerIngress{{
					IP: "192.168.33.2",
				},
				},
			},
		}),
		expectedIPs: []string{"192.168.33.2"},
		validateResult: func(status reconcileStatus, dnsClient *validatedDNSClient, err error) error {
			if status != reconcileStatusContinue || err != nil {
				return fmt.Errorf("expected reconcile status to be %v got %v. Expected err to be nil got %v", reconcileStatusContinue, status, err)
			}
			if dnsClient.createCalled != 1 {
				return fmt.Errorf("expected create dns to be called 1 time but was called %d", dnsClient.createCalled)
			}
			if dnsClient.updateCalled != 0 {
				return fmt.Errorf("expected update dns to be called 0 times but was called %d", dnsClient.updateCalled)
			}
			return nil
		},
	}, {
		Name: "test DNSRecord is created with correct values when it does exist",
		getDNS: func(ctx context.Context, ingress *networkingv1.Ingress) (*v1.DNSRecord, error) {
			return &v1.DNSRecord{
				Spec: v1.DNSRecordSpec{
					Endpoints: []*v1.Endpoint{
						{DNSName: "192.168.33.2", Targets: []string{"192.168.33.2"}},
					},
				},
			}, nil
		},
		validateResult: func(status reconcileStatus, dnsClient *validatedDNSClient, err error) error {
			if status != reconcileStatusContinue || err != nil {
				return fmt.Errorf("expected reconcile status to be %v got %v. Expected err to be nil got %v", reconcileStatusContinue, status, err)
			}
			if dnsClient.createCalled != 0 {
				return fmt.Errorf("expected create dns to be called 1 time but was called %d", dnsClient.createCalled)
			}
			if dnsClient.updateCalled != 1 {
				return fmt.Errorf("expected update dns to be called 0 times but was called %d", dnsClient.updateCalled)
			}
			return nil
		},
		ingresses: setupIngresses("test.cb.example.com", networkingv1.IngressStatus{
			LoadBalancer: corev1.LoadBalancerStatus{
				Ingress: []corev1.LoadBalancerIngress{{
					IP: "192.168.33.3",
				},
				},
			},
		}),
		expectedIPs: []string{"192.168.33.3"},
	}}

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			for _, ing := range tc.ingresses {
				fake := &validatedDNSClient{}
				rec := &dnsReconciler{
					getDNS:           fake.get(tc.getDNS),
					createDNS:        fake.create(commonDNSValidate(tc.expectedIPs)),
					updateDNS:        fake.update(commonDNSValidate(tc.expectedIPs)),
					listHostWatchers: fakewatcher,
					log:              log.New(),
				}
				result, err := rec.reconcile(context.TODO(), ing)

				if err := tc.validateResult(result, fake, err); err != nil {
					t.Fatalf("unexpected error %s", err)
				}
			}
		})
	}

}
