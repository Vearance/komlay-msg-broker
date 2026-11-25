package contracts

// OrderPlaced is the event published after a shop order is accepted.
type OrderPlaced struct {
	OrderID         string      `json:"order_id"`
	CustomerID      string      `json:"customer_id"`
	Items           []OrderItem `json:"items"`
	TotalAmount     int64       `json:"total_amount"`
	ShippingAddress string      `json:"shipping_address"`
}

// OrderItem is one SKU and quantity in an order_placed event.
type OrderItem struct {
	SKU   string `json:"sku"`
	Qty   int    `json:"qty"`
	Price int64  `json:"price"`
}
