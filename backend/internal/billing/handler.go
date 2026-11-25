package billing

import (
	"context"
	"log/slog"
)

// Outcome tells the consumer how to settle a delivery.
type Outcome int

const (
	Charged   Outcome = iota // ack
	Duplicate                // ack, no second charge
	Rejected                 // nack without requeue, lands in billing.dlq
	Retry                    // transient failure: nack with requeue
)

func (o Outcome) String() string {
	switch o {
	case Charged:
		return "charged"
	case Duplicate:
		return "duplicate"
	case Rejected:
		return "rejected"
	case Retry:
		return "retry"
	default:
		return "unknown"
	}
}

type Handler struct {
	store  Store
	logger *slog.Logger
}

func NewHandler(store Store, logger *slog.Logger) *Handler {
	return &Handler{store: store, logger: logger}
}

func (h *Handler) Handle(ctx context.Context, body []byte) Outcome {
	order, err := DecodeOrder(body)
	if err != nil {
		// DecodeOrder only fails with ErrInvalidOrder: retrying cannot help.
		h.logger.Warn("billing.rejected", "order_id", order.OrderID, "reason", err.Error())
		return Rejected
	}

	created, err := h.store.RecordCharge(ctx, order)
	if err != nil {
		h.logger.Error("billing.store_failed", "order_id", order.OrderID, "error", err)
		return Retry
	}
	if !created {
		h.logger.Info("billing.duplicate_ignored", "order_id", order.OrderID)
		return Duplicate
	}

	h.logger.Info("billing.charged",
		"order_id", order.OrderID,
		"customer_id", order.CustomerID,
		"amount", order.TotalAmount,
	)
	return Charged
}
