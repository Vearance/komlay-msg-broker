# Application Integration - Messaging dengan RabbitMQ (Study Case KilatMart)
### IF4052 Komputasi Layanan

Kelompok 8:
| Nama | NIM |
|---|---|
| Angelina Efrina P. | 13523060 |
| Shanice Feodora T. | 13523097 |
| Dita Maheswari | 13523125 |
| Samantha Laqueenna G. | 13523138 |
| Nathaniel Christian | 13524122 |

## Daftar Isi
- [Deskripsi Umum](#deskripsi-umum)
- [Komponen](#komponen)
  - [Container](#container)
  - [Topologi RabbitMQ](#topologi-rabbitmq)
  - [Database](#database)
  - [Endpoint Web Shop](#endpoint-web-shop)
  - [Kontrak pesan `order_placed`](#kontrak-pesan-order_placed)
  - [Variabel koneksi](#variabel-koneksi)
  - [Struktur repositori](#struktur-repositori)
- [Alur Interaksi Antarkomponen](#alur-interaksi-antarkomponen)
  - [Arsitektur](#arsitektur)
  - [Perjalanan satu order](#perjalanan-satu-order)
  - [Keputusan ack/nack di consumer](#keputusan-acknack-di-consumer)
  - [Coupling yang dihilangkan](#coupling-yang-dihilangkan)
- [Pembagian Peran](#pembagian-peran)
- [Alur Pengerjaan](#alur-pengerjaan)
- [Prerequisites](#prerequisites)
- [Cara Menjalankan (Demo)](#cara-menjalankan-demo)
  - [1. Menjalankan stack](#1-menjalankan-stack)
  - [2. Demo otomatis dengan `demo.sh`](#2-demo-otomatis-dengan-demosh)
  - [3. Demo manual per skenario](#3-demo-manual-per-skenario)
  - [4. Stop, restart, dan reset](#4-stop-restart-dan-reset)
- [Skenario dan Hasil](#skenario-dan-hasil)
  - [Skenario 1: Resiliency dan durability](#skenario-1-resiliency-dan-durability)
  - [Skenario 2: Idempotent consumer](#skenario-2-idempotent-consumer)
  - [Skenario 3: Dead-letter queue](#skenario-3-dead-letter-queue)
  - [Eksperimen tambahan](#eksperimen-tambahan)
  - [Keterbatasan yang diketahui](#keterbatasan-yang-diketahui)
- [Troubleshooting](#troubleshooting)

---

## Deskripsi Umum
Masalah yang akan diselesaikan di KilatMart: **Web Shop memanggil Billing secara langsung** sehingga saat Billing mati, checkout ikut gagal. Tim memilih **Messaging**: Web Shop cukup menerbitkan event `order_placed` ke RabbitMQ, lalu Billing dan Shipping (mitra kurir KirimCepat) mengambilnya dari queue masing-masing, kapanpun mereka siap.

Berikut hal-hal yang wajib dibuktikan keberhasilannya (detail dapat dilihat pada [Skenario dan Hasil](#skenario-dan-hasil)):

1. **Resiliency:** Billing dimatikan, order tetap dibuat, Billing dinyalakan lagi, dan tidak ada order yang hilang.
2. **Idempotent consumer:** pesan yang sama dikirim dua kali tidak menghasilkan tagihan ganda.
3. **Dead-letter queue:** pesan rusak berakhir di `billing.dlq`.

Seluruh sistem berjalan dengan satu perintah,
```sh
docker compose up -d
```
,tanpa perlu membuat `.env` atau menginstal Go.

---

## Komponen

### Container

| Service | Image / Teknologi | Port di laptop | Fungsi |
|---|---|---|---|
| `rabbitmq` | `rabbitmq:4.1-management` | 5672 (AMQP), 15672 (UI) | Message broker. Topologi dimuat dari `rabbitmq/definitions.json` |
| `postgres` | `postgres:17-alpine` | 15432 | Database Web Shop (order) dan Billing (tagihan) |
| `web-shop` | Go, dibangun dari `backend/` | 8080 | REST API order dan publisher event `order_placed` |
| `billing` | Go, dibangun dari `backend/` | hanya di network Docker | Consumer `billing.queue`: tagihan idempotent dan rute ke DLQ |
| `shipping` | Go, dibangun dari `backend/` | hanya di network Docker | Consumer `shipping.queue`: menerjemahkan order ke format KirimCepat |

Ketiga service Go berasal dari satu modul Go (`backend/`) yang dibangun menjadi tiga image lewat build argument `SERVICE`.

### Topologi RabbitMQ

Didefinisikan di `rabbitmq/definitions.json` dan dimuat setiap kali broker start.

| Objek | Jenis | Keterangan |
|---|---|---|
| `orders.exchange` | exchange fanout, durable | Menyalin setiap pesan ke semua queue yang terikat |
| `billing.queue` | queue durable | Terikat ke `orders.exchange`. Pesan yang di-nack tanpa requeue dialihkan ke `billing.dlx` dengan routing key `billing.dead` |
| `shipping.queue` | queue durable | Terikat ke `orders.exchange`. Tidak punya DLX |
| `billing.dlx` | exchange direct, durable | Dead-letter exchange untuk Billing |
| `billing.dlq` | queue durable | Terikat ke `billing.dlx` dengan routing key `billing.dead`. Tempat pesan rusak disimpan untuk diperiksa |
| user `kilatmart` | administrator | Satu-satunya user broker, akses penuh ke vhost `/` |

### Database

Dibuat oleh script di `postgres/init/`, dijalankan berurutan sesuai nama file saat volume PostgreSQL masih kosong.

| Tabel | Pemilik | Isi awal | Keterangan |
|---|---|---|---|
| `products` | Web Shop | 3 produk (PRD-01, PRD-02, PRD-03) | Katalog |
| `orders` | Web Shop | 5 order (ORD-1001 s.d. ORD-1005) | Order yang diterima Web Shop |
| `order_items` | Web Shop | 8 baris | Item per order. `sku` wajib ada di `products` |
| `billing_charges` | Billing | kosong | `order_id` sebagai PRIMARY KEY adalah kunci idempotency. Tanpa foreign key ke `orders`, karena Billing hanya mengenal order dari pesan |

### Endpoint Web Shop

Base URL: `http://localhost:8080`

| Method dan path | Fungsi |
|---|---|
| `GET /healthz` | Health check |
| `POST /orders` | Validasi order, simpan ke PostgreSQL, publish `order_placed` (persistent). |
| `GET /orders` | Daftar order beserta item |
| `POST /test/inject-duplicate` | Publish order yang sama **dua kali** (skenario 2) |
| `POST /test/inject-corrupt?type=...` | Publish pesan rusak: `malformed`, `negative_amount`, `empty_fields` (skenario 3) |
| `POST /test/inject-raw` | Publish body apa adanya ke `orders.exchange` |

Endpoint `/test/*` hanya untuk demo dan pengujian. Endpoint ini langsung publish ke broker tanpa menyimpan ke database.

### Kontrak pesan `order_placed`

Satu-satunya kontrak antar-service, didefinisikan di `backend/internal/contracts/order_placed.go`:

```json
{
  "order_id": "ORD-1001",
  "customer_id": "CUST-88",
  "items": [{ "sku": "PRD-01", "qty": 2, "price": 25000 }],
  "total_amount": 50000,
  "shipping_address": "Jl. Ganesha No. 10, Bandung"
}
```

Shipping menerjemahkannya ke nama field KirimCepat:

| KilatMart | KirimCepat |
|---|---|
| `order_id` | `nomor_pesanan` |
| `customer_id` | `id_pelanggan` |
| `shipping_address` | `alamat_tujuan` |
| `items[].sku`, `items[].qty` | `daftar_barang[].kode_barang`, `daftar_barang[].jumlah` |
| `total_amount` | `nilai_barang` |

### Variabel koneksi

Compose memberi alamat broker dan database ke service lewat environment variable.

| Variabel | Nilai di dalam Compose | Diberikan ke |
|---|---|---|
| `AMQP_URL` | `amqp://kilatmart:kilatmart_dev_only@rabbitmq:5672/%2F` | web-shop, billing, shipping |
| `DATABASE_URL` | `postgres://kilatmart:kilatmart_dev_only@postgres:5432/kilatmart?sslmode=disable` | web-shop, billing |

- Host adalah nama service Compose (`rabbitmq`, `postgres`) dan port adalah port container. URL ini hanya berlaku di dalam network Docker. Dari laptop, pakai `localhost` dan port laptop (15432 untuk PostgreSQL).
- `%2F` adalah vhost `/` yang di-encode. `sslmode=disable` diperlukan karena PostgreSQL lokal tidak memakai TLS.
- Password RabbitMQ di `AMQP_URL` harus sama dengan user di `rabbitmq/definitions.json`.
- Kredensial ini **khusus pengembangan lokal**. Semua port hanya dibuka di `127.0.0.1`, sehingga tidak bisa diakses dari komputer lain.

### Struktur repositori

```text
docker-compose.yml              Seluruh stack: RabbitMQ, PostgreSQL, 3 service Go
rabbitmq/rabbitmq.conf          Memuat definitions.json saat broker start
rabbitmq/definitions.json       User, exchange, queue, binding, DLX
postgres/init/001_init.sql      Tabel Web Shop dan seed data
postgres/init/002_billing.sql   Tabel billing_charges
backend/cmd/<service>/          Entry point web-shop, billing, shipping
backend/internal/webshop/       REST API, publisher, akses DB
backend/internal/billing/       Consumer, validasi, idempotency, DLQ
backend/internal/shipping/      Consumer dan pemetaan ke KirimCepat
backend/internal/contracts/     Kontrak pesan order_placed
demo.sh                         Script demo tiga skenario wajib
.env.example                    Contoh konfigurasi port (opsional)
```

---

## Alur Interaksi Antarkomponen

### Arsitektur

```mermaid
flowchart LR
    C(["Klien / curl / demo.sh"]) -->|"POST /orders"| WS["Web Shop :8080"]
    WS -->|"INSERT orders"| PG[("PostgreSQL")]
    WS -->|"publish order_placed (persistent)"| EX{{"orders.exchange (fanout)"}}
    EX --> BQ[["billing.queue"]]
    EX --> SQ[["shipping.queue"]]
    BQ -->|consume| BI["Billing"]
    SQ -->|consume| SH["Shipping"]
    BI -->|"INSERT billing_charges"| PG
    BQ -.->|"nack, requeue=false"| DLX{{"billing.dlx (direct)"}}
    DLX -->|"billing.dead"| DLQ[["billing.dlq"]]
    SH -->|"log payload"| KC(["KirimCepat (simulasi)"])
```

Web Shop hanya mengenal `orders.exchange` dan tidak mengetahui banyak consumer, di mana mereka berjalan, atau apakah mereka sedang hidup. Exchange fanout menyalin setiap order ke dua queue, jadi Billing dan Shipping bekerja terpisah dan tidak saling menunggu.

### Perjalanan satu order

```mermaid
sequenceDiagram
    autonumber
    participant K as Klien
    participant W as Web Shop
    participant P as PostgreSQL
    participant R as RabbitMQ
    participant B as Billing
    participant S as Shipping
    K->>W: POST /orders (JSON)
    W->>W: validasi, total harus sama dengan jumlah qty x price
    W->>P: INSERT orders dan order_items (satu transaksi)
    W->>R: publish order_placed ke orders.exchange (persistent)
    W-->>K: 201 Created
    Note over K,W: Web Shop selesai di sini tanpa menunggu Billing atau Shipping
    R->>B: kirim dari billing.queue
    alt valid dan order_id baru
        B->>P: INSERT billing_charges ON CONFLICT DO NOTHING (1 baris)
        B->>R: ack
    else valid dan order_id sudah ditagih
        B->>R: ack, tagihan tidak dibuat lagi
    else pesan rusak
        B->>R: nack requeue=false, dialihkan ke billing.dlq
    else database error
        B->>R: nack requeue=true setelah jeda 1 detik
    end
    R->>S: kirim dari shipping.queue
    S->>S: terjemahkan ke format KirimCepat, log shipping.dispatched
    S->>R: ack
```

### Keputusan ack/nack di consumer

Kedua consumer memakai *manual ack* dengan prefetch 10. Pesan baru dihapus dari queue setelah consumer mengirim `ack`. Kalau consumer mati sebelum `ack`, RabbitMQ mengirim ulang pesannya.

| Kondisi pesan | Billing | Shipping |
|---|---|---|
| Valid, `order_id` baru | Simpan ke `billing_charges`, `ack`, log `billing.charged` | Terjemahkan, `ack`, log `shipping.dispatched` |
| Valid, `order_id` sudah pernah diproses | `ack` tanpa tagihan baru, log `billing.duplicate_ignored` | `ack` tanpa kirim ulang, log `shipping.duplicate_ignored` |
| JSON rusak atau field tidak valid | `nack(requeue=false)` → masuk `billing.dlq`, log `billing.rejected` | `nack(requeue=false)` → dibuang (tidak ada DLX), log `shipping.rejected` |
| Database error | Jeda 1 detik, `nack(requeue=true)`, log `billing.store_failed` | Tidak berlaku (Shipping tidak memakai DB) |

Billing mencatat tagihan **sebelum** `ack`. Kalau Billing mati di antara keduanya, pesan dikirim ulang dan `ON CONFLICT DO NOTHING` mencegah tagihan ganda.

Validasi Billing: `order_id`, `customer_id`, `shipping_address` tidak kosong; `items` tidak kosong; setiap `qty > 0` dan `price >= 0`; `total_amount > 0` dan sama dengan jumlah `qty x price`.

### Coupling yang dihilangkan

| Jenis coupling | Sebelum (panggilan langsung) | Sesudah (RabbitMQ) |
|---|---|---|
| Temporal | Billing harus hidup saat checkout | Pesan menunggu di queue sampai Billing siap |
| Lokasi | Web Shop harus tahu alamat Billing dan Shipping | Web Shop hanya tahu `orders.exchange` |
| Jumlah penerima | Web Shop memanggil setiap service satu per satu | Fanout menyalin ke semua queue. Consumer baru cukup membuat queue dan binding |

---

## Pembagian Peran

| NIM | Peran | Fokus | Deliverable utama |
|---|---|---|---|
| 13523060 | Platform & Broker Lead | DevOps / infra | `docker-compose.yml`, `definitions.json`, init PostgreSQL, reproducibility satu perintah |
| 13523138 | Web Shop & Producer Lead | Backend producer | `POST/GET /orders`, publisher persistent, endpoint `/test/*` |
| 13523125 | Billing & DLQ Specialist | Core consumer | Billing consumer, idempotency, rute DLQ |
| 13523097 | Shipping & QA/Experiments | Consumer dan pengujian | Shipping consumer, `demo.sh`, bukti eksperimen |
| 13524122 | Architecture & Story Lead | Teori, slide, demo | Kaitan konsep, slide maksimal 8, README, video demo cadangan |

---

## Alur Pengerjaan

| Tahap | Tugas |
|---|---|
| 1 | Setup awal: compose, topologi RabbitMQ, seed PostgreSQL, kerangka 3 service Go dan kontrak `order_placed` |
| 2  | Billing consumer, tabel `billing_charges`, user broker di `definitions.json` | 
| 3 | Perbaikan platform hasil verifikasi: port PostgreSQL 15432, variabel koneksi untuk semua service, healthcheck RabbitMQ, hostname tetap | 
| 4 | REST API Web Shop, publisher, endpoint uji | 
| 5 | Shipping consumer dan pemetaan KirimCepat | 
| 6 | `demo.sh` untuk tiga skenario wajib |
| 7 | Verifikasi end-to-end dan penyusunan README |

Kontrak yang disepakati lebih dulu supaya setiap anggota bisa bekerja paralel:

- **Bentuk pesan**: `order_placed` di `backend/internal/contracts/`.
- **Topologi broker**: nama exchange dan queue di `definitions.json`. Service hanya consume atau publish, tidak membuat topologi sendiri.
- **Variabel koneksi**: `AMQP_URL` dan `DATABASE_URL` dari compose.
- **Kepemilikan data**: tabel Web Shop dan tabel Billing terpisah, tanpa foreign key di antaranya.

---

## Prerequisites

| Kebutuhan | Keterangan |
|---|---|
| Docker Desktop atau Docker Engine + Compose v2 | Wajib. Satu-satunya yang perlu diinstal untuk menjalankan stack |
| Internet saat build pertama | Untuk mengunduh image dan modul Go. Setelah image ter-build, `up` bisa berjalan offline |
| Shell POSIX dan `curl` | Untuk `demo.sh` dan contoh perintah: Git Bash (Windows), macOS, Linux, atau WSL |
| Port kosong di laptop | 5672, 15672, 15432, 8080. Bisa diubah lewat `.env` (lihat [Troubleshooting](#troubleshooting)) |
| Go 1.27 (opsional) | Hanya untuk menjalankan tes atau service di luar Docker |

---

## Cara Menjalankan (Demo)

Bagian ini menjelaskan cara menyalakan stack dan menjalankan demo. Untuk presentasi di kelas, skrip langkah demi langkahnya ada di **[LIVE_DEMO_GUIDE.md](LIVE_DEMO_GUIDE.md)** (lihat [subbagian 3](#3-demo-manual-per-skenario)).

### 1. Menjalankan stack

```sh
git clone git@github.com:Vearance/komlay-msg-broker.git
cd komlay-msg-broker
docker compose up -d
```

Run pertama mem-build tiga image Go, tunggu sampai kelima service berstatus `(healthy)`:

```sh
docker compose ps
```

Pastikan ketiga service Go sudah tersambung ke RabbitMQ. `/healthz` hanya mengecek server HTTP, jadi bukti koneksi ada di log:

```sh
docker compose logs web-shop billing shipping | grep -E "amqp_connected|consumer_started"
```

Harus ada `webshop.amqp_connected`, `billing.consumer_started`, dan `shipping.consumer_started`.

| Akses | Alamat | Login |
|---|---|---|
| RabbitMQ Management UI | <http://localhost:15672> | `kilatmart` / `kilatmart_dev_only` |
| Web Shop API | <http://localhost:8080> | tidak perlu |
| PostgreSQL | `localhost:15432`, database `kilatmart` | `kilatmart` / `kilatmart_dev_only` |

Contoh membuat satu order:

```sh
curl -X POST http://localhost:8080/orders -H 'content-type: application/json' \
  -d '{"order_id":"ORD-2001","customer_id":"CUST-88","items":[{"sku":"PRD-01","qty":2,"price":25000}],"total_amount":50000,"shipping_address":"Jl. Ganesha No. 10, Bandung"}'
docker compose logs billing shipping --since 1m | grep ORD-2001
```

SKU harus salah satu dari `PRD-01` (25000), `PRD-02` (15000), atau `PRD-03` (40000), dan `total_amount` harus sama dengan jumlah `qty x price`.

### 2. Demo otomatis dengan `demo.sh`

Script `demo.sh` digunakan untuk menjalankan ketiga skenario wajib. Untuk setiap skenario, script menampilkan prediksi, menjalankan perintahnya, lalu mengecek hasilnya dengan ✅ atau ❌. Order ID dibuat unik per run (`ORD-<timestamp>-R1`), jadi script bisa dijalankan berulang kali.

```sh
./demo.sh                          # menjalankan ketiga skenario, berhenti di setiap langkah (tekan Enter)
./demo.sh all --no-pause           # menjalankan ketiga skenario tanpa jeda, untuk merekam video cadangan
./demo.sh all --restart-broker     # menjalankan ketiga skenario, skenario 1 juga me-restart RabbitMQ
./demo.sh 1                        # hanya menjalankan skenario 1
./demo.sh 2                        # hanya menjalankan skenario 2
./demo.sh 3                        # hanya menjalankan skenario 3
```

| Argumen | Arti |
|---|---|
| `all` (default), `1`, `2`, `3` | Skenario yang dijalankan |
| `--restart-broker` | Skenario 1 me-restart RabbitMQ saat Billing mati, untuk membuktikan durability broker |
| `--no-pause` | Tidak menunggu Enter di setiap langkah |
| `WEB_SHOP_URL=...` | Alamat Web Shop kalau port 8080 diganti |

Kalau Billing sedang dimatikan dan script dihentikan dengan Ctrl+C, script otomatis menyalakan Billing lagi.

Saran tampilan untuk observasi: terminal yang menjalankan `demo.sh` di satu sisi, dan RabbitMQ UI (tab **Queues and Streams**) di sisi lain.

### 3. Demo manual per skenario

Untuk live demo di depan kelas, ikuti **[LIVE_DEMO_GUIDE.md](LIVE_DEMO_GUIDE.md)**. Panduan itu menjalankan ketiga skenario wajib langkah demi langkah dengan `curl` di terminal dan RabbitMQ Management UI di browser, supaya audiens bisa melihat pesan bergerak antar-queue.

| Bagian panduan | Yang ditunjukkan |
|---|---|
| Skenario 1: Resiliency & Durability | Billing dimatikan, 2 order (`ORD-LIVE-01`, `ORD-LIVE-02`) tetap `201 Created` dan menunggu di `billing.queue`, lalu ditagih setelah Billing dinyalakan |
| Skenario 2: Idempotent Consumer | `ORD-DUP-999` dikirim 2× lewat `/test/inject-duplicate`, tetapi hanya 1 baris di `billing_charges` |
| Skenario 3: Dead-Letter Queue | Pesan `malformed` dan `negative_amount` ditolak Billing dan masuk `billing.dlq` |
| Verifikasi Bonus | Log Shipping yang menunjukkan order diterjemahkan ke format KirimCepat |

**Reset stack sebelum mengikuti panduan.** Order ID di panduan bersifat tetap. Kalau panduan pernah dijalankan (misalnya saat gladi), menjalankannya lagi tanpa reset akan gagal: `POST /orders` untuk `ORD-LIVE-01` mendapat `500 duplicate key`, dan kedua salinan `ORD-DUP-999` hanya tercatat sebagai `duplicate_ignored`.

```sh
docker compose down -v
docker compose up -d
docker compose ps        # tunggu sampai kelima service (healthy)
```

Saat mengikuti panduan:

- Di RabbitMQ 4.1, tab antrean bernama **Queues and Streams**.
- Statistik di UI diperbarui sekitar 5 detik sekali. Tunggu sebentar sebelum menunjuk angkanya, atau pakai perintah live berikut:

  ```sh
  docker compose exec rabbitmq rabbitmqctl -q list_queues name messages messages_persistent consumers
  ```

- Log Shipping untuk order yang berhasil bernama `shipping.dispatched`. Payload-nya memakai field KirimCepat (`nomor_pesanan`, `id_pelanggan`, `alamat_tujuan`, `daftar_barang`, `nilai_barang`). Untuk melihat order tertentu, pakai `docker compose logs shipping | grep ORD-LIVE-01` karena `--tail 4` hanya menampilkan 4 baris terakhir.

Kalau ingin menjalankan semuanya tanpa langkah manual, pakai [`demo.sh`](#2-demo-otomatis-dengan-demosh). Script itu membuat order ID unik di setiap run, jadi tidak perlu reset.

### 4. Stop, restart, dan reset

Semua service memakai `restart: unless-stopped`: Docker hanya menyalakan ulang container yang crash, bukan yang dihentikan dengan perintah.

| Perintah | Efek | Data tetap ada? |
|---|---|---|
| `docker compose stop <service>` | Berhenti dengan rapi, tidak dinyalakan ulang otomatis | Ya |
| `docker compose kill <service>` | Berhenti paksa (exit 137), tidak dinyalakan ulang otomatis | Ya |
| Proses crash sendiri | Docker menyalakan ulang dalam beberapa detik | Ya |
| `docker compose start <service>` | Menyalakan container yang dihentikan | Ya |
| `docker compose restart <service>` | Stop lalu start container yang sama | Ya |
| `docker compose down` | Hapus container dan network, volume tetap | Ya |
| `docker compose down -v` | Hapus juga volume PostgreSQL dan RabbitMQ | **Tidak** |

"Data tetap ada" berlaku untuk pesan persistent. Web Shop selalu publish dengan `delivery_mode = 2` (persistent).

## Skenario dan Hasil

### Skenario 1: Resiliency dan durability

**Prediksi:** order yang dibuat saat Billing mati menunggu di `billing.queue`, tetap ada walaupun RabbitMQ di-restart, lalu masing-masing ditagih tepat satu kali setelah Billing hidup lagi.

| Langkah | Hasil |
|---|---|
| `docker compose stop billing`, lalu 3× `POST /orders` | Ketiganya `201 Created`. Checkout tidak terganggu |
| Isi queue | `billing.queue`: 3 pesan, 3 persistent, 0 consumer. `shipping.queue`: 0 (Shipping tetap memproses) |
| `docker compose restart rabbitmq` | Healthy lagi dalam 6 detik. `billing.queue` **tetap 3** |
| `docker compose start billing` | Dalam 3 detik `billing.queue` = 0 dengan 1 consumer |
| `billing_charges` | R1, R2, R3 masing-masing tepat 1 baris |

```text
billing.queue	3	3	0      <- Billing mati, order menunggu
billing.queue	3	3	0      <- setelah restart RabbitMQ
billing.queue	0	0	1      <- Billing hidup lagi, semua diproses
{"msg":"billing.charged","order_id":"ORD-1795669361-R1","amount":50000}
{"msg":"billing.charged","order_id":"ORD-1795669361-R2","amount":50000}
{"msg":"billing.charged","order_id":"ORD-1795669361-R3","amount":50000}
```

**Kenapa berhasil:** queue durable (`definitions.json`), pesan persistent (publisher Web Shop), dan `hostname: rabbitmq` di compose. Hostname tetap ini membuat RabbitMQ selalu membaca folder data yang sama (`rabbit@rabbitmq`).

### Skenario 2: Idempotent consumer

**Prediksi:** order yang sama dikirim dua kali hanya menghasilkan satu tagihan. Salinan kedua tetap di-`ack` dan diabaikan.

| Pengecekan | Hasil |
|---|---|
| `POST /test/inject-duplicate` untuk `ORD-1795669361-D1` | `200`, pesan dipublish 2× |
| Baris di `billing_charges` | **1** |
| Log Billing | 1× `billing.charged`, 1× `billing.duplicate_ignored` |
| Log Shipping | 1× `shipping.dispatched`, 1× `shipping.duplicate_ignored` |

**Kenapa berhasil:** Billing memakai `INSERT ... ON CONFLICT (order_id) DO NOTHING`. Database sendiri yang menolak baris kedua, jadi tetap aman walaupun dua salinan diproses hampir bersamaan. Shipping memakai daftar `order_id` di memori (lihat [temuan 3](#keterbatasan-yang-diketahui)).

### Skenario 3: Dead-letter queue

**Prediksi:** Billing menolak pesan rusak dengan `nack(requeue=false)`, dan RabbitMQ mengalihkannya ke `billing.dlq` lewat `billing.dlx`.

| Pengecekan | Hasil |
|---|---|
| `inject-corrupt?type=malformed` dan `?type=negative_amount` | Keduanya `200` |
| `billing.dlq` | 0 → **2** |
| `billing.queue` | 0 (tidak ada pesan yang di-requeue terus-menerus) |

```text
billing.rejected  reason: malformed JSON: invalid character '}' looking for beginning of value
billing.rejected  ORD-NEG  reason: items[0].price is negative; total_amount must be positive
```

Shipping juga menolak kedua pesan itu (`shipping.rejected`). Karena `shipping.queue` tidak punya DLX, pesannya dibuang. Ini disengaja: `billing.dlq` sudah menyimpan salinan untuk diperiksa.

### Keterbatasan yang diketahui

| # | Temuan | Dampak | Bukti | Arah perbaikan |
|---|---|---|---|---|
| 1 | **Idempotency Shipping hanya di memori.** Daftar `order_id` yang sudah dikirim hilang saat Shipping restart | Duplikat yang datang setelah restart dikirim ulang ke KirimCepat (pengiriman ganda). Billing tidak terdampak | Setelah `docker compose restart shipping`, duplikat ORD-1795669361-D1 → `shipping.dispatched` lagi | Simpan `order_id` yang sudah dikirim di penyimpanan tahan restart, seperti `billing_charges` |
| 2 | **SKU yang tidak ada di `products` menghasilkan `500`**, bukan `400` | Pesan error kurang jelas bagi klien. Data tetap konsisten dan tidak ada event yang dipublish | `PRD-99` → `500` dengan `violates foreign key constraint` | Cek SKU saat validasi, atau ubah error foreign key menjadi `400` |

Selain itu, KirimCepat disimulasikan dengan log (`shipping.dispatched`). Belum ada mock REST API mitra dengan API key seperti di skenario spesifikasi. Ini tidak termasuk *must show* Track B3.

---

## Troubleshooting

| Gejala | Penyebab | Solusi |
|---|---|---|
| `ports are not available` / `bind: ... forbidden` saat `up` | Port sudah dipakai program lain, misalnya PostgreSQL yang terinstal langsung di laptop | Salin `.env.example` menjadi `.env`, ubah port yang bentrok (`POSTGRES_PORT`, `RABBITMQ_AMQP_PORT`, `RABBITMQ_MANAGEMENT_PORT`, `WEB_SHOP_PORT`), lalu `up` lagi. Kalau `WEB_SHOP_PORT` diubah, jalankan demo dengan `WEB_SHOP_URL=http://localhost:<port> ./demo.sh` |
| Login RabbitMQ ditolak (401), atau service gagal connect ke broker | Volume RabbitMQ berasal dari versi repo lama yang belum punya user di `definitions.json` | `docker compose down -v` sekali, lalu `up -d` |
| Pesan di queue hilang setelah `down` lalu `up` | Volume dibuat sebelum `hostname: rabbitmq` ditambahkan, jadi data ada di folder node lama | `docker compose down -v` sekali. Setelah itu pesan persistent selamat |
| Tabel `billing_charges` tidak ada | Volume PostgreSQL dibuat sebelum `002_billing.sql` ada. Script init hanya jalan di volume kosong | Tanpa menghapus data: `docker compose exec -T postgres psql -U kilatmart -d kilatmart < postgres/init/002_billing.sql` |
| Service `(healthy)` tapi tidak memproses pesan | `/healthz` hanya mengecek server HTTP, bukan koneksi ke RabbitMQ | Cek `docker compose logs <service>`: cari `consumer_started` atau `consumer_disconnected` |
| `rabbitmqctl` error `requires the 'rabbit' app to be running` | RabbitMQ masih boot setelah `restart` | Tunggu sampai `docker compose ps rabbitmq` menunjukkan `(healthy)` |
| `POST /orders` → `500 failed to publish order event` | RabbitMQ mati atau belum siap. Order sudah tersimpan di DB tapi tidak dipublish (temuan 2) | Nyalakan RabbitMQ. Kirim ulang dengan `order_id` **baru**, karena ID lama ditolak `duplicate key` |
| `POST /orders` → `400` | Validasi gagal, misalnya `total_amount` tidak sama dengan jumlah `qty x price` | Baca pesan `error` di respons dan perbaiki payload |
| `POST /orders` → `500 ... violates foreign key constraint` | SKU tidak ada di `products` (temuan 4) | Pakai `PRD-01`, `PRD-02`, atau `PRD-03` |
| `demo.sh`: `web shop not reachable` atau `<service> is not healthy` | Stack belum jalan atau belum healthy | `docker compose up -d`, tunggu semua `(healthy)`, lalu ulangi |
| `demo.sh`: `$'\r': command not found` (WSL) | Git di Windows mengubah akhir baris menjadi CRLF | Jalankan lewat Git Bash, atau `sed -i 's/\r$//' demo.sh` |
| Angka di RabbitMQ UI tidak cocok dengan `rabbitmqctl` | Statistik UI diperbarui sekitar 5 detik sekali | Tunggu beberapa detik, atau pakai `rabbitmqctl list_queues` |
| Timestamp log selisih 7 jam | Container berjalan dalam UTC | WIB = UTC + 7 jam |
| Password RabbitMQ di `.env` tidak berpengaruh | `RABBITMQ_DEFAULT_USER/PASS` diabaikan karena user diambil dari `definitions.json` | Buat hash baru dengan `docker compose exec rabbitmq rabbitmqctl hash_password <password>`, ganti `password_hash` di `definitions.json`, samakan password di `x-amqp-url` pada compose, lalu `docker compose restart rabbitmq` |
