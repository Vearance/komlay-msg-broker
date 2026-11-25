package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/Vearance/komlay-msg-broker/backend/internal/billing"
	"github.com/Vearance/komlay-msg-broker/backend/internal/service"
)

func main() {
	// "healthcheck" runs without the consumer.
	if len(os.Args) > 1 {
		service.Main("billing")
		return
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := billing.ConfigFromEnv()
	if err != nil {
		logger.Error("billing config invalid", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	consumerDone := make(chan struct{})
	go func() {
		defer close(consumerDone)
		if err := billing.Run(ctx, cfg, logger); err != nil {
			logger.Error("billing consumer stopped", "error", err)
			os.Exit(1)
		}
	}()

	// Serves /healthz and blocks until SIGINT/SIGTERM.
	service.Main("billing")
	stop()
	<-consumerDone
}
