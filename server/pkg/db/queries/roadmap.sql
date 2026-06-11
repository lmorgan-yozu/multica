-- Roadmap foundation queries (ADA-57): milestones, roadmap dependency
-- links, and the projection inputs for GET /api/projects/{id}/roadmap.

-- name: CreateMilestone :one
INSERT INTO milestone (
    project_id, name, description, target_date, position
) VALUES (
    $1, $2, $3, $4, $5
) RETURNING *;

-- name: GetMilestoneInWorkspace :one
-- Tenant guard: a milestone is reachable only through a project in the
-- caller's workspace.
SELECT m.* FROM milestone m
JOIN project p ON p.id = m.project_id
WHERE m.id = $1 AND p.workspace_id = $2;

-- name: ListProjectMilestones :many
SELECT * FROM milestone
WHERE project_id = $1
ORDER BY target_date ASC NULLS LAST, position ASC, created_at ASC, id ASC;

-- name: UpdateMilestone :one
UPDATE milestone SET
    name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    target_date = sqlc.narg('target_date'),
    position = COALESCE(sqlc.narg('position'), position),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteMilestone :exec
DELETE FROM milestone WHERE id = $1;

-- name: SetIssueMilestone :one
-- Workspace_id in the WHERE clause is a SQL-layer tenant guard.
UPDATE issue SET
    milestone_id = sqlc.narg('milestone_id'),
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: CreateIssueDependencyLink :one
-- Roadmap links are type='blocked_by': issue_id depends on
-- depends_on_issue_id. ON CONFLICT makes repeat creates idempotent.
INSERT INTO issue_dependency (issue_id, depends_on_issue_id, type)
VALUES ($1, $2, 'blocked_by')
ON CONFLICT (issue_id, depends_on_issue_id, type) DO UPDATE SET type = EXCLUDED.type
RETURNING *;

-- name: DeleteIssueDependencyLink :exec
DELETE FROM issue_dependency
WHERE issue_id = $1 AND depends_on_issue_id = $2 AND type = 'blocked_by';

-- name: ListProjectDependencyLinks :many
-- All roadmap (blocked_by) links whose dependent issue is in the project.
-- The same-project rule is enforced at write time, so joining one side is
-- sufficient; the projection drops any link whose other end it can't see.
SELECT d.issue_id, d.depends_on_issue_id FROM issue_dependency d
JOIN issue i ON i.id = d.issue_id
WHERE i.project_id = $1 AND d.type = 'blocked_by';

-- name: ListIssueDependencyLinksForIssues :many
-- Roadmap links between any of the given issues, used for write-time cycle
-- detection across a project's issue set.
SELECT d.issue_id, d.depends_on_issue_id FROM issue_dependency d
WHERE d.type = 'blocked_by' AND d.issue_id = ANY(sqlc.arg('issue_ids')::uuid[]);

-- name: ListProjectIssuesForRoadmap :many
-- Projection input: every issue in the project, any depth. The Go layer
-- derives epics, leaf rollups, and ordering from this single read.
SELECT id, workspace_id, number, title, status, priority, parent_issue_id,
       milestone_id, position, start_date, due_date, created_at
FROM issue
WHERE project_id = $1;
