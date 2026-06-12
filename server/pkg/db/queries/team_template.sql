-- name: ListTeamTemplates :many
SELECT * FROM team_template
WHERE workspace_id = $1 AND archived_at IS NULL
ORDER BY name ASC;

-- name: GetTeamTemplateInWorkspace :one
SELECT * FROM team_template
WHERE id = $1 AND workspace_id = $2;

-- name: CreateTeamTemplate :one
INSERT INTO team_template (
    workspace_id, name, description, created_by
) VALUES (
    $1, $2, $3, $4
) RETURNING *;

-- name: UpdateTeamTemplate :one
UPDATE team_template
SET name        = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    updated_at  = now()
WHERE id = $1
RETURNING *;

-- name: ArchiveTeamTemplate :one
UPDATE team_template
SET archived_at = now(),
    archived_by = $2,
    updated_at  = now()
WHERE id = $1 AND archived_at IS NULL
RETURNING *;

-- name: ListTeamTemplateVersions :many
SELECT id, template_id, version, source_workspace_id, notes, created_by, created_at
FROM team_template_version
WHERE template_id = $1
ORDER BY version DESC;

-- name: GetTeamTemplateVersion :one
SELECT * FROM team_template_version
WHERE template_id = $1 AND version = $2;

-- name: GetLatestTeamTemplateVersion :one
SELECT * FROM team_template_version
WHERE template_id = $1
ORDER BY version DESC
LIMIT 1;

-- name: MaxTeamTemplateVersion :one
SELECT COALESCE(MAX(version), 0)::int FROM team_template_version
WHERE template_id = $1;

-- name: CreateTeamTemplateVersion :one
INSERT INTO team_template_version (
    template_id, version, manifest, source_workspace_id, notes, created_by
) VALUES (
    $1, $2, $3, $4, $5, $6
) RETURNING *;
