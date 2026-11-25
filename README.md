# KilatMart Messaging (Track B3)

Local development infrastructure for the Web Shop, Billing, and Shipping
integration exercise. The initial setup starts RabbitMQ and PostgreSQL; the
application services will be added to this Compose project as their owners
implement them.

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

Both `rabbitmq` and `postgres` should report healthy. RabbitMQ Management UI
is at <http://localhost:15672> (defaults: `kilatmart` / `kilatmart_dev_only`).
AMQP is exposed on `localhost:5672`; PostgreSQL is exposed on `localhost:5432`
(default database/user/password: `kilatmart` / `kilatmart` /
`kilatmart_dev_only`). Change host ports in `.env` if those ports are already
in use.

Services on the Compose network can connect using hostnames `rabbitmq` and
`postgres` and the container ports `5672` and `5432` respectively. Use the
matching `.env` credentials from the application containers.

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
docker-compose.yml          RabbitMQ and PostgreSQL local stack
rabbitmq/rabbitmq.conf      Loads broker definitions on startup
rabbitmq/definitions.json   Exchanges, queues, bindings, and DLX routing
postgres/init/001_init.sql  Schema and sample seed data
.env.example                Example local configuration (copy to .env)
```
