-- Team templates: exportable, versioned snapshots of a delivery team
-- (role agents, skills, squad shape, workflows, autopilots) that can be
-- applied to a new engagement. See docs/adr/0002-team-templates.md.

CREATE TABLE team_template (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_by UUID,
    archived_at TIMESTAMPTZ,
    archived_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT team_template_workspace_name_unique UNIQUE (workspace_id, name)
);

CREATE INDEX idx_team_template_workspace ON team_template(workspace_id);

-- Versions are immutable: a re-export appends a new row, never rewrites.
-- The manifest is the whole template document (format-versioned JSON);
-- integrity is enforced by the Go validator before insert.
CREATE TABLE team_template_version (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    template_id UUID NOT NULL REFERENCES team_template(id) ON DELETE CASCADE,
    version INT NOT NULL,
    manifest JSONB NOT NULL,
    source_workspace_id UUID,
    notes TEXT NOT NULL DEFAULT '',
    created_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT team_template_version_unique UNIQUE (template_id, version)
);

CREATE INDEX idx_team_template_version_template ON team_template_version(template_id, version DESC);
