-- Project roadmap foundation (ADA-57): milestone grouping plus hardening of
-- the (previously unused) issue_dependency table from 001_init so it can
-- carry explicit roadmap depends-on links. Additive only — projects with no
-- milestone or dependency rows derive their roadmap purely from the issue
-- tree.

CREATE TABLE milestone (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id  UUID NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    target_date DATE,
    position    FLOAT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_milestone_project ON milestone(project_id);

-- Optional grouping of an issue (in practice: an epic) under a milestone.
ALTER TABLE issue ADD COLUMN milestone_id UUID REFERENCES milestone(id) ON DELETE SET NULL;

CREATE INDEX idx_issue_milestone ON issue(milestone_id) WHERE milestone_id IS NOT NULL;

-- issue_dependency exists since 001_init but has never had a write path, so
-- it ships without indexes, uniqueness, or a self-link guard. Roadmap links
-- are stored as type='blocked_by' rows meaning: issue_id depends on
-- depends_on_issue_id (depends_on_issue_id must land first).
ALTER TABLE issue_dependency
    ADD COLUMN created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ADD CONSTRAINT chk_issue_dependency_no_self CHECK (issue_id <> depends_on_issue_id);

CREATE UNIQUE INDEX uq_issue_dependency_link
    ON issue_dependency(issue_id, depends_on_issue_id, type);
CREATE INDEX idx_issue_dependency_issue ON issue_dependency(issue_id);
CREATE INDEX idx_issue_dependency_depends_on ON issue_dependency(depends_on_issue_id);
