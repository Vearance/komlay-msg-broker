package webshop

import (
	"context"
	"fmt"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

type Publisher interface {
	PublishOrderPlaced(ctx context.Context, body []byte) error
	PublishRaw(ctx context.Context, exchange, routingKey string, body []byte, contentType string) error
	Close() error
}

const confirmTimeout = 5 * time.Second

type RabbitMQPublisher struct {
	url      string
	exchange string

	mu   sync.Mutex
	conn *amqp.Connection
	ch   *amqp.Channel

	// publishMu serializes publish + confirm wait, so one confirm always
	// belongs to the publish that is waiting for it.
	publishMu sync.Mutex
}

func NewRabbitMQPublisher(url, exchange string) (*RabbitMQPublisher, error) {
	p := &RabbitMQPublisher{
		url:      url,
		exchange: exchange,
	}
	if err := p.connect(); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *RabbitMQPublisher) connect() error {
	conn, err := amqp.Dial(p.url)
	if err != nil {
		return fmt.Errorf("dial RabbitMQ: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("open AMQP channel: %w", err)
	}

	if err := ch.Confirm(false); err != nil {
		_ = conn.Close()
		return fmt.Errorf("enable publisher confirms: %w", err)
	}

	p.conn = conn
	p.ch = ch
	return nil
}

func (p *RabbitMQPublisher) getChannel() (*amqp.Channel, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.conn != nil && !p.conn.IsClosed() && p.ch != nil && !p.ch.IsClosed() {
		return p.ch, nil
	}

	if p.conn != nil && !p.conn.IsClosed() {
		_ = p.conn.Close()
	}

	if err := p.connect(); err != nil {
		return nil, err
	}
	return p.ch, nil
}

func (p *RabbitMQPublisher) PublishOrderPlaced(ctx context.Context, body []byte) error {
	return p.PublishRaw(ctx, p.exchange, "", body, "application/json")
}

func (p *RabbitMQPublisher) PublishRaw(ctx context.Context, exchange, routingKey string, body []byte, contentType string) error {
	p.publishMu.Lock()
	defer p.publishMu.Unlock()

	ch, err := p.getChannel()
	if err != nil {
		return fmt.Errorf("get channel: %w", err)
	}

	if contentType == "" {
		contentType = "application/json"
	}

	msg := amqp.Publishing{
		DeliveryMode: amqp.Persistent, // Delivery Mode: Persistent (Mode 2)
		ContentType:  contentType,
		Body:         body,
		Timestamp:    time.Now().UTC(),
	}

	confirm, err := ch.PublishWithDeferredConfirmWithContext(
		ctx,
		exchange,
		routingKey,
		false, // mandatory
		false, // immediate
		msg,
	)
	if err != nil {
		p.invalidateChannel()
		return fmt.Errorf("publish message: %w", err)
	}

	waitCtx, cancel := context.WithTimeout(ctx, confirmTimeout)
	defer cancel()
	acked, err := confirm.WaitContext(waitCtx)
	if err != nil {
		p.invalidateChannel()
		return fmt.Errorf("wait for publisher confirm: %w", err)
	}
	if !acked {
		return fmt.Errorf("broker nacked the message")
	}
	return nil
}

func (p *RabbitMQPublisher) invalidateChannel() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ch != nil {
		_ = p.ch.Close()
		p.ch = nil
	}
}

func (p *RabbitMQPublisher) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	var chErr, connErr error
	if p.ch != nil {
		chErr = p.ch.Close()
		p.ch = nil
	}
	if p.conn != nil {
		connErr = p.conn.Close()
		p.conn = nil
	}
	if chErr != nil {
		return chErr
	}
	return connErr
}
