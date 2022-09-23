//go:build performance && ingress

package performance

import (
	"fmt"
	"os"
	"sync"
	"testing"

	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
	"github.com/rs/xid"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kcp-dev/logicalcluster/v2"

	. "github.com/kuadrant/kcp-glbc/e2e/support"
	"github.com/kuadrant/kcp-glbc/pkg/access"
	kuadrantv1 "github.com/kuadrant/kcp-glbc/pkg/apis/kuadrant/v1"
	"github.com/kuadrant/kcp-glbc/pkg/util/env"
)

func createTestIngress(t Test, namespace *corev1.Namespace) *networkingv1.Ingress {
	name := "perf-test-" + xid.New().String()

	ingress, err := t.Client().Core().Cluster(logicalcluster.From(namespace)).NetworkingV1().Ingresses(namespace.Name).
		Apply(t.Ctx(), IngressConfiguration(namespace.Name, name, "test.glbc.com"), ApplyOptions)
	t.Expect(err).NotTo(HaveOccurred())

	return ingress
}

func deleteTestIngress(t Test, namespace *corev1.Namespace, ingress *networkingv1.Ingress) {
	err := t.Client().Core().Cluster(logicalcluster.From(namespace)).NetworkingV1().Ingresses(namespace.Name).
		Delete(t.Ctx(), ingress.Name, metav1.DeleteOptions{})
	t.Expect(err).NotTo(HaveOccurred())
}

func testIngress(t Test, ingressCount int, zoneID, glbcDomain string) {

	// Create the test workspace
	workspace := t.NewTestWorkspace()

	// Create GLBC APIBinding in workspace
	t.CreateGLBCAPIBindings(workspace, GLBCWorkspace, GLBCExportName)

	// Create a namespace
	namespace := t.NewTestNamespace(InWorkspace(workspace))
	t.Expect(namespace).NotTo(BeNil())

	name := "perf-test-echo"
	// Create test Deployment
	_, err := t.Client().Core().Cluster(logicalcluster.From(namespace)).AppsV1().Deployments(namespace.Name).
		Apply(t.Ctx(), DeploymentConfiguration(namespace.Name, name), ApplyOptions)
	t.Expect(err).NotTo(HaveOccurred())

	// Create test Service
	_, err = t.Client().Core().Cluster(logicalcluster.From(namespace)).CoreV1().Services(namespace.Name).
		Apply(t.Ctx(), ServiceConfiguration(namespace.Name, name, map[string]string{}), ApplyOptions)
	t.Expect(err).NotTo(HaveOccurred())

	t.T().Log(fmt.Sprintf("Creating %d Ingresses in %s", ingressCount, workspace.Name))

	// Create Ingresses
	wg := sync.WaitGroup{}
	for i := 1; i <= ingressCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ingress := createTestIngress(t, namespace)
			t.Expect(ingress).NotTo(BeNil())
		}()
	}
	wg.Wait()

	// Retrieve Ingresses
	ingresses := GetIngresses(t, namespace, "")
	t.Expect(ingresses).Should(HaveLen(ingressCount))

	// Assert Ingresses reconcile success
	for _, ingress := range ingresses {
		t.Eventually(Ingress(t, namespace, ingress.Name)).WithTimeout(TestTimeoutMedium).Should(And(
			WithTransform(Annotations, And(
				HaveKey(access.ANNOTATION_HCG_HOST),
				HaveKey(access.ANNOTATION_PENDING_CUSTOM_HOSTS),
			)),
			WithTransform(Labels, And(
				HaveKey(access.LABEL_HAS_PENDING_HOSTS),
			)),
			WithTransform(LoadBalancerIngresses, HaveLen(1)),
		))
	}

	// Retrieve DNSRecords
	t.Eventually(DNSRecords(t, namespace, "")).Should(HaveLen(ingressCount))
	dnsRecords := GetDNSRecords(t, namespace, "")
	t.Expect(dnsRecords).Should(HaveLen(ingressCount))

	// Assert DNSRecords reconcile success
	for _, record := range dnsRecords {
		t.Eventually(DNSRecord(t, namespace, record.Name)).Should(And(
			WithTransform(DNSRecordEndpoints, HaveLen(1)),
			WithTransform(DNSRecordCondition(zoneID, kuadrantv1.DNSRecordFailedConditionType), MatchFieldsP(IgnoreExtras,
				Fields{
					"Status":  Equal("False"),
					"Reason":  Equal("ProviderSuccess"),
					"Message": Equal("The DNS provider succeeded in ensuring the record"),
				})),
		))
	}

	// Retrieve TLS Secrets
	t.Eventually(Secrets(t, namespace, "kuadrant.dev/hcg.managed=true")).WithTimeout(
		TestTimeoutLong).Should(HaveLen(ingressCount))

	// Delete Ingresses
	for _, ingress := range ingresses {
		deleteTestIngress(t, namespace, &ingress)
	}

	// Assert Ingresses, DNSRecord and TLS Secret deletion success
	t.Eventually(Ingresses(t, namespace, "")).Should(HaveLen(0))
	t.Eventually(DNSRecords(t, namespace, "")).Should(HaveLen(0))
	t.Eventually(Secrets(t, namespace, "kuadrant.dev/hcg.managed=true")).Should(HaveLen(0))

	// Finally, delete the test deployment and service resources
	t.Expect(t.Client().Core().Cluster(logicalcluster.From(namespace)).CoreV1().Services(namespace.Name).
		Delete(t.Ctx(), name, metav1.DeleteOptions{})).
		To(Succeed())
	t.Expect(t.Client().Core().Cluster(logicalcluster.From(namespace)).AppsV1().Deployments(namespace.Name).
		Delete(t.Ctx(), name, metav1.DeleteOptions{})).
		To(Succeed())

}

func TestIngress(t *testing.T) {
	test := With(t)
	test.T().Parallel()

	// To run this test you must know the DNSZone and Domain the target GLBC instance is configured to use.
	// Check the a DNS zone id has been set
	zoneID := os.Getenv("AWS_DNS_PUBLIC_ZONE_ID")
	test.Expect(zoneID).NotTo(Equal(""))
	// Check the a DNS domain name has been set
	glbcDomain := os.Getenv("GLBC_DOMAIN")
	test.Expect(glbcDomain).NotTo(Equal(""))

	workspaceCount := env.GetEnvInt(TestWorkspaceCount, DefaultTestWorkspaceCount)
	test.Expect(workspaceCount > 0).To(BeTrue())

	ingressCount := env.GetEnvInt(TestIngressCount, DefaultTestIngressCount)
	test.Expect(ingressCount > 0).To(BeTrue())

	test.T().Log(fmt.Sprintf("Creating %d Workspaces, each with %d Ingresses", workspaceCount, ingressCount))

	// Create Workspaces and run ingress test in each
	wg := sync.WaitGroup{}
	for i := 1; i <= workspaceCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			testIngress(test, ingressCount, zoneID, glbcDomain)
		}()
	}
	wg.Wait()
}
