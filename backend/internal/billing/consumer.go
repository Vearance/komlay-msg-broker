package billing

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	defaultQueue  = "billing.queue"
	prefetchCount = 10
	retryDelay    = time.Second
	maxBackoff    = 15 * time.Second
)

type Config struct {
	AMQPURL     string
	DatabaseURL string
	Queue       string
}

func ConfigFromEnv() (Config, error) {
	cfg := Config{
		AMQPURL:     os.Getenv("AMQP_URL"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
		Queue:       os.Getenv("BILLING_QUEUE"),
	}
	if cfg.Queue == "" {
		cfg.Queue = defaultQueue
	}
	if cfg.AMQPURL == "" {
		return cfg, errors.New("AMQP_URL is not set")
	}
	if cfg.DatabaseURL == "" {
		return cfg, errors.New("DATABASE_URL is not set")
	}
	return cfg, nil
}

func Run(ctx context.Context, cfg Config, logger *slog.Logger) error {
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("open database pool: %w", err)
	}
	defer pool.Close()

	handler := NewHandler(NewPostgresStore(pool), logger)

	backoff := retryDelay
	for {
		err := consumeOnce(ctx, cfg, handler, logger)
		if ctx.Err() != nil {
			return nil
		}
		logger.Warn("billing.consumer_disconnected", "error", err, "retry_in", backoff.String())
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, maxBackoff)
	}
}

func consumeOnce(ctx context.Context, cfg Config, handler *Handler, logger *slog.Logger) error {
	conn, err := amqp.Dial(cfg.AMQPURL)
	if err != nil {
		return fmt.Errorf("dial RabbitMQ: %w", err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("open channel: %w", err)
	}
	defer ch.Close()

	if err := ch.Qos(prefetchCount, 0, false); err != nil {
		return fmt.Errorf("set prefetch: %w", err)
	}

	// Topology comes from rabbitmq/definitions.json; the consumer only reads.
	deliveries, err := ch.Consume(cfg.Queue, "billing", false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("consume %s: %w", cfg.Queue, err)
	}
	logger.Info("billing.consumer_started", "queue", cfg.Queue)

	closed := conn.NotifyClose(make(chan *amqp.Error, 1))
	for {
		select {
		case <-ctx.Done():
			return nil
		case amqpErr := <-closed:
			return fmt.Errorf("connection closed: %v", amqpErr)
		case delivery, ok := <-deliveries:
			if !ok {
				return errors.New("delivery channel closed")
			}
			if err := settle(ctx, handler, delivery, logger); err != nil {
				return err
			}
		}
	}
}

func settle(ctx context.Context, handler *Handler, delivery amqp.Delivery, logger *slog.Logger) error {
	outcome := handler.Handle(ctx, delivery.Body)

	var err error
	switch outcome {
	case Charged, Duplicate:
		err = delivery.Ack(false)
	case Rejected:
		// requeue=false: billing.queue's x-dead-letter-exchange sends it to billing.dlq.
		err = delivery.Nack(false, false)
	case Retry:
		// Pause so a down database does not spin the message in a hot loop.
		select {
		case <-ctx.Done():
		case <-time.After(retryDelay):
		}
		err = delivery.Nack(false, true)
	}
	if err != nil {
		return fmt.Errorf("settle delivery %d as %s: %w", delivery.DeliveryTag, outcome, err)
	}
	logger.Debug("billing.settled", "outcome", outcome.String(), "redelivered", delivery.Redelivered)
	return nil
}
