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

Queues are durable, but publishers must also publish order events as persistent
(delivery mode 2) for messages to survive a broker restart. The billing
consumer must reject invalid messages with `nack(requeue=False)` to send them
to the DLQ. The broker topology alone does not implement publisher confirms,
consumer validation, or retry/idempotency logic; those belong to the services.

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
