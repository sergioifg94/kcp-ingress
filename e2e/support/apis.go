//go:build e2e
// +build e2e

package support

import (
	"github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kcp-dev/apimachinery/pkg/logicalcluster"
	apisv1alpha1 "github.com/kcp-dev/kcp/pkg/apis/apis/v1alpha1"
)

func createGLBCAPIBinding(t Test, options ...Option) *apisv1alpha1.APIBinding {
	binding := &apisv1alpha1.APIBinding{
		TypeMeta: metav1.TypeMeta{
			APIVersion: apisv1alpha1.SchemeGroupVersion.String(),
			Kind:       "APIBinding",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "glbc",
		},
		Spec: apisv1alpha1.APIBindingSpec{
			Reference: apisv1alpha1.ExportReference{
				Workspace: &apisv1alpha1.WorkspaceExportReference{
					WorkspaceName: "kcp-glbc",
					ExportName:    "glbc",
				},
			},
		},
	}

	for _, option := range options {
		t.Expect(option.applyTo(binding)).To(gomega.Succeed())
	}

	binding, err := t.Client().Kcp().Cluster(logicalcluster.From(binding)).ApisV1alpha1().APIBindings().
		Create(t.Ctx(), binding, metav1.CreateOptions{})
	t.Expect(err).NotTo(gomega.HaveOccurred())

	return binding
}

func APIBinding(t Test, workspace, name string) func(g gomega.Gomega) *apisv1alpha1.APIBinding {
	return func(g gomega.Gomega) *apisv1alpha1.APIBinding {
		c, err := t.Client().Kcp().Cluster(logicalcluster.New(workspace)).ApisV1alpha1().APIBindings().Get(t.Ctx(), name, metav1.GetOptions{})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		return c
	}
}

func APIBindingPhase(binding *apisv1alpha1.APIBinding) apisv1alpha1.APIBindingPhaseType {
	return binding.Status.Phase
}
