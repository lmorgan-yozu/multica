-- Handoff workflows: ordered, role-based agent handoff chains.
-- See docs/handoff-workflows.md. All tables are additive and opt-in: an issue
-- is only subject to a workflow when it has a row in issue_workflow_run.

CREATE TABLE workflow (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id    UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    created_by_type TEXT NOT NULL,
    created_by_id   UUID NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    archived_at     TIMESTAMPTZ
);

CREATE INDEX idx_workflow_workspace ON workflow(workspace_id) WHERE archived_at IS NULL;

CREATE TABLE workflow_step (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workflow_id    UUID NOT NULL REFERENCES workflow(id) ON DELETE CASCADE,
    step_order     INTEGER NOT NULL,
    agent_id       UUID NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    name           TEXT NOT NULL DEFAULT '',
    start_status   TEXT NOT NULL DEFAULT 'todo',
    advance_status TEXT NOT NULL DEFAULT 'in_review',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workflow_id, step_order)
);

CREATE INDEX idx_workflow_step_workflow ON workflow_step(workflow_id, step_order);

CREATE TABLE issue_workflow_run (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    issue_id        UUID NOT NULL UNIQUE REFERENCES issue(id) ON DELETE CASCADE,
    workflow_id     UUID NOT NULL REFERENCES workflow(id) ON DELETE CASCADE,
    current_step_id UUID NOT NULL REFERENCES workflow_step(id),
    state           TEXT NOT NULL DEFAULT 'active',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_issue_workflow_run_workflow ON issue_workflow_run(workflow_id);
