package domainverification

import (
	"fmt"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func GetValidatingWebhookConfiguration() *admissionv1.ValidatingWebhookConfiguration {
	var failurePolicy admissionv1.FailurePolicyType = admissionv1.Fail
	var matchPolicy admissionv1.MatchPolicyType = admissionv1.Exact
	var scope admissionv1.ScopeType = admissionv1.AllScopes
	var sideEffects admissionv1.SideEffectClass = admissionv1.SideEffectClassNone
	var timeoutSeconds int32 = 5

	return &admissionv1.ValidatingWebhookConfiguration{
		ObjectMeta: v1.ObjectMeta{
			Name: "glbc.domainverification.dev",
		},
		Webhooks: []admissionv1.ValidatingWebhook{
			{
				FailurePolicy: &failurePolicy,
				MatchPolicy:   &matchPolicy,
				Name:          "glbc.domainverification.dev",
				Rules: []admissionv1.RuleWithOperations{
					{
						Rule: admissionv1.Rule{
							APIGroups:   []string{"kuadrant.dev"},
							APIVersions: []string{"v1"},
							Resources:   []string{"domainverifications"},
							Scope:       &scope,
						},
						Operations: []admissionv1.OperationType{
							admissionv1.Create,
							admissionv1.Update,
						},
					},
				},
				SideEffects:             &sideEffects,
				TimeoutSeconds:          &timeoutSeconds,
				AdmissionReviewVersions: []string{"v1"},
			},
		},
	}
}

func MakeURL(domain string) string {
	// TODO: Refactor into interface in order to generalise this logic
	// for many possible webhooks
	return fmt.Sprintf("https://%s/domainverifications", domain)
}
