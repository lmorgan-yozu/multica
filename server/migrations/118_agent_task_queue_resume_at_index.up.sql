-- Covers slice 2's resume sweep: failed cap tasks whose resume_at has
-- passed. Partial on exactly the sweep predicate so the index stays tiny
-- (cap failures are a small fraction of rows; everything else has
-- resume_at NULL).
--
-- agent_task_queue is hot, so this must stay in its own single-statement
-- migration and must use CONCURRENTLY — same rationale as 114.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_queue_failed_resume_at
    ON agent_task_queue (resume_at)
    WHERE status = 'failed' AND resume_at IS NOT NULL;
