# Easy-Cron

[![Go](https://img.shields.io/badge/Go-1.22-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![CI](https://github.com/djlord-it/easy-cron/actions/workflows/ci.yml/badge.svg)](https://github.com/djlord-it/easy-cron/actions/workflows/ci.yml)

A cron job scheduler with webhook delivery.

## Overview

EasyCron schedules jobs based on cron expressions and delivers payloads to configured webhook endpoints. It consists of:

- **Scheduler**: Evaluates cron expressions and emits trigger events
- **Dispatcher**: Delivers webhooks with retry logic and HMAC signing
- **API**: REST endpoints for job management
- **Analytics**: Optional Redis-based execution metrics

## Architecture

The scheduler evaluates enabled jobs each tick, computes the next fire time using cron expressions, and inserts executions into the store. It then emits trigger events through an in-memory event bus. The dispatcher consumes these events, retrieves job configuration from the store, and delivers webhooks with exponential backoff retries.

## Configuration

### Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `DATABASE_URL` | Yes | - | PostgreSQL connection string |
| `REDIS_ADDR` | No | - | Redis address for analytics (optional) |
| `HTTP_ADDR` | No | `:8080` | API listen address |
| `TICK_INTERVAL` | No | `30s` | Scheduler tick interval |

### Tick Interval and Cron Resolution

Cron expressions use **minute-level resolution** (standard 5-field syntax). The `TICK_INTERVAL` controls how frequently the scheduler checks for due jobs:

- Tick intervals < 1 minute are allowed but do not increase cron precision
- A job scheduled for `*/5 * * * *` fires every 5 minutes regardless of tick interval
- Shorter tick intervals reduce latency between scheduled time and actual firing
- Default 30s provides good balance between responsiveness and overhead

### Job Configuration

Jobs are configured via the API with:

- `name`: Job identifier
- `cron_expression`: Standard 5-field cron syntax (minute hour day month weekday)
- `timezone`: IANA timezone
- `webhook_url`: Delivery endpoint
- `webhook_secret`: Optional HMAC secret
- `webhook_timeout_seconds`: Request timeout (1-60s, default 30)
- `analytics`: Optional metrics configuration
