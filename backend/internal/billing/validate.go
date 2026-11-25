package billing

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Vearance/komlay-msg-broker/backend/internal/contracts"
)

// ErrInvalidOrder means retrying cannot help, so the message is dead-lettered.
var ErrInvalidOrder = errors.New("invalid order")

func DecodeOrder(body []byte) (contracts.OrderPlaced, error) {
	var order contracts.OrderPlaced
	if err := json.Unmarshal(body, &order); err != nil {
		return order, fmt.Errorf("%w: malformed JSON: %v", ErrInvalidOrder, err)
	}
	if err := Validate(order); err != nil {
		return order, err
	}
	return order, nil
}

func Validate(order contracts.OrderPlaced) error {
	var problems []string
	if strings.TrimSpace(order.OrderID) == "" {
		problems = append(problems, "order_id is empty")
	}
	if strings.TrimSpace(order.CustomerID) == "" {
		problems = append(problems, "customer_id is empty")
	}
	if strings.TrimSpace(order.ShippingAddress) == "" {
		problems = append(problems, "shipping_address is empty")
	}
	if len(order.Items) == 0 {
		problems = append(problems, "items is empty")
	}

	var sum int64
	for i, item := range order.Items {
		if strings.TrimSpace(item.SKU) == "" {
			problems = append(problems, fmt.Sprintf("items[%d].sku is empty", i))
		}
		if item.Qty <= 0 {
			problems = append(problems, fmt.Sprintf("items[%d].qty must be positive", i))
		}
		if item.Price < 0 {
			problems = append(problems, fmt.Sprintf("items[%d].price is negative", i))
		}
		sum += int64(item.Qty) * item.Price
	}

	if order.TotalAmount <= 0 {
		problems = append(problems, "total_amount must be positive")
	} else if len(order.Items) > 0 && sum != order.TotalAmount {
		problems = append(problems, fmt.Sprintf("total_amount %d does not match items sum %d", order.TotalAmount, sum))
	}

	if len(problems) > 0 {
		return fmt.Errorf("%w: %s", ErrInvalidOrder, strings.Join(problems, "; "))
	}
	return nil
}
