DROP INDEX IF EXISTS idx_document_curation_document;
ALTER TABLE document_curation DROP COLUMN IF EXISTS document_id;
DROP TABLE IF EXISTS document_comment;
DROP TABLE IF EXISTS document_version;
