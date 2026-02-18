# EasyCron Operator Guide

This document defines the operational contract for EasyCron. It describes what the system guarantees, what it does not guarantee, and how it behaves under failure.

## Runtime Configuration

### Timeouts & Pooling

| Variable | Default | Description |
|----------|---------|-------------|
| `DB_OP_TIMEOUT` | `5s` | Maximum time for any single database operation |
| `DB_MAX_OPEN_CONNS` | `25` | Maximum open connections to PostgreSQL |
| `DB_MAX_IDLE_CONNS` | `5` | Maximum idle connections in the pool |
| `DB_CONN_MAX_LIFETIME` | `30m` | Maximum lifetime of a connection |
| `DB_CONN_MAX_IDLE_TIME` | `5m` | Maximum idle time before connection is closed |
| `HTTP_SHUTDOWN_TIMEOUT` | `10s` | Time to wait for in-flight HTTP requests during shutdown |
| `DISPATCHER_DRAIN_TIMEOUT` | `30s` | Time to wait for buffered events during shutdown |
| `CIRCUIT_BREAKER_THRESHOLD` | `5` | Consecutive execution failures before circuit opens (0 = disabled) |
| `CIRCUIT_BREAKER_COOLDOWN` | `2m` | Cooldown before allowing a probe attempt to an open circuit |
| `DISPATCH_MODE` | `channel` | Dispatch mode: `channel` (in-memory EventBus) or `db` (Postgres polling) |
| `DB_POLL_INTERVAL` | `500ms` | Sleep between DB polls when idle (DB mode only) |
| `DISPATCHER_WORKERS` | `1` | Concurrent dispatch workers (DB mode only) |

### Why These Defaults

- **DB_OP_TIMEOUT=5s**: Prevents indefinite hangs during DB outages. Most operations complete in <100ms.
- **DB_MAX_OPEN_CONNS=25**: Conservative limit for a single-node service. Prevents connection exhaustion.
- **DB_MAX_IDLE_CONNS=5**: Keeps some connections warm without wasting resources.
- **DB_CONN_MAX_LIFETIME=30m**: Ensures connections are recycled, respecting load balancer/proxy limits.
- **HTTP_SHUTDOWN_TIMEOUT=10s**: Gives in-flight API requests time to complete gracefully.
- **DISPATCHER_DRAIN_TIMEOUT=30s**: Allows pending webhook deliveries to complete during shutdown.
- **CIRCUIT_BREAKER_THRESHOLD=5**: Opens circuit after 5 consecutive failed executions. Prevents wasting retry budget on endpoints that are consistently down. Set to 0 to disable.
- **CIRCUIT_BREAKER_COOLDOWN=2m**: Gives failing endpoints time to recover before probing again. Short enough to resume delivery quickly once an endpoint recovers.

## Required Production Flags

The following flags are **mandatory** for production deployments:

| Variable | Value | Why |
|----------|-------|-----|
| `RECONCILE_ENABLED` | `true` | Without this, orphaned executions are **permanently lost**. Default is `false`. |
| `METRICS_ENABLED` | `true` | Required for observability and alerting on buffer saturation. |

**WARNING:** Running with `RECONCILE_ENABLED=false` in production means:
- Any execution orphaned by buffer full, crash, or shutdown is **never delivered**
- No automatic recovery mechanism exists
- You must manually detect and handle orphans via database queries

## Do Not Use EasyCron If...

EasyCron is not suitable for your use case if:

1. **You require exactly-once delivery** — Webhooks may be delivered 0, 1, or 2+ times
2. **You cannot tolerate any missed webhooks** — In-memory queue means events can be lost on crash
3. **You need guaranteed delivery of high-frequency jobs** — Jobs firing >1000 times per tick interval will have fires **permanently lost** (not queued)
4. **Your webhook handlers are not idempotent** — Duplicate deliveries will cause data corruption
5. **You need cross-restart retry continuation** — Retry sequences are abandoned on restart

## Guarantees

### Execution Idempotency

Each job fires at most once per scheduled minute. The database enforces a unique constraint on `(job_id, scheduled_at)`. If the scheduler restarts and attempts to emit the same execution, the insert fails silently and no duplicate webhook is sent.

### Bounded Retries

Webhook delivery attempts are bounded to exactly 4 attempts with fixed backoff:
- Attempt 1: Immediate
- Attempt 2: 30 seconds after attempt 1
- Attempt 3: 2 minutes after attempt 2
- Attempt 4: 10 minutes after attempt 3

After 4 failed attempts, the execution is marked `failed`. No further retries occur.

### Failure Isolation

- Redis failures do not affect webhook delivery. Analytics is best-effort.
- Individual job failures do not affect other jobs.
- Webhook timeouts are bounded (default 30s, max 60s per job).
- The circuit breaker is per-URL: a failing endpoint does not affect delivery to other URLs.

### Graceful Shutdown

On SIGINT or SIGTERM:
1. Scheduler stops immediately (no new executions emitted)
2. Reconciler stops (if enabled, no new re-emits)
3. Dispatcher drains buffered events (bounded by `DISPATCHER_DRAIN_TIMEOUT`, default 30s)
4. HTTP server gracefully shuts down (bounded by `HTTP_SHUTDOWN_TIMEOUT`, default 10s)
5. Metrics server gracefully shuts down (if enabled)
6. Process exits with code 0

**Shutdown is bounded** for dispatcher, HTTP, and metrics servers. Scheduler and reconciler exit immediately on context cancellation (no draining).

## Operator Contract

This section defines the explicit contract between EasyCron and operators.

### What EasyCron Guarantees

1. **At-most-once execution creation** per `(job_id, scheduled_at)` — DB constraint prevents duplicates
2. **At-most 4 webhook delivery attempts** — bounded retries, no infinite loops
3. **Terminal state immutability** — once `delivered` or `failed`, status cannot change
4. **Bounded DB operations** — all queries timeout after `DB_OP_TIMEOUT` (default 5s)
5. **Ordered shutdown** — scheduler stops before dispatcher drains
6. **Idempotent re-emits** — reconciler uses original execution ID

### What EasyCron Does NOT Guarantee

1. **Webhook delivery** — events may be lost before delivery (crash, buffer full)
2. **Exactly-once delivery** — webhooks may be delivered 0, 1, or 2+ times
3. **Retry continuation after restart** — incomplete retry sequences are abandoned
4. **Recovery without reconciler** — orphans are permanent if `RECONCILE_ENABLED=false`
5. **Delivery of high-frequency fires** — >1000 fires per job per tick are permanently lost
6. **Event persistence** — in-memory buffer (100 events) is lost on crash
7. **Circuit breaker persistence** — breaker state is in-memory, reset on restart

### What Operators MUST Do

1. **Enable reconciler in production** — set `RECONCILE_ENABLED=true`
2. **Design idempotent webhook handlers** — duplicate deliveries will occur
3. **Monitor buffer saturation** — alert on `easycron_eventbus_buffer_saturation > 0.8`
4. **Monitor orphan count** — alert on `easycron_orphaned_executions > 0`
5. **Keep job frequency reasonable** — <1000 fires per job per tick interval
6. **Ensure webhook endpoints respond within timeout** — slow endpoints cause backpressure

### What Will Break the System

| Action | Consequence |
|--------|-------------|
| Running `RECONCILE_ENABLED=false` in production | Orphaned executions are never recovered |
| Creating jobs that fire >1000 times per tick | Fires beyond 1000 are permanently lost |
| Webhook endpoints that never respond | Circuit breaker opens, executions short-circuited to failed |
| Ignoring buffer saturation alerts | Events lost, orphaned executions |
| Non-idempotent webhook handlers | Data corruption on duplicate delivery |

## Non-Guarantees

### No Durable Queue

The event bus is an in-memory buffered channel (capacity 100). Events are not persisted. If the process crashes between scheduler emit and dispatcher delivery, those events are lost.

### No Exactly-Once Delivery

Webhooks may be delivered zero times (crash before delivery) or multiple times (crash after delivery but before status update). Design webhook handlers to be idempotent.

### No Retry After Restart

If the process restarts during a retry sequence, remaining retries are abandoned. The execution status reflects the last known state before crash.

### Analytics Best-Effort

Redis analytics may undercount executions if Redis is unavailable. Analytics failures are logged but never block delivery.

## Circuit Breaker

The dispatcher includes a per-URL circuit breaker that protects against repeatedly hitting failing webhook endpoints. This is in-memory only and resets on restart.

### State Machine

```
CLOSED ──(N consecutive failed executions)──> OPEN
OPEN   ──(cooldown elapsed)─────────────────> HALF_OPEN (one probe allowed)
HALF_OPEN ──(probe succeeds)────────────────> CLOSED
HALF_OPEN ──(probe fails)──────────────────> OPEN (cooldown resets)
```

### Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `CIRCUIT_BREAKER_THRESHOLD` | `5` | Consecutive failed executions before opening. 0 disables. |
| `CIRCUIT_BREAKER_COOLDOWN` | `2m` | Time before allowing a probe attempt |

### Behavior

- The breaker counts consecutive fully-failed **executions** (not individual HTTP attempts). An execution that exhausts all 4 retry attempts counts as one failure.
- When a circuit is open, executions are immediately marked `failed` without making HTTP calls. This is fast — no retry delays.
- After the cooldown, exactly one probe execution is allowed through. If it succeeds (any attempt delivers), the circuit closes. If all attempts fail, the circuit re-opens with a fresh cooldown.
- Each webhook URL has an independent circuit. One failing URL does not affect others.
- The breaker is in-memory. On restart, all circuits reset to closed.

### Interaction with Reconciler

If the circuit is open and the reconciler re-emits an orphaned execution for that URL, the execution will be immediately failed by the circuit breaker. The reconciler has its own batch size limit, so this does not cause an infinite loop.

### When to Disable

Set `CIRCUIT_BREAKER_THRESHOLD=0` to disable. Consider disabling if:
- You prefer every execution to attempt delivery regardless of endpoint health
- You have very few jobs and want maximum delivery attempts

### Log Signals

```
easycron: circuit breaker enabled (threshold=5, cooldown=2m0s)
dispatcher: job=X circuit open for http://example.com/hook, skipping
```

## Dispatch Modes

EasyCron supports two dispatch modes, controlled by `DISPATCH_MODE`.

### Channel Mode (default)

The scheduler emits events to an in-memory EventBus (buffered channel, capacity 100). A single dispatcher goroutine reads events and delivers webhooks. This is the original behavior.

- Simple, low-latency, no extra DB load
- Events in the buffer are lost on crash
- Single dispatcher goroutine

### DB Mode

Set `DISPATCH_MODE=db`. Workers poll Postgres directly using `SELECT ... FOR UPDATE SKIP LOCKED` to claim `emitted` executions. A `claimed_at` timestamp is set on claim; the reconciler can requeue rows where `claimed_at` is stale.

- No in-memory buffer to lose on crash
- Multiple concurrent workers via `DISPATCHER_WORKERS`
- Suitable for horizontal scaling (multiple processes can poll the same table)
- Slightly higher DB load due to polling (`DB_POLL_INTERVAL` controls frequency)

### When to Use Each

| | Channel | DB |
|---|---|---|
| **Simplicity** | Simpler, fewer moving parts | Requires `schema/003_add_claimed_at.sql` |
| **Crash resilience** | Events in buffer lost on crash | No in-memory buffer; rows survive crash |
| **Horizontal scaling** | Single process only | Multiple workers / processes |
| **DB load** | Lower (reads only at tick) | Higher (polling every `DB_POLL_INTERVAL`) |

### Schema Requirement

DB mode requires the `claimed_at` column. Apply the migration before switching:

```bash
psql easycron < schema/003_add_claimed_at.sql
```

## Failure Modes

### PostgreSQL Unavailable

| Phase | Behavior |
|-------|----------|
| Startup | Server refuses to start (exit code 1) |
| Runtime (scheduler) | Tick fails after `DB_OP_TIMEOUT`, logged, retried next tick |
| Runtime (dispatcher) | Job fetch fails after `DB_OP_TIMEOUT`, event logged, not retried |
| Runtime (API) | Request fails with HTTP 500 after `DB_OP_TIMEOUT` |

**DB operations cannot hang indefinitely.** All queries are bounded by `DB_OP_TIMEOUT` (default 5s).

### Redis Unavailable

| Phase | Behavior |
|-------|----------|
| Startup | Server starts, analytics disabled warning logged |
| Runtime | Analytics write fails, logged, delivery unaffected |

### Webhook Failures

| Failure Type | Behavior |
|--------------|----------|
| Network error | Retryable, up to 4 attempts |
| Timeout | Retryable, up to 4 attempts |
| HTTP 5xx | Retryable, up to 4 attempts |
| HTTP 429 | Retryable, up to 4 attempts |
| HTTP 4xx (not 429) | Non-retryable, marked failed immediately |
| HTTP 2xx | Success, marked delivered |
| Circuit open | Execution immediately marked failed, no HTTP call made |

### Invalid Configuration

| Error | Exit Code |
|-------|-----------|
| Missing DATABASE_URL | 2 |
| Invalid TICK_INTERVAL | 2 |
| Invalid DATABASE_URL format | 1 (runtime) |

## Failure Scenarios & Outcomes

This section describes specific failure scenarios and their outcomes.

### Event Loss Scenarios

| Scenario | Outcome | Recoverable? |
|----------|---------|--------------|
| Process crash with events in buffer | Events lost | Only if `RECONCILE_ENABLED=true` AND execution was inserted to DB |
| Buffer full (100 events) for >5s | New events dropped | Only if `RECONCILE_ENABLED=true` |
| Crash during retry backoff | Retry sequence abandoned | No — execution stays in last known state |
| Job fires >1000 times in one tick | Fires beyond 1000 **permanently lost** | No — not inserted to DB |
| Shutdown during active webhook delivery | Delivery may complete or be abandoned | Depends on timing within drain timeout |
| DB failure during status update | Status not updated, execution appears orphaned | Yes — reconciler will re-emit |

### Execution Status State Machine

Executions follow this state machine:

```
                  ┌─────────────────┐
                  │                 │
    [created] ──► │    emitted      │ ──► [delivered] (terminal)
                  │                 │
                  └────────┬────────┘
                           │
                           └──────────► [failed] (terminal)
```

In DB dispatch mode, an intermediate `in_progress` state is used:

```
emitted → in_progress → delivered
                      → failed
emitted → delivered (channel mode, no intermediate state)
emitted → failed
in_progress → emitted (stale requeue by reconciler)
```

**Status values:**
| Status | Meaning | Terminal? |
|--------|---------|-----------|
| `emitted` | Execution created, webhook delivery pending | No |
| `in_progress` | Claimed by a DB-mode worker, delivery underway | No |
| `delivered` | Webhook returned 2xx | Yes |
| `failed` | All 4 retry attempts exhausted or non-retryable error | Yes |

**Valid transitions:**
- `emitted` → `delivered` (channel mode: webhook returned 2xx)
- `emitted` → `in_progress` (DB mode: worker claims execution)
- `emitted` → `failed` (all 4 attempts exhausted or 4xx response)
- `in_progress` → `delivered` (webhook returned 2xx)
- `in_progress` → `failed` (all attempts exhausted or non-retryable error)
- `in_progress` → `emitted` (reconciler requeues stale claim)

**Invalid transitions (blocked by DB query):**
- `delivered` → any state
- `failed` → any state

### What the Reconciler Can and Cannot Do

| Can Do | Cannot Do |
|--------|-----------|
| Re-emit executions with `status='emitted'` | Recover executions with `status='delivered'` or `status='failed'` |
| Detect orphans older than threshold | Recover executions lost before DB insert (e.g., >1000 fires/tick) |
| Re-emit using original execution ID | Create new executions |
| Process up to `RECONCILE_BATCH_SIZE` orphans per cycle | Guarantee delivery (still at-most-once) |

## Shutdown Semantics

### Order of Operations

```
1. Signal received (SIGINT/SIGTERM)
2. Scheduler context cancelled
3. Scheduler goroutine exits (immediate)
4. Reconciler context cancelled (if enabled)
5. Reconciler goroutine exits (immediate)
6. Dispatcher context cancelled
7. Dispatcher drains buffered events (max DISPATCHER_DRAIN_TIMEOUT, default 30s)
8. Dispatcher goroutine exits
9. HTTP server graceful shutdown (max HTTP_SHUTDOWN_TIMEOUT, default 10s)
10. Metrics server graceful shutdown (if enabled)
11. Process exits (code 0)
```

### Bounded Timeouts

| Phase | Timeout | What Happens on Timeout |
|-------|---------|------------------------|
| Dispatcher drain | `DISPATCHER_DRAIN_TIMEOUT` | Remaining events logged and abandoned |
| HTTP shutdown | `HTTP_SHUTDOWN_TIMEOUT` | Remaining connections forcibly closed |

**Total maximum shutdown time:** `DISPATCHER_DRAIN_TIMEOUT` + `HTTP_SHUTDOWN_TIMEOUT` (default 40s)

### What May Be Lost

- Events emitted but not yet delivered (in-memory buffer)
- Retry attempts in progress (backoff timers cancelled)
- Events where delivery succeeded but status update failed

## Restart Semantics

### What Is Safe

- Restart at any time. The scheduler will resume on next tick.
- Duplicate execution inserts are rejected by database constraint.
- Terminal execution states (delivered/failed) cannot regress.

### What Is Not Replayed

- Events in the in-memory buffer at crash time
- Incomplete retry sequences
- Analytics writes that failed before crash
- Circuit breaker state (all circuits reset to closed on restart)

**WARNING:** The in-memory event buffer (100 events) is NOT persisted. Events in the buffer at crash or restart time are permanently lost.

### Recommended Practice

For critical jobs, monitor execution status via the API. If an execution remains in `emitted` state beyond expected delivery time, investigate manually.

## Orphaned Executions

An execution is **orphaned** when it has `status = 'emitted'` but will never be delivered. This happens when:

1. **Buffer full:** Dispatcher can't keep up, scheduler drops event after 5s timeout
2. **Process crash:** Between DB insert and event emit (rare window)
3. **Shutdown during dispatch:** Context cancelled before status update

### Detection

**Log signals:**
```
scheduler: ORPHAN execution=<uuid> job=<uuid> ...
eventbus: buffer full, event dropped ...
```

**Database query:**
```sql
-- Find orphaned executions (emitted > RECONCILE_THRESHOLD ago, no delivery)
-- Default threshold is 10 minutes
SELECT e.id, e.job_id, e.scheduled_at, e.created_at
FROM executions e
LEFT JOIN delivery_attempts d ON d.execution_id = e.id
WHERE e.status = 'emitted'
  AND e.created_at < NOW() - INTERVAL '10 minutes'
  AND d.id IS NULL;
```

### Definition

An execution is considered orphaned if:
- Status is `emitted`
- Created more than `RECONCILE_THRESHOLD` ago (default: 10 minutes)
- Has zero delivery attempts

### Automatic Recovery (Reconciler)

EasyCron includes an optional **reconciler** that automatically detects and re-emits orphaned executions.

**To enable:**
```bash
RECONCILE_ENABLED=true ./easycron serve
```

**How it works:**
1. Periodically scans for executions with `status='emitted'` older than threshold
2. Re-emits `TriggerEvent` for each orphan to the event bus
3. Dispatcher processes normally (idempotency is guaranteed)

**Configuration:**

| Variable | Default | Description |
|----------|---------|-------------|
| `RECONCILE_ENABLED` | `false` | Enable reconciler (must be explicit) |
| `RECONCILE_INTERVAL` | `5m` | How often to scan for orphans |
| `RECONCILE_THRESHOLD` | `10m` | Age before execution is considered orphaned |
| `RECONCILE_BATCH_SIZE` | `100` | Max orphans processed per cycle |

**Log signals:**
```
reconciler: started (interval=5m0s, threshold=10m0s, batch=100)
reconciler: found 3 orphaned executions
reconciler: re-emitted execution=<uuid> job=<uuid> scheduled_at=<time> (age=12m30s)
reconciler: cycle complete, re-emitted=3, failed=0
```

**Safety guarantees:**
- Cannot re-open terminal executions (delivered/failed)
- Cannot create new executions
- Bounded batch size prevents unbounded work
- DB errors abort cycle (retry next interval)
- Emit errors are logged, orphan retried next cycle

**What reconciler does NOT guarantee:**
- Still at-most-once under crash (event may be lost before re-emit)
- Still no exactly-once delivery (webhook may be sent twice)
- Does not recover executions orphaned during current cycle

### Manual Recovery (Without Reconciler)

If reconciler is disabled, orphaned executions remain in the database.

Manual options:
1. **Accept the loss:** Most common for non-critical jobs
2. **Manual trigger:** Call the webhook endpoint directly using the job's configuration
3. **Database cleanup:** Delete orphaned execution records

### Prevention

- Enable reconciler for automatic recovery
- Monitor buffer utilization (Prometheus metric: `easycron_eventbus_buffer_size`)
- Monitor emit errors (Prometheus metric: `easycron_eventbus_emit_errors_total`)
- Ensure webhook endpoints respond within timeout
- Scale down job frequency if buffer fills regularly

## Monitoring

### Health Check

```
GET /health
```

Returns `{"status":"ok"}` with HTTP 200 if the server is running.

**Verbose health check:**
```
GET /health?verbose=true
```

Returns component-level health status:
```json
{
  "status": "ok",
  "components": {
    "database": "healthy"
  }
}
```

When degraded:
```json
{
  "status": "degraded",
  "components": {
    "database": "unhealthy: connection refused"
  }
}
```

| Status | HTTP Code | Meaning |
|--------|-----------|---------|
| `ok` | 200 | All components healthy |
| `degraded` | 503 | One or more components unhealthy |

### Prometheus Metrics

Enable with `METRICS_ENABLED=true`. Metrics are exposed on a separate port (default 9090).

#### Key Metrics & Alert Thresholds

| Metric | Type | Alert Threshold | Meaning |
|--------|------|-----------------|---------|
| `easycron_eventbus_buffer_saturation` | Gauge (0.0-1.0) | **> 0.8** | Buffer nearing capacity, event loss imminent |
| `easycron_eventbus_buffer_size` | Gauge | > 80 (of 100) | Same as above, absolute value |
| `easycron_eventbus_emit_errors_total` | Counter | Any increase | Events were dropped (orphaned executions) |
| `easycron_orphaned_executions` | Gauge | **> 0** | Orphaned executions detected by reconciler |
| `easycron_execution_latency_seconds` | Histogram | **p99 > 60s** | Execution taking too long (scheduled_at → delivered) |
| `easycron_dispatcher_events_in_flight` | Gauge | > 10 | Many concurrent webhook deliveries |
| `easycron_dispatcher_delivery_outcomes_total{outcome="failed"}` | Counter | Sustained increase | Webhooks failing after all retries |
| `easycron_dispatcher_delivery_outcomes_total{outcome="circuit_open"}` | Counter | Sustained increase | Executions skipped due to open circuit breaker |
| `easycron_scheduler_tick_errors_total` | Counter | Any increase | Scheduler tick errors (likely DB issues) |

#### Recommended Alerts

**Critical - Immediate Action Required:**
```yaml
# Buffer saturation critical - event loss happening
- alert: EasyCronBufferCritical
  expr: easycron_eventbus_buffer_saturation > 0.9
  for: 1m
  labels:
    severity: critical
  annotations:
    summary: "Event buffer nearly full, events being dropped"

# Orphaned executions present
- alert: EasyCronOrphanedExecutions
  expr: easycron_orphaned_executions > 0
  for: 5m
  labels:
    severity: critical
  annotations:
    summary: "{{ $value }} orphaned executions detected"
```

**Warning - Investigate Soon:**
```yaml
# Buffer saturation warning
- alert: EasyCronBufferWarning
  expr: easycron_eventbus_buffer_saturation > 0.8
  for: 5m
  labels:
    severity: warning
  annotations:
    summary: "Event buffer > 80% full"

# High execution latency
- alert: EasyCronHighLatency
  expr: histogram_quantile(0.99, rate(easycron_execution_latency_seconds_bucket[5m])) > 120
  for: 5m
  labels:
    severity: warning
  annotations:
    summary: "p99 execution latency > 2 minutes"

# Events dropped
- alert: EasyCronEventsDropped
  expr: increase(easycron_eventbus_emit_errors_total[5m]) > 0
  labels:
    severity: warning
  annotations:
    summary: "Events were dropped in the last 5 minutes"

# Circuit breaker open for a URL
- alert: EasyCronCircuitBreakerOpen
  expr: increase(easycron_dispatcher_delivery_outcomes_total{outcome="circuit_open"}[5m]) > 0
  labels:
    severity: warning
  annotations:
    summary: "Circuit breaker is open for one or more webhook URLs"
```

#### Quick Diagnosis Queries

**"Are we losing executions?"**
```promql
# Check for any event drops
increase(easycron_eventbus_emit_errors_total[1h]) > 0

# Check for orphans
easycron_orphaned_executions > 0
```

**"Is the system saturated?"**
```promql
# Buffer saturation (1.0 = full)
easycron_eventbus_buffer_saturation

# Events waiting for delivery
easycron_eventbus_buffer_size
```

**"How long are executions taking?"**
```promql
# p50, p90, p99 latency (scheduled_at → delivered)
histogram_quantile(0.5, rate(easycron_execution_latency_seconds_bucket[5m]))
histogram_quantile(0.9, rate(easycron_execution_latency_seconds_bucket[5m]))
histogram_quantile(0.99, rate(easycron_execution_latency_seconds_bucket[5m]))
```

**"Are webhooks failing?"**
```promql
# Failed deliveries rate
rate(easycron_dispatcher_delivery_outcomes_total{outcome="failed"}[5m])

# Circuit breaker activations
rate(easycron_dispatcher_delivery_outcomes_total{outcome="circuit_open"}[5m])

# Success rate
sum(rate(easycron_dispatcher_delivery_outcomes_total{outcome="success"}[5m])) /
sum(rate(easycron_dispatcher_delivery_outcomes_total[5m]))
```

### Key Metrics (via logs)

- `scheduler: emitted job=X` - Execution created and event sent
- `dispatcher: job=X delivered attempt=N` - Successful delivery
- `dispatcher: job=X failed` - All retries exhausted
- `dispatcher: job=X circuit open for URL, skipping` - Circuit breaker blocked delivery
- `db_dispatcher: worker N started` - DB mode worker started
- `db_dispatcher: claimed execution=X` - Worker claimed a row via `SELECT ... FOR UPDATE SKIP LOCKED`
- `db_dispatcher: no pending executions, sleeping` - Poll found nothing, sleeping `DB_POLL_INTERVAL`
- `reconciler: requeued N stale executions` - Reconciler reset stale `in_progress` rows to `emitted`
- `analytics: redis error` - Analytics write failed

### Database Queries

Check for stuck executions:
```sql
SELECT * FROM executions 
WHERE status = 'emitted' 
AND created_at < NOW() - INTERVAL '10 minutes';
```

Check delivery attempts for an execution:
```sql
SELECT * FROM delivery_attempts 
WHERE execution_id = '<uuid>' 
ORDER BY attempt;
```

## Capacity & Hard Limits

### Event Buffer

| Limit | Value | What Happens When Exceeded |
|-------|-------|---------------------------|
| Buffer capacity | 100 events | Scheduler blocks for up to 5s, then drops event |
| Emit timeout | 5 seconds | Event dropped, execution orphaned |

When buffer fills, you will see:
```
eventbus: buffer full, event dropped execution=<uuid> job=<uuid>
scheduler: ORPHAN execution=<uuid> job=<uuid> scheduled_at=<time> emit failed: event buffer full
```

### Database Connection Pool

| Setting | Default | Environment Variable |
|---------|---------|---------------------|
| Max open connections | 25 | `DB_MAX_OPEN_CONNS` |
| Max idle connections | 5 | `DB_MAX_IDLE_CONNS` |
| Connection max lifetime | 30m | `DB_CONN_MAX_LIFETIME` |
| Connection max idle time | 5m | `DB_CONN_MAX_IDLE_TIME` |
| Operation timeout | 5s | `DB_OP_TIMEOUT` |

### HTTP Client (Webhook Delivery)

| Limit | Value |
|-------|-------|
| Max idle connections | 100 total |
| Max idle per host | 10 |
| TLS handshake timeout | 10 seconds |
| Response header timeout | 30 seconds |

### Scheduler Limits

| Limit | Value | What Happens When Exceeded |
|-------|-------|---------------------------|
| Fire times per job per tick | 1000 (hardcoded) | Additional fires **permanently lost** (not inserted to DB) |
| Tick interval | 30s default | Shorter = more DB load, still minute resolution |

### Webhook Limits

| Limit | Value |
|-------|-------|
| Timeout per attempt | 1-60 seconds (default 30) |
| Max attempts | 4 (not configurable) |
| Max total retry duration | ~12 minutes (0 + 30s + 2m + 10m) |

### Conservative Operating Ranges

| Metric | Safe | Warning | Investigate |
|--------|------|---------|-------------|
| Jobs per instance | < 500 | 500-1000 | > 1000 |
| Executions per minute | < 50 | 50-100 | > 100 |
| Buffer utilization | < 50% | 50-80% | > 80% |
| Webhook p99 latency | < 5s | 5-15s | > 15s |

**What breaks first:** Buffer fills → events dropped → orphaned executions.

## Deployment

### Systemd Example

Create `/etc/systemd/system/easycron.service`:

```ini
[Unit]
Description=EasyCron Scheduler
After=network.target postgresql.service

[Service]
Type=simple
User=easycron
Environment=DATABASE_URL=postgres://localhost/easycron
Environment=HTTP_ADDR=:8080
ExecStart=/usr/local/bin/easycron serve
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
```

Then enable and start:

```bash
systemctl daemon-reload
systemctl enable easycron
systemctl start easycron
```
