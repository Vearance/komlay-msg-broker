package shipping

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	"github.com/Vearance/komlay-msg-broker/backend/internal/contracts"
)

const validOrder = `{"order_id":"ORD-1001","customer_id":"CUST-88","items":[{"sku":"PRD-01","qty":2,"price":25000}],"total_amount":50000,"shipping_address":"Jl. Ganesha No. 10, Bandung"}`

func TestToKirimCepatUsesPartnerFieldNames(t *testing.T) {
	tests := []struct {
		name  string
		order contracts.OrderPlaced
		want  string
	}{
		{
			name: "assignment payload",
			order: contracts.OrderPlaced{
				OrderID:         "ORD-1001",
				CustomerID:      "CUST-88",
				Items:           []contracts.OrderItem{{SKU: "PRD-01", Qty: 2, Price: 25000}},
				TotalAmount:     50000,
				ShippingAddress: "Jl. Ganesha No. 10, Bandung",
			},
			want: `{"nomor_pesanan":"ORD-1001","id_pelanggan":"CUST-88","alamat_tujuan":"Jl. Ganesha No. 10, Bandung","daftar_barang":[{"kode_barang":"PRD-01","jumlah":2}],"nilai_barang":50000}`,
		},
		{
			name: "several items keep their order",
			order: contracts.OrderPlaced{
				OrderID:         "ORD-2",
				CustomerID:      "CUST-1",
				Items:           []contracts.OrderItem{{SKU: "A", Qty: 1, Price: 10}, {SKU: "B", Qty: 3, Price: 5}},
				TotalAmount:     25,
				ShippingAddress: "Bandung",
			},
			want: `{"nomor_pesanan":"ORD-2","id_pelanggan":"CUST-1","alamat_tujuan":"Bandung","daftar_barang":[{"kode_barang":"A","jumlah":1},{"kode_barang":"B","jumlah":3}],"nilai_barang":25}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(ToKirimCepat(tt.order))
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(got) != tt.want {
				t.Fatalf("got  %s\nwant %s", got, tt.want)
			}
		})
	}
}

func TestHandle(t *testing.T) {
	tests := []struct {
		name       string
		deliveries []string
		want       []Outcome
	}{
		{"valid order", []string{validOrder}, []Outcome{Dispatched}},
		{"same order twice", []string{validOrder, validOrder}, []Outcome{Dispatched, Duplicate}},
		{"malformed JSON", []string{`{"order_id": "ORD-BROKEN", "items": [{"sku": "PRD-01", "qty": }`}, []Outcome{Rejected}},
		{"empty fields", []string{`{"order_id": "", "customer_id": "", "items": [], "total_amount": 0, "shipping_address": ""}`}, []Outcome{Rejected}},
		{"negative amount", []string{`{"order_id": "ORD-NEG", "customer_id": "CUST-99", "items": [{"sku": "PRD-01", "qty": 1, "price": -100}], "total_amount": -100, "shipping_address": "Bandung"}`}, []Outcome{Rejected}},
		{"zero qty", []string{`{"order_id":"ORD-3","customer_id":"C","items":[{"sku":"P","qty":0,"price":1}],"total_amount":1,"shipping_address":"x"}`}, []Outcome{Rejected}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))
			for i, body := range tt.deliveries {
				if got := h.Handle(context.Background(), []byte(body)); got != tt.want[i] {
					t.Fatalf("delivery %d = %s, want %s", i+1, got, tt.want[i])
				}
			}
		})
	}
}
