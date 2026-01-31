# EasyCron Platform Context

## What We Are

We are a developer-focused infrastructure platform whose core responsibility is **TIME**.

We provide reliable, time-based triggers (cron) that tell customer systems **WHEN** something should run and **WHERE** the trigger should be delivered.

We do NOT:
- Execute customer code
- Deploy customer services
- Host applications

We only own:
- Scheduling
- Triggering
- Delivery guarantees

## What Problem We Solve

Developers often need:
- Reliable cron jobs
- Time-gated computation (e.g. only during peak hours)
- Scheduled analytics
- Periodic background work

They do NOT want:
- To embed cron logic in their app
- To manage retries, drift, or idempotency
- To operate Redis or Kafka unless necessary

Our product externalizes time and delivery into a dedicated infrastructure component.

## Core Product

**Cron is the core and always enabled.**

Cron evaluates schedules and emits trigger events at the correct time.

A trigger event represents:
- A specific job
- A specific scheduled time
- A guaranteed, idempotent execution signal

## Optional Plugins

Redis and Kafka are **OPTIONAL** plugins that customers may enable if and only if their workload requires them.

### Redis Plugin
- Used by customers for state, sliding windows, counters, locks, or queues
- Managed by us
- Never required

### Kafka Plugin
- Used by customers for buffering, fan-out, replay, or heavy data streams
- Managed by us
- Never required

**Cron works without Redis or Kafka.**

## Architecture

We operate a strict **two-plane architecture**.

### Control Plane (Ours)
- Go services only
- Cron scheduler
- Trigger dispatcher
- API
- Metadata database (PostgreSQL)
- Small internal Redis for locks, deduplication, and rate limiting
- Control plane NEVER uses customer Redis or Kafka

### Data Plane (Customer, Optional)
- Redis plugin (customer-facing)
- Kafka plugin (customer-facing)
- Completely isolated from control plane state

### Trigger Flow
Scheduler emits abstract trigger events.

Dispatcher routes trigger events to:
- Webhook (default)
- Redis plugin (if enabled)
- Kafka plugin (if enabled)

## Tech Constraints

- Backend language: **Go only**
- No Rust
- No Python
- No JVM
- Explicit, boring infrastructure patterns
- No hidden magic
- Clear separation of concerns

## Non-Goals

- No serverless runtime
- No container execution
- No application hosting
- No internal Kafka usage
- No real-time UI unless explicitly requested

## Guidance for Code

- Prefer simple, explicit designs
- Avoid premature optimization
- Enforce cron-core + plugin architecture
- Do not collapse control plane and data plane
- Treat Redis and Kafka as pluggable capabilities, not assumptions

---

**If any suggestion conflicts with the above, the suggestion is incorrect.**
