package billing

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Vearance/komlay-msg-broker/backend/internal/contracts"
)

// RecordCharge returns created=false when the order was already charged.
type Store interface {
	RecordCharge(ctx context.Context, order contracts.OrderPlaced) (created bool, err error)
}

// The order_id primary key keeps the insert idempotent, even when deliveries race.
type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

func (s *PostgresStore) RecordCharge(ctx context.Context, order contracts.OrderPlaced) (bool, error) {
	tag, err := s.pool.Exec(ctx,
		`INSERT INTO billing_charges (order_id, customer_id, amount)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (order_id) DO NOTHING`,
		order.OrderID, order.CustomerID, order.TotalAmount,
	)
	if err != nil {
		return false, fmt.Errorf("insert billing charge %s: %w", order.OrderID, err)
	}
	return tag.RowsAffected() == 1, nil
}
