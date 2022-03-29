//go:build e2e
// +build e2e

package support

import (
	"github.com/onsi/gomega"

	kuadrantcluster "github.com/kuadrant/kcp-glbc/pkg/cluster"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func GetIngress(t Test, namespace *corev1.Namespace, name string) *networkingv1.Ingress {
	return Ingress(t, namespace, name)(t)
}

func Ingress(t Test, namespace *corev1.Namespace, name string) func(g gomega.Gomega) *networkingv1.Ingress {
	return func(g gomega.Gomega) *networkingv1.Ingress {
		ingress, err := t.Client().Core().Cluster(namespace.ClusterName).NetworkingV1().Ingresses(namespace.Name).Get(t.Ctx(), name, metav1.GetOptions{})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		return ingress
	}
}

func GetIngresses(t Test, namespace *corev1.Namespace, labelSelector string) []networkingv1.Ingress {
	return Ingresses(t, namespace, labelSelector)(t)
}

func Ingresses(t Test, namespace *corev1.Namespace, labelSelector string) func(g gomega.Gomega) []networkingv1.Ingress {
	return func(g gomega.Gomega) []networkingv1.Ingress {
		ingresses, err := t.Client().Core().Cluster(namespace.ClusterName).NetworkingV1().Ingresses(namespace.Name).List(t.Ctx(), metav1.ListOptions{LabelSelector: labelSelector})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		return ingresses.Items
	}
}

func LoadBalancerIngresses(ingress *networkingv1.Ingress) []corev1.LoadBalancerIngress {
	return ingress.Status.LoadBalancer.Ingress
}

// checkes ingress hosts are the same as the generated hosts
func HostsEqualsToGeneratedHost(ingress *networkingv1.Ingress) bool {
	equals := true
	for _, rule := range ingress.Spec.Rules {
		if rule.Host != Annotations(ingress)[kuadrantcluster.ANNOTATION_HCG_HOST] {
			equals = false
		}
	}
	return equals
}
