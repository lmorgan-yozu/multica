CREATE TABLE issue_handoff (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id          UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    issue_id              UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    task_id               UUID REFERENCES agent_task_queue(id) ON DELETE SET NULL,
    author_type           TEXT CHECK (author_type IN ('agent', 'member')),
    author_id             UUID,
    next_assignee_type    TEXT CHECK (next_assignee_type IN ('agent', 'member', 'squad')),
    next_assignee_id      UUID,
    workflow_run_id       UUID REFERENCES issue_workflow_run(id) ON DELETE SET NULL,
    workflow_step_id      UUID REFERENCES workflow_step(id) ON DELETE SET NULL,
    work_completed        TEXT NOT NULL DEFAULT '',
    work_remaining        TEXT NOT NULL DEFAULT '',
    decisions_made        TEXT NOT NULL DEFAULT '',
    uncertainties         TEXT NOT NULL DEFAULT '',
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((author_type IS NULL) = (author_id IS NULL)),
    CHECK ((next_assignee_type IS NULL) = (next_assignee_id IS NULL))
);

CREATE INDEX idx_issue_handoff_issue
    ON issue_handoff(workspace_id, issue_id, created_at, id);

CREATE INDEX idx_issue_handoff_task
    ON issue_handoff(task_id)
    WHERE task_id IS NOT NULL;

CREATE TABLE issue_handoff_follow_up_issue (
    issue_handoff_id UUID NOT NULL REFERENCES issue_handoff(id) ON DELETE CASCADE,
    issue_id         UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (issue_handoff_id, issue_id)
);

CREATE INDEX idx_issue_handoff_follow_up_issue_issue
    ON issue_handoff_follow_up_issue(issue_id);
