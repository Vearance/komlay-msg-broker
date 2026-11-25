package webshop

import (
	"context"
	"fmt"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Publisher defines the contract for publishing messages to RabbitMQ.
type Publisher interface {
	PublishOrderPlaced(ctx context.Context, body []byte) error
	PublishRaw(ctx context.Context, exchange, routingKey string, body []byte, contentType string) error
	Close() error
}

type RabbitMQPublisher struct {
	url      string
	exchange string

	mu   sync.Mutex
	conn *amqp.Connection
	ch   *amqp.Channel
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

// PublishOrderPlaced publishes to the configured exchange with DeliveryMode = Persistent.
func (p *RabbitMQPublisher) PublishOrderPlaced(ctx context.Context, body []byte) error {
	return p.PublishRaw(ctx, p.exchange, "", body, "application/json")
}

// PublishRaw allows publishing arbitrary payload/content-type, useful for test injection.
func (p *RabbitMQPublisher) PublishRaw(ctx context.Context, exchange, routingKey string, body []byte, contentType string) error {
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

	err = ch.PublishWithContext(
		ctx,
		exchange,
		routingKey,
		false, // mandatory
		false, // immediate
		msg,
	)
	if err != nil {
		// Invalidate channel on publish error to trigger reconnect next time
		p.mu.Lock()
		if p.ch != nil {
			_ = p.ch.Close()
			p.ch = nil
		}
		p.mu.Unlock()
		return fmt.Errorf("publish message: %w", err)
	}
	return nil
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
