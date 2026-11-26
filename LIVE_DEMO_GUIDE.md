# Panduan Live Demo Manual: Track B3 (RabbitMQ Messaging)

Panduan ini disusun untuk demonstrasi langsung (*Live Demo* ~3 menit) di depan kelas/dosen menggunakan kombinasi **Terminal (`curl`)** dan **RabbitMQ Management Dashboard (Browser)** agar audiens dapat melihat visualisasi pergerakan pesan secara *real-time*.

---

## 0. Persiapan Sebelum Demo (Setup 1 Kali)

### A. Nyalakan Sistem
Buka terminal dan jalankan seluruh container:
```bash
docker compose up -d
```
Pastikan seluruh container dalam status `healthy`:
```bash
docker compose ps
```

### B. Buka RabbitMQ Management UI di Browser
Buka browser dan arahkan ke:
- **URL**: `http://localhost:15672`
- **Username**: `kilatmart`
- **Password**: `kilatmart_dev_only`
- Buka tab menu: **`Queues`** (Anda akan melihat 3 antrean utama: `billing.queue`, `shipping.queue`, dan `billing.dlq`).

---

## Skenario 1: Resiliency & Durability (Temporal Decoupling)

> **Tujuan:** Membuktikan bahwa Web Shop dan Billing telah ter-decouple secara waktu (*temporal decoupling*). Saat service Billing mati, checkout Web Shop tidak terganggu, antrean tersimpan aman di broker (durable), dan langsung diproses saat Billing menyala kembali tanpa ada data hilang.

### Langkah 1: Matikan container Billing
Jalankan di terminal:
```bash
docker compose stop billing
```
*Tunjukkan ke audiens:* Container Billing sudah mati (`docker compose ps` menunjukkan billing berhenti).

---

### Langkah 2: Buat 2 order baru di Web Shop saat Billing mati
Jalankan order pertama:
```bash
curl -i -X POST http://localhost:8080/orders \
  -H "Content-Type: application/json" \
  -d '{
    "order_id": "ORD-LIVE-01",
    "customer_id": "CUST-88",
    "items": [{"sku": "PRD-01", "qty": 2, "price": 25000}],
    "total_amount": 50000,
    "shipping_address": "Jl. Ganesha No. 10, Bandung"
  }'
```
Jalankan order kedua:
```bash
curl -i -X POST http://localhost:8080/orders \
  -H "Content-Type: application/json" \
  -d '{
    "order_id": "ORD-LIVE-02",
    "customer_id": "CUST-99",
    "items": [{"sku": "PRD-02", "qty": 1, "price": 15000}],
    "total_amount": 15000,
    "shipping_address": "Jl. Dago No. 15, Bandung"
  }'
```

*Tunjukkan ke audiens:* 
- Web Shop langsung merespons **`HTTP/1.1 201 Created`** seketika tanpa *blocking* / tanpa *timeout*.
- Pindah ke tab browser **RabbitMQ UI (`Queues`)**:
  - Kolom **Ready** pada `billing.queue` bertambah menjadi **`2`** (pesan tersimpan aman di antrean broker).
  - Sementara itu, `shipping.queue` tetap **`0`** karena Shipping Service tetap jalan memproses paket secara asinkron.

---

### Langkah 3: Nyalakan kembali container Billing
```bash
docker compose start billing
```

*Tunjukkan ke audiens:*
- Refresh halaman browser RabbitMQ: antrean di `billing.queue` langsung otomatis **ter-drain menjadi `0`**!
- Cek log Billing di terminal:
```bash
docker compose logs --tail 4 billing
```
Log akan mencetak event bahwa kedua order berhasil ditagih:
```json
{"level":"INFO","msg":"billing.charged","order_id":"ORD-LIVE-01","amount":50000}
{"level":"INFO","msg":"billing.charged","order_id":"ORD-LIVE-02","amount":15000}
```

---

## Skenario 2: Idempotent Consumer (Anti Double Charge)

> **Tujuan:** Menunjukkan bahwa jika jaringan atau client mengirim ulang pesan order yang sama (duplikat), konsumen mengenali duplikasi dan tidak melakukan pemotongan saldo ganda (*no double charge*).

### Langkah: Kirim order duplikat berturut-turut
Gunakan endpoint helper `/test/inject-duplicate` yang mengirimkan order dengan ID sama sebanyak 2x sekaligus ke broker:
```bash
curl -i -X POST http://localhost:8080/test/inject-duplicate \
  -H "Content-Type: application/json" \
  -d '{
    "order_id": "ORD-DUP-999",
    "customer_id": "CUST-IDEMP",
    "items": [{"sku": "PRD-03", "qty": 1, "price": 40000}],
    "total_amount": 40000,
    "shipping_address": "Jl. Dipatiukur No. 8, Bandung"
  }'
```

*Tunjukkan ke audiens:*
Periksa log Billing Consumer:
```bash
docker compose logs --tail 4 billing
```
Output log membuktikan pesan pertama diproses, pesan kedua diabaikan:
```json
{"level":"INFO","msg":"billing.charged","order_id":"ORD-DUP-999","amount":40000}
{"level":"INFO","msg":"billing.duplicate_ignored","order_id":"ORD-DUP-999"}
```

Buktikan di database bahwa baris tagihan hanya tercatat **1 kali**:
```bash
docker compose exec -T postgres psql -U kilatmart -d kilatmart -c "SELECT * FROM billing_charges WHERE order_id = 'ORD-DUP-999';"
```

---

## Skenario 3: Dead-Letter Queue (DLQ Routing)

> **Tujuan:** Menunjukkan bahwa pesan cacat/korup tidak menyebabkan antrean macet (*infinite retry / poison pill*). Pesan yang rusak langsung di-reject oleh konsumen dan otomatis dialihkan oleh RabbitMQ ke `billing.dlq`.

### Langkah 1: Tunjukkan status awal `billing.dlq` di browser
Buka browser di RabbitMQ UI:
- Klik antrean **`billing.dlq`**.
- Catat jumlah pesan sebelum injeksi.

---

### Langkah 2: Injeksi pesan rusak / cacat
Kirim pesan korup (JSON sengaja dibuat rusak syntax-nya):
```bash
curl -i -X POST "http://localhost:8080/test/inject-corrupt?type=malformed"
```

Kirim pesan cacat skema (harga/total bernilai negatif):
```bash
curl -i -X POST "http://localhost:8080/test/inject-corrupt?type=negative_amount"
```

---

### Langkah 3: Buktikan pesan masuk ke DLQ
1. Cek log Billing Consumer di terminal:
```bash
docker compose logs --tail 4 billing
```
Terlihat log reject tanpa requeue:
```json
{"level":"WARN","msg":"billing.rejected","reason":"invalid order: malformed JSON: ..."}
{"level":"WARN","msg":"billing.rejected","order_id":"ORD-NEG","reason":"total_amount must be positive"}
```

2. **Tunjukkan di Browser RabbitMQ UI:**
   - Masuk ke menu **Queues** $\rightarrow$ **`billing.dlq`**.
   - Terlihat angka pesan bertambah **2**.
   - Scroll ke bawah ke panel **"Get messages"**, klik tombol **Get Message(s)**:
   - Audiens bisa melihat langsung payload JSON rusak yang tertampung di dalam DLQ untuk dianalisis oleh engineer.

---

## Verifikasi Bonus: Integrasi Partner Shipping (KirimCepat)

> **Tujuan:** Membuktikan decoupling fanout di mana event order yang sama ditransformasi sesuai skema mitra KirimCepat.

Jalankan perintah berikut di terminal:
```bash
docker compose logs --tail 4 shipping
```
Output log menunjukkan nama field telah bertransformasi ke spesifikasi mitra:
```json
{"level":"INFO","msg":"shipping.dispatched_to_kirimcepat","booking_id":"ORD-LIVE-01","tracking_ref":"KC-ORD-LIVE-01","recipient":"CUST-88","parcel_count":2}
```
