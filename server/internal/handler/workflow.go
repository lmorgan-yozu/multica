package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgconn"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Handlers for handoff workflows. See docs/handoff-workflows.md and the advance
// engine in issue_workflow.go. The CRUD here only defines and binds workflows;
// the actual handoff routing happens in advanceWorkflowOnStatusChange.

type WorkflowResponse struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type WorkflowStepResponse struct {
	ID            string `json:"id"`
	WorkflowID    string `json:"workflow_id"`
	StepOrder     int32  `json:"step_order"`
	AgentID       string `json:"agent_id"`
	Name          string `json:"name"`
	StartStatus   string `json:"start_status"`
	AdvanceStatus string `json:"advance_status"`
}

func workflowToResponse(wf db.Workflow) WorkflowResponse {
	return WorkflowResponse{
		ID:          uuidToString(wf.ID),
		WorkspaceID: uuidToString(wf.WorkspaceID),
		Name:        wf.Name,
		Description: wf.Description,
		CreatedAt:   timestampToString(wf.CreatedAt),
		UpdatedAt:   timestampToString(wf.UpdatedAt),
	}
}

func workflowStepToResponse(s db.WorkflowStep) WorkflowStepResponse {
	return WorkflowStepResponse{
		ID:            uuidToString(s.ID),
		WorkflowID:    uuidToString(s.WorkflowID),
		StepOrder:     s.StepOrder,
		AgentID:       uuidToString(s.AgentID),
		Name:          s.Name,
		StartStatus:   s.StartStatus,
		AdvanceStatus: s.AdvanceStatus,
	}
}

// ListWorkflows GET /api/workflows
func (h *Handler) ListWorkflows(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	workflows, err := h.Queries.ListWorkflows(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list workflows")
		return
	}
	resp := make([]WorkflowResponse, len(workflows))
	for i, wf := range workflows {
		resp[i] = workflowToResponse(wf)
	}
	writeJSON(w, http.StatusOK, map[string]any{"workflows": resp, "total": len(resp)})
}

type createWorkflowRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// CreateWorkflow POST /api/workflows
func (h *Handler) CreateWorkflow(w http.ResponseWriter, r *http.Request) {
	var req createWorkflowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)

	wf, err := h.Queries.CreateWorkflow(r.Context(), db.CreateWorkflowParams{
		WorkspaceID:   parseUUID(workspaceID),
		Name:          req.Name,
		Description:   req.Description,
		CreatedByType: "member",
		CreatedByID:   parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create workflow")
		return
	}
	// No realtime publish: there is no workflow-specific event type yet and
	// reusing EventIssueUpdated would deliver a payload with no issue to
	// issue:updated subscribers. The REST response is the only consumer until
	// a frontend (and a dedicated event) lands.
	writeJSON(w, http.StatusCreated, workflowToResponse(wf))
}

// GetWorkflow GET /api/workflows/{id} — workflow plus its ordered steps.
func (h *Handler) GetWorkflow(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wfID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workflow id")
	if !ok {
		return
	}
	wf, err := h.Queries.GetWorkflowInWorkspace(r.Context(), db.GetWorkflowInWorkspaceParams{
		ID:          wfID,
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "workflow not found")
		return
	}
	steps, err := h.Queries.ListWorkflowSteps(r.Context(), wf.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load steps")
		return
	}
	stepResp := make([]WorkflowStepResponse, len(steps))
	for i, s := range steps {
		stepResp[i] = workflowStepToResponse(s)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"workflow": workflowToResponse(wf),
		"steps":    stepResp,
	})
}

// isValidIssueStatus reports whether s is one of the issue.status CHECK values
// (server/migrations/001_init.up.sql). Workflow steps reference issue statuses
// for start_status / advance_status.
func isValidIssueStatus(s string) bool {
	switch s {
	case "backlog", "todo", "in_progress", "in_review", "done", "blocked", "cancelled":
		return true
	}
	return false
}

type createWorkflowStepRequest struct {
	AgentID       string `json:"agent_id"`
	Name          string `json:"name"`
	StartStatus   string `json:"start_status"`
	AdvanceStatus string `json:"advance_status"`
}

// CreateWorkflowStep POST /api/workflows/{id}/steps — appends a step at the end.
func (h *Handler) CreateWorkflowStep(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wfID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workflow id")
	if !ok {
		return
	}
	var req createWorkflowStepRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	agentUUID, ok := parseUUIDOrBadRequest(w, req.AgentID, "agent_id")
	if !ok {
		return
	}
	wf, err := h.Queries.GetWorkflowInWorkspace(r.Context(), db.GetWorkflowInWorkspaceParams{
		ID:          wfID,
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "workflow not found")
		return
	}
	// Validate the agent belongs to the workspace.
	if _, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
		ID:          agentUUID,
		WorkspaceID: parseUUID(workspaceID),
	}); err != nil {
		writeError(w, http.StatusBadRequest, "agent not found in workspace")
		return
	}

	startStatus := req.StartStatus
	if startStatus == "" {
		startStatus = "todo"
	}
	advanceStatus := req.AdvanceStatus
	if advanceStatus == "" {
		advanceStatus = "in_review"
	}
	if !isValidIssueStatus(startStatus) {
		writeError(w, http.StatusBadRequest, "invalid start_status")
		return
	}
	if !isValidIssueStatus(advanceStatus) {
		writeError(w, http.StatusBadRequest, "invalid advance_status")
		return
	}

	maxOrder, err := h.Queries.MaxWorkflowStepOrder(r.Context(), wf.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to compute step order")
		return
	}
	step, err := h.Queries.CreateWorkflowStep(r.Context(), db.CreateWorkflowStepParams{
		WorkflowID:    wf.ID,
		StepOrder:     maxOrder + 1,
		AgentID:       agentUUID,
		Name:          req.Name,
		StartStatus:   startStatus,
		AdvanceStatus: advanceStatus,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create step")
		return
	}
	writeJSON(w, http.StatusCreated, workflowStepToResponse(step))
}

type bindWorkflowRequest struct {
	IssueID string `json:"issue_id"`
}

// BindIssueWorkflow POST /api/workflows/{id}/bind — starts the workflow on an
// issue: creates the run at step 1, assigns the issue to the first step's
// agent at its start_status, and triggers that agent.
func (h *Handler) BindIssueWorkflow(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wfID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workflow id")
	if !ok {
		return
	}
	var req bindWorkflowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	issueUUID, ok := parseUUIDOrBadRequest(w, req.IssueID, "issue_id")
	if !ok {
		return
	}
	wf, err := h.Queries.GetWorkflowInWorkspace(r.Context(), db.GetWorkflowInWorkspaceParams{
		ID:          wfID,
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "workflow not found")
		return
	}
	steps, err := h.Queries.ListWorkflowSteps(r.Context(), wf.ID)
	if err != nil || len(steps) == 0 {
		writeError(w, http.StatusBadRequest, "workflow has no steps")
		return
	}
	issue, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
		ID:          issueUUID,
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "issue not found")
		return
	}
	if _, err := h.Queries.GetIssueWorkflowRun(r.Context(), issue.ID); err == nil {
		writeError(w, http.StatusConflict, "issue is already bound to a workflow")
		return
	}

	first := steps[0]
	run, err := h.Queries.CreateIssueWorkflowRun(r.Context(), db.CreateIssueWorkflowRunParams{
		IssueID:       issue.ID,
		WorkflowID:    wf.ID,
		CurrentStepID: first.ID,
	})
	if err != nil {
		// UNIQUE(issue_id) — a concurrent bind won the race between the check
		// above and this insert. Report the conflict rather than a 500.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeError(w, http.StatusConflict, "issue is already bound to a workflow")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to bind issue")
		return
	}

	updated, err := h.Queries.AssignIssueToWorkflowStep(r.Context(), db.AssignIssueToWorkflowStepParams{
		ID:         issue.ID,
		AssigneeID: first.AgentID,
		Status:     first.StartStatus,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to assign first step")
		return
	}

	prefix := h.getIssuePrefix(r.Context(), updated.WorkspaceID)
	h.publish(protocol.EventIssueUpdated, uuidToString(updated.WorkspaceID), "system", "", map[string]any{
		"issue":            issueToResponse(updated, prefix),
		"assignee_changed": true,
		"status_changed":   true,
	})

	mention := h.buildAgentMention(r.Context(), updated.WorkspaceID, first.AgentID)
	content := mention + "Workflow \"" + wf.Name + "\" started — you're up for " + stepLabel(first) + "."
	comment := h.postWorkflowSystemComment(r.Context(), updated, content)
	h.triggerWorkflowAgent(r.Context(), updated, first.AgentID, comment)

	writeJSON(w, http.StatusCreated, map[string]any{
		"run_id":          uuidToString(run.ID),
		"workflow_id":     uuidToString(wf.ID),
		"issue_id":        uuidToString(issue.ID),
		"current_step_id": uuidToString(first.ID),
		"state":           run.State,
	})
}
