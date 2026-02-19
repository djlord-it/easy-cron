# EasyCron Operator Guide

Operational contract: what EasyCron guarantees, how it fails, and how to run it.

## Configuration Reference

| Variable | Default | Description |
|----------|---------|-------------|
| `DATABASE_URL` | *required* | PostgreSQL connection string |
| `HTTP_ADDR` | `:8080` | Listen address |
| `TICK_INTERVAL` | `30s` | Scheduler polling interval |
| `DISPATCH_MODE` | `channel` | `channel` (in-memory) or `db` (Postgres polling) |
| `DISPATCHER_WORKERS` | `1` | Concurrent dispatch workers (DB mode) |
| `DB_POLL_INTERVAL` | `500ms` | Sleep between polls when idle (DB mode) |
| `RECONCILE_ENABLED` | `false` | Enable orphan recovery |
| `RECONCILE_INTERVAL` | `5m` | Orphan scan frequency |
| `RECONCILE_THRESHOLD` | `10m` | Age before execution is considered orphaned |
| `RECONCILE_BATCH_SIZE` | `100` | Max orphans per cycle |
| `METRICS_ENABLED` | `false` | Enable Prometheus `/metrics` |
| `CIRCUIT_BREAKER_THRESHOLD` | `5` | Consecutive failures to open circuit (0 = disabled) |
| `CIRCUIT_BREAKER_COOLDOWN` | `2m` | Cooldown before probe attempt |
| `DB_OP_TIMEOUT` | `5s` | Max time per DB operation |
| `DB_MAX_OPEN_CONNS` | `25` | Max open DB connections |
| `DB_MAX_IDLE_CONNS` | `5` | Max idle DB connections |
| `DB_CONN_MAX_LIFETIME` | `30m` | Connection max lifetime |
| `DB_CONN_MAX_IDLE_TIME` | `5m` | Connection idle timeout |
| `HTTP_SHUTDOWN_TIMEOUT` | `10s` | Graceful shutdown timeout for HTTP |
| `DISPATCHER_DRAIN_TIMEOUT` | `30s` | Drain timeout for buffered events |
| `LEADER_LOCK_KEY` | `728379` | Postgres advisory lock key (DB mode) |
| `LEADER_RETRY_INTERVAL` | `5s` | Follower lock acquisition retry (DB mode) |
| `LEADER_HEARTBEAT_INTERVAL` | `2s` | Leader connection health check (DB mode) |

### Required in Production

| Variable | Value | Why |
|----------|-------|-----|
| `RECONCILE_ENABLED` | `true` | Without this, orphaned executions are **permanently lost** |
| `METRICS_ENABLED` | `true` | Required for observability and alerting |

## Guarantees and Non-Guarantees

**EasyCron guarantees:**
- At-most-once execution creation per `(job_id, scheduled_at)` — DB unique constraint
- At-most 4 webhook delivery attempts with bounded backoff
- Terminal state immutability — `delivered` and `failed` never change
- Bounded DB operations — all queries timeout after `DB_OP_TIMEOUT`
- Ordered shutdown — scheduler stops before dispatcher drains
- Idempotent re-emits — reconciler reuses original execution ID

**EasyCron does NOT guarantee:**
- Webhook delivery — events can be lost (crash, buffer full)
- Exactly-once delivery — webhooks may arrive 0, 1, or 2+ times
- Retry continuation after restart — incomplete retries are abandoned
- Recovery without reconciler — orphans are permanent if disabled
- In-memory state persistence — event buffer and circuit breaker reset on restart

**Your responsibilities:**
1. Set `RECONCILE_ENABLED=true` in production
2. Design idempotent webhook handlers (use `X-EasyCron-Execution-ID` for dedup)
3. Monitor metrics (see [Monitoring](#monitoring))
4. Keep job frequency under 1000 fires per job per tick

## Dispatch Modes

| | Channel (default) | DB |
|---|---|---|
| **How** | In-memory EventBus (100-event buffer) | Postgres polling with `SKIP LOCKED` |
| **Crash resilience** | Buffer lost on crash | Rows survive crash |
| **Scaling** | Single process only | Multiple workers / instances |
| **DB load** | Lower | Higher (polling every `DB_POLL_INTERVAL`) |

DB mode requires migration: `psql easycron < schema/003_add_claimed_at.sql`

## Execution Lifecycle

```
emitted ──→ delivered  (terminal)
   │
   ├──→ failed  (terminal)
   │
   └──→ in_progress ──→ delivered  (DB mode only)
            │
            ├──→ failed
            │
            └──→ emitted  (stale requeue by reconciler)
```

Terminal states (`delivered`, `failed`) are immutable.

## Circuit Breaker

Per-URL, in-memory, resets on restart.

```
CLOSED ──(N consecutive failures)──→ OPEN ──(cooldown)──→ HALF_OPEN
                                       ↑                      │
                                       └──(probe fails)───────┘
                                              │
                                        (probe succeeds) → CLOSED
```

- Counts fully-failed **executions** (not individual HTTP attempts)
- Open circuit → executions immediately marked `failed`, no HTTP calls
- Each URL has an independent circuit
- Set `CIRCUIT_BREAKER_THRESHOLD=0` to disable

## Failure Modes

| Failure | Behavior |
|---------|----------|
| **Postgres down at startup** | Refuses to start (exit 1) |
| **Postgres down at runtime** | Tick/dispatch fails after `DB_OP_TIMEOUT`, retried next cycle |
| **Redis down** | Analytics disabled, delivery unaffected |
| **Webhook timeout/5xx/429** | Retryable, up to 4 attempts |
| **Webhook 4xx (not 429)** | Non-retryable, marked `failed` immediately |
| **Circuit open** | Execution marked `failed`, no HTTP call |
| **Buffer full (channel mode)** | Event dropped after 5s, execution orphaned |
| **Process crash** | Buffer lost; reconciler recovers DB-inserted orphans |

## Orphaned Executions

An execution is orphaned when `status = 'emitted'` but will never be delivered (buffer full, crash, shutdown).

**With reconciler enabled** (recommended): automatically detected and re-emitted every `RECONCILE_INTERVAL`.

**Without reconciler**: remains stuck in DB. Manual options: accept the loss, call webhook directly, or delete the record.

**Detection query:**
```sql
SELECT id, job_id, scheduled_at, created_at
FROM executions
WHERE status = 'emitted'
  AND created_at < NOW() - INTERVAL '10 minutes';
```

## Shutdown

On SIGINT/SIGTERM: scheduler stops → reconciler stops → dispatcher drains (`DISPATCHER_DRAIN_TIMEOUT`) → HTTP server stops (`HTTP_SHUTDOWN_TIMEOUT`) → exit 0.

**Max shutdown time:** `DISPATCHER_DRAIN_TIMEOUT` + `HTTP_SHUTDOWN_TIMEOUT` (default 40s).

Events in the in-memory buffer and incomplete retry sequences may be lost.

## Horizontal Scaling (Multi-Instance HA)

Run multiple instances against the same Postgres for high availability. Requires `DISPATCH_MODE=db`.

```
┌──────────────┐   ┌──────────────┐   ┌──────────────┐
│  Instance 1  │   │  Instance 2  │   │  Instance 3  │
│  (leader)    │   │  (follower)  │   │  (follower)  │
│  scheduler ✓ │   │  scheduler ✗ │   │  scheduler ✗ │
│  reconciler ✓│   │  reconciler ✗│   │  reconciler ✗│
│  dispatcher ✓│   │  dispatcher ✓│   │  dispatcher ✓│
│  API ✓       │   │  API ✓       │   │  API ✓       │
└──────┬───────┘   └──────┬───────┘   └──────┬───────┘
       └──────────────────┼──────────────────┘
                   ┌──────┴──────┐
                   │  PostgreSQL  │
                   └─────────────┘
```

Leader election uses `pg_try_advisory_lock`. The leader runs scheduler + reconciler. All instances run dispatch workers and serve the API.

### Required Configuration (same on all instances)

```bash
DISPATCH_MODE=db
DATABASE_URL=postgres://...        # same DB
LEADER_LOCK_KEY=728379             # same lock key
RECONCILE_ENABLED=true
METRICS_ENABLED=true
```

### Tuning

| Variable | Default | HA Recommendation | Trade-off |
|----------|---------|-------------------|-----------|
| `LEADER_RETRY_INTERVAL` | `5s` | `3s`–`5s` | Faster failover vs. lock contention |
| `LEADER_HEARTBEAT_INTERVAL` | `2s` | `1s`–`2s` | Faster death detection vs. DB pings |
| `DISPATCHER_WORKERS` | `1` | `2`–`4` | Throughput vs. DB connections |
| `TICK_INTERVAL` | `30s` | `10s`–`30s` | Scheduling gap vs. DB load |

### Failover

When the leader dies: Postgres releases the advisory lock (TCP keepalive, 0–5s) → follower acquires lock on next retry → new leader starts scheduler + reconciler.

**Worst-case failover: 3–10s.** During the gap, no new executions are scheduled but dispatch continues on all instances.

The `(job_id, scheduled_at)` unique constraint prevents double-scheduling even during brief split-brain.

### HA Test Harness

```bash
./scripts/ha_test.sh
```

Starts 3 instances, verifies single leader, kills leader, asserts failover, checks no double-scheduling. See [`docs/ha-test.md`](docs/ha-test.md).

## Monitoring

### Health Check

`GET /health` → `{"status":"ok"}` (200) or `{"status":"degraded"}` (503). Add `?verbose=true` for component details.

### Key Metrics

Enable with `METRICS_ENABLED=true`.

| Metric | Alert When | Meaning |
|--------|------------|---------|
| `easycron_eventbus_buffer_saturation` | > 0.8 | Buffer filling, event loss imminent |
| `easycron_orphaned_executions` | > 0 | Orphans detected |
| `easycron_execution_latency_seconds` | p99 > 60s | Slow delivery |
| `easycron_scheduler_tick_errors_total` | any increase | DB issues |
| `easycron_dispatcher_delivery_outcomes_total{outcome="failed"}` | sustained increase | Webhooks failing |
| `easycron_dispatcher_delivery_outcomes_total{outcome="circuit_open"}` | sustained increase | Circuit breaker active |
| `easycron_leader_is_leader` | `sum() == 0` | No leader (HA mode) |
| `easycron_leader_is_leader` | `sum() > 1` | Split brain (HA mode) |

### Recommended Alerts

```yaml
# Critical
- alert: EasyCronOrphanedExecutions
  expr: easycron_orphaned_executions > 0
  for: 5m
  labels: { severity: critical }

- alert: EasyCronBufferCritical
  expr: easycron_eventbus_buffer_saturation > 0.9
  for: 1m
  labels: { severity: critical }

- alert: EasyCronNoLeader
  expr: sum(easycron_leader_is_leader) == 0
  for: 30s
  labels: { severity: critical }

# Warning
- alert: EasyCronBufferWarning
  expr: easycron_eventbus_buffer_saturation > 0.8
  for: 5m
  labels: { severity: warning }

- alert: EasyCronHighLatency
  expr: histogram_quantile(0.99, rate(easycron_execution_latency_seconds_bucket[5m])) > 120
  for: 5m
  labels: { severity: warning }

- alert: EasyCronLeaderFlapping
  expr: increase(easycron_leader_acquisitions_total[5m]) > 3
  for: 1m
  labels: { severity: warning }
```

### Quick Diagnosis

```promql
# Losing executions?
increase(easycron_eventbus_emit_errors_total[1h])
easycron_orphaned_executions

# System saturated?
easycron_eventbus_buffer_saturation

# Delivery latency?
histogram_quantile(0.99, rate(easycron_execution_latency_seconds_bucket[5m]))

# Webhook success rate?
sum(rate(easycron_dispatcher_delivery_outcomes_total{outcome="success"}[5m])) /
sum(rate(easycron_dispatcher_delivery_outcomes_total[5m]))
```

### Log Signals

```
scheduler: emitted job=X                          # Execution created
dispatcher: job=X delivered attempt=N              # Webhook delivered
dispatcher: job=X failed                           # All retries exhausted
dispatcher: job=X circuit open for URL, skipping   # Circuit breaker active
reconciler: found N orphaned executions            # Orphans detected
leader: acquired advisory lock 728379              # Became leader
leader: lock 728379 held by another instance       # Follower
leader: released advisory lock 728379              # Lost leadership
```

## Capacity Limits

| Resource | Limit | Consequence |
|----------|-------|-------------|
| Event buffer (channel mode) | 100 events | Drops after 5s block → orphan |
| Fires per job per tick | 1000 (hardcoded) | Excess **permanently lost** |
| Webhook timeout | 1–60s (default 30) | Retried up to 4 attempts |
| Max retry duration | ~12 min | Then marked `failed` |
| Max shutdown time | 40s default | Buffer + HTTP drain |

### Safe Operating Ranges

| Metric | Safe | Warning | Investigate |
|--------|------|---------|-------------|
| Jobs per instance | < 500 | 500–1000 | > 1000 |
| Executions per minute | < 50 | 50–100 | > 100 |
| Buffer utilization | < 50% | 50–80% | > 80% |
| Webhook p99 latency | < 5s | 5–15s | > 15s |

## Deployment

### Systemd

```ini
[Unit]
Description=EasyCron Scheduler
After=network.target postgresql.service

[Service]
Type=simple
User=easycron
Environment=DATABASE_URL=postgres://localhost/easycron
Environment=HTTP_ADDR=:8080
Environment=RECONCILE_ENABLED=true
Environment=METRICS_ENABLED=true
ExecStart=/usr/local/bin/easycron serve
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
```

```bash
systemctl daemon-reload && systemctl enable --now easycron
```
