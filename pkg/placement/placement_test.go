package placement_test

import (
	"fmt"
	"testing"

	kcp "github.com/kcp-dev/kcp/pkg/reconciler/workload/namespace"
	"github.com/kuadrant/kcp-glbc/pkg/placement"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newIngress(labels map[string]string) *networkingv1.Ingress {
	return &networkingv1.Ingress{
		ObjectMeta: v1.ObjectMeta{
			Labels: labels,
		},
	}
}

func newService(labels map[string]string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: v1.ObjectMeta{
			Labels: labels,
		},
	}
}

func TestPlacement_Ingress(t *testing.T) {

	cases := []struct {
		Name        string
		Ingress     func() *networkingv1.Ingress
		ExpectError bool
		Services    func() []*corev1.Service
		Validate    func(*networkingv1.Ingress) error
	}{
		{
			Name: "Test place ingress based on service placement label",
			Ingress: func() *networkingv1.Ingress {
				return newIngress(map[string]string{"some": "label"})
			},
			Services: func() []*corev1.Service {
				return []*corev1.Service{
					newService(map[string]string{kcp.ClusterLabel: "value"}),
				}
			},
			ExpectError: false,
			Validate: func(i *networkingv1.Ingress) error {
				if i == nil {
					return fmt.Errorf("no ingress. Expected an ingress")
				}
				_, ok := i.Labels[kcp.ClusterLabel]
				if !ok {
					return fmt.Errorf("expected the placement label %s but found none ", kcp.ClusterLabel)
				}
				return nil
			},
		},
		{
			Name: "Test no placement when no service placement label",
			Ingress: func() *networkingv1.Ingress {
				return newIngress(map[string]string{"some": "label"})
			},
			Services: func() []*corev1.Service {
				return []*corev1.Service{
					newService(map[string]string{"some": "value"}),
				}
			},
			ExpectError: false,
			Validate: func(i *networkingv1.Ingress) error {
				if i == nil {
					return fmt.Errorf("no ingress. Expected an ingress")
				}
				val, ok := i.Labels[kcp.ClusterLabel]
				if ok {
					return fmt.Errorf("did not expect the placement label %s but found  ", val)
				}
				return nil
			},
		},
		{
			Name: "Test no placement when no services",
			Ingress: func() *networkingv1.Ingress {
				return newIngress(map[string]string{"some": "label"})
			},
			Services: func() []*corev1.Service {
				return []*corev1.Service{}
			},
			ExpectError: true,
			Validate: func(i *networkingv1.Ingress) error {
				if i == nil {
					return fmt.Errorf("no ingress. Expected an ingress")
				}
				val, ok := i.Labels[kcp.ClusterLabel]
				if ok {
					return fmt.Errorf("did not expect the placement label %s but found  ", val)
				}
				return nil
			},
		},
		{
			Name: "Test no placement when multiple services with multiple placements",
			Ingress: func() *networkingv1.Ingress {
				return newIngress(map[string]string{"some": "label"})
			},
			Services: func() []*corev1.Service {
				return []*corev1.Service{
					newService(map[string]string{kcp.ClusterLabel: "some"}),
					newService(map[string]string{kcp.ClusterLabel: "other"}),
				}
			},
			ExpectError: true,
			Validate: func(i *networkingv1.Ingress) error {
				if i == nil {
					return fmt.Errorf("no ingress. Expected an ingress")
				}
				val, ok := i.Labels[kcp.ClusterLabel]
				if ok {
					return fmt.Errorf("did not expect the placement label %s but found  ", val)
				}
				return nil
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			p := placement.NewPlacer()
			ingress := tc.Ingress()
			err := p.PlaceRoutingObj(tc.Services(), ingress)
			if tc.ExpectError && err == nil {
				t.Fatalf("expected an error but got none")
			}
			if !tc.ExpectError && err != nil {
				t.Fatalf("did not expect an error but got none")
			}
			if err := tc.Validate(ingress); err != nil {
				t.Fatalf("ingress was invalid %v", err)
			}

		})
	}

}
