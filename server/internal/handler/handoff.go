package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type HandoffResponse struct {
	ID               string   `json:"id"`
	WorkspaceID      string   `json:"workspace_id"`
	IssueID          string   `json:"issue_id"`
	TaskID           *string  `json:"task_id"`
	AuthorType       *string  `json:"author_type"`
	AuthorID         *string  `json:"author_id"`
	NextAssigneeType *string  `json:"next_assignee_type"`
	NextAssigneeID   *string  `json:"next_assignee_id"`
	WorkflowRunID    *string  `json:"workflow_run_id"`
	WorkflowStepID   *string  `json:"workflow_step_id"`
	WorkCompleted    string   `json:"work_completed"`
	WorkRemaining    string   `json:"work_remaining"`
	DecisionsMade    string   `json:"decisions_made"`
	Uncertainties    string   `json:"uncertainties"`
	FollowUpIssueIDs []string `json:"follow_up_issue_ids"`
	CreatedAt        string   `json:"created_at"`
	UpdatedAt        string   `json:"updated_at"`
}

type CreateHandoffRequest struct {
	TaskID           *string  `json:"task_id"`
	NextAssigneeType *string  `json:"next_assignee_type"`
	NextAssigneeID   *string  `json:"next_assignee_id"`
	WorkflowRunID    *string  `json:"workflow_run_id"`
	WorkflowStepID   *string  `json:"workflow_step_id"`
	WorkCompleted    string   `json:"work_completed"`
	WorkRemaining    string   `json:"work_remaining"`
	DecisionsMade    string   `json:"decisions_made"`
	Uncertainties    string   `json:"uncertainties"`
	FollowUpIssueIDs []string `json:"follow_up_issue_ids"`
}

func handoffToResponse(h db.IssueHandoff, followUps []pgtype.UUID) HandoffResponse {
	ids := make([]string, 0, len(followUps))
	for _, id := range followUps {
		ids = append(ids, uuidToString(id))
	}
	return HandoffResponse{
		ID:               uuidToString(h.ID),
		WorkspaceID:      uuidToString(h.WorkspaceID),
		IssueID:          uuidToString(h.IssueID),
		TaskID:           uuidToPtr(h.TaskID),
		AuthorType:       textToPtr(h.AuthorType),
		AuthorID:         uuidToPtr(h.AuthorID),
		NextAssigneeType: textToPtr(h.NextAssigneeType),
		NextAssigneeID:   uuidToPtr(h.NextAssigneeID),
		WorkflowRunID:    uuidToPtr(h.WorkflowRunID),
		WorkflowStepID:   uuidToPtr(h.WorkflowStepID),
		WorkCompleted:    h.WorkCompleted,
		WorkRemaining:    h.WorkRemaining,
		DecisionsMade:    h.DecisionsMade,
		Uncertainties:    h.Uncertainties,
		FollowUpIssueIDs: ids,
		CreatedAt:        timestampToString(h.CreatedAt),
		UpdatedAt:        timestampToString(h.UpdatedAt),
	}
}

func handoffResponses(rows []db.IssueHandoff, followUps map[string][]pgtype.UUID) []HandoffResponse {
	out := make([]HandoffResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, handoffToResponse(row, followUps[uuidToString(row.ID)]))
	}
	return out
}

func (h *Handler) ListHandoffs(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}

	rows, err := h.Queries.ListIssueHandoffsForIssue(r.Context(), db.ListIssueHandoffsForIssueParams{
		WorkspaceID: issue.WorkspaceID,
		IssueID:     issue.ID,
	})
	if err != nil {
		slog.Warn("list handoffs failed", append(logger.RequestAttrs(r), "error", err, "issue_id", issueID)...)
		writeError(w, http.StatusInternalServerError, "failed to list handoffs")
		return
	}

	followUps, err := h.followUpIssueIDsForHandoffs(r, rows)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list handoff follow-up issues")
		return
	}
	writeJSON(w, http.StatusOK, handoffResponses(rows, followUps))
}

func (h *Handler) GetHandoff(w http.ResponseWriter, r *http.Request) {
	handoffID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "handoff id")
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	row, err := h.Queries.GetIssueHandoffInWorkspace(r.Context(), db.GetIssueHandoffInWorkspaceParams{
		ID:          handoffID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "handoff not found")
			return
		}
		slog.Warn("get handoff failed", append(logger.RequestAttrs(r), "error", err, "handoff_id", uuidToString(handoffID))...)
		writeError(w, http.StatusInternalServerError, "failed to get handoff")
		return
	}
	if _, ok := h.loadIssueForUser(w, r, uuidToString(row.IssueID)); !ok {
		return
	}

	followUps, err := h.followUpIssueIDsForHandoffs(r, []db.IssueHandoff{row})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list handoff follow-up issues")
		return
	}
	writeJSON(w, http.StatusOK, handoffToResponse(row, followUps[uuidToString(row.ID)]))
}

func (h *Handler) CreateHandoff(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req CreateHandoffRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	normaliseHandoffRequest(&req)
	if req.WorkCompleted == "" && req.WorkRemaining == "" && req.DecisionsMade == "" && req.Uncertainties == "" {
		writeError(w, http.StatusBadRequest, "at least one structured content field is required")
		return
	}

	taskID, ok := parseOptionalUUIDField(w, req.TaskID, "task_id")
	if !ok {
		return
	}
	if ok := h.validateHandoffTaskLink(w, r, issue, taskID); !ok {
		return
	}
	nextType, nextID, ok := h.parseAndValidateNextAssignee(w, r, issue.WorkspaceID, req.NextAssigneeType, req.NextAssigneeID)
	if !ok {
		return
	}
	workflowRunID, ok := parseOptionalUUIDField(w, req.WorkflowRunID, "workflow_run_id")
	if !ok {
		return
	}
	workflowStepID, ok := parseOptionalUUIDField(w, req.WorkflowStepID, "workflow_step_id")
	if !ok {
		return
	}
	if ok := h.validateHandoffWorkflowLinks(w, r, issue, workflowRunID, workflowStepID); !ok {
		return
	}
	followUpIDs, ok := parseUUIDSliceOrBadRequest(w, req.FollowUpIssueIDs, "follow_up_issue_ids")
	if !ok {
		return
	}
	if ok := h.validateFollowUpIssuesInWorkspace(w, r, issue.WorkspaceID, followUpIDs); !ok {
		return
	}

	authorType, authorID := h.resolveActor(r, userID, uuidToString(issue.WorkspaceID))
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		slog.Warn("create handoff begin tx failed", append(logger.RequestAttrs(r), "error", err, "issue_id", issueID)...)
		writeError(w, http.StatusInternalServerError, "failed to create handoff")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	row, err := qtx.CreateIssueHandoff(r.Context(), db.CreateIssueHandoffParams{
		WorkspaceID:      issue.WorkspaceID,
		IssueID:          issue.ID,
		TaskID:           taskID,
		AuthorType:       strToText(authorType),
		AuthorID:         parseUUID(authorID),
		NextAssigneeType: nextType,
		NextAssigneeID:   nextID,
		WorkflowRunID:    workflowRunID,
		WorkflowStepID:   workflowStepID,
		WorkCompleted:    req.WorkCompleted,
		WorkRemaining:    req.WorkRemaining,
		DecisionsMade:    req.DecisionsMade,
		Uncertainties:    req.Uncertainties,
	})
	if err != nil {
		slog.Warn("create handoff failed", append(logger.RequestAttrs(r), "error", err, "issue_id", issueID)...)
		writeError(w, http.StatusInternalServerError, "failed to create handoff")
		return
	}
	if len(followUpIDs) > 0 {
		if err := qtx.AddIssueHandoffFollowUpIssues(r.Context(), db.AddIssueHandoffFollowUpIssuesParams{
			IssueHandoffID: row.ID,
			IssueIds:       followUpIDs,
		}); err != nil {
			slog.Warn("add handoff follow-ups failed", append(logger.RequestAttrs(r), "error", err, "handoff_id", uuidToString(row.ID))...)
			writeError(w, http.StatusInternalServerError, "failed to create handoff follow-up issues")
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		slog.Warn("create handoff commit failed", append(logger.RequestAttrs(r), "error", err, "handoff_id", uuidToString(row.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to create handoff")
		return
	}

	writeJSON(w, http.StatusCreated, handoffToResponse(row, followUpIDs))
}

func normaliseHandoffRequest(req *CreateHandoffRequest) {
	req.WorkCompleted = strings.TrimSpace(req.WorkCompleted)
	req.WorkRemaining = strings.TrimSpace(req.WorkRemaining)
	req.DecisionsMade = strings.TrimSpace(req.DecisionsMade)
	req.Uncertainties = strings.TrimSpace(req.Uncertainties)
}

func parseOptionalUUIDField(w http.ResponseWriter, value *string, fieldName string) (pgtype.UUID, bool) {
	if value == nil || strings.TrimSpace(*value) == "" {
		return pgtype.UUID{}, true
	}
	return parseUUIDOrBadRequest(w, *value, fieldName)
}

func optionalText(s *string) pgtype.Text {
	if s == nil || strings.TrimSpace(*s) == "" {
		return pgtype.Text{}
	}
	return strToText(strings.TrimSpace(*s))
}

func (h *Handler) parseAndValidateNextAssignee(w http.ResponseWriter, r *http.Request, workspaceID pgtype.UUID, assigneeType, assigneeID *string) (pgtype.Text, pgtype.UUID, bool) {
	if assigneeType == nil && assigneeID == nil {
		return pgtype.Text{}, pgtype.UUID{}, true
	}
	if assigneeType == nil || strings.TrimSpace(*assigneeType) == "" || assigneeID == nil || strings.TrimSpace(*assigneeID) == "" {
		writeError(w, http.StatusBadRequest, "next_assignee_type and next_assignee_id must be provided together")
		return pgtype.Text{}, pgtype.UUID{}, false
	}
	typ := strings.TrimSpace(*assigneeType)
	id, ok := parseUUIDOrBadRequest(w, *assigneeID, "next_assignee_id")
	if !ok {
		return pgtype.Text{}, pgtype.UUID{}, false
	}
	switch typ {
	case "agent":
		if _, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{ID: id, WorkspaceID: workspaceID}); err != nil {
			writeError(w, http.StatusBadRequest, "invalid next assignee")
			return pgtype.Text{}, pgtype.UUID{}, false
		}
	case "member":
		if _, err := h.Queries.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{UserID: id, WorkspaceID: workspaceID}); err != nil {
			writeError(w, http.StatusBadRequest, "invalid next assignee")
			return pgtype.Text{}, pgtype.UUID{}, false
		}
	case "squad":
		if _, err := h.Queries.GetSquadInWorkspace(r.Context(), db.GetSquadInWorkspaceParams{ID: id, WorkspaceID: workspaceID}); err != nil {
			writeError(w, http.StatusBadRequest, "invalid next assignee")
			return pgtype.Text{}, pgtype.UUID{}, false
		}
	default:
		writeError(w, http.StatusBadRequest, "next_assignee_type must be agent, member, or squad")
		return pgtype.Text{}, pgtype.UUID{}, false
	}
	return optionalText(&typ), id, true
}

func (h *Handler) validateHandoffTaskLink(w http.ResponseWriter, r *http.Request, issue db.Issue, taskID pgtype.UUID) bool {
	if !taskID.Valid {
		return true
	}
	task, err := h.Queries.GetAgentTask(r.Context(), taskID)
	if err != nil || !task.IssueID.Valid || uuidToString(task.IssueID) != uuidToString(issue.ID) {
		writeError(w, http.StatusBadRequest, "task_id must reference a task for this issue")
		return false
	}
	return true
}

func (h *Handler) validateHandoffWorkflowLinks(w http.ResponseWriter, r *http.Request, issue db.Issue, runID, stepID pgtype.UUID) bool {
	if !runID.Valid && !stepID.Valid {
		return true
	}

	run, err := h.Queries.GetIssueWorkflowRun(r.Context(), issue.ID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "workflow links require a workflow run for this issue")
		return false
	}
	if runID.Valid && uuidToString(run.ID) != uuidToString(runID) {
		writeError(w, http.StatusBadRequest, "workflow_run_id must reference this issue's workflow run")
		return false
	}
	if stepID.Valid {
		step, err := h.Queries.GetWorkflowStep(r.Context(), stepID)
		if err != nil || uuidToString(step.WorkflowID) != uuidToString(run.WorkflowID) {
			writeError(w, http.StatusBadRequest, "workflow_step_id must reference a step in this issue's workflow")
			return false
		}
	}
	return true
}

func (h *Handler) validateFollowUpIssuesInWorkspace(w http.ResponseWriter, r *http.Request, workspaceID pgtype.UUID, ids []pgtype.UUID) bool {
	if len(ids) == 0 {
		return true
	}
	seen := make(map[string]struct{}, len(ids))
	unique := make([]pgtype.UUID, 0, len(ids))
	for _, id := range ids {
		key := uuidToString(id)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, id)
	}
	count, err := h.Queries.CountIssuesInWorkspaceByIDs(r.Context(), db.CountIssuesInWorkspaceByIDsParams{
		WorkspaceID: workspaceID,
		IssueIds:    unique,
	})
	if err != nil {
		slog.Warn("validate handoff follow-ups failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to validate follow-up issues")
		return false
	}
	if count != int32(len(unique)) {
		writeError(w, http.StatusBadRequest, "follow_up_issue_ids must reference issues in the same workspace")
		return false
	}
	return true
}

func (h *Handler) followUpIssueIDsForHandoffs(r *http.Request, rows []db.IssueHandoff) (map[string][]pgtype.UUID, error) {
	out := make(map[string][]pgtype.UUID, len(rows))
	if len(rows) == 0 {
		return out, nil
	}
	ids := make([]pgtype.UUID, 0, len(rows))
	for _, row := range rows {
		id := row.ID
		ids = append(ids, id)
		out[uuidToString(id)] = []pgtype.UUID{}
	}
	followUps, err := h.Queries.ListIssueHandoffFollowUpIssues(r.Context(), ids)
	if err != nil {
		slog.Warn("list handoff follow-ups failed", append(logger.RequestAttrs(r), "error", err)...)
		return nil, err
	}
	for _, row := range followUps {
		key := uuidToString(row.IssueHandoffID)
		out[key] = append(out[key], row.IssueID)
	}
	return out, nil
}
