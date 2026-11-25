package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Vearance/komlay-msg-broker/backend/internal/service"
	"github.com/Vearance/komlay-msg-broker/backend/internal/webshop"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		service.Main("web-shop")
		return
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	amqpURL := os.Getenv("AMQP_URL")
	databaseURL := os.Getenv("DATABASE_URL")
	exchange := os.Getenv("EXCHANGE_NAME")
	if exchange == "" {
		exchange = "orders.exchange"
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var store webshop.Store
	if databaseURL != "" {
		pool, err := pgxpool.New(ctx, databaseURL)
		if err != nil {
			logger.Error("webshop.db_connect_failed", "error", err)
			os.Exit(1)
		}
		defer pool.Close()
		store = webshop.NewPostgresStore(pool)
		logger.Info("webshop.db_connected")
	}

	var publisher webshop.Publisher
	if amqpURL != "" {
		var err error
		publisher, err = webshop.NewRabbitMQPublisher(amqpURL, exchange)
		if err != nil {
			logger.Error("webshop.amqp_connect_failed", "error", err)
			os.Exit(1)
		}
		defer publisher.Close()
		logger.Info("webshop.amqp_connected", "exchange", exchange)
	}

	handler := webshop.NewHandler(store, publisher, logger).Routes()

	server := &http.Server{
		Addr:              net.JoinHostPort("", port),
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serveErrors := make(chan error, 1)
	go func() {
		logger.Info("web-shop service starting", "address", server.Addr)
		serveErrors <- server.ListenAndServe()
	}()

	select {
	case err := <-serveErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return
		}
		logger.Error("serve HTTP failed", "error", err)
		os.Exit(1)
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown failed", "error", err)
	}
	logger.Info("web-shop service stopped gracefully")
}
