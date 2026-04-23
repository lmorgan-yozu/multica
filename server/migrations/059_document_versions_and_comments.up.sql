-- Document versioning, per-document comments, and summarisation support.
-- Phase 1b of the document library feature. Additive to migration 058.

-- document_version links multiple attachments as revisions of the same
-- logical document. `document_id` is the durable identifier that survives
-- attachment replacement; comments and curation context travel with it
-- rather than dying when a new upload lands.
--
-- First upload: fresh document_id, version_number = 1.
-- Subsequent "replace" upload: same document_id, version_number increments.
-- An attachment belongs to exactly one document (UNIQUE on attachment_id).
CREATE TABLE document_version (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    document_id UUID NOT NULL,
    attachment_id UUID NOT NULL REFERENCES attachment(id) ON DELETE CASCADE,
    version_number INT NOT NULL,
    notes TEXT,
    author_type TEXT NOT NULL CHECK (author_type IN ('member', 'agent')),
    author_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (document_id, version_number),
    UNIQUE (attachment_id)
);

CREATE INDEX idx_document_version_document ON document_version (document_id, version_number DESC);
CREATE INDEX idx_document_version_workspace ON document_version (workspace_id);

-- document_comment: a thread scoped to a logical document, not an issue.
-- Comments persist across version bumps of the document they're attached to.
-- Shape follows the issue-comment pattern (parent_id for threading, author
-- attribution with type) so UI affordances carry over.
CREATE TABLE document_comment (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    document_id UUID NOT NULL,
    parent_id UUID REFERENCES document_comment(id) ON DELETE CASCADE,
    content TEXT NOT NULL,
    author_type TEXT NOT NULL CHECK (author_type IN ('member', 'agent')),
    author_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_document_comment_doc ON document_comment (document_id, created_at);
CREATE INDEX idx_document_comment_workspace ON document_comment (workspace_id);

-- Add document_id to curation so the library layer can resolve an attachment
-- back to its logical-document identity without a join through version history
-- on every query. Nullable by design — legacy curation rows written before
-- this migration get backfilled lazily when their attachment is next viewed.
ALTER TABLE document_curation
    ADD COLUMN IF NOT EXISTS document_id UUID;

CREATE INDEX IF NOT EXISTS idx_document_curation_document ON document_curation (document_id)
    WHERE document_id IS NOT NULL;

-- Backfill: give every attachment that already has a curation row a document
-- identity, with version 1. This is a one-shot seeded from current data.
-- Attachments without a curation row get their document identity lazily on
-- first view.
INSERT INTO document_version (workspace_id, document_id, attachment_id, version_number, author_type, author_id)
SELECT a.workspace_id, gen_random_uuid(), a.id, 1, a.uploader_type, a.uploader_id
FROM attachment a
LEFT JOIN document_version v ON v.attachment_id = a.id
WHERE v.id IS NULL;

UPDATE document_curation c
SET document_id = v.document_id
FROM document_version v
WHERE v.attachment_id = c.attachment_id
  AND c.document_id IS NULL;
