package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Roadmap foundation (ADA-57). Actor contract for every handler in this
// file: any authenticated workspace member or agent (the routes sit under
// RequireWorkspaceMember); cross-workspace ids resolve to 404 exactly like
// GetProject. No owner/admin-only reach.

// MilestoneResponse is the JSON shape for a roadmap milestone.
type MilestoneResponse struct {
	ID          string  `json:"id"`
	ProjectID   string  `json:"project_id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	TargetDate  *string `json:"target_date"`
	Position    float64 `json:"position"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

func milestoneToResponse(m db.Milestone) MilestoneResponse {
	return MilestoneResponse{
		ID:          uuidToString(m.ID),
		ProjectID:   uuidToString(m.ProjectID),
		Name:        m.Name,
		Description: m.Description,
		TargetDate:  dateToPtr(m.TargetDate),
		Position:    m.Position,
		CreatedAt:   timestampToString(m.CreatedAt),
		UpdatedAt:   timestampToString(m.UpdatedAt),
	}
}

// IssueDependencyResponse is the JSON shape for a roadmap dependency link:
// issue_id depends on depends_on_issue_id.
type IssueDependencyResponse struct {
	IssueID          string `json:"issue_id"`
	DependsOnIssueID string `json:"depends_on_issue_id"`
	Type             string `json:"type"`
	CreatedAt        string `json:"created_at"`
}

// loadProjectInWorkspace resolves the {id} URL param to a project in the
// caller's workspace, writing the error response on failure.
func (h *Handler) loadProjectInWorkspace(w http.ResponseWriter, r *http.Request) (db.Project, bool) {
	idUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "project id")
	if !ok {
		return db.Project{}, false
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace id")
	if !ok {
		return db.Project{}, false
	}
	project, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
		ID: idUUID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return db.Project{}, false
	}
	return project, true
}

// GetProjectRoadmap returns the derived roadmap projection for a project:
// ordered milestones, ordered epics with leaf progress rollups, and the
// dependency links that drive the ordering.
func (h *Handler) GetProjectRoadmap(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectInWorkspace(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	issues, err := h.Queries.ListProjectIssuesForRoadmap(ctx, project.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load project issues")
		return
	}
	milestones, err := h.Queries.ListProjectMilestones(ctx, project.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load project milestones")
		return
	}
	deps, err := h.Queries.ListProjectDependencyLinks(ctx, project.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load project dependencies")
		return
	}
	prefix := h.getIssuePrefix(ctx, project.WorkspaceID)
	resp := buildRoadmap(uuidToString(project.ID), project.Title, prefix, issues, milestones, deps)
	writeJSON(w, http.StatusOK, resp)
}

type CreateMilestoneRequest struct {
	Name        string   `json:"name"`
	Description *string  `json:"description"`
	TargetDate  *string  `json:"target_date"`
	Position    *float64 `json:"position"`
}

// CreateMilestone adds a milestone to a project.
func (h *Handler) CreateMilestone(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectInWorkspace(w, r)
	if !ok {
		return
	}
	var req CreateMilestoneRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	params := db.CreateMilestoneParams{
		ProjectID: project.ID,
		Name:      name,
	}
	if req.Description != nil {
		params.Description = *req.Description
	}
	if req.TargetDate != nil && *req.TargetDate != "" {
		d, err := util.ParseCalendarDate(*req.TargetDate)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid target_date format, expected YYYY-MM-DD")
			return
		}
		params.TargetDate = d
	}
	if req.Position != nil {
		params.Position = *req.Position
	}
	milestone, err := h.Queries.CreateMilestone(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create milestone")
		return
	}
	writeJSON(w, http.StatusCreated, milestoneToResponse(milestone))
}

type UpdateMilestoneRequest struct {
	Name        *string  `json:"name"`
	Description *string  `json:"description"`
	TargetDate  *string  `json:"target_date"`
	Position    *float64 `json:"position"`
}

// loadMilestoneForProject resolves {milestoneId} and verifies it belongs to
// the already-scoped project from the URL.
func (h *Handler) loadMilestoneForProject(w http.ResponseWriter, r *http.Request, project db.Project) (db.Milestone, bool) {
	msUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "milestoneId"), "milestone id")
	if !ok {
		return db.Milestone{}, false
	}
	milestone, err := h.Queries.GetMilestoneInWorkspace(r.Context(), db.GetMilestoneInWorkspaceParams{
		ID: msUUID, WorkspaceID: project.WorkspaceID,
	})
	if err != nil || milestone.ProjectID != project.ID {
		writeError(w, http.StatusNotFound, "milestone not found")
		return db.Milestone{}, false
	}
	return milestone, true
}

// UpdateMilestone partially updates a milestone. target_date follows the
// UpdateIssue convention: omitted = unchanged, null/empty = cleared.
func (h *Handler) UpdateMilestone(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectInWorkspace(w, r)
	if !ok {
		return
	}
	milestone, ok := h.loadMilestoneForProject(w, r, project)
	if !ok {
		return
	}
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}
	var req UpdateMilestoneRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var rawFields map[string]json.RawMessage
	json.Unmarshal(bodyBytes, &rawFields)

	params := db.UpdateMilestoneParams{
		ID:         milestone.ID,
		TargetDate: milestone.TargetDate, // narg: pre-fill with current value
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			writeError(w, http.StatusBadRequest, "name cannot be empty")
			return
		}
		params.Name = pgtype.Text{String: name, Valid: true}
	}
	if req.Description != nil {
		params.Description = pgtype.Text{String: *req.Description, Valid: true}
	}
	if _, present := rawFields["target_date"]; present {
		if req.TargetDate != nil && *req.TargetDate != "" {
			d, err := util.ParseCalendarDate(*req.TargetDate)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid target_date format, expected YYYY-MM-DD")
				return
			}
			params.TargetDate = d
		} else {
			params.TargetDate = pgtype.Date{Valid: false} // explicit null = clear
		}
	}
	if req.Position != nil {
		params.Position = pgtype.Float8{Float64: *req.Position, Valid: true}
	}
	updated, err := h.Queries.UpdateMilestone(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update milestone")
		return
	}
	writeJSON(w, http.StatusOK, milestoneToResponse(updated))
}

// DeleteMilestone removes a milestone. Issues referencing it fall back to
// ungrouped via the ON DELETE SET NULL foreign key.
func (h *Handler) DeleteMilestone(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectInWorkspace(w, r)
	if !ok {
		return
	}
	milestone, ok := h.loadMilestoneForProject(w, r, project)
	if !ok {
		return
	}
	if err := h.Queries.DeleteMilestone(r.Context(), milestone.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete milestone")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type SetIssueMilestoneRequest struct {
	MilestoneID *string `json:"milestone_id"`
}

// SetIssueMilestone assigns an issue (in practice: an epic) to a milestone,
// or clears the assignment with milestone_id: null. The milestone must
// belong to the issue's project.
func (h *Handler) SetIssueMilestone(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	var req SetIssueMilestoneRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	params := db.SetIssueMilestoneParams{
		ID:          issue.ID,
		WorkspaceID: issue.WorkspaceID,
	}
	if req.MilestoneID != nil {
		msUUID, ok := parseUUIDOrBadRequest(w, *req.MilestoneID, "milestone_id")
		if !ok {
			return
		}
		milestone, err := h.Queries.GetMilestoneInWorkspace(r.Context(), db.GetMilestoneInWorkspaceParams{
			ID: msUUID, WorkspaceID: issue.WorkspaceID,
		})
		if err != nil {
			writeError(w, http.StatusNotFound, "milestone not found")
			return
		}
		if !issue.ProjectID.Valid || milestone.ProjectID != issue.ProjectID {
			writeError(w, http.StatusBadRequest, "milestone must belong to the issue's project")
			return
		}
		params.MilestoneID = milestone.ID
	}
	updated, err := h.Queries.SetIssueMilestone(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to set issue milestone")
		return
	}
	prefix := h.getIssuePrefix(r.Context(), updated.WorkspaceID)
	writeJSON(w, http.StatusOK, issueToResponse(updated, prefix))
}

type CreateIssueDependencyRequest struct {
	DependsOnIssueID string `json:"depends_on_issue_id"`
}

// CreateIssueDependency links {id} -> depends_on_issue_id as a roadmap
// dependency (type='blocked_by'). Both issues must belong to the same
// project, and links that would close a cycle are rejected so the roadmap
// ordering stays well-defined.
func (h *Handler) CreateIssueDependency(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	var req CreateIssueDependencyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	dependsOnUUID, ok := parseUUIDOrBadRequest(w, req.DependsOnIssueID, "depends_on_issue_id")
	if !ok {
		return
	}
	if dependsOnUUID == issue.ID {
		writeError(w, http.StatusBadRequest, "an issue cannot depend on itself")
		return
	}
	target, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
		ID: dependsOnUUID, WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "depends_on issue not found")
		return
	}
	if !issue.ProjectID.Valid || !target.ProjectID.Valid || issue.ProjectID != target.ProjectID {
		writeError(w, http.StatusBadRequest, "dependency links must connect issues in the same project")
		return
	}
	cyclic, err := h.dependencyWouldCreateCycle(r, issue.ProjectID, issue.ID, target.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to validate dependency graph")
		return
	}
	if cyclic {
		writeError(w, http.StatusBadRequest, "dependency would create a cycle")
		return
	}
	link, err := h.Queries.CreateIssueDependencyLink(r.Context(), db.CreateIssueDependencyLinkParams{
		IssueID:          issue.ID,
		DependsOnIssueID: target.ID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create dependency link")
		return
	}
	writeJSON(w, http.StatusCreated, IssueDependencyResponse{
		IssueID:          uuidToString(link.IssueID),
		DependsOnIssueID: uuidToString(link.DependsOnIssueID),
		Type:             link.Type,
		CreatedAt:        timestampToString(link.CreatedAt),
	})
}

// dependencyWouldCreateCycle reports whether adding issueID -> dependsOnID
// closes a cycle, i.e. whether issueID is already reachable from dependsOnID
// by following existing depends-on links within the project.
func (h *Handler) dependencyWouldCreateCycle(r *http.Request, projectID, issueID, dependsOnID pgtype.UUID) (bool, error) {
	rows, err := h.Queries.ListProjectDependencyLinks(r.Context(), projectID)
	if err != nil {
		return false, err
	}
	adjacent := map[pgtype.UUID][]pgtype.UUID{}
	for _, row := range rows {
		adjacent[row.IssueID] = append(adjacent[row.IssueID], row.DependsOnIssueID)
	}
	visited := map[pgtype.UUID]bool{}
	stack := []pgtype.UUID{dependsOnID}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if cur == issueID {
			return true, nil
		}
		if visited[cur] {
			continue
		}
		visited[cur] = true
		stack = append(stack, adjacent[cur]...)
	}
	return false, nil
}

// DeleteIssueDependency removes a roadmap dependency link. Idempotent:
// deleting a link that does not exist still returns 204.
func (h *Handler) DeleteIssueDependency(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	dependsOnUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "dependsOnId"), "depends_on issue id")
	if !ok {
		return
	}
	if err := h.Queries.DeleteIssueDependencyLink(r.Context(), db.DeleteIssueDependencyLinkParams{
		IssueID:          issue.ID,
		DependsOnIssueID: dependsOnUUID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete dependency link")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
