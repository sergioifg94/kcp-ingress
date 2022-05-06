//go:build e2e
// +build e2e

package support

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"

	tenancyv1alpha1 "github.com/kcp-dev/kcp/pkg/apis/tenancy/v1alpha1"
	"github.com/onsi/gomega"
	"k8s.io/cli-runtime/pkg/genericclioptions"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kcp-dev/apimachinery/pkg/logicalcluster"
	workloadv1alpha1 "github.com/kcp-dev/kcp/pkg/apis/workload/v1alpha1"
	workloadplugin "github.com/kcp-dev/kcp/pkg/cliplugins/workload/plugin"

	"k8s.io/apimachinery/pkg/util/sets"
)

func createWorkloadCluster(t Test, workspace *tenancyv1alpha1.ClusterWorkspace, name string) *workloadv1alpha1.WorkloadCluster {

	logicalClusterName := logicalcluster.From(workspace).Join(workspace.Name).String()

	//Run workload plugin sync command
	syncerResources, err := execKcpWorkloadSync(t, name, logicalClusterName)
	t.Expect(err).NotTo(gomega.HaveOccurred())

	//Apply syncer resources to workload cluster
	err = workloadClusterKubeCtlApply(name, syncerResources)
	t.Expect(err).NotTo(gomega.HaveOccurred())

	// Get the workload cluster and return it
	c, err := t.Client().Kcp().Cluster(logicalcluster.New(logicalClusterName)).WorkloadV1alpha1().WorkloadClusters().Get(t.Ctx(), name, metav1.GetOptions{})
	t.Expect(err).NotTo(gomega.HaveOccurred())

	return c
}

func execKcpWorkloadSync(t Test, workloadClusterName, logicalClusterName string) (string, error) {
	clusterServer := fmt.Sprintf("%s/clusters/%s", t.Client().GetConfig().Host, logicalClusterName)
	//Configure workload plugin kubeconfig for test workspace
	outBuffer := new(bytes.Buffer)
	opts := workloadplugin.NewOptions(genericclioptions.IOStreams{In: os.Stdin, Out: outBuffer, ErrOut: os.Stderr})
	opts.KubectlOverrides.ClusterInfo.Server = clusterServer
	kubeconfig, err := workloadplugin.NewConfig(opts)
	if err != nil {
		return "", err
	}

	//Workload plugin sync options
	//ToDo allow this to be set based on the KCP version exported
	syncerImage := "ghcr.io/kcp-dev/kcp/syncer:release-0.4"
	replicas := 1
	kcpNamespaceName := "default" //Creates service account here
	requiredResourcesToSync := sets.NewString("deployments.apps", "secrets", "configmaps", "serviceaccounts")
	userResourcesToSync := sets.NewString("ingresses.networking.k8s.io", "services")
	resourcesToSync := userResourcesToSync.Union(requiredResourcesToSync).List()

	//Run workload plugin sync command
	err = kubeconfig.Sync(t.Ctx(), workloadClusterName, kcpNamespaceName, syncerImage, resourcesToSync, replicas)
	if err != nil {
		return "", err
	}

	//Write temp file with syncer resources
	tmpFile, err := ioutil.TempFile(t.T().TempDir(), "syncer-")
	if err != nil {
		return "", err
	}
	_, err = tmpFile.Write(outBuffer.Bytes())
	if err != nil {
		return "", err
	}
	err = tmpFile.Close()

	return tmpFile.Name(), err
}

func kubeCtlApply(kubeconfig, file string) error {
	cmd := exec.Command("kubectl", "--kubeconfig", kubeconfig, "apply", "-f", file)
	return cmd.Run()
}

func workloadClusterKubeConfig(name string) (string, error) {
	dir := os.Getenv(workloadClusterKubeConfigDir)
	if dir == "" {
		return "", fmt.Errorf("%s environment variable is not set", workloadClusterKubeConfigDir)
	}
	return path.Join(dir, name+".kubeconfig"), nil
}

func workloadClusterKubeCtlApply(clusterName, file string) error {
	clusterKubeconfig, err := workloadClusterKubeConfig(clusterName)
	if err != nil {
		return err
	}
	return kubeCtlApply(clusterKubeconfig, file)
}

func WorkloadCluster(t Test, workspace, name string) func(g gomega.Gomega) *workloadv1alpha1.WorkloadCluster {
	return func(g gomega.Gomega) *workloadv1alpha1.WorkloadCluster {
		c, err := t.Client().Kcp().Cluster(logicalcluster.New(workspace)).WorkloadV1alpha1().WorkloadClusters().Get(t.Ctx(), name, metav1.GetOptions{})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		return c
	}
}
