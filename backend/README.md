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
