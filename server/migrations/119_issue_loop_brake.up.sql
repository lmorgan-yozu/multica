CREATE TABLE issue_loop_brake_config (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    project_id UUID REFERENCES project(id) ON DELETE CASCADE,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    window_minutes INTEGER NOT NULL CHECK (window_minutes > 0),
    min_run_count INTEGER NOT NULL CHECK (min_run_count > 1),
    min_total_tokens BIGINT NOT NULL DEFAULT 0 CHECK (min_total_tokens >= 0),
    min_estimated_cost_usd NUMERIC(12, 6) NOT NULL DEFAULT 0 CHECK (min_estimated_cost_usd >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, project_id)
);

CREATE UNIQUE INDEX uq_issue_loop_brake_config_workspace_default
    ON issue_loop_brake_config (workspace_id)
    WHERE project_id IS NULL;

CREATE TABLE issue_loop_brake (
    issue_id UUID PRIMARY KEY REFERENCES issue(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    project_id UUID REFERENCES project(id) ON DELETE SET NULL,
    state TEXT NOT NULL CHECK (state IN ('active', 'cleared')),
    reason TEXT NOT NULL,
    evidence JSONB NOT NULL,
    window_started_at TIMESTAMPTZ NOT NULL,
    window_ended_at TIMESTAMPTZ NOT NULL,
    triggered_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    triggered_by_task_id UUID REFERENCES agent_task_queue(id) ON DELETE SET NULL,
    cleared_at TIMESTAMPTZ,
    cleared_by_type TEXT,
    cleared_by_id UUID,
    clear_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_issue_loop_brake_workspace_active
    ON issue_loop_brake (workspace_id, triggered_at DESC)
    WHERE state = 'active';

CREATE TABLE issue_loop_brake_audit (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    issue_id UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    actor_type TEXT NOT NULL,
    actor_id UUID,
    action TEXT NOT NULL CHECK (action IN ('triggered', 'cleared')),
    reason TEXT NOT NULL,
    evidence JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_issue_loop_brake_audit_issue
    ON issue_loop_brake_audit (issue_id, created_at DESC);
