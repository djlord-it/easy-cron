# EasyCron

A distributed cron job scheduler with webhook delivery. Applications register jobs via REST API, and EasyCron delivers HTTP webhooks when jobs fire according to their cron schedules.

## Quick Start

1. Start PostgreSQL and create a database:
```bash
createdb easycron
```

2. Apply the schema:
```bash
psql easycron < schema/001_initial.sql
```

3. Set environment variables:
```bash
export DATABASE_URL="postgres://localhost/easycron?sslmode=disable"
```

4. Run the server:
```bash
./easycron serve
```

5. Verify it's running:
```bash
curl http://localhost:8080/health
# {"status":"ok"}
```

## Configuration

All configuration is via environment variables. No config files.

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `DATABASE_URL` | Yes | - | PostgreSQL connection string |
| `REDIS_ADDR` | No | - | Redis address for analytics |
| `HTTP_ADDR` | No | `:8080` | HTTP server listen address |
| `TICK_INTERVAL` | No | `30s` | Scheduler polling interval |

### Behavior

- Missing `DATABASE_URL`: Server refuses to start (exit code 2)
- Invalid `TICK_INTERVAL`: Server refuses to start (exit code 2)
- Missing `REDIS_ADDR`: Analytics disabled, server runs normally
- Validate config without starting: `./easycron validate`

## API Usage

### Create a Job

```bash
curl -X POST http://localhost:8080/jobs \
  -H "Content-Type: application/json" \
  -d '{
    "name": "hourly-report",
    "cron_expression": "0 * * * *",
    "timezone": "America/New_York",
    "webhook_url": "https://example.com/webhook",
    "webhook_secret": "your-secret-key"
  }'
```

Response:
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "project_id": "00000000-0000-0000-0000-000000000001",
  "name": "hourly-report",
  "enabled": true,
  "cron_expression": "0 * * * *",
  "timezone": "America/New_York",
  "webhook_url": "https://example.com/webhook",
  "created_at": "2024-01-15T10:30:00Z"
}
```

### Validation

The API validates:
- `name`: Required
- `cron_expression`: Required, must be valid 5-field cron syntax
- `timezone`: Required, must be valid IANA timezone
- `webhook_url`: Required, must be http or https with valid host
- `webhook_timeout_seconds`: Optional, 1-60 (default 30)

**Note:** Webhook timeout must be between 1 and 60 seconds. Requests exceeding the timeout are retried (up to 4 attempts total).

Invalid requests return HTTP 400 with error message.

### List Jobs

```bash
curl http://localhost:8080/jobs
```

### List Executions

```bash
curl http://localhost:8080/jobs/{job_id}/executions
```

### Webhook Delivery

When a job fires, EasyCron sends a POST request to the configured webhook URL:

```http
POST /webhook HTTP/1.1
Host: example.com
Content-Type: application/json
X-EasyCron-Event-ID: <attempt-uuid>
X-EasyCron-Execution-ID: <execution-uuid>
X-EasyCron-Signature: <hmac-sha256-hex>

{
  "job_id": "550e8400-e29b-41d4-a716-446655440000",
  "execution_id": "660e8400-e29b-41d4-a716-446655440001",
  "scheduled_at": "2024-01-15T11:00:00Z",
  "fired_at": "2024-01-15T11:00:02Z"
}
```

#### Signature Verification

The `X-EasyCron-Signature` header contains an HMAC-SHA256 signature of the request body, computed using the job's `webhook_secret`. To verify:

```go
import (
    "crypto/hmac"
    "crypto/sha256"
    "encoding/hex"
)

func verifySignature(secret string, body []byte, signature string) bool {
    mac := hmac.New(sha256.New, []byte(secret))
    mac.Write(body)
    expected := hex.EncodeToString(mac.Sum(nil))
    // Use constant-time comparison to prevent timing attacks
    return hmac.Equal([]byte(expected), []byte(signature))
}
```

#### Header Descriptions

| Header | Description |
|--------|-------------|
| `X-EasyCron-Event-ID` | Unique ID for this delivery attempt (changes on each retry) |
| `X-EasyCron-Execution-ID` | Unique ID for the scheduled execution (same across retries) |
| `X-EasyCron-Signature` | HMAC-SHA256 signature for verification |

Use `X-EasyCron-Execution-ID` for idempotency checks in your webhook handler.

#### Retry Behavior

Failed deliveries are retried with stepped backoff (fixed intervals, not exponential):
- Attempt 1: Immediate
- Attempt 2: After 30 seconds
- Attempt 3: After 2 minutes
- Attempt 4: After 10 minutes

Retryable failures: 5xx responses, 429 (rate limit), network errors.
Non-retryable: 4xx responses (except 429).

After 4 failed attempts, the execution is marked as `failed`.

## Integration Modes

EasyCron runs in single-tenant mode with a fixed project ID. Each application should run its own EasyCron instance.

### Sidecar (Local/Dev)

Run EasyCron alongside your application:

```bash
# Terminal 1: Your application
./your-app

# Terminal 2: EasyCron
DATABASE_URL="postgres://localhost/easycron" ./easycron serve
```

Your application registers jobs via `http://localhost:8080/jobs`.

### Standalone (Shared Service)

Deploy EasyCron as a shared service. Multiple applications register jobs via the API. Each application receives webhooks at its configured URL.

### What Another Repository Needs

To integrate with EasyCron, your application needs:

1. HTTP client to call the EasyCron API
2. HTTP endpoint to receive webhooks
3. (Optional) HMAC verification for webhook authenticity

No SDK required. No special dependencies. HTTP only.

## CLI Commands

```
easycron serve      Start the scheduler and dispatcher
easycron validate   Validate configuration (no connections)
easycron config     Print effective configuration as JSON
easycron version    Print version information
easycron --help     Show usage
```

## Building

```bash
go build ./cmd/easycron
```

With version injection:
```bash
go build -ldflags "-X main.version=v1.0.0 -X main.commit=$(git rev-parse --short HEAD)" ./cmd/easycron
```

## License

See LICENSE file.
