package billing

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/Vearance/komlay-msg-broker/backend/internal/contracts"
)

const validOrder = `{"order_id":"ORD-1001","customer_id":"CUST-88","items":[{"sku":"PRD-01","qty":2,"price":25000}],"total_amount":50000,"shipping_address":"Jl. Ganesha No. 10, Bandung"}`

func TestDecodeOrderRejectsCorruptMessages(t *testing.T) {
	cases := map[string]string{
		"broken JSON":      `{"order_id":"ORD-1",`,
		"not an object":    `"hello"`,
		"wrong type":       `{"order_id":"ORD-1","customer_id":"C","items":[{"sku":"P","qty":"two","price":1}],"total_amount":2,"shipping_address":"x"}`,
		"empty order_id":   `{"order_id":"","customer_id":"C","items":[{"sku":"P","qty":1,"price":1}],"total_amount":1,"shipping_address":"x"}`,
		"missing customer": `{"order_id":"ORD-1","items":[{"sku":"P","qty":1,"price":1}],"total_amount":1,"shipping_address":"x"}`,
		"no items":         `{"order_id":"ORD-1","customer_id":"C","items":[],"total_amount":1,"shipping_address":"x"}`,
		"negative qty":     `{"order_id":"ORD-1","customer_id":"C","items":[{"sku":"P","qty":-1,"price":1}],"total_amount":1,"shipping_address":"x"}`,
		"negative price":   `{"order_id":"ORD-1","customer_id":"C","items":[{"sku":"P","qty":1,"price":-5}],"total_amount":1,"shipping_address":"x"}`,
		"negative total":   `{"order_id":"ORD-1","customer_id":"C","items":[{"sku":"P","qty":1,"price":1}],"total_amount":-1,"shipping_address":"x"}`,
		"total mismatch":   `{"order_id":"ORD-1","customer_id":"C","items":[{"sku":"P","qty":2,"price":10}],"total_amount":5,"shipping_address":"x"}`,
		"blank address":    `{"order_id":"ORD-1","customer_id":"C","items":[{"sku":"P","qty":1,"price":1}],"total_amount":1,"shipping_address":"   "}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeOrder([]byte(body)); !errors.Is(err, ErrInvalidOrder) {
				t.Fatalf("err = %v, want ErrInvalidOrder", err)
			}
		})
	}
}

func TestDecodeOrderAcceptsAssignmentPayload(t *testing.T) {
	order, err := DecodeOrder([]byte(validOrder))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if order.OrderID != "ORD-1001" || order.TotalAmount != 50000 {
		t.Fatalf("decoded %+v", order)
	}
}

// memStore mimics the ON CONFLICT DO NOTHING behaviour of PostgresStore.
type memStore struct {
	charged map[string]int
	err     error
}

func (m *memStore) RecordCharge(_ context.Context, order contracts.OrderPlaced) (bool, error) {
	if m.err != nil {
		return false, m.err
	}
	m.charged[order.OrderID]++
	return m.charged[order.OrderID] == 1, nil
}

func newTestHandler(store Store) *Handler {
	return NewHandler(store, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestHandleDuplicateIsNotChargedTwice(t *testing.T) {
	store := &memStore{charged: map[string]int{}}
	h := newTestHandler(store)

	if got := h.Handle(context.Background(), []byte(validOrder)); got != Charged {
		t.Fatalf("first delivery = %s, want charged", got)
	}
	if got := h.Handle(context.Background(), []byte(validOrder)); got != Duplicate {
		t.Fatalf("second delivery = %s, want duplicate", got)
	}
}

func TestHandleCorruptIsRejectedWithoutTouchingStore(t *testing.T) {
	store := &memStore{charged: map[string]int{}}
	h := newTestHandler(store)

	if got := h.Handle(context.Background(), []byte(`{not json`)); got != Rejected {
		t.Fatalf("outcome = %s, want rejected", got)
	}
	if len(store.charged) != 0 {
		t.Fatalf("store touched: %v", store.charged)
	}
}

func TestHandleStoreFailureIsRetried(t *testing.T) {
	h := newTestHandler(&memStore{err: errors.New("db down")})
	if got := h.Handle(context.Background(), []byte(validOrder)); got != Retry {
		t.Fatalf("outcome = %s, want retry", got)
	}
}
