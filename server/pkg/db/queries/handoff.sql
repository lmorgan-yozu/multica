-- name: CreateIssueHandoff :one
INSERT INTO issue_handoff (
    workspace_id,
    issue_id,
    task_id,
    author_type,
    author_id,
    next_assignee_type,
    next_assignee_id,
    workflow_run_id,
    workflow_step_id,
    work_completed,
    work_remaining,
    decisions_made,
    uncertainties
) VALUES (
    @workspace_id,
    @issue_id,
    sqlc.narg('task_id'),
    sqlc.narg('author_type'),
    sqlc.narg('author_id'),
    sqlc.narg('next_assignee_type'),
    sqlc.narg('next_assignee_id'),
    sqlc.narg('workflow_run_id'),
    sqlc.narg('workflow_step_id'),
    @work_completed,
    @work_remaining,
    @decisions_made,
    @uncertainties
)
RETURNING *;

-- name: GetIssueHandoffInWorkspace :one
SELECT * FROM issue_handoff
WHERE id = @id AND workspace_id = @workspace_id;

-- name: ListIssueHandoffsForIssue :many
SELECT * FROM issue_handoff
WHERE workspace_id = @workspace_id AND issue_id = @issue_id
ORDER BY created_at ASC, id ASC;

-- name: AddIssueHandoffFollowUpIssues :exec
INSERT INTO issue_handoff_follow_up_issue (issue_handoff_id, issue_id)
SELECT @issue_handoff_id::uuid, unnest(@issue_ids::uuid[])
ON CONFLICT DO NOTHING;

-- name: ListIssueHandoffFollowUpIssues :many
SELECT issue_handoff_id, issue_id
FROM issue_handoff_follow_up_issue
WHERE issue_handoff_id = ANY(@issue_handoff_ids::uuid[])
ORDER BY created_at ASC, issue_id ASC;

-- name: CountIssuesInWorkspaceByIDs :one
SELECT count(*)::int
FROM issue
WHERE workspace_id = @workspace_id AND id = ANY(@issue_ids::uuid[]);

-- name: GetLatestIssueHandoff :one
-- The single most recent handoff for an issue — the live handoff state used
-- when injecting structured context into a claimed agent task. Deliberately
-- LIMIT 1: prompts get the latest relevant handoff, not the full history
-- (summarisation of history is later ADA-6 memory work).
SELECT * FROM issue_handoff
WHERE workspace_id = @workspace_id AND issue_id = @issue_id
ORDER BY created_at DESC, id DESC
LIMIT 1;

-- name: LinkIssueHandoffToWorkflow :one
-- Adopts an agent-authored handoff as the workflow advancement record:
-- stamps the run/step linkage and the workflow's routing decision (next
-- assignee) onto it instead of inserting a thin duplicate row that would
-- shadow the agent's richer record as "latest". task_id is only filled in
-- when the record doesn't already carry one.
UPDATE issue_handoff
SET workflow_run_id = @workflow_run_id,
    workflow_step_id = @workflow_step_id,
    next_assignee_type = @next_assignee_type,
    next_assignee_id = @next_assignee_id,
    task_id = COALESCE(task_id, sqlc.narg('task_id')),
    updated_at = now()
WHERE id = @id
RETURNING *;

-- name: GetLatestTaskIDForIssueAndAgent :one
-- Best-effort task linkage for workflow-created handoff records: the most
-- recent task this agent ran (or is running) on the issue. Any status counts
-- — at advance time the outgoing agent's task is usually still 'running'
-- because the status change that triggers advancement happens mid-task.
SELECT id FROM agent_task_queue
WHERE issue_id = @issue_id AND agent_id = @agent_id
ORDER BY COALESCE(started_at, dispatched_at, created_at) DESC, created_at DESC
LIMIT 1;
