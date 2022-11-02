package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"testing"
)

func TestGetToken(t *testing.T) {
	tests := []struct {
		testName string
		obj      DomainVerification
		verify   func(obj DomainVerification, t *testing.T)
	}{
		{
			testName: "returns hashed token",
			obj: DomainVerification{
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
					"kcp.dev/cluster": "kcp-glbc",
				}},
			},
			verify: func(obj DomainVerification, t *testing.T) {

				expectedToken := "7K2kvu2bO7fC331UKtz4EEM2KdaLldG0wLHwKt"

				if obj.GetToken() != expectedToken {
					t.Errorf("expected Token '%s' to match expectedToken: '%s'", obj.GetToken(), expectedToken)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.testName, func(t *testing.T) {
			tt.verify(tt.obj, t)
		})
	}
}
