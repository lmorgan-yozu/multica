DROP INDEX IF EXISTS idx_issue_dependency_depends_on;
DROP INDEX IF EXISTS idx_issue_dependency_issue;
DROP INDEX IF EXISTS uq_issue_dependency_link;
ALTER TABLE issue_dependency
    DROP CONSTRAINT IF EXISTS chk_issue_dependency_no_self,
    DROP COLUMN IF EXISTS created_at;

ALTER TABLE issue DROP COLUMN IF EXISTS milestone_id;
DROP TABLE IF EXISTS milestone;
