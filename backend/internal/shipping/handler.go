package shipping

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/Vearance/komlay-msg-broker/backend/internal/contracts"
)

// Outcome tells the consumer how to settle a delivery.
type Outcome int

const (
	Dispatched Outcome = iota // ack
	Duplicate                 // ack, not sent to the partner again
	Rejected                  // nack without requeue; shipping.queue has no DLX, so it is dropped
)

func (o Outcome) String() string {
	switch o {
	case Dispatched:
		return "dispatched"
	case Duplicate:
		return "duplicate"
	case Rejected:
		return "rejected"
	default:
		return "unknown"
	}
}

// Handler is not safe for concurrent use: the consumer calls it from one goroutine.
type Handler struct {
	logger *slog.Logger
	// seen holds every dispatched order_id. It lives in memory, so it resets
	// when the service restarts. A real service would keep it in a durable
	// store, as billing does with the billing_charges primary key.
	seen map[string]struct{}
}

func NewHandler(logger *slog.Logger) *Handler {
	return &Handler{logger: logger, seen: map[string]struct{}{}}
}

func (h *Handler) Handle(_ context.Context, body []byte) Outcome {
	order, err := decodeOrder(body)
	if err != nil {
		h.logger.Warn("shipping.rejected", "order_id", order.OrderID, "reason", err.Error())
		return Rejected
	}

	if _, ok := h.seen[order.OrderID]; ok {
		h.logger.Info("shipping.duplicate_ignored", "order_id", order.OrderID)
		return Duplicate
	}
	h.seen[order.OrderID] = struct{}{}

	// No real partner call: logging the translated payload is the dispatch.
	h.logger.Info("shipping.dispatched",
		"order_id", order.OrderID,
		"partner", "KirimCepat",
		"payload", ToKirimCepat(order),
	)
	return Dispatched
}

// decodeOrder checks only what a courier needs. Billing validates the money
// side; shipping also refuses non-positive totals and quantities so a corrupt
// order never reaches the partner.
func decodeOrder(body []byte) (contracts.OrderPlaced, error) {
	var order contracts.OrderPlaced
	if err := json.Unmarshal(body, &order); err != nil {
		return order, fmt.Errorf("malformed JSON: %v", err)
	}

	var problems []string
	if strings.TrimSpace(order.OrderID) == "" {
		problems = append(problems, "order_id is empty")
	}
	if strings.TrimSpace(order.ShippingAddress) == "" {
		problems = append(problems, "shipping_address is empty")
	}
	if len(order.Items) == 0 {
		problems = append(problems, "items is empty")
	}
	for i, item := range order.Items {
		if item.Qty <= 0 {
			problems = append(problems, fmt.Sprintf("items[%d].qty must be positive", i))
		}
	}
	if order.TotalAmount <= 0 {
		problems = append(problems, "total_amount must be positive")
	}

	if len(problems) > 0 {
		return order, errors.New(strings.Join(problems, "; "))
	}
	return order, nil
}
