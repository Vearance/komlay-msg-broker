# KilatMart Messaging (Track B3)

Local development stack for the Web Shop, Billing, and Shipping integration
exercise. It starts RabbitMQ, PostgreSQL, and three Go service skeletons from
`backend/`. The service health endpoints are wired, while business flows are
left for their owners to implement.

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
is at <http://localhost:15672> (defaults: `kilatmart` / `kilatmart_dev_only`).
AMQP is exposed on `localhost:5672`; PostgreSQL is exposed on `localhost:5432`
(default database/user/password: `kilatmart` / `kilatmart` /
`kilatmart_dev_only`). Change host ports in `.env` if those ports are already
in use.

Services on the Compose network can reach RabbitMQ and PostgreSQL at
`rabbitmq:5672` and `postgres:5432`. The Go skeletons do not connect to these
backing services yet; their broker and database clients will be added with the
business flows.

## Go service skeletons

`backend/` is a single Go module with three independently built executables:
`web-shop`, `billing`, and `shipping`. Each currently exposes only
`GET /healthz`; order APIs, RabbitMQ consumers/publisher, and database access
are not implemented yet. The Web Shop health endpoint is available at
<http://localhost:8080/healthz>; the other health endpoints stay inside the
Compose network.

## RabbitMQ topology

`rabbitmq/definitions.json` is loaded at broker startup from
`rabbitmq/rabbitmq.conf` and provisions:

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

## PostgreSQL seed data

On the first start with a fresh data volume, `postgres/init/001_init.sql`
creates `products`, `orders`, and `order_items`, then inserts 3 products and
5 sample orders. Initialization scripts only run when PostgreSQL initializes
a new data directory. To reset the local database and rerun the seed:

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
rabbitmq/definitions.json   Exchanges, queues, bindings, and DLX routing
postgres/init/001_init.sql  Schema and sample seed data
backend/                    Go service skeletons and shared event contract
.env.example                Example local configuration (copy to .env)
```
