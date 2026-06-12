ALTER TABLE project
    ADD COLUMN quality_gate_config JSONB NOT NULL DEFAULT '{}'::jsonb;

CREATE TABLE issue_quality_gate_event (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    project_id UUID NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    issue_id UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    gate_key TEXT NOT NULL,
    gate_name TEXT NOT NULL,
    from_status TEXT NOT NULL,
    to_status TEXT NOT NULL,
    actor_type TEXT NOT NULL CHECK (actor_type IN ('member', 'agent', 'system')),
    actor_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_issue_quality_gate_event_issue
    ON issue_quality_gate_event(issue_id, gate_key, created_at DESC);

CREATE INDEX idx_issue_quality_gate_event_project
    ON issue_quality_gate_event(project_id, created_at DESC);
