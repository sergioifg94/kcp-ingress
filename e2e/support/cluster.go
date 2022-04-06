//go:build e2e
// +build e2e

package support

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"strings"

	"github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kcp "github.com/kcp-dev/kcp/pkg/reconciler/workload/namespace"

	"github.com/kcp-dev/apimachinery/pkg/logicalcluster"
	workloadv1alpha1 "github.com/kcp-dev/kcp/pkg/apis/workload/v1alpha1"
)

const ClusterLabel = kcp.ClusterLabel

var WithKubeConfigByName = &withKubeConfigByName{}

type withKubeConfigByName struct{}

func WithKubeConfigByID(id string) Option {
	return &withKubeConfigByID{id}
}

type withKubeConfigByID struct {
	ID string
}

func (o *withKubeConfigByName) applyTo(object metav1.Object) error {
	// FIXME: workaround for https://github.com/kcp-dev/kcp/issues/730
	return WithKubeConfigByID(strings.TrimRight(object.GetGenerateName(), "-")).applyTo(object)
}

func (o *withKubeConfigByID) applyTo(object metav1.Object) error {
	var cluster *workloadv1alpha1.WorkloadCluster
	if c, ok := object.(*workloadv1alpha1.WorkloadCluster); !ok {
		return fmt.Errorf("KubeConfig option can only be applied to Cluster resources")
	} else {
		cluster = c
	}

	dir := os.Getenv(workloadClusterKubeConfigDir)
	if dir == "" {
		return fmt.Errorf("%s environment variable is not set", workloadClusterKubeConfigDir)
	}
	data, err := ioutil.ReadFile(path.Join(dir, o.ID+".kubeconfig"))
	if err != nil {
		return fmt.Errorf("error reading cluster %q Kubeconfig: %v", o.ID, err)
	}

	cluster.Spec.KubeConfig = string(data)

	return nil
}

func createWorkloadCluster(t Test, name string, options ...Option) *workloadv1alpha1.WorkloadCluster {
	cluster := &workloadv1alpha1.WorkloadCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: workloadv1alpha1.SchemeGroupVersion.String(),
			Kind:       "WorkloadCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			// FIXME: workaround for https://github.com/kcp-dev/kcp/issues/730
			// Name: name,
			GenerateName: name + "-",
		},
	}

	for _, option := range options {
		t.Expect(option.applyTo(cluster)).To(gomega.Succeed())
	}

	cluster, err := t.Client().Kcp().Cluster(logicalcluster.From(cluster)).WorkloadV1alpha1().WorkloadClusters().Create(t.Ctx(), cluster, metav1.CreateOptions{})
	t.Expect(err).NotTo(gomega.HaveOccurred())

	return cluster
}

func WorkloadCluster(t Test, workspace, name string) func(g gomega.Gomega) *workloadv1alpha1.WorkloadCluster {
	return func(g gomega.Gomega) *workloadv1alpha1.WorkloadCluster {
		c, err := t.Client().Kcp().Cluster(logicalcluster.New(workspace)).WorkloadV1alpha1().WorkloadClusters().Get(t.Ctx(), name, metav1.GetOptions{})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		return c
	}
}
