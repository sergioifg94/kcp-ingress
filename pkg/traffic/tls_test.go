package traffic

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	basereconciler "github.com/kuadrant/kcp-glbc/pkg/reconciler"
	"github.com/kuadrant/kcp-glbc/pkg/tls"
)

func TestCertificateSecretFilter(t *testing.T) {
	testCases := []struct {
		Name     string
		Obj      interface{}
		Expected bool
	}{{
		Name:     "Not a secret",
		Obj:      struct{}{},
		Expected: false,
	},
		{
			Name: "No label, no annotations",
			Obj: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
			},
			Expected: false,
		},
		{
			Name: "Valid label, no annotations",
			Obj: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
					Labels: map[string]string{
						basereconciler.LABEL_HCG_MANAGED: "true",
					},
				},
			},
			Expected: false,
		},
		{
			Name: "No label, valid annotations",
			Obj: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
					Annotations: map[string]string{
						ANNOTATION_TRAFFIC_KEY:  "test",
						tls.TlsIssuerAnnotation: "test",
					},
				},
			},
			Expected: false,
		},
		{
			Name: "Valid label, valid annotations",
			Obj: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
					Annotations: map[string]string{
						ANNOTATION_TRAFFIC_KEY:  "test",
						tls.TlsIssuerAnnotation: "test",
					},
					Labels: map[string]string{
						basereconciler.LABEL_HCG_MANAGED: "true",
					},
				},
			},
			Expected: true,
		}}

	for _, testCase := range testCases {
		t.Run(testCase.Name, func(t *testing.T) {
			result := CertificateSecretFilter(testCase.Obj)

			if result != testCase.Expected {
				t.Fail()
			}
		})
	}
}
