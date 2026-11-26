#!/usr/bin/env bash
# KilatMart messaging demo: runs the 3 required scenarios against the local
# Compose stack, each with a prediction, the commands it runs and a result check.
# Needs only docker and curl. Works in Git Bash, macOS bash 3.2 and Linux.
set -euo pipefail

WEB_SHOP_URL="${WEB_SHOP_URL:-http://localhost:8080}"
# Unique per run, so order IDs never collide with earlier runs (billing would
# treat a reused ID as a duplicate, which is scenario 2, not scenario 1).
RUN_ID="$(date +%s)"
WAIT_TIMEOUT=30 # seconds to wait for queues, charges and log lines

SCENARIO="all"
PAUSE=1
RESTART_BROKER=0
FAILED=0
SCENARIO_OK=1
SUMMARY=""
BILLING_STOPPED=0

if [ -t 1 ]; then
  BOLD=$'\033[1m' DIM=$'\033[2m' CYAN=$'\033[36m' YELLOW=$'\033[33m'
  GREEN=$'\033[32m' RED=$'\033[31m' RESET=$'\033[0m'
else
  BOLD="" DIM="" CYAN="" YELLOW="" GREEN="" RED="" RESET=""
fi

usage() {
  cat <<EOF
Usage: ./demo.sh [all|1|2|3] [--no-pause] [--restart-broker]

  all               run scenarios 1, 2 and 3 (default)
  1                 resiliency: billing down, nothing lost
  2                 idempotency: duplicate message, no double charge
  3                 dead letter: corrupt message goes to billing.dlq
  --no-pause        do not wait for Enter between steps (for recording)
  --restart-broker  scenario 1 also restarts RabbitMQ while billing is down

Environment: WEB_SHOP_URL (default http://localhost:8080)
EOF
}

# ---------------------------------------------------------------- output ---

banner() { printf '\n%s%s========== %s ==========%s\n' "$BOLD" "$CYAN" "$1" "$RESET"; }
step() { printf '\n%s%s> %s%s\n' "$BOLD" "$CYAN" "$1" "$RESET"; }
predict() { printf '%sPrediction:%s %s\n' "$BOLD$YELLOW" "$RESET" "$1"; }
show_cmd() { printf '%s$ %s%s\n' "$DIM" "$1" "$RESET"; }
die() { printf '%sError:%s %s\n' "$RED" "$RESET" "$1" >&2; exit 1; }

run() {
  show_cmd "$*"
  "$@"
}

pause() {
  if [ "$PAUSE" -eq 1 ]; then
    read -r -p "${DIM}Press Enter to continue...${RESET}" _ || true
  fi
}

timestamps() {
  # Logs are in UTC; WIB (UTC+7) is for the evidence and slides.
  # "WIB-7" is a POSIX TZ string meaning UTC+7, so no zoneinfo is needed.
  printf 'UTC: %s\n' "$(date -u '+%Y-%m-%d %H:%M:%S')"
  printf 'WIB: %s\n' "$(TZ=WIB-7 date '+%Y-%m-%d %H:%M:%S')"
}

check() { # <description> <expected> <actual>
  if [ "$2" = "$3" ]; then
    printf '%s✅ %s%s (expected %s, got %s)\n' "$GREEN" "$1" "$RESET" "$2" "$3"
  else
    printf '%s❌ %s%s (expected %s, got %s)\n' "$RED" "$1" "$RESET" "$2" "$3"
    SCENARIO_OK=0
    FAILED=1
  fi
}

start_scenario() { # <title>
  banner "$1"
  SCENARIO_OK=1
  timestamps
}

finish_scenario() { # <title>
  if [ "$SCENARIO_OK" -eq 1 ]; then
    SUMMARY="${SUMMARY}✅ $1"$'\n'
  else
    SUMMARY="${SUMMARY}❌ $1"$'\n'
  fi
}

# ------------------------------------------------------------ inspection ---

queue_stat() { # <queue> <column: 2=messages 3=consumers>; prints -1 if unknown
  local out
  out="$(docker compose exec -T rabbitmq rabbitmqctl -q list_queues name messages consumers 2>/dev/null | tr -d '\r')" || out=""
  printf '%s\n' "$out" | awk -v q="$1" -v col="$2" '$1 == q { print $col; found = 1 } END { if (!found) print -1 }'
}
queue_depth() { queue_stat "$1" 2; }
queue_consumers() { queue_stat "$1" 3; }
# The *_is predicates run only through wait_until "$@", which ShellCheck cannot follow.
# shellcheck disable=SC2317
queue_depth_is() { [ "$(queue_depth "$1")" = "$2" ]; }

show_queues() {
  run docker compose exec -T rabbitmq rabbitmqctl -q list_queues name messages messages_persistent consumers
}

charge_count() { # <order_id>; rows in billing_charges, -1 if unknown
  docker compose exec -T postgres psql -U kilatmart -d kilatmart -tAc \
    "SELECT count(*) FROM billing_charges WHERE order_id = '$1'" 2>/dev/null | tr -d '[:space:]' || printf -- '-1'
}
# shellcheck disable=SC2317 # called through wait_until
charge_count_is() { [ "$(charge_count "$1")" = "$2" ]; }

log_count() { # <service> <event> <order_id> <since>
  docker compose logs "$1" --no-log-prefix --since "$4" 2>/dev/null |
    grep -F "\"msg\":\"$2\"" | grep -cF "\"order_id\":\"$3\"" || true
}
# shellcheck disable=SC2317 # called through wait_until
log_count_is() { [ "$(log_count "$1" "$2" "$3" "$4")" = "$5" ]; }

show_logs() { # <service> <event> <since>
  show_cmd "docker compose logs $1 --no-log-prefix --since <scenario start> | grep $2"
  docker compose logs "$1" --no-log-prefix --since "$3" 2>/dev/null |
    grep -F "\"msg\":\"$2\"" || printf '(no %s lines)\n' "$2"
}

service_is_healthy() {
  [ "$(docker compose ps --format '{{.Health}}' "$1" 2>/dev/null | tr -d '\r')" = "healthy" ]
}

wait_until() { # <timeout_seconds> <description> <command...>
  local timeout=$1 description=$2 waited=0
  shift 2
  printf '%swaiting until %s%s' "$DIM" "$description" "$RESET"
  until "$@"; do
    if [ "$waited" -ge "$timeout" ]; then
      printf ' timed out after %ss\n' "$timeout"
      return 0 # the check that follows reports the failure
    fi
    sleep 1
    waited=$((waited + 1))
    printf '.'
  done
  printf ' ok (%ss)\n' "$waited"
}

wait_healthy() { # <service>
  wait_until 90 "$1 is healthy" service_is_healthy "$1"
}

# --------------------------------------------------------------- actions ---

order_json() { # <order_id>
  printf '{"order_id":"%s","customer_id":"CUST-88","items":[{"sku":"PRD-01","qty":2,"price":25000}],"total_amount":50000,"shipping_address":"Jl. Ganesha No. 10, Bandung"}' "$1"
}

post() { # <path> [json body]; prints the HTTP status code (000 if unreachable)
  local body=${2:-}
  # The command goes to stderr: stdout is captured as the status code.
  if [ -n "$body" ]; then
    show_cmd "curl -X POST $WEB_SHOP_URL$1 -d '$body'" >&2
  else
    show_cmd "curl -X POST $WEB_SHOP_URL$1" >&2
  fi
  curl -sS -o /dev/null -w '%{http_code}' -X POST "$WEB_SHOP_URL$1" \
    -H 'content-type: application/json' -d "$body" 2>/dev/null || true
}

stop_billing() {
  BILLING_STOPPED=1
  run docker compose stop billing
}

start_billing() {
  run docker compose start billing
  BILLING_STOPPED=0
}

# Never leave billing stopped, even when the demo is interrupted with Ctrl+C.
# shellcheck disable=SC2317 # called by the EXIT trap
restore_billing() {
  if [ "$BILLING_STOPPED" -eq 1 ]; then
    printf '\nStarting billing again (it was stopped by an interrupted run)\n'
    docker compose start billing >/dev/null 2>&1 || true
  fi
}
trap restore_billing EXIT

preflight() {
  command -v docker >/dev/null 2>&1 || die "docker not found"
  command -v curl >/dev/null 2>&1 || die "curl not found"
  curl -fsS -o /dev/null "$WEB_SHOP_URL/healthz" 2>/dev/null ||
    die "web shop not reachable at $WEB_SHOP_URL (start the stack with 'docker compose up -d', or set WEB_SHOP_URL)"
  local svc
  for svc in rabbitmq postgres billing shipping; do
    service_is_healthy "$svc" || die "$svc is not healthy; check 'docker compose ps'"
  done
}

# ------------------------------------------------------------- scenarios ---

scenario_resiliency() {
  local title="1. Resiliency: billing down, nothing lost"
  local ids="ORD-$RUN_ID-R1 ORD-$RUN_ID-R2 ORD-$RUN_ID-R3"
  local since before id
  start_scenario "$title"
  predict "orders wait in billing.queue while billing is down and are charged exactly once after restart"
  since=$(($(date +%s) - 2))
  pause

  step "Stop billing"
  stop_billing
  before=$(queue_depth billing.queue)
  pause

  step "Place 3 orders while billing is down"
  for id in $ids; do
    check "POST /orders $id accepted" 201 "$(post /orders "$(order_json "$id")")"
  done
  wait_until "$WAIT_TIMEOUT" "billing.queue holds the 3 orders" queue_depth_is billing.queue "$((before + 3))"
  wait_until "$WAIT_TIMEOUT" "shipping has consumed them" queue_depth_is shipping.queue 0
  show_queues
  check "billing.queue holds the 3 orders" "$((before + 3))" "$(queue_depth billing.queue)"
  check "billing.queue has no consumer" 0 "$(queue_consumers billing.queue)"
  check "shipping keeps consuming (shipping.queue empty)" 0 "$(queue_depth shipping.queue)"
  pause

  if [ "$RESTART_BROKER" -eq 1 ]; then
    step "Restart RabbitMQ while billing is down (persistent messages must survive)"
    run docker compose restart rabbitmq
    wait_healthy rabbitmq
    show_queues
    check "orders survived the broker restart" "$((before + 3))" "$(queue_depth billing.queue)"
    pause
  fi

  step "Start billing again"
  start_billing
  wait_healthy billing
  wait_until "$WAIT_TIMEOUT" "billing.queue is drained" queue_depth_is billing.queue 0
  show_queues
  check "billing.queue is empty" 0 "$(queue_depth billing.queue)"
  check "billing is consuming again" 1 "$(queue_consumers billing.queue)"
  pause

  step "Each order is charged exactly once"
  show_cmd "docker compose exec -T postgres psql -U kilatmart -d kilatmart -tAc \"SELECT count(*) FROM billing_charges WHERE order_id = '<id>'\""
  for id in $ids; do
    check "$id has 1 row in billing_charges" 1 "$(charge_count "$id")"
  done
  show_logs billing billing.charged "$since"

  finish_scenario "$title"
}

scenario_idempotency() {
  local title="2. Idempotency: duplicate message, no double charge"
  local id="ORD-$RUN_ID-D1"
  local since
  start_scenario "$title"
  predict "the same order delivered twice produces one charge; the second copy is acked and ignored"
  since=$(($(date +%s) - 2))
  pause

  step "Publish the same order twice"
  check "POST /test/inject-duplicate accepted" 200 "$(post /test/inject-duplicate "$(order_json "$id")")"
  wait_until "$WAIT_TIMEOUT" "billing ignored the second copy" log_count_is billing billing.duplicate_ignored "$id" "$since" 1
  wait_until "$WAIT_TIMEOUT" "shipping ignored the second copy" log_count_is shipping shipping.duplicate_ignored "$id" "$since" 1
  pause

  step "Check the result"
  show_cmd "docker compose exec -T postgres psql -U kilatmart -d kilatmart -tAc \"SELECT count(*) FROM billing_charges WHERE order_id = '$id'\""
  check "$id has 1 row in billing_charges" 1 "$(charge_count "$id")"
  check "billing logged 1 charge" 1 "$(log_count billing billing.charged "$id" "$since")"
  check "billing logged 1 duplicate_ignored" 1 "$(log_count billing billing.duplicate_ignored "$id" "$since")"
  check "shipping dispatched once" 1 "$(log_count shipping shipping.dispatched "$id" "$since")"
  check "shipping logged 1 duplicate_ignored" 1 "$(log_count shipping shipping.duplicate_ignored "$id" "$since")"
  show_logs billing billing.duplicate_ignored "$since"
  show_logs shipping shipping.duplicate_ignored "$since"

  finish_scenario "$title"
}

scenario_dead_letter() {
  local title="3. Dead letter: corrupt messages go to billing.dlq"
  local since before
  start_scenario "$title"
  predict "billing nacks invalid messages without requeue and RabbitMQ routes them to billing.dlq"
  since=$(($(date +%s) - 2))
  pause

  step "DLQ depth before"
  before=$(queue_depth billing.dlq)
  printf 'billing.dlq depth: %s\n' "$before"
  pause

  step "Publish 2 corrupt messages: broken JSON and a negative amount"
  check "inject-corrupt?type=malformed accepted" 200 "$(post '/test/inject-corrupt?type=malformed')"
  check "inject-corrupt?type=negative_amount accepted" 200 "$(post '/test/inject-corrupt?type=negative_amount')"
  wait_until "$WAIT_TIMEOUT" "billing.dlq grows by 2" queue_depth_is billing.dlq "$((before + 2))"
  show_queues
  check "billing.dlq grew by 2" "$((before + 2))" "$(queue_depth billing.dlq)"
  check "billing.queue is empty (nothing requeued)" 0 "$(queue_depth billing.queue)"
  show_logs billing billing.rejected "$since"
  # shipping.queue has no DLX: shipping rejects the same messages and they are dropped.
  show_logs shipping shipping.rejected "$since"

  step "Screenshot"
  printf 'Open http://localhost:15672 -> Queues -> billing.dlq -> Get messages\n'

  finish_scenario "$title"
}

# ------------------------------------------------------------------ main ---

main() {
  local arg
  for arg in "$@"; do
    case "$arg" in
      all | 1 | 2 | 3) SCENARIO="$arg" ;;
      --no-pause) PAUSE=0 ;;
      --restart-broker) RESTART_BROKER=1 ;;
      -h | --help) usage; exit 0 ;;
      *) usage >&2; exit 2 ;;
    esac
  done

  # docker compose must run next to docker-compose.yml.
  cd "$(dirname "$0")"
  preflight
  printf 'Run ID: %s (order IDs look like ORD-%s-R1)\n' "$RUN_ID" "$RUN_ID"

  case "$SCENARIO" in
    1) scenario_resiliency ;;
    2) scenario_idempotency ;;
    3) scenario_dead_letter ;;
    all)
      scenario_resiliency
      pause
      scenario_idempotency
      pause
      scenario_dead_letter
      ;;
  esac

  banner "Summary"
  printf '%s' "$SUMMARY"
  exit "$FAILED"
}

main "$@"
