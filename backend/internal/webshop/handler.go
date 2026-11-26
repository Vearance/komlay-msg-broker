package webshop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Vearance/komlay-msg-broker/backend/internal/contracts"
)

type Handler struct {
	store     Store
	publisher Publisher
	logger    *slog.Logger
}

func NewHandler(store Store, publisher Publisher, logger *slog.Logger) *Handler {
	return &Handler{
		store:     store,
		publisher: publisher,
		logger:    logger,
	}
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", h.handleHealthz)
	mux.HandleFunc("POST /orders", h.handleCreateOrder)
	mux.HandleFunc("GET /orders", h.handleGetOrders)

	// Helper endpoints for demo & failure testing
	mux.HandleFunc("POST /test/inject-raw", h.handleInjectRaw)
	mux.HandleFunc("POST /test/inject-duplicate", h.handleInjectDuplicate)
	mux.HandleFunc("POST /test/inject-corrupt", h.handleInjectCorrupt)

	return mux
}

func (h *Handler) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	respondJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"service": "web-shop",
	})
}

// POST /orders
func (h *Handler) handleCreateOrder(w http.ResponseWriter, r *http.Request) {
	var req contracts.OrderPlaced
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid json payload: "+err.Error())
		return
	}

	if req.OrderID == "" {
		req.OrderID = fmt.Sprintf("ORD-%d", time.Now().UnixNano()%1000000)
	}

	if err := validateOrder(req); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx := r.Context()

	payload, err := json.Marshal(req)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "marshal event payload: "+err.Error())
		return
	}

	var publishErr error
	publish := func(ctx context.Context) error {
		if h.publisher == nil {
			return nil
		}
		publishErr = h.publisher.PublishOrderPlaced(ctx, payload)
		return publishErr
	}

	if h.store != nil {
		err = h.store.CreateOrder(ctx, req, publish)
	} else {
		err = publish(ctx)
	}

	if publishErr != nil {
		h.logger.Error("webshop.publish_failed", "order_id", req.OrderID, "error", publishErr)
		respondError(w, http.StatusServiceUnavailable, "order not placed: message broker unavailable, retry later: "+publishErr.Error())
		return
	}
	if err != nil {
		h.logger.Error("webshop.create_order_store_failed", "order_id", req.OrderID, "error", err)
		respondError(w, http.StatusInternalServerError, "failed to persist order: "+err.Error())
		return
	}

	h.logger.Info("webshop.order_placed", "order_id", req.OrderID, "customer_id", req.CustomerID, "total_amount", req.TotalAmount)

	respondJSON(w, http.StatusCreated, map[string]any{
		"status":  "created",
		"message": "order placed and published to orders.exchange",
		"order":   req,
	})
}

// GET /orders
func (h *Handler) handleGetOrders(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		respondJSON(w, http.StatusOK, []any{})
		return
	}

	orders, err := h.store.GetOrders(r.Context())
	if err != nil {
		h.logger.Error("webshop.get_orders_failed", "error", err)
		respondError(w, http.StatusInternalServerError, "failed to fetch orders: "+err.Error())
		return
	}

	if orders == nil {
		orders = []OrderWithItems{}
	}

	respondJSON(w, http.StatusOK, orders)
}

// POST /test/inject-raw
// Accepts any arbitrary JSON body or raw bytes and publishes directly to orders.exchange
func (h *Handler) handleInjectRaw(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		respondError(w, http.StatusBadRequest, "failed to read body: "+err.Error())
		return
	}

	if h.publisher == nil {
		respondError(w, http.StatusInternalServerError, "publisher not initialized")
		return
	}

	contentType := r.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/json"
	}

	if err := h.publisher.PublishRaw(r.Context(), "orders.exchange", "", body, contentType); err != nil {
		h.logger.Error("webshop.inject_raw_failed", "error", err)
		respondError(w, http.StatusInternalServerError, "failed to publish raw message: "+err.Error())
		return
	}

	h.logger.Info("webshop.test_inject_raw", "payload_len", len(body))
	respondJSON(w, http.StatusOK, map[string]any{
		"status":  "published",
		"message": "raw payload published to orders.exchange",
		"size":    len(body),
	})
}

// POST /test/inject-duplicate
func (h *Handler) handleInjectDuplicate(w http.ResponseWriter, r *http.Request) {
	var req contracts.OrderPlaced
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid json payload: "+err.Error())
		return
	}

	if req.OrderID == "" {
		req.OrderID = fmt.Sprintf("ORD-%d", time.Now().UnixNano()%1000000)
	}

	payload, err := json.Marshal(req)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "marshal event: "+err.Error())
		return
	}

	if h.publisher == nil {
		respondError(w, http.StatusInternalServerError, "publisher not initialized")
		return
	}

	ctx := r.Context()
	// Publish 1st copy
	if err := h.publisher.PublishOrderPlaced(ctx, payload); err != nil {
		respondError(w, http.StatusInternalServerError, "failed publishing copy 1: "+err.Error())
		return
	}

	// Publish 2nd copy immediately
	if err := h.publisher.PublishOrderPlaced(ctx, payload); err != nil {
		respondError(w, http.StatusInternalServerError, "failed publishing copy 2: "+err.Error())
		return
	}

	h.logger.Info("webshop.test_inject_duplicate", "order_id", req.OrderID)
	respondJSON(w, http.StatusOK, map[string]any{
		"status":   "published_twice",
		"message":  "order published 2x to test idempotency",
		"order_id": req.OrderID,
	})
}

// POST /test/inject-corrupt
func (h *Handler) handleInjectCorrupt(w http.ResponseWriter, r *http.Request) {
	scenario := r.URL.Query().Get("type")
	var corruptPayload []byte

	switch scenario {
	case "malformed":
		corruptPayload = []byte(`{"order_id": "ORD-BROKEN", "items": [{"sku": "PRD-01", "qty": }`) // broken syntax
	case "negative_amount":
		corruptPayload = []byte(`{"order_id": "ORD-NEG", "customer_id": "CUST-99", "items": [{"sku": "PRD-01", "qty": 1, "price": -100}], "total_amount": -100, "shipping_address": "Bandung"}`)
	case "empty_fields":
		corruptPayload = []byte(`{"order_id": "", "customer_id": "", "items": [], "total_amount": 0, "shipping_address": ""}`)
	default:
		// Default corrupted payload
		corruptPayload = []byte(`{"order_id": "ORD-CORRUPT", "customer_id": "CUST-BAD", "items": [], "total_amount": -5000, "shipping_address": ""}`)
	}

	if h.publisher == nil {
		respondError(w, http.StatusInternalServerError, "publisher not initialized")
		return
	}

	if err := h.publisher.PublishRaw(r.Context(), "orders.exchange", "", corruptPayload, "application/json"); err != nil {
		h.logger.Error("webshop.inject_corrupt_failed", "error", err)
		respondError(w, http.StatusInternalServerError, "failed publishing corrupt message: "+err.Error())
		return
	}

	h.logger.Info("webshop.test_inject_corrupt", "scenario", scenario, "payload", string(corruptPayload))
	respondJSON(w, http.StatusOK, map[string]any{
		"status":   "published_corrupt",
		"scenario": scenario,
		"message":  "corrupt payload published to orders.exchange, expected to route to DLQ",
		"payload":  string(corruptPayload),
	})
}

func validateOrder(order contracts.OrderPlaced) error {
	var problems []string
	if strings.TrimSpace(order.OrderID) == "" {
		problems = append(problems, "order_id is required")
	}
	if strings.TrimSpace(order.CustomerID) == "" {
		problems = append(problems, "customer_id is required")
	}
	if strings.TrimSpace(order.ShippingAddress) == "" {
		problems = append(problems, "shipping_address is required")
	}
	if len(order.Items) == 0 {
		problems = append(problems, "items cannot be empty")
	}

	var sum int64
	for i, it := range order.Items {
		if strings.TrimSpace(it.SKU) == "" {
			problems = append(problems, fmt.Sprintf("items[%d].sku is required", i))
		}
		if it.Qty <= 0 {
			problems = append(problems, fmt.Sprintf("items[%d].qty must be > 0", i))
		}
		if it.Price < 0 {
			problems = append(problems, fmt.Sprintf("items[%d].price must be >= 0", i))
		}
		sum += int64(it.Qty) * it.Price
	}

	if order.TotalAmount <= 0 {
		problems = append(problems, "total_amount must be > 0")
	} else if len(order.Items) > 0 && sum != order.TotalAmount {
		problems = append(problems, fmt.Sprintf("total_amount (%d) does not match sum of items (%d)", order.TotalAmount, sum))
	}

	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

func respondJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func respondError(w http.ResponseWriter, status int, message string) {
	respondJSON(w, status, map[string]string{"error": message})
}
