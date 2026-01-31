# Done

## 2026-01-31

### Phase 1: Scheduler
- Domain models: Job, Schedule, Execution, TriggerEvent
- Polling scheduler with Next(lastTick) loop
- Timezone-aware cron evaluation
- DB-enforced idempotency via unique constraint

### Phase 2: Dispatcher
- Webhook delivery with HMAC-SHA256 signing
- Retry: 4 attempts, backoff 0/30s/2m/10m
- Retryable: network errors, 5xx, 429
- Headers: X-EasyCron-Event-ID (attempt), X-EasyCron-Execution-ID, X-EasyCron-Signature
- HTTP client reuse with per-request context timeout
