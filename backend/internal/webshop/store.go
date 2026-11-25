package webshop

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Vearance/komlay-msg-broker/backend/internal/contracts"
)

type OrderWithItems struct {
	OrderID         string                `json:"order_id"`
	CustomerID      string                `json:"customer_id"`
	TotalAmount     int64                 `json:"total_amount"`
	ShippingAddress string                `json:"shipping_address"`
	Status          string                `json:"status"`
	CreatedAt       string                `json:"created_at"`
	Items           []contracts.OrderItem `json:"items"`
}

type Store interface {
	CreateOrder(ctx context.Context, order contracts.OrderPlaced) error
	GetOrders(ctx context.Context) ([]OrderWithItems, error)
	GetOrderByID(ctx context.Context, id string) (*OrderWithItems, error)
}

type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

func (s *PostgresStore) CreateOrder(ctx context.Context, order contracts.OrderPlaced) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx,
		`INSERT INTO orders (order_id, customer_id, total_amount, shipping_address, status)
		 VALUES ($1, $2, $3, $4, 'placed')`,
		order.OrderID, order.CustomerID, order.TotalAmount, order.ShippingAddress,
	)
	if err != nil {
		return fmt.Errorf("insert order: %w", err)
	}

	for _, item := range order.Items {
		_, err = tx.Exec(ctx,
			`INSERT INTO order_items (order_id, sku, qty, unit_price)
			 VALUES ($1, $2, $3, $4)`,
			order.OrderID, item.SKU, item.Qty, item.Price,
		)
		if err != nil {
			return fmt.Errorf("insert order item %s: %w", item.SKU, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

func (s *PostgresStore) GetOrders(ctx context.Context) ([]OrderWithItems, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT order_id, customer_id, total_amount, shipping_address, status, created_at::text
		 FROM orders
		 ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("query orders: %w", err)
	}
	defer rows.Close()

	var orders []OrderWithItems
	orderMap := make(map[string]*OrderWithItems)

	for rows.Next() {
		var o OrderWithItems
		if err := rows.Scan(&o.OrderID, &o.CustomerID, &o.TotalAmount, &o.ShippingAddress, &o.Status, &o.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan order: %w", err)
		}
		o.Items = make([]contracts.OrderItem, 0)
		orders = append(orders, o)
		orderMap[o.OrderID] = &orders[len(orders)-1]
	}

	if len(orders) == 0 {
		return orders, nil
	}

	itemRows, err := s.pool.Query(ctx,
		`SELECT order_id, sku, qty, unit_price
		 FROM order_items
		 ORDER BY order_id, sku`,
	)
	if err != nil {
		return nil, fmt.Errorf("query order items: %w", err)
	}
	defer itemRows.Close()

	for itemRows.Next() {
		var orderID string
		var item contracts.OrderItem
		if err := itemRows.Scan(&orderID, &item.SKU, &item.Qty, &item.Price); err != nil {
			return nil, fmt.Errorf("scan order item: %w", err)
		}
		if ord, ok := orderMap[orderID]; ok {
			ord.Items = append(ord.Items, item)
		}
	}

	return orders, nil
}

func (s *PostgresStore) GetOrderByID(ctx context.Context, id string) (*OrderWithItems, error) {
	var o OrderWithItems
	err := s.pool.QueryRow(ctx,
		`SELECT order_id, customer_id, total_amount, shipping_address, status, created_at::text
		 FROM orders
		 WHERE order_id = $1`,
		id,
	).Scan(&o.OrderID, &o.CustomerID, &o.TotalAmount, &o.ShippingAddress, &o.Status, &o.CreatedAt)
	if err != nil {
		return nil, err
	}

	itemRows, err := s.pool.Query(ctx,
		`SELECT sku, qty, unit_price
		 FROM order_items
		 WHERE order_id = $1
		 ORDER BY sku`,
		id,
	)
	if err != nil {
		return nil, fmt.Errorf("query order items: %w", err)
	}
	defer itemRows.Close()

	o.Items = make([]contracts.OrderItem, 0)
	for itemRows.Next() {
		var item contracts.OrderItem
		if err := itemRows.Scan(&item.SKU, &item.Qty, &item.Price); err != nil {
			return nil, fmt.Errorf("scan order item: %w", err)
		}
		o.Items = append(o.Items, item)
	}

	return &o, nil
}
