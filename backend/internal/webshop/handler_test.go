package webshop_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/Vearance/komlay-msg-broker/backend/internal/contracts"
	"github.com/Vearance/komlay-msg-broker/backend/internal/webshop"
)

type mockStore struct {
	mu     sync.Mutex
	orders []webshop.OrderWithItems
}

func (m *mockStore) CreateOrder(ctx context.Context, order contracts.OrderPlaced, beforeCommit func(context.Context) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, o := range m.orders {
		if o.OrderID == order.OrderID {
			return errors.New("duplicate key value violates unique constraint \"orders_pkey\"")
		}
	}
	if beforeCommit != nil {
		if err := beforeCommit(ctx); err != nil {
			return err
		}
	}
	m.orders = append(m.orders, webshop.OrderWithItems{
		OrderID:         order.OrderID,
		CustomerID:      order.CustomerID,
		TotalAmount:     order.TotalAmount,
		ShippingAddress: order.ShippingAddress,
		Status:          "placed",
		Items:           order.Items,
	})
	return nil
}

func (m *mockStore) GetOrders(_ context.Context) ([]webshop.OrderWithItems, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.orders, nil
}

func (m *mockStore) GetOrderByID(_ context.Context, id string) (*webshop.OrderWithItems, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, o := range m.orders {
		if o.OrderID == id {
			return &o, nil
		}
	}
	return nil, nil
}

type mockPublisher struct {
	mu        sync.Mutex
	published [][]byte
	raws      []string
	fail      bool // simulates a broker that is down
}

func (m *mockPublisher) PublishOrderPlaced(_ context.Context, body []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail {
		return errors.New("dial RabbitMQ: connection refused")
	}
	m.published = append(m.published, body)
	return nil
}

func (m *mockPublisher) PublishRaw(_ context.Context, exchange, routingKey string, body []byte, contentType string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.raws = append(m.raws, string(body))
	return nil
}

func (m *mockPublisher) Close() error {
	return nil
}

func TestWebShopEndpoints(t *testing.T) {
	store := &mockStore{}
	pub := &mockPublisher{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := webshop.NewHandler(store, pub, logger).Routes()

	server := httptest.NewServer(handler)
	defer server.Close()

	t.Run("POST /orders creates and publishes order", func(t *testing.T) {
		order := contracts.OrderPlaced{
			OrderID:         "ORD-TEST-1",
			CustomerID:      "CUST-01",
			TotalAmount:     50000,
			ShippingAddress: "Jl. Ganesha No. 10",
			Items: []contracts.OrderItem{
				{SKU: "PRD-01", Qty: 2, Price: 25000},
			},
		}
		body, _ := json.Marshal(order)
		resp, err := http.Post(server.URL+"/orders", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("expected 201, got %d", resp.StatusCode)
		}

		if len(pub.published) != 1 {
			t.Errorf("expected 1 message published, got %d", len(pub.published))
		}
	})

	t.Run("GET /orders returns placed orders", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/orders")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		var orders []webshop.OrderWithItems
		if err := json.NewDecoder(resp.Body).Decode(&orders); err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		if len(orders) != 1 {
			t.Errorf("expected 1 order, got %d", len(orders))
		}
	})

	t.Run("POST /test/inject-duplicate publishes twice", func(t *testing.T) {
		order := contracts.OrderPlaced{
			OrderID:         "ORD-DUP-1",
			CustomerID:      "CUST-02",
			TotalAmount:     25000,
			ShippingAddress: "Jl. Dago No. 1",
			Items: []contracts.OrderItem{
				{SKU: "PRD-01", Qty: 1, Price: 25000},
			},
		}
		body, _ := json.Marshal(order)
		resp, err := http.Post(server.URL+"/test/inject-duplicate", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		// 1 previously + 2 duplicates = 3 total published
		if len(pub.published) != 3 {
			t.Errorf("expected 3 total published, got %d", len(pub.published))
		}
	})

	t.Run("POST /test/inject-corrupt publishes raw to exchange", func(t *testing.T) {
		resp, err := http.Post(server.URL+"/test/inject-corrupt?type=malformed", "application/json", nil)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		if len(pub.raws) != 1 {
			t.Errorf("expected 1 raw published, got %d", len(pub.raws))
		}
	})

	t.Run("POST /test/inject-raw publishes arbitrary body", func(t *testing.T) {
		raw := `{"bad": "schema"}`
		resp, err := http.Post(server.URL+"/test/inject-raw", "application/json", bytes.NewBufferString(raw))
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		if len(pub.raws) != 2 {
			t.Errorf("expected 2 raw published, got %d", len(pub.raws))
		}
	})
}

func TestCreateOrderBrokerDownRollsBack(t *testing.T) {
	store := &mockStore{}
	pub := &mockPublisher{fail: true}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := httptest.NewServer(webshop.NewHandler(store, pub, logger).Routes())
	defer server.Close()

	body, _ := json.Marshal(contracts.OrderPlaced{
		OrderID:         "ORD-RETRY-1",
		CustomerID:      "CUST-01",
		TotalAmount:     25000,
		ShippingAddress: "Jl. Ganesha No. 10",
		Items:           []contracts.OrderItem{{SKU: "PRD-01", Qty: 1, Price: 25000}},
	})
	post := func() int {
		t.Helper()
		resp, err := http.Post(server.URL+"/orders", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}

	if got := post(); got != http.StatusServiceUnavailable {
		t.Fatalf("broker down: expected 503, got %d", got)
	}
	if n := len(store.orders); n != 0 {
		t.Fatalf("broker down: expected no stored order, got %d", n)
	}

	pub.mu.Lock()
	pub.fail = false
	pub.mu.Unlock()

	if got := post(); got != http.StatusCreated {
		t.Fatalf("retry with same order_id: expected 201, got %d", got)
	}
	if n := len(store.orders); n != 1 {
		t.Errorf("after retry: expected 1 stored order, got %d", n)
	}
	if n := len(pub.published); n != 1 {
		t.Errorf("after retry: expected 1 published event, got %d", n)
	}
}
