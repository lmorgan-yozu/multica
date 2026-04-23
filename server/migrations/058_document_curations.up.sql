-- Document curations: per-attachment metadata that turns issue/project
-- attachments into a browsable document library with agent-driven
-- organisation. Curation is orthogonal to the attachment record itself so
-- that upstream changes to attachment handling stay unaffected.

CREATE TABLE document_curation (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    attachment_id UUID NOT NULL REFERENCES attachment(id) ON DELETE CASCADE,

    -- Curation-layer fields. All optional; a row exists only when there is
    -- something to record beyond the default "uncategorised, unordered".
    title TEXT,                       -- Human-friendly override; attachment.filename is the fallback
    summary TEXT,                     -- Short description (1-2 sentences) surfaced in lists
    category TEXT,                    -- Free-form (e.g. "Research", "Architecture", "Decisions")
    tags TEXT[] NOT NULL DEFAULT '{}',
    sort_order DOUBLE PRECISION NOT NULL DEFAULT 0, -- Manual ordering within a category
    pinned BOOLEAN NOT NULL DEFAULT FALSE,          -- Surface first in the issue/project header
    archived BOOLEAN NOT NULL DEFAULT FALSE,        -- Hide from default views without deleting

    -- Who curated and when. curator_type is member | agent so we can
    -- attribute organisation work to the agent that did it.
    curator_type TEXT NOT NULL CHECK (curator_type IN ('member', 'agent')),
    curator_id UUID NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (attachment_id)
);

CREATE INDEX idx_document_curation_workspace ON document_curation (workspace_id);
CREATE INDEX idx_document_curation_category ON document_curation (workspace_id, category) WHERE category IS NOT NULL;
CREATE INDEX idx_document_curation_tags ON document_curation USING GIN (tags);

-- Library sections: ordered, named groupings of documents that can span
-- multiple attachments. A section lives at issue scope or project scope.
-- This is what the UI renders as the left-hand tree.
CREATE TABLE library_section (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,

    -- Exactly one of issue_id / project_id is non-null.
    issue_id UUID REFERENCES issue(id) ON DELETE CASCADE,
    project_id UUID REFERENCES project(id) ON DELETE CASCADE,

    name TEXT NOT NULL,
    description TEXT,
    sort_order DOUBLE PRECISION NOT NULL DEFAULT 0,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CHECK ((issue_id IS NOT NULL) <> (project_id IS NOT NULL))
);

CREATE INDEX idx_library_section_issue ON library_section (issue_id) WHERE issue_id IS NOT NULL;
CREATE INDEX idx_library_section_project ON library_section (project_id) WHERE project_id IS NOT NULL;

-- Section membership: an attachment can appear in zero or more sections.
-- position within a section is explicit so the UI has a stable render
-- order even when curation is partial.
CREATE TABLE library_section_item (
    section_id UUID NOT NULL REFERENCES library_section(id) ON DELETE CASCADE,
    attachment_id UUID NOT NULL REFERENCES attachment(id) ON DELETE CASCADE,
    position DOUBLE PRECISION NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (section_id, attachment_id)
);

CREATE INDEX idx_library_section_item_order ON library_section_item (section_id, position);
