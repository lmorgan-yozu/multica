package handler

// Team templates: exportable, versioned snapshots of a delivery team that
// can be applied to a new engagement (ADA-30; docs/adr/0002-team-templates.md).
//
// Actor contract: human workspace members only. Export and template
// management are owner/admin operations — a template is reusable company
// IP assembled from agent instructions and skills, and the export gate
// (autonomy/secret lint) is a governance control, so agents never call
// these endpoints. Agent actors get 403, members below admin get 403.

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/teamtmpl"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// --- Response shapes ---

type TeamTemplateResponse struct {
	ID            string `json:"id"`
	WorkspaceID   string `json:"workspace_id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	CreatedBy     string `json:"created_by,omitempty"`
	LatestVersion int32  `json:"latest_version"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

type TeamTemplateVersionSummaryResponse struct {
	ID                string `json:"id"`
	TemplateID        string `json:"template_id"`
	Version           int32  `json:"version"`
	SourceWorkspaceID string `json:"source_workspace_id,omitempty"`
	Notes             string `json:"notes"`
	CreatedBy         string `json:"created_by,omitempty"`
	CreatedAt         string `json:"created_at"`
}

type TeamTemplateVersionResponse struct {
	TeamTemplateVersionSummaryResponse
	Manifest json.RawMessage `json:"manifest"`
}

type TeamTemplateDetailResponse struct {
	TeamTemplateResponse
	Versions []TeamTemplateVersionSummaryResponse `json:"versions"`
}

func teamTemplateToResponse(t db.TeamTemplate, latestVersion int32) TeamTemplateResponse {
	return TeamTemplateResponse{
		ID:            uuidToString(t.ID),
		WorkspaceID:   uuidToString(t.WorkspaceID),
		Name:          t.Name,
		Description:   t.Description,
		CreatedBy:     uuidToString(t.CreatedBy),
		LatestVersion: latestVersion,
		CreatedAt:     t.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:     t.UpdatedAt.Time.Format(time.RFC3339),
	}
}

func teamTemplateVersionSummary(v db.TeamTemplateVersion) TeamTemplateVersionSummaryResponse {
	return TeamTemplateVersionSummaryResponse{
		ID:                uuidToString(v.ID),
		TemplateID:        uuidToString(v.TemplateID),
		Version:           v.Version,
		SourceWorkspaceID: uuidToString(v.SourceWorkspaceID),
		Notes:             v.Notes,
		CreatedBy:         uuidToString(v.CreatedBy),
		CreatedAt:         v.CreatedAt.Time.Format(time.RFC3339),
	}
}

// requireTeamTemplateAdmin enforces the actor contract shared by every
// team-template endpoint: a human member with owner/admin role. Returns
// ok=false after writing the error response.
func (h *Handler) requireTeamTemplateAdmin(w http.ResponseWriter, r *http.Request, workspaceID string) (string, bool) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return "", false
	}
	if actorType, _ := h.resolveActor(r, userID, workspaceID); actorType == "agent" {
		writeError(w, http.StatusForbidden, "team templates are managed by human workspace admins, not agents")
		return "", false
	}
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return "", false
	}
	if !roleAllowed(member.Role, "owner", "admin") {
		writeError(w, http.StatusForbidden, "team template management requires the owner or admin role")
		return "", false
	}
	return userID, true
}

// --- List / Get ---

func (h *Handler) ListTeamTemplates(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.requireTeamTemplateAdmin(w, r, workspaceID); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	templates, err := h.Queries.ListTeamTemplates(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list team templates")
		return
	}
	resp := make([]TeamTemplateResponse, 0, len(templates))
	for _, t := range templates {
		latest, err := h.Queries.MaxTeamTemplateVersion(r.Context(), t.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load template versions")
			return
		}
		resp = append(resp, teamTemplateToResponse(t, latest))
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) GetTeamTemplate(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.requireTeamTemplateAdmin(w, r, workspaceID); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	idUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "template id")
	if !ok {
		return
	}

	tmpl, err := h.Queries.GetTeamTemplateInWorkspace(r.Context(), db.GetTeamTemplateInWorkspaceParams{ID: idUUID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "team template not found")
		return
	}
	versions, err := h.Queries.ListTeamTemplateVersions(r.Context(), tmpl.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load template versions")
		return
	}
	var latest int32
	summaries := make([]TeamTemplateVersionSummaryResponse, 0, len(versions))
	for _, v := range versions {
		if v.Version > latest {
			latest = v.Version
		}
		summaries = append(summaries, TeamTemplateVersionSummaryResponse{
			ID:                uuidToString(v.ID),
			TemplateID:        uuidToString(v.TemplateID),
			Version:           v.Version,
			SourceWorkspaceID: uuidToString(v.SourceWorkspaceID),
			Notes:             v.Notes,
			CreatedBy:         uuidToString(v.CreatedBy),
			CreatedAt:         v.CreatedAt.Time.Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, TeamTemplateDetailResponse{
		TeamTemplateResponse: teamTemplateToResponse(tmpl, latest),
		Versions:             summaries,
	})
}

func (h *Handler) GetTeamTemplateVersion(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.requireTeamTemplateAdmin(w, r, workspaceID); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	idUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "template id")
	if !ok {
		return
	}
	versionNum, err := strconv.ParseInt(chi.URLParam(r, "version"), 10, 32)
	if err != nil || versionNum < 1 {
		writeError(w, http.StatusBadRequest, "invalid version number")
		return
	}

	tmpl, err := h.Queries.GetTeamTemplateInWorkspace(r.Context(), db.GetTeamTemplateInWorkspaceParams{ID: idUUID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "team template not found")
		return
	}
	version, err := h.Queries.GetTeamTemplateVersion(r.Context(), db.GetTeamTemplateVersionParams{TemplateID: tmpl.ID, Version: int32(versionNum)})
	if err != nil {
		writeError(w, http.StatusNotFound, "template version not found")
		return
	}
	writeJSON(w, http.StatusOK, TeamTemplateVersionResponse{
		TeamTemplateVersionSummaryResponse: teamTemplateVersionSummary(version),
		Manifest:                           json.RawMessage(version.Manifest),
	})
}

// --- Export ---

type ExportTeamTemplateRequest struct {
	// TemplateID, when set, appends a new version to an existing template
	// instead of creating one. Name/Description are then ignored.
	TemplateID  string `json:"template_id,omitempty"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Notes       string `json:"notes,omitempty"`

	AgentIDs     []string `json:"agent_ids"`
	SquadID      string   `json:"squad_id,omitempty"`
	WorkflowIDs  []string `json:"workflow_ids,omitempty"`
	AutopilotIDs []string `json:"autopilot_ids,omitempty"`
	// SkillIDs is the approved cross-project skill pack. Inclusion is an
	// explicit selection (ADA-30 AC 5): skills bound to exported agents
	// but not listed here are silently dropped from the role's bindings.
	SkillIDs []string `json:"skill_ids,omitempty"`

	Parameters    []teamtmpl.Parameter    `json:"parameters,omitempty"`
	Substitutions []teamtmpl.Substitution `json:"substitutions,omitempty"`
}

type ExportTeamTemplateResponse struct {
	Template TeamTemplateResponse        `json:"template"`
	Version  TeamTemplateVersionResponse `json:"version"`
}

type exportLintFailureResponse struct {
	Error    string             `json:"error"`
	Findings []teamtmpl.Finding `json:"findings"`
}

func (h *Handler) ExportTeamTemplate(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	userID, ok := h.requireTeamTemplateAdmin(w, r, workspaceID)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	var req ExportTeamTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.AgentIDs) == 0 {
		writeError(w, http.StatusBadRequest, "agent_ids is required: a team template needs at least one role")
		return
	}
	if req.TemplateID == "" && req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required when creating a new template")
		return
	}

	manifest, buildErr := h.buildTeamTemplateManifest(r, wsUUID, req)
	if buildErr != nil {
		writeError(w, buildErr.status, buildErr.message)
		return
	}

	// The export gate: autonomy language, blocked skills, and secret
	// patterns all fail closed with structured findings (ADA-30 AC 5/6).
	if findings := teamtmpl.Lint(manifest); len(findings) > 0 {
		slog.Info("team-template export: lint blocked",
			append(logger.RequestAttrs(r), "finding_count", len(findings))...)
		writeJSON(w, http.StatusUnprocessableEntity, exportLintFailureResponse{
			Error:    "export blocked: the selection carries autonomy language, a blocked skill, or a secret — fix the source material or exclude the artefact",
			Findings: findings,
		})
		return
	}

	if err := manifest.Validate(); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "manifest validation failed: "+err.Error())
		return
	}

	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode manifest")
		return
	}

	creatorUUID := parseUUID(userID)
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to begin tx")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	var tmpl db.TeamTemplate
	if req.TemplateID != "" {
		tmplUUID, ok := parseUUIDOrBadRequest(w, req.TemplateID, "template_id")
		if !ok {
			return
		}
		tmpl, err = qtx.GetTeamTemplateInWorkspace(r.Context(), db.GetTeamTemplateInWorkspaceParams{ID: tmplUUID, WorkspaceID: wsUUID})
		if err != nil {
			writeError(w, http.StatusNotFound, "team template not found")
			return
		}
	} else {
		tmpl, err = qtx.CreateTeamTemplate(r.Context(), db.CreateTeamTemplateParams{
			WorkspaceID: wsUUID,
			Name:        req.Name,
			Description: req.Description,
			CreatedBy:   creatorUUID,
		})
		if err != nil {
			if isUniqueViolation(err) {
				writeError(w, http.StatusConflict, fmt.Sprintf("a team template named %q already exists in this workspace", req.Name))
				return
			}
			writeError(w, http.StatusInternalServerError, "failed to create team template")
			return
		}
	}

	maxVersion, err := qtx.MaxTeamTemplateVersion(r.Context(), tmpl.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve template version")
		return
	}
	version, err := qtx.CreateTeamTemplateVersion(r.Context(), db.CreateTeamTemplateVersionParams{
		TemplateID:        tmpl.ID,
		Version:           maxVersion + 1,
		Manifest:          manifestJSON,
		SourceWorkspaceID: wsUUID,
		Notes:             req.Notes,
		CreatedBy:         creatorUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create template version")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "commit failed")
		return
	}

	slog.Info("team template exported",
		append(logger.RequestAttrs(r),
			"template_id", uuidToString(tmpl.ID),
			"version", version.Version,
			"role_count", len(manifest.Roles),
			"skill_count", len(manifest.Skills),
		)...)

	writeJSON(w, http.StatusCreated, ExportTeamTemplateResponse{
		Template: teamTemplateToResponse(tmpl, version.Version),
		Version: TeamTemplateVersionResponse{
			TeamTemplateVersionSummaryResponse: teamTemplateVersionSummary(version),
			Manifest:                           json.RawMessage(version.Manifest),
		},
	})
}

// exportError carries an HTTP status alongside the message so the build
// helper can distinguish caller mistakes (404/422) from server faults.
type exportError struct {
	status  int
	message string
}

// buildTeamTemplateManifest assembles the manifest from explicitly
// selected workspace entities. Sanitisation is structural: only the
// whitelisted fields below are ever read into the manifest — custom_env,
// custom_args, mcp_config, runtime IDs, and webhook tokens have no
// destination field to land in (docs/adr/0002-team-templates.md).
func (h *Handler) buildTeamTemplateManifest(r *http.Request, wsUUID pgtype.UUID, req ExportTeamTemplateRequest) (*teamtmpl.Manifest, *exportError) {
	ctx := r.Context()
	now := time.Now().UTC().Format(time.RFC3339)

	// Skill pack first, so role bindings can filter against it.
	skillNameByID := make(map[string]string, len(req.SkillIDs))
	skills := make([]teamtmpl.Skill, 0, len(req.SkillIDs))
	for _, raw := range req.SkillIDs {
		skillUUID, err := parseUUIDValue(raw)
		if err != nil {
			return nil, &exportError{http.StatusBadRequest, "invalid skill id: " + raw}
		}
		skill, err := h.Queries.GetSkillInWorkspace(ctx, db.GetSkillInWorkspaceParams{ID: skillUUID, WorkspaceID: wsUUID})
		if err != nil {
			return nil, &exportError{http.StatusNotFound, "skill not found in this workspace: " + raw}
		}
		files, err := h.Queries.ListSkillFiles(ctx, skill.ID)
		if err != nil {
			return nil, &exportError{http.StatusInternalServerError, "failed to load skill files"}
		}
		mf := teamtmpl.Skill{
			Name:        skill.Name,
			Description: skill.Description,
			Content:     skill.Content,
			Source: teamtmpl.SkillSource{
				WorkspaceID: uuidToString(wsUUID),
				SkillID:     uuidToString(skill.ID),
				ExportedAt:  now,
			},
		}
		for _, f := range files {
			mf.Files = append(mf.Files, teamtmpl.SkillFile{Path: f.Path, Content: f.Content})
		}
		skills = append(skills, mf)
		skillNameByID[uuidToString(skill.ID)] = skill.Name
	}

	// Roles. Role keys are derived from agent names; the key set is the
	// reference namespace for squad members, workflow steps, and
	// autopilot assignees.
	roleByAgentID := make(map[string]string, len(req.AgentIDs))
	usedKeys := make(map[string]bool, len(req.AgentIDs))
	roles := make([]teamtmpl.Role, 0, len(req.AgentIDs))
	for _, raw := range req.AgentIDs {
		agentUUID, err := parseUUIDValue(raw)
		if err != nil {
			return nil, &exportError{http.StatusBadRequest, "invalid agent id: " + raw}
		}
		agent, err := h.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: agentUUID, WorkspaceID: wsUUID})
		if err != nil {
			return nil, &exportError{http.StatusNotFound, "agent not found in this workspace: " + raw}
		}
		key := teamtmpl.SlugifyRoleKey(agent.Name)
		if key == "" || usedKeys[key] {
			return nil, &exportError{http.StatusUnprocessableEntity, fmt.Sprintf("agent %q does not produce a unique role key", agent.Name)}
		}
		usedKeys[key] = true
		roleByAgentID[uuidToString(agent.ID)] = key

		provider := ""
		if agent.RuntimeID.Valid {
			runtime, err := h.Queries.GetAgentRuntime(ctx, agent.RuntimeID)
			if err == nil {
				provider = runtime.Provider
			}
		}

		role := teamtmpl.Role{
			Key:                key,
			Name:               agent.Name,
			Description:        agent.Description,
			Instructions:       agent.Instructions,
			Provider:           provider,
			Model:              agent.Model.String,
			ThinkingLevel:      agent.ThinkingLevel.String,
			MaxConcurrentTasks: agent.MaxConcurrentTasks,
			AvatarURL:          agent.AvatarUrl.String,
		}

		bound, err := h.Queries.ListAgentSkills(ctx, agent.ID)
		if err != nil {
			return nil, &exportError{http.StatusInternalServerError, "failed to load agent skills"}
		}
		for _, s := range bound {
			// Bindings outside the explicit skill pack are dropped: the
			// pack is the approved transferable set (ADA-30 AC 5).
			if name, inPack := skillNameByID[uuidToString(s.ID)]; inPack {
				role.Skills = append(role.Skills, name)
			}
		}
		roles = append(roles, role)
	}

	manifest := &teamtmpl.Manifest{
		Format:      teamtmpl.FormatVersion,
		Name:        req.Name,
		Description: req.Description,
		Parameters:  req.Parameters,
		Roles:       roles,
		Skills:      skills,
		Provenance: teamtmpl.Provenance{
			SourceWorkspaceID: uuidToString(wsUUID),
			ExportedAt:        now,
		},
	}

	// Squad shape. Human members are dropped: a client engagement has its
	// own humans. Agent members outside the selection are an error — the
	// squad would arrive broken.
	var squadID string
	if req.SquadID != "" {
		squadUUID, err := parseUUIDValue(req.SquadID)
		if err != nil {
			return nil, &exportError{http.StatusBadRequest, "invalid squad id"}
		}
		squad, err := h.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{ID: squadUUID, WorkspaceID: wsUUID})
		if err != nil {
			return nil, &exportError{http.StatusNotFound, "squad not found in this workspace"}
		}
		squadID = uuidToString(squad.ID)
		leaderRole, ok := roleByAgentID[uuidToString(squad.LeaderID)]
		if !ok {
			return nil, &exportError{http.StatusUnprocessableEntity, fmt.Sprintf("squad %q leader is not in agent_ids", squad.Name)}
		}
		ms := &teamtmpl.Squad{
			Name:         squad.Name,
			Description:  squad.Description,
			Instructions: squad.Instructions,
			LeaderRole:   leaderRole,
		}
		members, err := h.Queries.ListSquadMembers(ctx, squad.ID)
		if err != nil {
			return nil, &exportError{http.StatusInternalServerError, "failed to load squad members"}
		}
		for _, m := range members {
			if m.MemberType != "agent" {
				continue
			}
			roleKey, ok := roleByAgentID[uuidToString(m.MemberID)]
			if !ok {
				return nil, &exportError{http.StatusUnprocessableEntity, fmt.Sprintf("squad %q has agent member %s outside agent_ids — include it or remove it from the squad", squad.Name, uuidToString(m.MemberID))}
			}
			ms.Members = append(ms.Members, teamtmpl.SquadMember{Role: roleKey, SquadRole: m.Role})
		}
		manifest.Squad = ms
	}

	// Workflows: steps reference roles, never UUIDs.
	for _, raw := range req.WorkflowIDs {
		wfUUID, err := parseUUIDValue(raw)
		if err != nil {
			return nil, &exportError{http.StatusBadRequest, "invalid workflow id: " + raw}
		}
		wf, err := h.Queries.GetWorkflowInWorkspace(ctx, db.GetWorkflowInWorkspaceParams{ID: wfUUID, WorkspaceID: wsUUID})
		if err != nil {
			return nil, &exportError{http.StatusNotFound, "workflow not found in this workspace: " + raw}
		}
		steps, err := h.Queries.ListWorkflowSteps(ctx, wf.ID)
		if err != nil {
			return nil, &exportError{http.StatusInternalServerError, "failed to load workflow steps"}
		}
		mwf := teamtmpl.Workflow{Name: wf.Name, Description: wf.Description}
		for _, st := range steps {
			roleKey, ok := roleByAgentID[uuidToString(st.AgentID)]
			if !ok {
				return nil, &exportError{http.StatusUnprocessableEntity, fmt.Sprintf("workflow %q step %d references an agent outside agent_ids", wf.Name, st.StepOrder)}
			}
			mwf.Steps = append(mwf.Steps, teamtmpl.WorkflowStep{
				Order:         st.StepOrder,
				Name:          st.Name,
				Role:          roleKey,
				StartStatus:   st.StartStatus,
				AdvanceStatus: st.AdvanceStatus,
			})
		}
		manifest.Workflows = append(manifest.Workflows, mwf)
	}

	// Autopilots: webhook tokens and signing secrets are never read —
	// only the trigger shape travels.
	for _, raw := range req.AutopilotIDs {
		apUUID, err := parseUUIDValue(raw)
		if err != nil {
			return nil, &exportError{http.StatusBadRequest, "invalid autopilot id: " + raw}
		}
		ap, err := h.Queries.GetAutopilotInWorkspace(ctx, db.GetAutopilotInWorkspaceParams{ID: apUUID, WorkspaceID: wsUUID})
		if err != nil {
			return nil, &exportError{http.StatusNotFound, "autopilot not found in this workspace: " + raw}
		}
		mAp := teamtmpl.Autopilot{
			Title:              ap.Title,
			Description:        ap.Description.String,
			ExecutionMode:      ap.ExecutionMode,
			IssueTitleTemplate: ap.IssueTitleTemplate.String,
		}
		switch ap.AssigneeType {
		case "squad":
			if uuidToString(ap.AssigneeID) != squadID {
				return nil, &exportError{http.StatusUnprocessableEntity, fmt.Sprintf("autopilot %q is assigned to a squad outside the selection", ap.Title)}
			}
			mAp.AssigneeSquad = true
		default:
			roleKey, ok := roleByAgentID[uuidToString(ap.AssigneeID)]
			if !ok {
				return nil, &exportError{http.StatusUnprocessableEntity, fmt.Sprintf("autopilot %q is assigned to an agent outside agent_ids", ap.Title)}
			}
			mAp.AssigneeRole = roleKey
		}
		triggers, err := h.Queries.ListAutopilotTriggers(ctx, ap.ID)
		if err != nil {
			return nil, &exportError{http.StatusInternalServerError, "failed to load autopilot triggers"}
		}
		for _, tr := range triggers {
			mAp.Triggers = append(mAp.Triggers, teamtmpl.AutopilotTrigger{
				Kind:           tr.Kind,
				CronExpression: tr.CronExpression.String,
				Timezone:       tr.Timezone.String,
				Label:          tr.Label.String,
			})
		}
		manifest.Autopilots = append(manifest.Autopilots, mAp)
	}

	// Parameterisation: replace source-workspace literals with declared
	// placeholders so the manifest carries parameters, not Ada's values
	// (ADA-30 AC 4).
	declared := make(map[string]bool, len(req.Parameters))
	for _, p := range req.Parameters {
		declared[p.Key] = true
	}
	for _, sub := range req.Substitutions {
		if !declared[sub.ParamKey] {
			return nil, &exportError{http.StatusBadRequest, fmt.Sprintf("substitution references undeclared parameter %q", sub.ParamKey)}
		}
		if sub.Find == "" {
			return nil, &exportError{http.StatusBadRequest, "substitution with empty find string"}
		}
		teamtmpl.Substitute(manifest, sub)
	}

	if req.TemplateID != "" && manifest.Name == "" {
		// Appending a version to an existing template: carry the
		// template's name into the manifest for self-containedness.
		tmplUUID, err := parseUUIDValue(req.TemplateID)
		if err != nil {
			return nil, &exportError{http.StatusBadRequest, "invalid template_id"}
		}
		tmpl, err := h.Queries.GetTeamTemplateInWorkspace(ctx, db.GetTeamTemplateInWorkspaceParams{ID: tmplUUID, WorkspaceID: wsUUID})
		if err != nil {
			return nil, &exportError{http.StatusNotFound, "team template not found"}
		}
		manifest.Name = tmpl.Name
		if manifest.Description == "" {
			manifest.Description = tmpl.Description
		}
	}

	return manifest, nil
}

// parseUUIDValue is the error-returning variant for loops where
// parseUUIDOrBadRequest's direct response write doesn't fit.
func parseUUIDValue(s string) (pgtype.UUID, error) {
	return util.ParseUUID(s)
}
