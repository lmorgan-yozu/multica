package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ---------------------------------------------------------------------------
// Response types
// ---------------------------------------------------------------------------

// Document is the library-layer projection of an attachment. It contains the
// attachment record joined with its curation metadata (category, summary,
// tags, order, pinned/archived) so the UI can render a ready-to-browse doc
// without a second round-trip.
type Document struct {
	AttachmentID string    `json:"attachment_id"`
	IssueID      *string   `json:"issue_id"`
	CommentID    *string   `json:"comment_id"`
	Filename     string    `json:"filename"`
	ContentType  string    `json:"content_type"`
	SizeBytes    int64     `json:"size_bytes"`
	DownloadURL  string    `json:"download_url"`
	InlineURL    string    `json:"inline_url"`
	CreatedAt    string    `json:"created_at"`
	UploaderType string    `json:"uploader_type"`
	UploaderID   string    `json:"uploader_id"`

	// Curation fields. Zero values when no curation record exists yet.
	Title     *string   `json:"title,omitempty"`
	Summary   *string   `json:"summary,omitempty"`
	Category  *string   `json:"category,omitempty"`
	Tags      []string  `json:"tags"`
	SortOrder float64   `json:"sort_order"`
	Pinned    bool      `json:"pinned"`
	Archived  bool      `json:"archived"`

	CuratorType *string `json:"curator_type,omitempty"`
	CuratorID   *string `json:"curator_id,omitempty"`
	CuratedAt   *string `json:"curated_at,omitempty"`

	// Version identity (populated when the attachment belongs to a
	// document_version record; null for legacy attachments until they're
	// first accessed through a versioning-aware path).
	DocumentID    *string `json:"document_id,omitempty"`
	VersionNumber *int    `json:"version_number,omitempty"`
}

type LibrarySection struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description *string  `json:"description,omitempty"`
	SortOrder   float64  `json:"sort_order"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
	DocumentIDs []string `json:"document_ids"`
}

type IssueLibraryResponse struct {
	IssueID   string           `json:"issue_id"`
	Documents []Document       `json:"documents"`
	Sections  []LibrarySection `json:"sections"`
}

type ProjectLibraryDocument struct {
	Document
	SourceIssueID         string  `json:"source_issue_id"`
	SourceIssueIdentifier string  `json:"source_issue_identifier"`
	SourceIssueTitle      string  `json:"source_issue_title"`
}

type ProjectLibraryResponse struct {
	ProjectID string                   `json:"project_id"`
	Documents []ProjectLibraryDocument `json:"documents"`
	Sections  []LibrarySection         `json:"sections"`
}

// ---------------------------------------------------------------------------
// SQL scanning helpers
// ---------------------------------------------------------------------------

// documentRow bridges the joined query into the Document response. It
// uses pgtype scanners so nullable curation columns round-trip cleanly.
type documentRow struct {
	AttachmentID pgtype.UUID
	IssueID      pgtype.UUID
	CommentID    pgtype.UUID
	Filename     string
	ContentType  string
	SizeBytes    int64
	URL          string
	CreatedAt    pgtype.Timestamptz
	UploaderType string
	UploaderID   pgtype.UUID

	Title       pgtype.Text
	Summary     pgtype.Text
	Category    pgtype.Text
	Tags        []string
	SortOrder   pgtype.Float8
	Pinned      pgtype.Bool
	Archived    pgtype.Bool
	CuratorType pgtype.Text
	CuratorID   pgtype.UUID
	CuratedAt   pgtype.Timestamptz

	DocumentID    pgtype.UUID
	VersionNumber pgtype.Int4
}

func (h *Handler) toDocument(row documentRow, cfSignerAvailable bool) Document {
	url := row.URL
	downloadURL := url
	if cfSignerAvailable && h.CFSigner != nil {
		downloadURL = h.CFSigner.SignedURL(url, time.Now().Add(30*time.Minute))
	}

	issueID := uuidToPtr(row.IssueID)
	commentID := uuidToPtr(row.CommentID)

	var tags []string
	if row.Tags != nil {
		tags = row.Tags
	} else {
		tags = []string{}
	}

	doc := Document{
		AttachmentID: uuidToString(row.AttachmentID),
		IssueID:      issueID,
		CommentID:    commentID,
		Filename:     row.Filename,
		ContentType:  row.ContentType,
		SizeBytes:    row.SizeBytes,
		DownloadURL:  downloadURL,
		InlineURL:    "/api/documents/" + uuidToString(row.AttachmentID) + "/content",
		CreatedAt:    timestampToString(row.CreatedAt),
		UploaderType: row.UploaderType,
		UploaderID:   uuidToString(row.UploaderID),
		Tags:         tags,
	}

	// Curation fields are nullable. Only populate when a curation row exists.
	if row.Title.Valid {
		t := row.Title.String
		doc.Title = &t
	}
	if row.Summary.Valid {
		s := row.Summary.String
		doc.Summary = &s
	}
	if row.Category.Valid {
		c := row.Category.String
		doc.Category = &c
	}
	if row.SortOrder.Valid {
		doc.SortOrder = row.SortOrder.Float64
	}
	if row.Pinned.Valid {
		doc.Pinned = row.Pinned.Bool
	}
	if row.Archived.Valid {
		doc.Archived = row.Archived.Bool
	}
	if row.CuratorType.Valid {
		ct := row.CuratorType.String
		doc.CuratorType = &ct
	}
	if row.CuratorID.Valid {
		ci := uuidToString(row.CuratorID)
		doc.CuratorID = &ci
	}
	if row.CuratedAt.Valid {
		ts := timestampToString(row.CuratedAt)
		doc.CuratedAt = &ts
	}
	if row.DocumentID.Valid {
		did := uuidToString(row.DocumentID)
		doc.DocumentID = &did
	}
	if row.VersionNumber.Valid {
		v := int(row.VersionNumber.Int32)
		doc.VersionNumber = &v
	}

	return doc
}

// ---------------------------------------------------------------------------
// SQL
// ---------------------------------------------------------------------------

const selectDocumentsForIssue = `
SELECT
    a.id, a.issue_id, a.comment_id, a.filename, a.content_type,
    a.size_bytes, a.url, a.created_at, a.uploader_type, a.uploader_id,
    c.title, c.summary, c.category, c.tags,
    c.sort_order, c.pinned, c.archived,
    c.curator_type, c.curator_id, c.updated_at,
    v.document_id, v.version_number
FROM attachment a
LEFT JOIN document_curation c ON c.attachment_id = a.id
LEFT JOIN document_version v ON v.attachment_id = a.id
WHERE a.workspace_id = $1
  AND (a.issue_id = $2 OR a.comment_id IN (
        SELECT id FROM comment WHERE issue_id = $2
  ))
  AND COALESCE(c.archived, FALSE) = FALSE
ORDER BY COALESCE(c.pinned, FALSE) DESC, COALESCE(c.sort_order, 0), a.created_at
`

// selectDocumentsForProject fans out to every issue in the project and
// attaches the source-issue summary to each document for the library UI.
// The issue identifier is computed from workspace.issue_prefix + issue.number,
// matching what issue.go does in response serialisation.
const selectDocumentsForProject = `
SELECT
    a.id, a.issue_id, a.comment_id, a.filename, a.content_type,
    a.size_bytes, a.url, a.created_at, a.uploader_type, a.uploader_id,
    c.title, c.summary, c.category, c.tags,
    c.sort_order, c.pinned, c.archived,
    c.curator_type, c.curator_id, c.updated_at,
    v.document_id, v.version_number,
    i.id AS source_issue_id,
    w.issue_prefix || '-' || i.number AS source_issue_identifier,
    i.title AS source_issue_title
FROM attachment a
JOIN issue i ON (
    (a.issue_id IS NOT NULL AND a.issue_id = i.id)
    OR (a.comment_id IS NOT NULL AND a.comment_id IN (
         SELECT cc.id FROM comment cc WHERE cc.issue_id = i.id
    ))
)
JOIN workspace w ON w.id = a.workspace_id
LEFT JOIN document_curation c ON c.attachment_id = a.id
LEFT JOIN document_version v ON v.attachment_id = a.id
WHERE a.workspace_id = $1 AND i.project_id = $2
  AND COALESCE(c.archived, FALSE) = FALSE
ORDER BY COALESCE(c.pinned, FALSE) DESC, COALESCE(c.sort_order, 0), a.created_at
`

// selectDocumentsForWorkspace aggregates every non-archived attachment
// across every issue (and every comment) in the workspace. Used by the
// workspace-scope library view in the sidebar.
// identifier is computed from workspace.issue_prefix + issue.number.
const selectDocumentsForWorkspace = `
SELECT
    a.id, a.issue_id, a.comment_id, a.filename, a.content_type,
    a.size_bytes, a.url, a.created_at, a.uploader_type, a.uploader_id,
    c.title, c.summary, c.category, c.tags,
    c.sort_order, c.pinned, c.archived,
    c.curator_type, c.curator_id, c.updated_at,
    v.document_id, v.version_number,
    COALESCE(i.id, ci.id) AS source_issue_id,
    COALESCE(w.issue_prefix || '-' || i.number::text, w.issue_prefix || '-' || ci.number::text, '') AS source_issue_identifier,
    COALESCE(i.title, ci.title, '') AS source_issue_title
FROM attachment a
JOIN workspace w ON w.id = a.workspace_id
LEFT JOIN issue i ON a.issue_id IS NOT NULL AND a.issue_id = i.id
LEFT JOIN comment cm ON a.comment_id IS NOT NULL AND a.comment_id = cm.id
LEFT JOIN issue ci ON cm.issue_id = ci.id
LEFT JOIN document_curation c ON c.attachment_id = a.id
LEFT JOIN document_version v ON v.attachment_id = a.id
WHERE a.workspace_id = $1
  AND COALESCE(c.archived, FALSE) = FALSE
ORDER BY COALESCE(c.pinned, FALSE) DESC, COALESCE(c.sort_order, 0), a.created_at DESC
`

const selectSectionsForIssue = `
SELECT s.id, s.name, s.description, s.sort_order, s.created_at, s.updated_at,
       COALESCE(array_agg(si.attachment_id ORDER BY si.position)
                FILTER (WHERE si.attachment_id IS NOT NULL), '{}'::uuid[]) AS items
FROM library_section s
LEFT JOIN library_section_item si ON si.section_id = s.id
WHERE s.issue_id = $1
GROUP BY s.id
ORDER BY s.sort_order, s.created_at
`

const selectSectionsForProject = `
SELECT s.id, s.name, s.description, s.sort_order, s.created_at, s.updated_at,
       COALESCE(array_agg(si.attachment_id ORDER BY si.position)
                FILTER (WHERE si.attachment_id IS NOT NULL), '{}'::uuid[]) AS items
FROM library_section s
LEFT JOIN library_section_item si ON si.section_id = s.id
WHERE s.project_id = $1
GROUP BY s.id
ORDER BY s.sort_order, s.created_at
`

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// GetIssueLibrary returns every document attached to the issue (directly on
// the issue or on any of its comments), merged with curation metadata, plus
// the section tree for the left-hand library navigation.
func (h *Handler) GetIssueLibrary(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	if issueID == "" {
		writeError(w, http.StatusBadRequest, "issue id is required")
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	docs, err := h.queryDocuments(r, selectDocumentsForIssue, parseUUID(workspaceID), parseUUID(issueID))
	if err != nil {
		slog.Warn("get issue library failed", "error", err, "issue_id", issueID)
		writeError(w, http.StatusInternalServerError, "failed to list documents")
		return
	}

	sections, err := h.querySections(r, selectSectionsForIssue, parseUUID(issueID))
	if err != nil {
		slog.Warn("get issue sections failed", "error", err, "issue_id", issueID)
		writeError(w, http.StatusInternalServerError, "failed to list sections")
		return
	}

	writeJSON(w, http.StatusOK, IssueLibraryResponse{
		IssueID:   issueID,
		Documents: docs,
		Sections:  sections,
	})
}

// GetWorkspaceLibrary returns every non-archived document across the whole
// workspace, with source-issue metadata attached. Powers the sidebar-level
// "Library" link that lets the team browse everything produced.
//
// Response shape matches ProjectLibrary so the UI can reuse the same shell
// and source-issue meta formatter without branching on scope.
func (h *Handler) GetWorkspaceLibrary(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	rows, err := h.DB.Query(r.Context(), selectDocumentsForWorkspace, parseUUID(workspaceID))
	if err != nil {
		slog.Warn("get workspace library failed", "error", err, "workspace_id", workspaceID)
		writeError(w, http.StatusInternalServerError, "failed to list documents")
		return
	}
	defer rows.Close()

	docs := []ProjectLibraryDocument{}
	for rows.Next() {
		var row documentRow
		var srcIssueID pgtype.UUID
		var srcIdentifier, srcTitle string
		if err := rows.Scan(
			&row.AttachmentID, &row.IssueID, &row.CommentID, &row.Filename,
			&row.ContentType, &row.SizeBytes, &row.URL, &row.CreatedAt,
			&row.UploaderType, &row.UploaderID,
			&row.Title, &row.Summary, &row.Category, &row.Tags,
			&row.SortOrder, &row.Pinned, &row.Archived,
			&row.CuratorType, &row.CuratorID, &row.CuratedAt,
			&row.DocumentID, &row.VersionNumber,
			&srcIssueID, &srcIdentifier, &srcTitle,
		); err != nil {
			slog.Warn("scan workspace library row failed", "error", err)
			continue
		}
		doc := ProjectLibraryDocument{
			Document: h.toDocument(row, true),
		}
		if srcIssueID.Valid {
			doc.SourceIssueID = uuidToString(srcIssueID)
			doc.SourceIssueIdentifier = srcIdentifier
			doc.SourceIssueTitle = srcTitle
		}
		docs = append(docs, doc)
	}

	// Workspace-scope library has no explicit sections (those are per
	// issue or per project). Return an empty array so the UI renders
	// the category grouping uniformly.
	writeJSON(w, http.StatusOK, ProjectLibraryResponse{
		ProjectID: "",
		Documents: docs,
		Sections:  []LibrarySection{},
	})
}

// GetProjectLibrary returns the project-wide curated library: every document
// attached to any issue in the project, plus section-level organisation.
// This is the view that turns a 40-issue project into a browsable book.
func (h *Handler) GetProjectLibrary(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	if projectID == "" {
		writeError(w, http.StatusBadRequest, "project id is required")
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	rows, err := h.DB.Query(r.Context(), selectDocumentsForProject, parseUUID(workspaceID), parseUUID(projectID))
	if err != nil {
		slog.Warn("get project library failed", "error", err, "project_id", projectID)
		writeError(w, http.StatusInternalServerError, "failed to list documents")
		return
	}
	defer rows.Close()

	var docs []ProjectLibraryDocument
	for rows.Next() {
		var row documentRow
		var srcIssueID pgtype.UUID
		var srcIdentifier, srcTitle string
		if err := rows.Scan(
			&row.AttachmentID, &row.IssueID, &row.CommentID, &row.Filename,
			&row.ContentType, &row.SizeBytes, &row.URL, &row.CreatedAt,
			&row.UploaderType, &row.UploaderID,
			&row.Title, &row.Summary, &row.Category, &row.Tags,
			&row.SortOrder, &row.Pinned, &row.Archived,
			&row.CuratorType, &row.CuratorID, &row.CuratedAt,
			&row.DocumentID, &row.VersionNumber,
			&srcIssueID, &srcIdentifier, &srcTitle,
		); err != nil {
			slog.Warn("scan project library row failed", "error", err)
			continue
		}
		docs = append(docs, ProjectLibraryDocument{
			Document:              h.toDocument(row, true),
			SourceIssueID:         uuidToString(srcIssueID),
			SourceIssueIdentifier: srcIdentifier,
			SourceIssueTitle:      srcTitle,
		})
	}
	if docs == nil {
		docs = []ProjectLibraryDocument{}
	}

	sections, err := h.querySections(r, selectSectionsForProject, parseUUID(projectID))
	if err != nil {
		slog.Warn("get project sections failed", "error", err, "project_id", projectID)
		writeError(w, http.StatusInternalServerError, "failed to list sections")
		return
	}

	writeJSON(w, http.StatusOK, ProjectLibraryResponse{
		ProjectID: projectID,
		Documents: docs,
		Sections:  sections,
	})
}

// GetDocumentContent proxies the attachment bytes through the authenticated
// API so the web UI can render markdown/text/PDF/image attachments inline
// without having to deal with relative upload paths or CORS on local
// storage deployments.
func (h *Handler) GetDocumentContent(w http.ResponseWriter, r *http.Request) {
	attachmentID := chi.URLParam(r, "id")
	if attachmentID == "" {
		writeError(w, http.StatusBadRequest, "attachment id is required")
		return
	}

	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	att, err := h.Queries.GetAttachment(r.Context(), db.GetAttachmentParams{
		ID:          parseUUID(attachmentID),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "attachment not found")
			return
		}
		slog.Warn("get attachment for content failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to fetch attachment")
		return
	}

	if local, ok := h.Storage.(localServeFiler); ok {
		// Local storage: strip the /uploads/ prefix and serve via the existing
		// ServeFile helper on the storage layer.
		path := strings.TrimPrefix(att.Url, "/uploads/")
		local.ServeFile(w, r, path)
		return
	}

	// Remote/signed storage: redirect to a signed URL good for a short window.
	target := att.Url
	if h.CFSigner != nil {
		target = h.CFSigner.SignedURL(att.Url, time.Now().Add(15*time.Minute))
	}
	http.Redirect(w, r, target, http.StatusTemporaryRedirect)
}

// localServeFiler is the subset of the Storage interface we use to stream
// local-disk attachments. Declaring it locally avoids adding dependencies
// to the handler package; the concrete LocalStorage type satisfies it.
type localServeFiler interface {
	ServeFile(w http.ResponseWriter, r *http.Request, filename string)
}

// ---------------------------------------------------------------------------
// Curation (agent-callable)
// ---------------------------------------------------------------------------

type curationPayload struct {
	Title     *string   `json:"title,omitempty"`
	Summary   *string   `json:"summary,omitempty"`
	Category  *string   `json:"category,omitempty"`
	Tags      *[]string `json:"tags,omitempty"`
	SortOrder *float64  `json:"sort_order,omitempty"`
	Pinned    *bool     `json:"pinned,omitempty"`
	Archived  *bool     `json:"archived,omitempty"`
}

// UpsertDocumentCuration sets or updates the curation metadata for an
// attachment. Callable by humans (members) and by agents; curator_type and
// curator_id are always recorded for attribution.
func (h *Handler) UpsertDocumentCuration(w http.ResponseWriter, r *http.Request) {
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

	_, err := h.Queries.GetAttachment(r.Context(), db.GetAttachmentParams{
		ID:          parseUUID(attachmentID),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "attachment not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load attachment")
		return
	}

	var payload curationPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	actorType, actorID := h.resolveActor(r, userID, workspaceID)

	// Upsert. We want partial updates: any non-nil field in the payload
	// wins; nil keeps the existing value. COALESCE lets us express that in
	// a single statement for both INSERT and UPDATE paths.
	const upsertSQL = `
INSERT INTO document_curation (
    workspace_id, attachment_id, title, summary, category, tags,
    sort_order, pinned, archived, curator_type, curator_id, updated_at
) VALUES (
    $1, $2, $3, $4, $5, COALESCE($6, '{}'::text[]),
    COALESCE($7, 0), COALESCE($8, FALSE), COALESCE($9, FALSE),
    $10, $11, now()
)
ON CONFLICT (attachment_id) DO UPDATE SET
    title       = COALESCE(EXCLUDED.title, document_curation.title),
    summary     = COALESCE(EXCLUDED.summary, document_curation.summary),
    category    = COALESCE(EXCLUDED.category, document_curation.category),
    tags        = CASE WHEN $6 IS NULL THEN document_curation.tags ELSE EXCLUDED.tags END,
    sort_order  = CASE WHEN $7 IS NULL THEN document_curation.sort_order ELSE EXCLUDED.sort_order END,
    pinned      = CASE WHEN $8 IS NULL THEN document_curation.pinned   ELSE EXCLUDED.pinned   END,
    archived    = CASE WHEN $9 IS NULL THEN document_curation.archived ELSE EXCLUDED.archived END,
    curator_type = EXCLUDED.curator_type,
    curator_id   = EXCLUDED.curator_id,
    updated_at   = now()
`

	_, err = h.DB.Exec(r.Context(), upsertSQL,
		parseUUID(workspaceID),
		parseUUID(attachmentID),
		payload.Title,
		payload.Summary,
		payload.Category,
		payload.Tags,
		payload.SortOrder,
		payload.Pinned,
		payload.Archived,
		actorType,
		parseUUID(actorID),
	)
	if err != nil {
		slog.Warn("upsert document curation failed", "error", err, "attachment_id", attachmentID)
		writeError(w, http.StatusInternalServerError, "failed to save curation")
		return
	}

	h.publish("document.curated", workspaceID, actorType, actorID, map[string]any{
		"attachment_id": attachmentID,
	})

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---------------------------------------------------------------------------
// Sections
// ---------------------------------------------------------------------------

type createSectionPayload struct {
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
	SortOrder   float64 `json:"sort_order,omitempty"`

	// Exactly one of the two below is required.
	IssueID   string `json:"issue_id,omitempty"`
	ProjectID string `json:"project_id,omitempty"`
}

// CreateLibrarySection makes a new section at issue or project scope.
// Sections are the primary organisational unit for the library UI.
func (h *Handler) CreateLibrarySection(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	var payload createSectionPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if payload.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	hasIssue := payload.IssueID != ""
	hasProject := payload.ProjectID != ""
	if hasIssue == hasProject {
		writeError(w, http.StatusBadRequest, "exactly one of issue_id or project_id must be set")
		return
	}

	const insertSQL = `
INSERT INTO library_section (workspace_id, issue_id, project_id, name, description, sort_order)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, created_at, updated_at
`

	var issueParam, projectParam pgtype.UUID
	if hasIssue {
		issueParam = parseUUID(payload.IssueID)
	}
	if hasProject {
		projectParam = parseUUID(payload.ProjectID)
	}

	var (
		id        pgtype.UUID
		createdAt pgtype.Timestamptz
		updatedAt pgtype.Timestamptz
	)
	err := h.DB.QueryRow(r.Context(), insertSQL,
		parseUUID(workspaceID), issueParam, projectParam,
		payload.Name, ptrToText(payload.Description), payload.SortOrder,
	).Scan(&id, &createdAt, &updatedAt)
	if err != nil {
		slog.Warn("create section failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create section")
		return
	}

	writeJSON(w, http.StatusCreated, LibrarySection{
		ID:          uuidToString(id),
		Name:        payload.Name,
		Description: payload.Description,
		SortOrder:   payload.SortOrder,
		CreatedAt:   timestampToString(createdAt),
		UpdatedAt:   timestampToString(updatedAt),
		DocumentIDs: []string{},
	})
}

type sectionItemPayload struct {
	AttachmentID string  `json:"attachment_id"`
	Position     float64 `json:"position,omitempty"`
}

// AddSectionItem places a document into a section at a given position.
// Idempotent via ON CONFLICT — re-adding the same document updates the
// position rather than failing.
func (h *Handler) AddSectionItem(w http.ResponseWriter, r *http.Request) {
	sectionID := chi.URLParam(r, "id")
	if sectionID == "" {
		writeError(w, http.StatusBadRequest, "section id is required")
		return
	}

	var payload sectionItemPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if payload.AttachmentID == "" {
		writeError(w, http.StatusBadRequest, "attachment_id is required")
		return
	}

	const insertSQL = `
INSERT INTO library_section_item (section_id, attachment_id, position)
VALUES ($1, $2, $3)
ON CONFLICT (section_id, attachment_id)
    DO UPDATE SET position = EXCLUDED.position
`
	_, err := h.DB.Exec(r.Context(), insertSQL,
		parseUUID(sectionID), parseUUID(payload.AttachmentID), payload.Position,
	)
	if err != nil {
		slog.Warn("add section item failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to add section item")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// RemoveSectionItem unlinks a document from a section without deleting it.
func (h *Handler) RemoveSectionItem(w http.ResponseWriter, r *http.Request) {
	sectionID := chi.URLParam(r, "id")
	attachmentID := chi.URLParam(r, "attachmentId")
	if sectionID == "" || attachmentID == "" {
		writeError(w, http.StatusBadRequest, "section id and attachment id are required")
		return
	}

	const delSQL = `DELETE FROM library_section_item WHERE section_id = $1 AND attachment_id = $2`
	_, err := h.DB.Exec(r.Context(), delSQL, parseUUID(sectionID), parseUUID(attachmentID))
	if err != nil {
		slog.Warn("remove section item failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to remove section item")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Query helpers
// ---------------------------------------------------------------------------

func (h *Handler) queryDocuments(r *http.Request, sqlQuery string, args ...any) ([]Document, error) {
	rows, err := h.DB.Query(r.Context(), sqlQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var docs []Document
	for rows.Next() {
		var row documentRow
		if err := rows.Scan(
			&row.AttachmentID, &row.IssueID, &row.CommentID, &row.Filename,
			&row.ContentType, &row.SizeBytes, &row.URL, &row.CreatedAt,
			&row.UploaderType, &row.UploaderID,
			&row.Title, &row.Summary, &row.Category, &row.Tags,
			&row.SortOrder, &row.Pinned, &row.Archived,
			&row.CuratorType, &row.CuratorID, &row.CuratedAt,
			&row.DocumentID, &row.VersionNumber,
		); err != nil {
			return nil, err
		}
		docs = append(docs, h.toDocument(row, true))
	}
	if docs == nil {
		docs = []Document{}
	}
	return docs, rows.Err()
}

func (h *Handler) querySections(r *http.Request, sqlQuery string, arg any) ([]LibrarySection, error) {
	rows, err := h.DB.Query(r.Context(), sqlQuery, arg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sections []LibrarySection
	for rows.Next() {
		var (
			id          pgtype.UUID
			name        string
			description pgtype.Text
			sortOrder   float64
			createdAt   pgtype.Timestamptz
			updatedAt   pgtype.Timestamptz
			items       []pgtype.UUID
		)
		if err := rows.Scan(&id, &name, &description, &sortOrder, &createdAt, &updatedAt, &items); err != nil {
			return nil, err
		}
		docIDs := make([]string, 0, len(items))
		for _, u := range items {
			docIDs = append(docIDs, uuidToString(u))
		}
		sections = append(sections, LibrarySection{
			ID:          uuidToString(id),
			Name:        name,
			Description: textToPtr(description),
			SortOrder:   sortOrder,
			CreatedAt:   timestampToString(createdAt),
			UpdatedAt:   timestampToString(updatedAt),
			DocumentIDs: docIDs,
		})
	}
	if sections == nil {
		sections = []LibrarySection{}
	}
	return sections, rows.Err()
}

// Sentinel for downstream handlers that want to short-circuit on missing
// curation without a DB error. Not currently used but kept for symmetry
// with other handler files that expose their own sentinels.
var ErrNoCuration = errors.New("no curation record")

var _ = pgx.ErrNoRows // suppress unused-import warnings when building without test file
