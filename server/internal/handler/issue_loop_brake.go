package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
)

type upsertIssueLoopBrakeConfigRequest struct {
	ProjectID           string  `json:"project_id,omitempty"`
	Enabled             bool    `json:"enabled"`
	WindowMinutes       int32   `json:"window_minutes"`
	MinRunCount         int32   `json:"min_run_count"`
	MinTotalTokens      int64   `json:"min_total_tokens"`
	MinEstimatedCostUSD float64 `json:"min_estimated_cost_usd"`
}

type clearIssueLoopBrakeRequest struct {
	Reason string `json:"reason"`
}

func (h *Handler) GetIssueLoopBrakeConfig(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	projectID, ok := optionalUUIDParam(w, r.URL.Query().Get("project_id"), "project_id")
	if !ok {
		return
	}
	cfg, err := h.TaskService.GetIssueLoopBrakeConfig(r.Context(), parseUUID(workspaceID), projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get loop brake config")
		return
	}
	if cfg == nil {
		writeError(w, http.StatusNotFound, "loop brake config not found")
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (h *Handler) UpsertIssueLoopBrakeConfig(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	if !roleAllowed(member.Role, "owner", "admin") {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}
	var req upsertIssueLoopBrakeConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	projectID, ok := optionalUUIDParam(w, req.ProjectID, "project_id")
	if !ok {
		return
	}
	cfg, err := h.TaskService.UpsertIssueLoopBrakeConfig(r.Context(), service.UpsertIssueLoopBrakeConfigParams{
		WorkspaceID:         parseUUID(workspaceID),
		ProjectID:           projectID,
		Enabled:             req.Enabled,
		WindowMinutes:       req.WindowMinutes,
		MinRunCount:         req.MinRunCount,
		MinTotalTokens:      req.MinTotalTokens,
		MinEstimatedCostUSD: req.MinEstimatedCostUSD,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (h *Handler) GetIssueLoopBrake(w http.ResponseWriter, r *http.Request) {
	issueID, ok := issueInWorkspace(w, r, h)
	if !ok {
		return
	}
	brake, err := h.TaskService.GetActiveIssueLoopBrake(r.Context(), issueID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get issue loop brake")
		return
	}
	if brake == nil {
		writeJSON(w, http.StatusOK, map[string]any{"state": "clear"})
		return
	}
	writeJSON(w, http.StatusOK, brake)
}

func (h *Handler) ClearIssueLoopBrake(w http.ResponseWriter, r *http.Request) {
	issueID, ok := issueInWorkspace(w, r, h)
	if !ok {
		return
	}
	issue, err := h.Queries.GetIssue(r.Context(), issueID)
	if err != nil {
		writeError(w, http.StatusNotFound, "issue not found")
		return
	}
	member, ok := h.workspaceMember(w, r, uuidToString(issue.WorkspaceID))
	if !ok {
		return
	}
	if !roleAllowed(member.Role, "owner", "admin") {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}
	var req clearIssueLoopBrakeRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	brake, err := h.TaskService.ClearIssueLoopBrake(r.Context(), issueID, "member", member.UserID, strings.TrimSpace(req.Reason))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clear issue loop brake")
		return
	}
	if brake == nil {
		writeJSON(w, http.StatusOK, map[string]any{"state": "clear"})
		return
	}
	writeJSON(w, http.StatusOK, brake)
}

func issueInWorkspace(w http.ResponseWriter, r *http.Request, h *Handler) (pgtype.UUID, bool) {
	raw := chi.URLParam(r, "id")
	issueID, err := util.ParseUUID(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid issue id")
		return pgtype.UUID{}, false
	}
	issue, err := h.Queries.GetIssue(r.Context(), issueID)
	if err != nil {
		writeError(w, http.StatusNotFound, "issue not found")
		return pgtype.UUID{}, false
	}
	if _, ok := h.workspaceMember(w, r, uuidToString(issue.WorkspaceID)); !ok {
		return pgtype.UUID{}, false
	}
	return issueID, true
}

func optionalUUIDParam(w http.ResponseWriter, raw, name string) (pgtype.UUID, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return pgtype.UUID{}, true
	}
	id, err := util.ParseUUID(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid "+name)
		return pgtype.UUID{}, false
	}
	return id, true
}
