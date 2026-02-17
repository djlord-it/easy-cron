-- 002_add_indexes.sql
-- Add performance indexes based on query usage patterns

CREATE INDEX IF NOT EXISTS idx_jobs_enabled ON jobs(enabled);
CREATE INDEX IF NOT EXISTS idx_jobs_project_id ON jobs(project_id);
CREATE INDEX IF NOT EXISTS idx_executions_status_created_at ON executions(status, created_at);
CREATE INDEX IF NOT EXISTS idx_executions_job_id ON executions(job_id);
CREATE INDEX IF NOT EXISTS idx_delivery_attempts_execution_id ON delivery_attempts(execution_id);
