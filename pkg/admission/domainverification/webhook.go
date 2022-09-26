package domainverification

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	controllerutil "github.com/kuadrant/kcp-glbc/pkg/util/controller"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	authenticationv1 "k8s.io/api/authentication/v1"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	kuadrantv1 "github.com/kuadrant/kcp-glbc/pkg/apis/kuadrant/v1"
)

func GetValidatingWebhookConfiguration() *admissionv1.ValidatingWebhookConfiguration {
	var matchPolicy admissionv1.MatchPolicyType = admissionv1.Exact
	var scope admissionv1.ScopeType = admissionv1.AllScopes
	var sideEffects admissionv1.SideEffectClass = admissionv1.SideEffectClassNone
	var timeoutSeconds int32 = 5

	var failurePolicy admissionv1.FailurePolicyType = admissionv1.Fail
	if controllerutil.IsRunningLocally() {
		failurePolicy = admissionv1.Ignore
	}

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
							Resources:   []string{"domainverifications", "domainverifications/status"},
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

type Handler struct {
	Logger  logr.Logger
	Decoder *admission.Decoder

	SkipUser func(u authenticationv1.UserInfo) bool
}

var _ admission.Handler = &Handler{}

func NewHandler(logger logr.Logger) (admission.Handler, error) {
	scheme := runtime.NewScheme()
	if err := kuadrantv1.AddToScheme(scheme); err != nil {
		return nil, err
	}

	decoder, err := admission.NewDecoder(scheme)
	if err != nil {
		return nil, err
	}

	return &Handler{
		SkipUser: func(_ authenticationv1.UserInfo) bool {
			return false
		},

		Decoder: decoder,
		Logger:  logger,
	}, nil
}

func (h *Handler) Handle(ctx context.Context, r admission.Request) admission.Response {
	allowed, reason, err := h.handle(ctx, r)
	if err != nil {
		return admission.Errored(-1, err)
	}

	response := admission.Allowed
	if !allowed {
		response = admission.Denied
	}

	return response(reason)
}

func (h *Handler) handle(ctx context.Context, r admission.Request) (bool, string, error) {
	// obj, err := h.decodeDomainVerification(object, &r)
	// if err != nil {
	// 	return false, "", err
	// }

	// oldObj, err := h.decodeDomainVerification(oldObject, &r)
	// if err != nil {
	// 	return false, "", err
	// }

	return true, "TODO", nil
}

// func (h *Handler) decodeDomainVerification(selectObj func(*admission.Request) runtime.RawExtension, r *admission.Request) (*kuadrantv1.DomainVerification, error) {
// 	obj := &kuadrantv1.DomainVerification{}
// 	err := h.Decoder.DecodeRaw(selectObj(r), obj)
// 	return obj, err
// }

// func object(r *admission.Request) runtime.RawExtension {
// 	return r.Object
// }

// func oldObject(r *admission.Request) runtime.RawExtension {
// 	return r.OldObject
// }
