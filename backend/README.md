# KilatMart Go services

One Go module builds three independently runnable service binaries:

- `cmd/web-shop` — future REST API and RabbitMQ producer.
- `cmd/billing` — future idempotent order consumer.
- `cmd/shipping` — future shipping consumer.

At this skeleton stage, each process only serves `GET /healthz`. No service
connects to RabbitMQ or PostgreSQL yet, and order, billing, and shipping
business flows are intentionally not implemented. The shared `contracts`
package records the `order_placed` JSON payload from the assignment.

## Local development

```sh
go test ./...
go run ./cmd/web-shop
```

The HTTP health endpoint is available at `http://localhost:8080/healthz`.
Set `PORT` to use a different port. The processes log JSON to stdout and shut
down gracefully on SIGINT/SIGTERM.

Docker Compose builds each executable from this module using the `SERVICE`
build argument. Add message handling, database access, and application routes
in the corresponding command/service package as those features are developed.

## Billing consumer

`cmd/billing` reads `billing.queue` with manual acks (prefetch 10) and needs
`AMQP_URL` and `DATABASE_URL` (set in `docker-compose.yml`). Each delivery is
settled one way:

| Message | Action | Log event |
|---|---|---|
| Valid, new `order_id` | insert into `billing_charges`, ack | `billing.charged` |
| Valid, `order_id` already charged | ack, no new charge | `billing.duplicate_ignored` |
| Broken JSON or bad field (empty, negative, total ≠ items sum) | `nack(requeue=false)` → `billing.dlq` | `billing.rejected` |
| Database error | wait 1s, `nack(requeue=true)` | `billing.store_failed` |

Idempotency relies on the `order_id` primary key of `billing_charges`
(`postgres/init/002_billing.sql`). Init scripts only run on an empty volume,
so after pulling this change run `docker compose down -v` once.
