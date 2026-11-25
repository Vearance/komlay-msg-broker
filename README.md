# KilatMart Messaging (Track B3)

Local development stack for the Web Shop, Billing, and Shipping integration
exercise. It starts RabbitMQ, PostgreSQL, and three Go services from
`backend/`. The Billing consumer is implemented; the Web Shop and Shipping
business flows are still left for their owners to implement.

## Start

Requirements: Docker Desktop (or Docker Engine) with the Compose plugin.

```sh
docker compose up -d
```

The command works with local-development defaults. To customize credentials
or host ports, copy `.env.example` to `.env` and edit it before starting.
`.env` is ignored by Git. The committed/default credentials are for local
coursework only; do not use them in production.

Check readiness:

```sh
docker compose ps
```

All five services (`rabbitmq`, `postgres`, `web-shop`, `billing`, and
`shipping`) should report healthy. RabbitMQ Management UI
is at <http://localhost:15672> (user/password: `kilatmart` /
`kilatmart_dev_only`, defined in `rabbitmq/definitions.json`).
AMQP is exposed on `localhost:5672`; PostgreSQL is exposed on
`localhost:15432` (default database/user/password: `kilatmart` / `kilatmart` /
`kilatmart_dev_only`). The PostgreSQL host port defaults to 15432 rather than
5432 so it does not clash with a PostgreSQL server installed directly on the
laptop. Change host ports in `.env` if those ports are already in use.

Host ports only matter for tools running on your laptop (browser, `psql`,
DBeaver). Containers talk to each other over the Compose network using the
service name and the container port, e.g. `postgres:5432`, regardless of the
host port.

The Go services start only after RabbitMQ and PostgreSQL are healthy
(`depends_on: condition: service_healthy`). RabbitMQ counts as healthy only
when `rabbitmq-diagnostics check_port_connectivity` can connect to its
listeners, including AMQP on 5672. A plain `ping` would pass several seconds
earlier, before the broker has finished importing definitions and accepts
connections. Note that the Go services' own `/healthz` only shows that the
HTTP server is up; it does not report whether a consumer is connected to the
broker, so check the service logs for that.

## Connecting services

Compose passes connection strings to the Go services as environment variables.
These names are the contract between the platform and the service code: read
them with `os.Getenv` instead of hard-coding hosts or credentials.

| Variable | Value inside Compose | Given to |
|---|---|---|
| `AMQP_URL` | `amqp://kilatmart:kilatmart_dev_only@rabbitmq:5672/%2F` | `web-shop`, `billing`, `shipping` |
| `DATABASE_URL` | `postgres://kilatmart:kilatmart_dev_only@postgres:5432/kilatmart?sslmode=disable` | `web-shop`, `billing` |

- Hosts are Compose service names (`rabbitmq`, `postgres`) and ports are
  container ports, so the URLs work only inside the Compose network.
- `%2F` is the URL-encoded default vhost `/`.
- `sslmode=disable` is needed because the local PostgreSQL container does not
  use TLS.
- `DATABASE_URL` is built from `POSTGRES_USER`, `POSTGRES_PASSWORD`, and
  `POSTGRES_DB`, so it follows changes in `.env`. The RabbitMQ credentials in
  `AMQP_URL` must match the user in `rabbitmq/definitions.json`.
- `shipping` gets no `DATABASE_URL` because it does not use PostgreSQL.

```go
amqpURL := os.Getenv("AMQP_URL")
dbURL := os.Getenv("DATABASE_URL")
```

To run a service with `go run` on your laptop instead of in Compose, point the
URLs at the host ports (and pick a free `PORT`, since the `web-shop` container
already uses 8080 on the host):

```sh
PORT=8081 \
AMQP_URL='amqp://kilatmart:kilatmart_dev_only@localhost:5672/%2F' \
DATABASE_URL='postgres://kilatmart:kilatmart_dev_only@localhost:15432/kilatmart?sslmode=disable' \
go run ./cmd/billing
```

`billing` already reads both variables and exits with an error if either is
missing. `web-shop` and `shipping` receive them for their upcoming broker and
database clients.

## Go services

`backend/` is a single Go module with three independently built executables:
`web-shop`, `billing`, and `shipping`. Each exposes `GET /healthz`. `billing`
also consumes `billing.queue` (see [backend/README.md](backend/README.md) for
its ack/nack behaviour). The Web Shop order API and publisher and the Shipping
consumer are not implemented yet. The Web Shop health endpoint is available at
<http://localhost:8080/healthz>; the other health endpoints stay inside the
Compose network.

## RabbitMQ topology

`rabbitmq/definitions.json` is loaded at broker startup from
`rabbitmq/rabbitmq.conf` and provisions:

- User `kilatmart` (administrator) with full permissions on vhost `/`.
- Durable fanout exchange `orders.exchange`.
- Durable queues `billing.queue` and `shipping.queue`, both bound to the
  fanout exchange.
- Durable direct exchange `billing.dlx` and durable queue `billing.dlq`.
- `billing.queue` dead-letter routing to `billing.dlx` with routing key
  `billing.dead`, bound to `billing.dlq`.

The billing consumer must reject invalid messages with `nack(requeue=False)` to
send them to the DLQ. The broker topology alone does not implement publisher
confirms, consumer validation, or retry/idempotency logic; those belong to the
services.

### Message durability

A queued message survives a broker restart only when all three hold:

1. **The queue is durable.** Done in `definitions.json`.
2. **The message is persistent** (delivery mode 2). This is the publisher's job.
   In `amqp091-go` the default is transient, so set it explicitly:

   ```go
   ch.PublishWithContext(ctx, "orders.exchange", "", false, false, amqp.Publishing{
       ContentType:  "application/json",
       DeliveryMode: amqp.Persistent,
       Body:         body,
   })
   ```

   Enabling publisher confirms (`ch.Confirm(false)`) lets the Web Shop wait
   until the broker has accepted a message before answering the HTTP request.
3. **The broker finds its data again.** RabbitMQ stores data under its node
   name, `rabbit@<hostname>`. Compose sets `hostname: rabbitmq`, so the node is
   always `rabbit@rabbitmq`. Without a fixed hostname, every recreated
   container gets a new random hostname and starts with empty queues, even
   though the old data is still in the volume.

Tested: after `docker compose restart rabbitmq`, persistent messages were still
queued and transient ones were gone. After `docker compose down` and `up`
(without `-v`), persistent messages were still queued.

If you started the stack before `hostname: rabbitmq` was added, your volume
holds data under an old random node name. Run `docker compose down -v` once to
remove it.

### Broker credentials

When a definitions file is loaded at boot, RabbitMQ does not create its usual
default user, so `RABBITMQ_DEFAULT_USER`/`RABBITMQ_DEFAULT_PASS` have no effect.
The only user is the one in `definitions.json`, which stores a salted SHA-256
`password_hash` rather than the plain password. To change the password, generate
a new hash and replace `password_hash`:

```sh
docker compose exec rabbitmq rabbitmqctl hash_password <new-password>
```

Definitions are applied when the broker starts. Run `docker compose restart rabbitmq`
for the new password to take effect.

## PostgreSQL seed data

On the first start with a fresh data volume, PostgreSQL runs the scripts in
`postgres/init/` in file-name order:

- `001_init.sql` creates the Web Shop tables `products`, `orders`, and
  `order_items`, then inserts 3 products and 5 sample orders.
- `002_billing.sql` creates the Billing table `billing_charges`. Its
  `order_id` primary key is what makes the billing consumer idempotent: a
  second message for the same order cannot insert a second charge. It has no
  foreign key to `orders`, because Billing learns about orders only through
  `billing.queue`, not through the Web Shop's tables. It starts empty.

Initialization scripts only run when PostgreSQL initializes a new data
directory. If your volume already exists, apply a new script without losing
data (the scripts use `CREATE TABLE IF NOT EXISTS`, so rerunning is safe):

```sh
docker compose exec -T postgres psql -U kilatmart -d kilatmart < postgres/init/002_billing.sql
```

To reset the local database and rerun all scripts and the seed:

```sh
docker compose down -v
docker compose up -d
```

`down -v` permanently deletes the local PostgreSQL and RabbitMQ volumes,
including queued messages. Do not use it when you need to retain data.

## Stopping, restarting, and resetting

All services use `restart: unless-stopped`. Docker restarts a container whose
process exits on its own (a crash), but not one you stopped yourself.

| Command | Effect | Data kept? |
|---|---|---|
| `docker compose stop <svc>` | Graceful stop. Docker does **not** restart it. | Yes |
| `docker compose kill <svc>` | Immediate stop (exit 137). Docker does **not** restart it. | Yes |
| Process crashes | Docker restarts the container within seconds. | Yes |
| `docker compose start <svc>` | Starts a stopped container. | Yes |
| `docker compose restart <svc>` | Stops and starts the same container. | Yes |
| `docker compose down` | Removes containers and network; keeps volumes. | Yes |
| `docker compose down -v` | Also deletes the PostgreSQL and RabbitMQ volumes. | **No** |

"Data kept" assumes persistent messages (see
[Message durability](#message-durability)).

## Resiliency demo (scenario 1)

This shows that orders placed while Billing is down wait in RabbitMQ and are
charged when Billing comes back. Until the Web Shop `POST /orders` endpoint
exists, step 2 publishes test orders through the RabbitMQ Management API.
The commands are for a POSIX shell (Git Bash, WSL, macOS, or Linux).

1. Stop Billing:

   ```sh
   docker compose stop billing
   ```

2. Publish three persistent orders to `orders.exchange`:

   ```sh
   for id in ORD-9001 ORD-9002 ORD-9003; do
     curl -s -u kilatmart:kilatmart_dev_only -H 'content-type: application/json' \
       http://localhost:15672/api/exchanges/%2F/orders.exchange/publish \
       -d '{"properties":{"delivery_mode":2,"content_type":"application/json"},"routing_key":"","payload_encoding":"string","payload":"{\"order_id\":\"'"$id"'\",\"customer_id\":\"CUST-88\",\"items\":[{\"sku\":\"PRD-01\",\"qty\":2,\"price\":25000}],\"total_amount\":50000,\"shipping_address\":\"Jl. Ganesha No. 10, Bandung\"}"}'
     echo
   done
   ```

   Each call should print `{"routed":true}`.

3. Show that the orders are waiting with no consumer: `billing.queue` should
   have 3 messages, 3 persistent, 0 consumers.

   ```sh
   docker compose exec rabbitmq rabbitmqctl -q list_queues name messages messages_persistent consumers
   ```

4. Start Billing and show that it charges the waiting orders:

   ```sh
   docker compose start billing
   docker compose logs billing --since 1m | grep billing.charged
   ```

5. Show the result: `billing.queue` has 0 messages and 1 consumer, and
   `billing_charges` has the three orders.

   ```sh
   docker compose exec rabbitmq rabbitmqctl -q list_queues name messages messages_persistent consumers
   docker compose exec postgres psql -U kilatmart -d kilatmart -c "select order_id, amount, charged_at from billing_charges order by charged_at"
   ```

`shipping.queue` also receives every order, because `orders.exchange` is a
fanout. Its messages wait there until the Shipping consumer is implemented.

Notes for collecting evidence:

- The Management UI and API refresh queue statistics about every 5 seconds,
  so their counts can lag. `rabbitmqctl list_queues` shows live values.
- Service logs and PostgreSQL timestamps are in UTC. In WIB, add 7 hours.
- Use a new `order_id` for each run. Billing ignores order IDs it has already
  charged, which is the idempotency scenario, not this one.
- To also show broker durability, run `docker compose restart rabbitmq` after
  step 3 and before step 4. The 3 persistent messages are still queued
  afterwards.

## Project files

```text
docker-compose.yml          RabbitMQ, PostgreSQL, and Go services
rabbitmq/rabbitmq.conf      Loads broker definitions on startup
rabbitmq/definitions.json   Broker user, exchanges, queues, bindings, and DLX routing
postgres/init/001_init.sql  Web Shop schema and sample seed data
postgres/init/002_billing.sql  Billing idempotency table (billing_charges)
backend/                    Go services and shared event contract
.env.example                Example local configuration (copy to .env)
```
