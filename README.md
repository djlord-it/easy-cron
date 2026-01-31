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

Jobs are configured via the API with:

- `name`: Job identifier
- `cron_expression`: Standard 5-field cron syntax
- `timezone`: IANA timezone
- `webhook_url`: Delivery endpoint
- `webhook_secret`: Optional HMAC secret
- `webhook_timeout_seconds`: Request timeout (1-60s, default 30)
- `analytics`: Optional metrics configuration
