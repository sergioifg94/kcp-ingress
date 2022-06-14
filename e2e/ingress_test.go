//go:build e2e
// +build e2e

package e2e

import (
	"testing"

	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apisv1alpha1 "github.com/kcp-dev/kcp/pkg/apis/apis/v1alpha1"
	conditionsapi "github.com/kcp-dev/kcp/pkg/apis/third_party/conditions/apis/conditions/v1alpha1"
	"github.com/kcp-dev/logicalcluster"

	. "github.com/kuadrant/kcp-glbc/e2e/support"
	kuadrantv1 "github.com/kuadrant/kcp-glbc/pkg/apis/kuadrant/v1"
	kuadrantcluster "github.com/kuadrant/kcp-glbc/pkg/cluster"
)

func TestIngress(t *testing.T) {
	test := With(t)
	// Tests cannot be run in parallel as of kcp 0.5.0
	// test.T().Parallel()

	// Create the test workspace
	workspace := test.NewTestWorkspace()

	// Import GLBC APIs
	binding := test.NewAPIBinding("glbc", WithExportReference(GLBCWorkspace, "glbc"), InWorkspace(workspace))

	// Wait until the APIBinding is actually in bound phase
	test.Eventually(APIBinding(test, binding.ClusterName, binding.Name)).
		Should(WithTransform(APIBindingPhase, Equal(apisv1alpha1.APIBindingPhaseBound)))

	// And check the APIs are imported into the test workspace
	test.Expect(HasImportedAPIs(test, workspace, kuadrantv1.SchemeGroupVersion.WithKind("DNSRecord"))(test)).
		Should(BeTrue())

	// Register workload cluster 1 into the compute-service workspace
	cluster1 := test.NewWorkloadCluster("kcp-cluster-1", InWorkspace(ComputeWorkspace), WithKubeConfigByName, Syncer().ResourcesToSync(GLBCResources...))

	// Wait until cluster 1 is ready
	test.Eventually(WorkloadCluster(test, cluster1.ClusterName, cluster1.Name)).WithTimeout(TestTimeoutShort).Should(WithTransform(
		ConditionStatus(conditionsapi.ReadyCondition),
		Equal(corev1.ConditionTrue),
	))

	// Bind compute workspace APIs
	binding = test.NewAPIBinding("kubernetes", WithComputeServiceExport(ComputeWorkspace), InWorkspace(workspace))

	// Wait until the APIBinding is actually in bound phase
	test.Eventually(APIBinding(test, binding.ClusterName, binding.Name)).
		Should(WithTransform(APIBindingPhase, Equal(apisv1alpha1.APIBindingPhaseBound)))

	// Wait until the APIs are imported into the test workspace
	test.Eventually(HasImportedAPIs(test, workspace,
		corev1.SchemeGroupVersion.WithKind("Service"),
		appsv1.SchemeGroupVersion.WithKind("Deployment"),
		networkingv1.SchemeGroupVersion.WithKind("Ingress"),
	)).Should(BeTrue())

	// Create a namespace
	namespace := test.NewTestNamespace(InWorkspace(workspace))

	name := "echo"

	// Create the Deployment
	_, err := test.Client().Core().Cluster(logicalcluster.From(namespace)).AppsV1().Deployments(namespace.Name).
		Apply(test.Ctx(), DeploymentConfiguration(namespace.Name, name), ApplyOptions)
	test.Expect(err).NotTo(HaveOccurred())

	// Create the Service
	_, err = test.Client().Core().Cluster(logicalcluster.From(namespace)).CoreV1().Services(namespace.Name).
		Apply(test.Ctx(), ServiceConfiguration(namespace.Name, name, map[string]string{}), ApplyOptions)
	test.Expect(err).NotTo(HaveOccurred())

	// Create the Ingress
	_, err = test.Client().Core().Cluster(logicalcluster.From(namespace)).NetworkingV1().Ingresses(namespace.Name).
		Apply(test.Ctx(), IngressConfiguration(namespace.Name, name), ApplyOptions)
	test.Expect(err).NotTo(HaveOccurred())

	// Wait until the Ingress is reconciled with the load balancer Ingresses
	test.Eventually(Ingress(test, namespace, name)).WithTimeout(TestTimeoutMedium).Should(And(
		WithTransform(Annotations, And(
			HaveKey(kuadrantcluster.ANNOTATION_HCG_HOST),
			HaveKey(kuadrantcluster.ANNOTATION_HCG_CUSTOM_HOST_REPLACED)),
		),
		WithTransform(LoadBalancerIngresses, HaveLen(1)),
		Satisfy(HostsEqualsToGeneratedHost),
	))

	// Retrieve the Ingress
	ingress := GetIngress(test, namespace, name)

	// Check a DNSRecord for the Ingress is created with the expected Spec
	test.Eventually(DNSRecord(test, namespace, name)).Should(And(
		WithTransform(DNSRecordEndpoints, HaveLen(1)),
		WithTransform(DNSRecordEndpoints, ContainElement(MatchFieldsP(IgnoreExtras,
			Fields{
				"DNSName":          Equal(ingress.Annotations[kuadrantcluster.ANNOTATION_HCG_HOST]),
				"Targets":          ConsistOf(ingress.Status.LoadBalancer.Ingress[0].IP),
				"RecordType":       Equal("A"),
				"RecordTTL":        Equal(kuadrantv1.TTL(60)),
				"SetIdentifier":    Equal(ingress.Status.LoadBalancer.Ingress[0].IP),
				"ProviderSpecific": ConsistOf(kuadrantv1.ProviderSpecific{{Name: "aws/weight", Value: "120"}}),
			})),
		),
	))

	// Finally, delete the resources
	test.Expect(test.Client().Core().Cluster(logicalcluster.From(namespace)).NetworkingV1().Ingresses(namespace.Name).
		Delete(test.Ctx(), name, metav1.DeleteOptions{})).
		To(Succeed())
	test.Expect(test.Client().Core().Cluster(logicalcluster.From(namespace)).CoreV1().Services(namespace.Name).
		Delete(test.Ctx(), name, metav1.DeleteOptions{})).
		To(Succeed())
	test.Expect(test.Client().Core().Cluster(logicalcluster.From(namespace)).AppsV1().Deployments(namespace.Name).
		Delete(test.Ctx(), name, metav1.DeleteOptions{})).
		To(Succeed())
}
