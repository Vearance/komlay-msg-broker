package shipping

import "github.com/Vearance/komlay-msg-broker/backend/internal/contracts"

// ShipmentRequest is an order in the field names of the courier partner
// KirimCepat, which differ from KilatMart's order_placed contract.
type ShipmentRequest struct {
	NomorPesanan string   `json:"nomor_pesanan"`
	IDPelanggan  string   `json:"id_pelanggan"`
	AlamatTujuan string   `json:"alamat_tujuan"`
	DaftarBarang []Barang `json:"daftar_barang"`
	NilaiBarang  int64    `json:"nilai_barang"`
}

// Barang is one line item in a KirimCepat shipment.
type Barang struct {
	KodeBarang string `json:"kode_barang"`
	Jumlah     int    `json:"jumlah"`
}

// ToKirimCepat translates a KilatMart order into KirimCepat's format.
func ToKirimCepat(order contracts.OrderPlaced) ShipmentRequest {
	items := make([]Barang, 0, len(order.Items))
	for _, item := range order.Items {
		items = append(items, Barang{KodeBarang: item.SKU, Jumlah: item.Qty})
	}
	return ShipmentRequest{
		NomorPesanan: order.OrderID,
		IDPelanggan:  order.CustomerID,
		AlamatTujuan: order.ShippingAddress,
		DaftarBarang: items,
		NilaiBarang:  order.TotalAmount,
	}
}
