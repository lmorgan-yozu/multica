-- =====================
-- Handoff workflows
-- =====================

-- name: CreateWorkflow :one
INSERT INTO workflow (workspace_id, name, description, created_by_type, created_by_id)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetWorkflowInWorkspace :one
SELECT * FROM workflow
WHERE id = $1 AND workspace_id = $2 AND archived_at IS NULL;

-- name: ListWorkflows :many
SELECT * FROM workflow
WHERE workspace_id = $1 AND archived_at IS NULL
ORDER BY created_at DESC;

-- name: ArchiveWorkflow :exec
UPDATE workflow
SET archived_at = now(), updated_at = now()
WHERE id = $1 AND workspace_id = $2;

-- name: CreateWorkflowStep :one
INSERT INTO workflow_step (workflow_id, step_order, agent_id, name, start_status, advance_status)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListWorkflowSteps :many
SELECT * FROM workflow_step
WHERE workflow_id = $1
ORDER BY step_order ASC;

-- name: GetWorkflowStep :one
SELECT * FROM workflow_step
WHERE id = $1;

-- name: MaxWorkflowStepOrder :one
SELECT COALESCE(MAX(step_order), 0)::int AS max_order
FROM workflow_step
WHERE workflow_id = $1;

-- name: CreateIssueWorkflowRun :one
INSERT INTO issue_workflow_run (issue_id, workflow_id, current_step_id, state)
VALUES ($1, $2, $3, 'active')
RETURNING *;

-- name: GetIssueWorkflowRun :one
SELECT * FROM issue_workflow_run
WHERE issue_id = $1;

-- name: SetIssueWorkflowRunStep :exec
UPDATE issue_workflow_run
SET current_step_id = $2, updated_at = now()
WHERE id = $1;

-- name: SetIssueWorkflowRunState :exec
UPDATE issue_workflow_run
SET state = $2, updated_at = now()
WHERE id = $1;

-- name: AssignIssueToWorkflowStep :one
-- Narrow update used by the handoff engine: reassign an issue to the next
-- step's agent and set its status to that step's start_status in one write.
UPDATE issue SET
    assignee_type = 'agent',
    assignee_id = $2,
    status = $3,
    updated_at = now()
WHERE id = $1
RETURNING *;
