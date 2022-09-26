package admission

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/kuadrant/kcp-glbc/pkg/_internal/log"
	"github.com/kuadrant/kcp-glbc/pkg/admission/domainverification"

	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

type WebhookConfig struct {
	ServerPort int
}

func StartServer(ctx context.Context, config *WebhookConfig) error {
	logger := log.Logger.WithName("webhooks-server")
	logger.Info("Started webhook server")

	mux := http.NewServeMux()

	handler, err := domainverification.NewHandler(logger)
	if err != nil {
		return err
	}
	webhook := &webhook.Admission{
		Handler: handler,
	}
	if err := webhook.InjectLogger(logger); err != nil {
		return err
	}

	mux.Handle("/domainverifications", webhook)

	httpErr := make(chan error)
	go func() {
		httpErr <- http.ListenAndServe(fmt.Sprintf(":%d", config.ServerPort), mux)
	}()

	select {
	case err := <-httpErr:
		return err
	case <-ctx.Done():
		ctxErr := ctx.Err()
		if errors.Is(ctxErr, context.Canceled) {
			return nil
		}

		return ctxErr
	}
}
