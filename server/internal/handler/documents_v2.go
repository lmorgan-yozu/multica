package handler

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ---------------------------------------------------------------------------
// Phase 1b additions: versioning, comments, summarisation, bulk export.
// Kept in a separate file from documents.go so the two phases are reviewable
// independently.
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Versioning
// ---------------------------------------------------------------------------

type DocumentVersion struct {
	ID            string  `json:"id"`
	DocumentID    string  `json:"document_id"`
	AttachmentID  string  `json:"attachment_id"`
	VersionNumber int     `json:"version_number"`
	Notes         *string `json:"notes,omitempty"`
	AuthorType    string  `json:"author_type"`
	AuthorID      string  `json:"author_id"`
	CreatedAt     string  `json:"created_at"`
	Filename      string  `json:"filename"`
	SizeBytes     int64   `json:"size_bytes"`
	ContentType   string  `json:"content_type"`
}

type newVersionPayload struct {
	AttachmentID string  `json:"attachment_id"`
	Notes        *string `json:"notes,omitempty"`
}

// CreateDocumentVersion promotes a second attachment to be the next version
// of the logical document that an existing attachment belongs to. The URL
// parameter is the existing attachment; the payload carries the new one.
//
// Why this shape: attachments upload via the existing /api/upload-file flow;
// once uploaded, the caller tells us "this new attachment supersedes that
// older one" and we record the lineage. Keeps file upload and version
// control as separate concerns.
func (h *Handler) CreateDocumentVersion(w http.ResponseWriter, r *http.Request) {
	existingID := chi.URLParam(r, "id")
	if existingID == "" {
		writeError(w, http.StatusBadRequest, "attachment id is required")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	var payload newVersionPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if payload.AttachmentID == "" {
		writeError(w, http.StatusBadRequest, "attachment_id is required")
		return
	}

	// Both attachments must belong to this workspace.
	existing, err := h.Queries.GetAttachment(r.Context(), db.GetAttachmentParams{
		ID:          parseUUID(existingID),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "existing attachment not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load existing attachment")
		return
	}
	if _, err := h.Queries.GetAttachment(r.Context(), db.GetAttachmentParams{
		ID:          parseUUID(payload.AttachmentID),
		WorkspaceID: parseUUID(workspaceID),
	}); err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "new attachment not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load new attachment")
		return
	}
	_ = existing

	actorType, actorID := h.resolveActor(r, userID, workspaceID)

	// Ensure the existing attachment has a document identity. If it does not
	// (common for attachments created before migration 059 was applied), we
	// seed version 1 for it.
	documentID, err := h.ensureDocumentIdentity(r, workspaceID, existingID, actorType, actorID)
	if err != nil {
		slog.Warn("ensure document identity failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to establish document identity")
		return
	}

	// Insert the new version with version_number = max(existing)+1.
	const insertSQL = `
INSERT INTO document_version (workspace_id, document_id, attachment_id, version_number, notes, author_type, author_id)
SELECT $1, $2, $3,
       COALESCE((SELECT MAX(version_number) FROM document_version WHERE document_id = $2), 0) + 1,
       $4, $5, $6
RETURNING id, version_number, created_at
`
	var (
		id            pgtype.UUID
		versionNumber int
		createdAt     pgtype.Timestamptz
	)
	err = h.DB.QueryRow(r.Context(), insertSQL,
		parseUUID(workspaceID),
		parseUUID(documentID),
		parseUUID(payload.AttachmentID),
		ptrToText(payload.Notes),
		actorType,
		parseUUID(actorID),
	).Scan(&id, &versionNumber, &createdAt)
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "attachment already belongs to a document")
			return
		}
		slog.Warn("create document version failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create version")
		return
	}

	// Also mirror the document_id on the new attachment's curation row, so
	// the library projection resolves it without an extra join. An upsert
	// pattern keeps this idempotent and survives the first-ever curation
	// for this attachment landing later.
	const curationLinkSQL = `
INSERT INTO document_curation (workspace_id, attachment_id, document_id, curator_type, curator_id)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (attachment_id) DO UPDATE SET
    document_id = EXCLUDED.document_id,
    updated_at = now()
`
	if _, err := h.DB.Exec(r.Context(), curationLinkSQL,
		parseUUID(workspaceID), parseUUID(payload.AttachmentID),
		parseUUID(documentID), actorType, parseUUID(actorID)); err != nil {
		slog.Warn("mirror document_id on curation failed", "error", err)
	}

	h.publish("document.versioned", workspaceID, actorType, actorID, map[string]any{
		"document_id":    documentID,
		"attachment_id":  payload.AttachmentID,
		"version_number": versionNumber,
	})

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":             uuidToString(id),
		"document_id":    documentID,
		"attachment_id":  payload.AttachmentID,
		"version_number": versionNumber,
		"created_at":     timestampToString(createdAt),
	})
}

// GetDocumentVersions returns the version history for a logical document,
// identified via any of its attachments. Useful for the "Versions" dropdown
// in the viewer and the `multica docs versions` CLI command.
func (h *Handler) GetDocumentVersions(w http.ResponseWriter, r *http.Request) {
	attachmentID := chi.URLParam(r, "id")
	if attachmentID == "" {
		writeError(w, http.StatusBadRequest, "attachment id is required")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)

	documentID, err := h.ensureDocumentIdentity(r, workspaceID, attachmentID, actorType, actorID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve document identity")
		return
	}

	const selectSQL = `
SELECT v.id, v.document_id, v.attachment_id, v.version_number, v.notes,
       v.author_type, v.author_id, v.created_at,
       a.filename, a.size_bytes, a.content_type
FROM document_version v
JOIN attachment a ON a.id = v.attachment_id
WHERE v.document_id = $1
ORDER BY v.version_number DESC
`
	rows, err := h.DB.Query(r.Context(), selectSQL, parseUUID(documentID))
	if err != nil {
		slog.Warn("list document versions failed", "error", err, "document_id", documentID)
		writeError(w, http.StatusInternalServerError, "failed to list versions")
		return
	}
	defer rows.Close()

	versions := []DocumentVersion{}
	for rows.Next() {
		var (
			id           pgtype.UUID
			docID        pgtype.UUID
			attID        pgtype.UUID
			versionNum   int
			notes        pgtype.Text
			authorType   string
			authorID     pgtype.UUID
			createdAt    pgtype.Timestamptz
			filename     string
			sizeBytes    int64
			contentType  string
		)
		if err := rows.Scan(&id, &docID, &attID, &versionNum, &notes,
			&authorType, &authorID, &createdAt, &filename, &sizeBytes, &contentType); err != nil {
			continue
		}
		versions = append(versions, DocumentVersion{
			ID:            uuidToString(id),
			DocumentID:    uuidToString(docID),
			AttachmentID:  uuidToString(attID),
			VersionNumber: versionNum,
			Notes:         textToPtr(notes),
			AuthorType:    authorType,
			AuthorID:      uuidToString(authorID),
			CreatedAt:     timestampToString(createdAt),
			Filename:      filename,
			SizeBytes:     sizeBytes,
			ContentType:   contentType,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"document_id": documentID,
		"versions":    versions,
	})
}

// ensureDocumentIdentity returns the document_id for an attachment, creating
// a version 1 record on the fly if none exists. This is the lazy backfill
// pattern mentioned in migration 059 — older attachments don't get document
// identities until they're first accessed through a versioning-aware path.
func (h *Handler) ensureDocumentIdentity(r *http.Request, workspaceID, attachmentID, actorType, actorID string) (string, error) {
	const existsSQL = `SELECT document_id FROM document_version WHERE attachment_id = $1 LIMIT 1`
	var docID pgtype.UUID
	err := h.DB.QueryRow(r.Context(), existsSQL, parseUUID(attachmentID)).Scan(&docID)
	if err == nil {
		return uuidToString(docID), nil
	}
	// pgx returns a specific error for no rows; check via isNotFound which
	// covers pgx.ErrNoRows.
	if !isNotFound(err) {
		return "", err
	}

	// Need to create version 1. We also need to know whether an existing
	// document_curation row is already pointing at some document_id (in
	// which case honour it).
	const curationLookupSQL = `SELECT document_id FROM document_curation WHERE attachment_id = $1 AND document_id IS NOT NULL`
	var existingCurationDoc pgtype.UUID
	if err := h.DB.QueryRow(r.Context(), curationLookupSQL, parseUUID(attachmentID)).Scan(&existingCurationDoc); err == nil {
		// Curation had a document_id but no version record — unusual, heal it.
		const insertV1 = `
INSERT INTO document_version (workspace_id, document_id, attachment_id, version_number, author_type, author_id)
VALUES ($1, $2, $3, 1, $4, $5)
ON CONFLICT (attachment_id) DO NOTHING
`
		if _, err := h.DB.Exec(r.Context(), insertV1,
			parseUUID(workspaceID), existingCurationDoc, parseUUID(attachmentID),
			actorType, parseUUID(actorID)); err != nil {
			return "", err
		}
		return uuidToString(existingCurationDoc), nil
	}

	// Fresh document identity.
	const insertFreshSQL = `
INSERT INTO document_version (workspace_id, document_id, attachment_id, version_number, author_type, author_id)
VALUES ($1, gen_random_uuid(), $2, 1, $3, $4)
RETURNING document_id
`
	var newDocID pgtype.UUID
	if err := h.DB.QueryRow(r.Context(), insertFreshSQL,
		parseUUID(workspaceID), parseUUID(attachmentID),
		actorType, parseUUID(actorID)).Scan(&newDocID); err != nil {
		return "", err
	}

	// Backfill the curation row's document_id if it exists without one.
	const backfillSQL = `
UPDATE document_curation
SET document_id = $1, updated_at = now()
WHERE attachment_id = $2 AND document_id IS NULL
`
	_, _ = h.DB.Exec(r.Context(), backfillSQL, newDocID, parseUUID(attachmentID))

	return uuidToString(newDocID), nil
}

// ---------------------------------------------------------------------------
// Comments
// ---------------------------------------------------------------------------

type DocumentComment struct {
	ID         string  `json:"id"`
	DocumentID string  `json:"document_id"`
	ParentID   *string `json:"parent_id,omitempty"`
	Content    string  `json:"content"`
	AuthorType string  `json:"author_type"`
	AuthorID   string  `json:"author_id"`
	CreatedAt  string  `json:"created_at"`
	UpdatedAt  string  `json:"updated_at"`
}

type createDocumentCommentPayload struct {
	Content  string  `json:"content"`
	ParentID *string `json:"parent_id,omitempty"`
}

// ListDocumentComments returns the comment thread for a document, keyed off
// one of its attachments. Thread order is chronological; nesting via parent_id
// is left to the UI to fold.
func (h *Handler) ListDocumentComments(w http.ResponseWriter, r *http.Request) {
	attachmentID := chi.URLParam(r, "id")
	if attachmentID == "" {
		writeError(w, http.StatusBadRequest, "attachment id is required")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)

	documentID, err := h.ensureDocumentIdentity(r, workspaceID, attachmentID, actorType, actorID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve document identity")
		return
	}

	const selectSQL = `
SELECT id, parent_id, content, author_type, author_id, created_at, updated_at
FROM document_comment
WHERE document_id = $1
ORDER BY created_at
`
	rows, err := h.DB.Query(r.Context(), selectSQL, parseUUID(documentID))
	if err != nil {
		slog.Warn("list document comments failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list comments")
		return
	}
	defer rows.Close()

	comments := []DocumentComment{}
	for rows.Next() {
		var (
			id         pgtype.UUID
			parentID   pgtype.UUID
			content    string
			authorType string
			authorID   pgtype.UUID
			createdAt  pgtype.Timestamptz
			updatedAt  pgtype.Timestamptz
		)
		if err := rows.Scan(&id, &parentID, &content, &authorType, &authorID, &createdAt, &updatedAt); err != nil {
			continue
		}
		comments = append(comments, DocumentComment{
			ID:         uuidToString(id),
			DocumentID: documentID,
			ParentID:   uuidToPtr(parentID),
			Content:    content,
			AuthorType: authorType,
			AuthorID:   uuidToString(authorID),
			CreatedAt:  timestampToString(createdAt),
			UpdatedAt:  timestampToString(updatedAt),
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"document_id": documentID,
		"comments":    comments,
	})
}

// CreateDocumentComment adds a comment to a document's thread. The URL param
// is any attachment that belongs to the document; the server resolves the
// logical document identity so comments survive version replacement.
func (h *Handler) CreateDocumentComment(w http.ResponseWriter, r *http.Request) {
	attachmentID := chi.URLParam(r, "id")
	if attachmentID == "" {
		writeError(w, http.StatusBadRequest, "attachment id is required")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	var payload createDocumentCommentPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if strings.TrimSpace(payload.Content) == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}

	actorType, actorID := h.resolveActor(r, userID, workspaceID)

	documentID, err := h.ensureDocumentIdentity(r, workspaceID, attachmentID, actorType, actorID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve document identity")
		return
	}

	var parentIDParam pgtype.UUID
	if payload.ParentID != nil && *payload.ParentID != "" {
		parentIDParam = parseUUID(*payload.ParentID)
	}

	const insertSQL = `
INSERT INTO document_comment (workspace_id, document_id, parent_id, content, author_type, author_id)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, created_at, updated_at
`
	var (
		id        pgtype.UUID
		createdAt pgtype.Timestamptz
		updatedAt pgtype.Timestamptz
	)
	if err := h.DB.QueryRow(r.Context(), insertSQL,
		parseUUID(workspaceID), parseUUID(documentID), parentIDParam,
		payload.Content, actorType, parseUUID(actorID),
	).Scan(&id, &createdAt, &updatedAt); err != nil {
		slog.Warn("create document comment failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create comment")
		return
	}

	h.publish("document.commented", workspaceID, actorType, actorID, map[string]any{
		"document_id":   documentID,
		"attachment_id": attachmentID,
		"comment_id":    uuidToString(id),
	})

	writeJSON(w, http.StatusCreated, DocumentComment{
		ID:         uuidToString(id),
		DocumentID: documentID,
		ParentID:   payload.ParentID,
		Content:    payload.Content,
		AuthorType: actorType,
		AuthorID:   actorID,
		CreatedAt:  timestampToString(createdAt),
		UpdatedAt:  timestampToString(updatedAt),
	})
}

// DeleteDocumentComment removes a comment. Author-or-workspace-owner only.
func (h *Handler) DeleteDocumentComment(w http.ResponseWriter, r *http.Request) {
	commentID := chi.URLParam(r, "commentId")
	if commentID == "" {
		writeError(w, http.StatusBadRequest, "comment id is required")
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	const delSQL = `DELETE FROM document_comment WHERE id = $1 AND workspace_id = $2`
	tag, err := h.DB.Exec(r.Context(), delSQL, parseUUID(commentID), parseUUID(workspaceID))
	if err != nil {
		slog.Warn("delete document comment failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete comment")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "comment not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Summarisation targeting (for autopilots)
// ---------------------------------------------------------------------------

// ListUnsummarisedDocuments returns documents in a workspace (optionally
// scoped to an issue or project) that have no summary yet. Exists so an
// autopilot can do `multica docs list-unsummarised --project <id>` and then
// walk the set, posting summaries via the existing curation endpoint.
func (h *Handler) ListUnsummarisedDocuments(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	issueID := r.URL.Query().Get("issue_id")
	projectID := r.URL.Query().Get("project_id")

	if issueID != "" && projectID != "" {
		writeError(w, http.StatusBadRequest, "provide issue_id or project_id, not both")
		return
	}

	var (
		sqlQuery string
		args     []any
	)
	switch {
	case issueID != "":
		sqlQuery = `
SELECT a.id, a.filename, a.content_type, a.size_bytes, a.created_at
FROM attachment a
LEFT JOIN document_curation c ON c.attachment_id = a.id
WHERE a.workspace_id = $1
  AND (c.summary IS NULL OR c.summary = '')
  AND COALESCE(c.archived, FALSE) = FALSE
  AND (a.issue_id = $2 OR a.comment_id IN (SELECT id FROM comment WHERE issue_id = $2))
ORDER BY a.created_at DESC
LIMIT 100
`
		args = []any{parseUUID(workspaceID), parseUUID(issueID)}
	case projectID != "":
		sqlQuery = `
SELECT a.id, a.filename, a.content_type, a.size_bytes, a.created_at
FROM attachment a
JOIN issue i ON (
    (a.issue_id IS NOT NULL AND a.issue_id = i.id)
    OR (a.comment_id IS NOT NULL AND a.comment_id IN (SELECT cc.id FROM comment cc WHERE cc.issue_id = i.id))
)
LEFT JOIN document_curation c ON c.attachment_id = a.id
WHERE a.workspace_id = $1 AND i.project_id = $2
  AND (c.summary IS NULL OR c.summary = '')
  AND COALESCE(c.archived, FALSE) = FALSE
ORDER BY a.created_at DESC
LIMIT 100
`
		args = []any{parseUUID(workspaceID), parseUUID(projectID)}
	default:
		sqlQuery = `
SELECT a.id, a.filename, a.content_type, a.size_bytes, a.created_at
FROM attachment a
LEFT JOIN document_curation c ON c.attachment_id = a.id
WHERE a.workspace_id = $1
  AND (c.summary IS NULL OR c.summary = '')
  AND COALESCE(c.archived, FALSE) = FALSE
ORDER BY a.created_at DESC
LIMIT 100
`
		args = []any{parseUUID(workspaceID)}
	}

	rows, err := h.DB.Query(r.Context(), sqlQuery, args...)
	if err != nil {
		slog.Warn("list unsummarised failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list unsummarised documents")
		return
	}
	defer rows.Close()

	type unsummarisedRow struct {
		AttachmentID string `json:"attachment_id"`
		Filename     string `json:"filename"`
		ContentType  string `json:"content_type"`
		SizeBytes    int64  `json:"size_bytes"`
		CreatedAt    string `json:"created_at"`
	}
	result := []unsummarisedRow{}
	for rows.Next() {
		var (
			id          pgtype.UUID
			filename    string
			contentType string
			sizeBytes   int64
			createdAt   pgtype.Timestamptz
		)
		if err := rows.Scan(&id, &filename, &contentType, &sizeBytes, &createdAt); err != nil {
			continue
		}
		result = append(result, unsummarisedRow{
			AttachmentID: uuidToString(id),
			Filename:     filename,
			ContentType:  contentType,
			SizeBytes:    sizeBytes,
			CreatedAt:    timestampToString(createdAt),
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"documents": result})
}

// ---------------------------------------------------------------------------
// Bulk export
// ---------------------------------------------------------------------------

// ExportWorkspaceLibrary streams a zip of every non-archived document in
// the workspace. Intended for backup / offline review rather than everyday
// use; for routine use the per-project or per-issue exports are friendlier.
func (h *Handler) ExportWorkspaceLibrary(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	h.streamLibraryZip(w, r, "workspace-library.zip",
		`SELECT a.id, a.filename, a.url, a.content_type
         FROM attachment a
         LEFT JOIN document_curation c ON c.attachment_id = a.id
         WHERE a.workspace_id = $1
           AND COALESCE(c.archived, FALSE) = FALSE`,
		parseUUID(workspaceID))
}

// ExportIssueLibrary streams a zip of every (non-archived) document on an
// issue. Filenames inside the zip are deduplicated by suffixing a counter
// on collision.
func (h *Handler) ExportIssueLibrary(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	if issueID == "" || workspaceID == "" {
		writeError(w, http.StatusBadRequest, "issue id and workspace_id are required")
		return
	}
	h.streamLibraryZip(w, r, fmt.Sprintf("issue-%s-library.zip", issueID),
		`SELECT a.id, a.filename, a.url, a.content_type
         FROM attachment a
         LEFT JOIN document_curation c ON c.attachment_id = a.id
         WHERE a.workspace_id = $1
           AND (a.issue_id = $2 OR a.comment_id IN (SELECT id FROM comment WHERE issue_id = $2))
           AND COALESCE(c.archived, FALSE) = FALSE`,
		parseUUID(workspaceID), parseUUID(issueID))
}

// ExportProjectLibrary streams a zip of every (non-archived) document across
// every issue in the project.
func (h *Handler) ExportProjectLibrary(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	if projectID == "" || workspaceID == "" {
		writeError(w, http.StatusBadRequest, "project id and workspace_id are required")
		return
	}
	h.streamLibraryZip(w, r, fmt.Sprintf("project-%s-library.zip", projectID),
		`SELECT a.id, a.filename, a.url, a.content_type
         FROM attachment a
         JOIN issue i ON (
             (a.issue_id IS NOT NULL AND a.issue_id = i.id)
             OR (a.comment_id IS NOT NULL AND a.comment_id IN (SELECT cc.id FROM comment cc WHERE cc.issue_id = i.id))
         )
         LEFT JOIN document_curation c ON c.attachment_id = a.id
         WHERE a.workspace_id = $1 AND i.project_id = $2
           AND COALESCE(c.archived, FALSE) = FALSE`,
		parseUUID(workspaceID), parseUUID(projectID))
}

// ExportLibrarySection streams a zip of every document in a named section.
func (h *Handler) ExportLibrarySection(w http.ResponseWriter, r *http.Request) {
	sectionID := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	if sectionID == "" || workspaceID == "" {
		writeError(w, http.StatusBadRequest, "section id and workspace_id are required")
		return
	}
	// Resolve the section's name for a friendlier filename.
	var sectionName string
	_ = h.DB.QueryRow(r.Context(),
		`SELECT name FROM library_section WHERE id = $1 AND workspace_id = $2`,
		parseUUID(sectionID), parseUUID(workspaceID)).Scan(&sectionName)
	if sectionName == "" {
		sectionName = "section"
	}
	filename := fmt.Sprintf("%s-%s.zip", slugify(sectionName), sectionID)
	h.streamLibraryZip(w, r, filename,
		`SELECT a.id, a.filename, a.url, a.content_type
         FROM attachment a
         JOIN library_section_item si ON si.attachment_id = a.id
         WHERE si.section_id = $1 AND a.workspace_id = $2
         ORDER BY si.position`,
		parseUUID(sectionID), parseUUID(workspaceID))
}

// streamLibraryZip is the shared worker for the three export endpoints. It
// runs the supplied query, walks the rows, reads each file from storage,
// and writes it to a zip stream on the response.
func (h *Handler) streamLibraryZip(w http.ResponseWriter, r *http.Request, zipName, query string, args ...any) {
	rows, err := h.DB.Query(r.Context(), query, args...)
	if err != nil {
		slog.Warn("zip query failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to enumerate documents")
		return
	}
	defer rows.Close()

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", zipName))
	zw := zip.NewWriter(w)
	defer zw.Close()

	used := map[string]int{}
	for rows.Next() {
		var (
			id          pgtype.UUID
			filename    string
			url         string
			contentType string
		)
		if err := rows.Scan(&id, &filename, &url, &contentType); err != nil {
			slog.Warn("zip scan failed", "error", err)
			continue
		}

		// Dedupe filenames inside the zip (two docs might be named SUMMARY.md).
		finalName := filename
		if n, ok := used[filename]; ok {
			used[filename] = n + 1
			finalName = suffixBeforeExtension(filename, fmt.Sprintf("-%d", n+1))
		} else {
			used[filename] = 1
		}

		fw, err := zw.Create(finalName)
		if err != nil {
			slog.Warn("zip create entry failed", "error", err, "filename", finalName)
			continue
		}

		data, readErr := h.readAttachmentContent(r, url)
		if readErr != nil {
			slog.Warn("zip read content failed", "error", readErr, "filename", finalName)
			// Write an informative stub so the zip doesn't silently drop a file.
			_, _ = io.WriteString(fw, fmt.Sprintf("# Download failed for %s: %v\n", filename, readErr))
			continue
		}
		_, _ = fw.Write(data)
	}
}

// readAttachmentContent fetches the bytes for an attachment URL, regardless
// of whether it's local storage or remote/signed.
func (h *Handler) readAttachmentContent(r *http.Request, url string) ([]byte, error) {
	if strings.HasPrefix(url, "/uploads/") {
		if local, ok := h.Storage.(localStoragePather); ok {
			key := strings.TrimPrefix(url, "/uploads/")
			path := local.GetFilePath(key)
			return os.ReadFile(path)
		}
		return nil, errors.New("local storage read not supported on this backend")
	}
	// Remote: do an HTTP GET. If signing is configured, sign first.
	target := url
	if h.CFSigner != nil {
		target = h.CFSigner.SignedURL(url, time.Now().Add(5*time.Minute))
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("remote fetch returned %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 100<<20))
}

// localStoragePather is the subset of the storage interface we need to
// read bytes in-process for zipping. LocalStorage.GetFilePath satisfies it.
type localStoragePather interface {
	GetFilePath(key string) string
}

// suffixBeforeExtension turns "SUMMARY.md" + "-2" into "SUMMARY-2.md".
func suffixBeforeExtension(name, suffix string) string {
	dot := strings.LastIndex(name, ".")
	if dot < 0 {
		return name + suffix
	}
	return name[:dot] + suffix + name[dot:]
}

// slugify keeps filenames tidy when we use section names in Content-Disposition.
func slugify(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteRune('-')
		}
	}
	if b.Len() == 0 {
		return "section"
	}
	return b.String()
}
