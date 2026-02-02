# EasyCron Operator Guide

This document defines the operational contract for EasyCron. It describes what the system guarantees, what it does not guarantee, and how it behaves under failure.

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

### Graceful Shutdown

On SIGINT or SIGTERM:
1. Scheduler stops immediately (no new executions emitted)
2. Reconciler stops (if enabled, no new re-emits)
3. Dispatcher drains buffered events (up to 30 seconds)
4. HTTP server closes
5. Process exits with code 0

## Non-Guarantees

### No Durable Queue

The event bus is an in-memory buffered channel (capacity 100). Events are not persisted. If the process crashes between scheduler emit and dispatcher delivery, those events are lost.

### No Exactly-Once Delivery

Webhooks may be delivered zero times (crash before delivery) or multiple times (crash after delivery but before status update). Design webhook handlers to be idempotent.

### No Retry After Restart

If the process restarts during a retry sequence, remaining retries are abandoned. The execution status reflects the last known state before crash.

### Analytics Best-Effort

Redis analytics may undercount executions if Redis is unavailable. Analytics failures are logged but never block delivery.

## Failure Modes

### PostgreSQL Unavailable

| Phase | Behavior |
|-------|----------|
| Startup | Server refuses to start (exit code 1) |
| Runtime (scheduler) | Tick fails, logged, retried next tick |
| Runtime (dispatcher) | Job fetch fails, event logged, not retried |
| Runtime (API) | Request fails with HTTP 500 |

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

### Invalid Configuration

| Error | Exit Code |
|-------|-----------|
| Missing DATABASE_URL | 2 |
| Invalid TICK_INTERVAL | 2 |
| Invalid DATABASE_URL format | 1 (runtime) |

## Shutdown Semantics

### Order of Operations

```
1. Signal received (SIGINT/SIGTERM)
2. Scheduler context cancelled
3. Scheduler goroutine exits (immediate)
4. Reconciler context cancelled (if enabled)
5. Reconciler goroutine exits (immediate)
6. Dispatcher context cancelled
7. Dispatcher drains buffered events (max 30s)
8. Dispatcher goroutine exits
9. HTTP server closed
10. Process exits (code 0)
```

### Drain Timeout

The dispatcher has 30 seconds to process remaining buffered events. Events still in the buffer after timeout are lost.

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
-- Find orphaned executions (emitted > 15 minutes ago, no delivery)
SELECT e.id, e.job_id, e.scheduled_at, e.created_at
FROM executions e
LEFT JOIN delivery_attempts d ON d.execution_id = e.id
WHERE e.status = 'emitted'
  AND e.created_at < NOW() - INTERVAL '15 minutes'
  AND d.id IS NULL;
```

### Definition

An execution is considered orphaned if:
- Status is `emitted`
- Created more than 15 minutes ago
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

### Key Metrics (via logs)

- `scheduler: emitted job=X` - Execution created and event sent
- `dispatcher: job=X delivered attempt=N` - Successful delivery
- `dispatcher: job=X failed` - All retries exhausted
- `analytics: redis error` - Analytics write failed

### Database Queries

Check for stuck executions:
```sql
SELECT * FROM executions 
WHERE status = 'emitted' 
AND created_at < NOW() - INTERVAL '15 minutes';
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

### Connection Pooling

| Limit | Value |
|-------|-------|
| Max idle connections | 100 total |
| Max idle per host | 10 |
| TLS handshake timeout | 10 seconds |
| Response header timeout | 30 seconds |

### Scheduler Limits

| Limit | Value | What Happens When Exceeded |
|-------|-------|---------------------------|
| Fire times per job per tick | 1000 | Additional fires skipped silently |
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
