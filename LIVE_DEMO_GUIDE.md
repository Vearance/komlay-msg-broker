# Panduan Live Demo Manual: Track B3 (RabbitMQ Messaging)

Panduan ini disusun untuk demonstrasi langsung (*Live Demo* ~3 menit) di depan kelas/dosen menggunakan kombinasi **Terminal (`curl`)** dan **RabbitMQ Management Dashboard (Browser)** agar audiens dapat melihat visualisasi pergerakan pesan secara *real-time*.

---

## 0. Persiapan & Reset Sebelum Demo (WAJIB)

> [!IMPORTANT]
> **Jebakan Demo Ulang (Gladi vs Demo Asli):**
> Tabel `orders` dan `billing_charges` menyimpan data secara permanen di volume Docker. Jika Anda sudah pernah latihan/gladi sebelumnya dengan ID seperti `ORD-LIVE-01` atau `ORD-DUP-999`, menjalankan ulang tanpa reset akan menghasilkan **HTTP 500 (duplicate key)** pada Skenario 1 dan **`duplicate_ignored` dua kali (tanpa `charged`)** pada Skenario 2.

### A. Reset Bersih & Nyalakan Sistem
Selalu jalankan perintah reset volume ini sebelum memulai latihan atau sebelum maju presentasi:
```bash
docker compose down -v && docker compose up -d
```
Pastikan seluruh 5 container sudah berstatus `healthy`:
```bash
docker compose ps
```

### B. Buka RabbitMQ Management UI di Browser
Buka browser dan arahkan ke:
- **URL**: `http://localhost:15672`
- **Username**: `kilatmart`
- **Password**: `kilatmart_dev_only`
- Buka tab menu atas: **`Queues and Streams`** (Anda akan melihat 3 antrean: `billing.queue`, `shipping.queue`, dan `billing.dlq`).

---

## Skenario 1: Resiliency & Durability (Temporal Decoupling)

> **Tujuan:** Membuktikan bahwa Web Shop dan Billing telah ter-decouple secara waktu (*temporal decoupling*). Saat service Billing mati, checkout Web Shop tetap berhasil (`201 Created`), pesan tersimpan aman di broker (`billing.queue`), sementara Shipping tetap jalan memproses pengiriman secara paralel.

### Langkah 1: Matikan container Billing
Jalankan di terminal:
```bash
docker compose stop billing
```
*Tunjukkan ke audiens:* Container Billing sudah mati.

---

### Langkah 2: Buat 2 order baru di Web Shop saat Billing mati
Jalankan order pertama (`ORD-LIVE-01`):
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
Jalankan order kedua (`ORD-LIVE-02`):
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
1. Web Shop langsung merespons **`HTTP/1.1 201 Created`** seketika tanpa *blocking* / tanpa *timeout*.
2. Pindah ke tab browser **RabbitMQ UI (`Queues and Streams`)** *(tunggu ~5 detik agar statistik UI ter-refresh)*:
   - Kolom **Ready** pada `billing.queue` bertambah menjadi **`2`** (pesan tersimpan aman di antrean broker).
   - Sementara itu, `shipping.queue` tetap **`0`** karena Shipping Service tetap hidup dan langsung memproses paket secara asinkron.
3. **Verifikasi Shipping Consumer (Transformasi Field KirimCepat):**
   Tunjukkan bahwa meskipun Billing mati, Shipping Service sudah menerima dan mentransformasi payload `ORD-LIVE-01` ke skema bahasa Indonesia milik mitra **KirimCepat**:
   ```bash
   docker compose logs shipping | grep ORD-LIVE-01
   ```
   Output aktual dari kode:
   ```json
   {"time":"...","level":"INFO","msg":"shipping.dispatched","order_id":"ORD-LIVE-01","partner":"KirimCepat","payload":{"nomor_pesanan":"ORD-LIVE-01","id_pelanggan":"CUST-88","alamat_tujuan":"Jl. Ganesha No. 10, Bandung","daftar_barang":[{"kode_barang":"PRD-01","jumlah":2}],"nilai_barang":50000}}
   ```

---

### Langkah 3: Nyalakan kembali container Billing
```bash
docker compose start billing
```

*Tunjukkan ke audiens:*
- Tunggu ~5 detik hingga container `billing` healthy dan UI RabbitMQ memperbarui angka statistik: antrean **Ready** di `billing.queue` otomatis **ter-drain menjadi `0`**!
- Cek log Billing di terminal:
```bash
docker compose logs billing | grep ORD-LIVE
```
Log aktual akan mencetak bahwa kedua order yang tertunda tadi langsung ditagih tanpa ada yang hilang:
```json
{"time":"...","level":"INFO","msg":"billing.charged","order_id":"ORD-LIVE-01","customer_id":"CUST-88","amount":50000}
{"time":"...","level":"INFO","msg":"billing.charged","order_id":"ORD-LIVE-02","customer_id":"CUST-99","amount":15000}
```

---

## Skenario 2: Idempotent Consumer (Anti Double Charge)

> **Tujuan:** Menunjukkan bahwa jika broker/jaringan mengirim pesan order dengan `order_id` yang sama lebih dari sekali (duplikat/retry), consumer mengenali duplikasi dan mengabaikan proses tagihan kedua (*no double charge*).

### Langkah: Kirim order duplikat berturut-turut
Gunakan endpoint helper `/test/inject-duplicate` yang mem-publish event `ORD-DUP-999` sebanyak 2x berturut-turut ke `orders.exchange` (endpoint helper membalas **`HTTP/1.1 200 OK`**):
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
1. Periksa log Billing Consumer untuk `ORD-DUP-999`:
```bash
docker compose logs billing | grep ORD-DUP-999
```
Output aktual membuktikan salinan pertama di-`charged` dan salinan kedua di-`duplicate_ignored`:
```json
{"time":"...","level":"INFO","msg":"billing.charged","order_id":"ORD-DUP-999","customer_id":"CUST-IDEMP","amount":40000}
{"time":"...","level":"INFO","msg":"billing.duplicate_ignored","order_id":"ORD-DUP-999"}
```

2. (Opsional) Shipping Consumer juga menerapkan idempotency in-memory:
```bash
docker compose logs shipping | grep ORD-DUP-999
```
```json
{"time":"...","level":"INFO","msg":"shipping.dispatched","order_id":"ORD-DUP-999","partner":"KirimCepat","payload":{"nomor_pesanan":"ORD-DUP-999","id_pelanggan":"CUST-IDEMP","alamat_tujuan":"Jl. Dipatiukur No. 8, Bandung","daftar_barang":[{"kode_barang":"PRD-03","jumlah":1}],"nilai_barang":40000}}
{"time":"...","level":"INFO","msg":"shipping.duplicate_ignored","order_id":"ORD-DUP-999"}
```

3. Buktikan di database PostgreSQL bahwa baris tagihan `ORD-DUP-999` hanya tercatat **1 baris**:
```bash
docker compose exec -T postgres psql -U kilatmart -d kilatmart -c "SELECT * FROM billing_charges WHERE order_id = 'ORD-DUP-999';"
```

---

## Skenario 3: Dead-Letter Queue (DLQ Routing)

> **Tujuan:** Menunjukkan bahwa pesan cacat/korup tidak menyebabkan antrean macet (*poison pill / infinite retry*). Pesan gagal validasi langsung di-`nack(requeue=false)` oleh Billing dan otomatis dialihkan RabbitMQ melalui `billing.dlx` ke `billing.dlq`.

### Langkah 1: Tunjukkan status awal `billing.dlq` di browser
Buka browser di RabbitMQ UI (`Queues and Streams`):
- Lihat baris **`billing.dlq`** (jika baru di-reset, jumlah pesan Ready = **`0`**; jika sudah ada sisa uji sebelumnya, catat jumlah awalnya).

---

### Langkah 2: Injeksi 2 pesan rusak / cacat
1. Kirim pesan korup (syntax JSON sengaja terpotong/rusak — membalas `HTTP/1.1 200 OK`):
```bash
curl -i -X POST "http://localhost:8080/test/inject-corrupt?type=malformed"
```

2. Kirim pesan cacat aturan bisnis (harga item dan total tagihan bernilai negatif `-100` — membalas `HTTP/1.1 200 OK`):
```bash
curl -i -X POST "http://localhost:8080/test/inject-corrupt?type=negative_amount"
```

---

### Langkah 3: Buktikan pesan ditolak dan masuk ke `billing.dlq`
1. Cek log penolakan di Billing Consumer:
```bash
docker compose logs billing | grep billing.rejected
```
Output aktual (pesan pertama memiliki `"order_id":""` karena gagal parse JSON, pesan kedua `"order_id":"ORD-NEG"` gagal validasi nilai negatif):
```json
{"time":"...","level":"WARN","msg":"billing.rejected","order_id":"","reason":"invalid order: malformed JSON: invalid character '}' looking for beginning of value"}
{"time":"...","level":"WARN","msg":"billing.rejected","order_id":"ORD-NEG","reason":"invalid order: items[0].price is negative; total_amount must be positive"}
```

2. **Tunjukkan di Browser RabbitMQ UI:**
   - Masuk ke tab **`Queues and Streams`** *(tunggu ~5 detik hingga angka ter-update)*.
   - Jumlah pesan **Ready** pada **`billing.dlq`** bertambah **`2`** (sementara `billing.queue` tetap `0` karena pesan tidak di-requeue).
   - Klik **`billing.dlq`** $\rightarrow$ scroll ke bagian **"Get messages"** $\rightarrow$ isi `Messages: 2` $\rightarrow$ klik tombol **"Get Message(s)"**.
   - Audiens dapat melihat langsung payload JSON rusak (`{"order_id": "ORD-BROKEN", "items": [{"sku": "PRD-01", "qty": }`) dan payload nilai negatif (`ORD-NEG`) beserta header `x-death` (`reason: rejected`, `queue: billing.queue`).
