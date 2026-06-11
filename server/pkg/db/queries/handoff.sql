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
