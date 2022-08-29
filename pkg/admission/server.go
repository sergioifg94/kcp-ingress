package admission

import (
	"context"
	"fmt"
	"net/http"

	"github.com/kuadrant/kcp-glbc/pkg/log"

	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

type WebhookConfig struct {
	ServerPort int
}

func StartServer(ctx context.Context, config *WebhookConfig) error {
	logger := log.Logger.WithName("webhooks-server")
	logger.Info("Started webhook server")

	mux := http.NewServeMux()

	webhook := &webhook.Admission{
		Handler: admission.HandlerFunc(func(ctx context.Context, r admission.Request) admission.Response {
			logger.Info("Got a webhook request")
			return admission.Denied("Just a test")
		}),
	}
	webhook.InjectLogger(logger)

	mux.Handle("/validating", webhook)

	return http.ListenAndServe(fmt.Sprintf(":%d", config.ServerPort), mux)
}
