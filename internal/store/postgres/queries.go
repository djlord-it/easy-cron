package postgres

const queryGetEnabledJobs = `
SELECT
    j.id, j.project_id, j.name, j.enabled, j.schedule_id,
    j.delivery_type, j.webhook_url, j.secret, j.timeout_ms,
    j.analytics_enabled, j.analytics_retention_seconds,
    j.created_at, j.updated_at,
    s.id, s.cron_expression, s.timezone, s.created_at, s.updated_at
FROM jobs j
JOIN schedules s ON j.schedule_id = s.id
WHERE j.enabled = true
`

const queryInsertExecution = `
INSERT INTO executions (id, job_id, project_id, scheduled_at, fired_at, status, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
`

const queryGetJobByID = `
SELECT
    id, project_id, name, enabled, schedule_id,
    delivery_type, webhook_url, secret, timeout_ms,
    analytics_enabled, analytics_retention_seconds,
    created_at, updated_at
FROM jobs
WHERE id = $1
`

const queryInsertDeliveryAttempt = `
INSERT INTO delivery_attempts (id, execution_id, attempt, status_code, error, started_at, finished_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
`

const queryGetExecutionStatus = `
SELECT status FROM executions WHERE id = $1
`

const queryUpdateExecutionStatus = `
UPDATE executions SET status = $1 WHERE id = $2
`

const queryInsertSchedule = `
INSERT INTO schedules (id, cron_expression, timezone, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5)
`

const queryInsertJob = `
INSERT INTO jobs (id, project_id, name, enabled, schedule_id, delivery_type, webhook_url, secret, timeout_ms, analytics_enabled, analytics_retention_seconds, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
`

const queryListJobs = `
SELECT
    j.id, j.project_id, j.name, j.enabled, j.schedule_id,
    j.delivery_type, j.webhook_url, j.secret, j.timeout_ms,
    j.analytics_enabled, j.analytics_retention_seconds,
    j.created_at, j.updated_at,
    s.id, s.cron_expression, s.timezone, s.created_at, s.updated_at
FROM jobs j
JOIN schedules s ON j.schedule_id = s.id
WHERE j.project_id = $1
ORDER BY j.created_at DESC
`

const queryListExecutions = `
SELECT id, job_id, project_id, scheduled_at, fired_at, status, created_at
FROM executions
WHERE job_id = $1
ORDER BY scheduled_at DESC
`

const queryDeleteJob = `
WITH deleted_attempts AS (
    DELETE FROM delivery_attempts
    WHERE execution_id IN (SELECT id FROM executions WHERE job_id = $1)
),
deleted_executions AS (
    DELETE FROM executions WHERE job_id = $1
)
DELETE FROM jobs WHERE id = $1 AND project_id = $2
RETURNING id`
