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
2. Dispatcher drains buffered events (up to 30 seconds)
3. HTTP server closes
4. Process exits with code 0

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
4. Dispatcher context cancelled
5. Dispatcher drains buffered events (max 30s)
6. Dispatcher goroutine exits
7. HTTP server closed
8. Process exits (code 0)
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

## Capacity

### Buffer Size

The in-memory event buffer holds 100 events. If the dispatcher cannot keep up, the scheduler blocks on emit (with 5-second timeout), then drops the event with `ErrBufferFull`.

### Connection Pooling

The HTTP webhook sender maintains up to 100 idle connections (10 per host). Adjust if targeting many unique webhook hosts.

### Tick Interval

Default 30 seconds. Shorter intervals increase database load but reduce latency. Cron resolution remains minute-level regardless of tick interval.

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
