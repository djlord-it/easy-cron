-- 003_add_claimed_at.sql
-- Add claimed_at column for DB-backed dispatch mode.
-- NULL when not claimed, set to NOW() on dequeue.
ALTER TABLE executions ADD COLUMN claimed_at TIMESTAMPTZ;
