package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type qualityGateConfig struct {
	Gates []qualityGate `json:"gates"`
}

type qualityGate struct {
	Key               string                `json:"key"`
	Name              string                `json:"name"`
	Order             int                   `json:"order"`
	RequiredActorType string                `json:"required_actor_type"`
	RequiredRole      string                `json:"required_role"`
	Independent       bool                  `json:"independent"`
	Transition        qualityGateTransition `json:"transition"`
}

type qualityGateTransition struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type qualityGateProjectState struct {
	ProjectID pgtype.UUID
	Config    qualityGateConfig
}

type qualityGateDecision struct {
	Gate   qualityGate
	Reason string
}

type qualityGateOverrideRequest struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
}

type skippedQualityGateResponse struct {
	Key        string                `json:"key"`
	Name       string                `json:"name"`
	Transition qualityGateTransition `json:"transition"`
	NextActor  string                `json:"next_actor"`
}

func normalizeProjectQualityGateConfig(raw json.RawMessage) []byte {
	if len(raw) == 0 || string(raw) == "null" {
		return []byte(`{}`)
	}
	return raw
}

func validateAndNormalizeProjectQualityGateConfig(raw json.RawMessage) ([]byte, error) {
	normalized := normalizeProjectQualityGateConfig(raw)
	if string(normalized) == "{}" {
		return normalized, nil
	}
	if !bytes.HasPrefix(bytes.TrimSpace(normalized), []byte("{")) {
		return nil, fmt.Errorf("quality_gate_config must be an object with a gates array")
	}
	var cfg qualityGateConfig
	if err := json.Unmarshal(normalized, &cfg); err != nil {
		return nil, fmt.Errorf("quality_gate_config must be an object with a gates array")
	}
	for i, gate := range cfg.Gates {
		if strings.TrimSpace(gate.Key) == "" {
			return nil, fmt.Errorf("quality_gate_config.gates[%d].key is required", i)
		}
		if strings.TrimSpace(gate.Transition.From) == "" || strings.TrimSpace(gate.Transition.To) == "" {
			return nil, fmt.Errorf("quality_gate_config.gates[%d].transition.from and transition.to are required", i)
		}
		switch gate.RequiredActorType {
		case "", "member", "agent", "system":
		default:
			return nil, fmt.Errorf("quality_gate_config.gates[%d].required_actor_type must be member, agent, or system", i)
		}
	}
	return normalized, nil
}

func (h *Handler) enforceIssueQualityGates(ctx context.Context, issue db.Issue, fromStatus, toStatus, actorType, actorID string) ([]qualityGate, *qualityGateDecision, error) {
	state, ok, err := h.loadIssueQualityGateProjectState(ctx, issue)
	if err != nil || !ok {
		return nil, nil, err
	}

	var passed []qualityGate
	for _, gate := range state.Config.Gates {
		if gate.Transition.To != toStatus {
			continue
		}

		eventExists, err := h.issueQualityGateEventExists(ctx, issue.ID, gate)
		if err != nil {
			return nil, nil, err
		}
		if eventExists {
			continue
		}

		currentTransitionCanPassGate := gate.Transition.From == fromStatus
		if !currentTransitionCanPassGate {
			return nil, &qualityGateDecision{
				Gate:   gate,
				Reason: fmt.Sprintf("missing quality gate %q: %s must pass before moving to %s", gate.Name, gateRole(gate), gate.Transition.To),
			}, nil
		}

		if gate.RequiredActorType != "" && gate.RequiredActorType != actorType {
			return nil, &qualityGateDecision{
				Gate:   gate,
				Reason: fmt.Sprintf("missing quality gate %q: %s must be performed by %s", gate.Name, gateRole(gate), gate.RequiredActorType),
			}, nil
		}
		if gate.Independent && actorType == "agent" && actorID != "" {
			same, err := h.agentMateriallyImplementedIssue(ctx, issue, actorID)
			if err != nil {
				return nil, nil, err
			}
			if same {
				return nil, &qualityGateDecision{
					Gate:   gate,
					Reason: fmt.Sprintf("missing quality gate %q: %s requires an independent reviewer; the implementing agent cannot approve its own work", gate.Name, gateRole(gate)),
				}, nil
			}
		}
		passed = append(passed, gate)
	}
	return passed, nil, nil
}

func (h *Handler) loadIssueQualityGateProjectState(ctx context.Context, issue db.Issue) (qualityGateProjectState, bool, error) {
	if !issue.ProjectID.Valid {
		return qualityGateProjectState{}, false, nil
	}

	var projectID pgtype.UUID
	var raw []byte
	err := h.DB.QueryRow(ctx, `
		SELECT id, quality_gate_config
		FROM project
		WHERE id = $1 AND workspace_id = $2
	`, issue.ProjectID, issue.WorkspaceID).Scan(&projectID, &raw)
	if err != nil {
		return qualityGateProjectState{}, false, err
	}
	if len(raw) == 0 || string(raw) == "{}" {
		return qualityGateProjectState{}, false, nil
	}

	var cfg qualityGateConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return qualityGateProjectState{}, false, err
	}
	gates := make([]qualityGate, 0, len(cfg.Gates))
	for _, gate := range cfg.Gates {
		if gate.Key == "" || gate.Transition.From == "" || gate.Transition.To == "" {
			continue
		}
		if gate.Name == "" {
			gate.Name = gate.Key
		}
		gates = append(gates, gate)
	}
	if len(gates) == 0 {
		return qualityGateProjectState{}, false, nil
	}
	sort.SliceStable(gates, func(i, j int) bool {
		return gates[i].Order < gates[j].Order
	})
	cfg.Gates = gates
	return qualityGateProjectState{ProjectID: projectID, Config: cfg}, true, nil
}

func (h *Handler) issueQualityGateEventExists(ctx context.Context, issueID pgtype.UUID, gate qualityGate) (bool, error) {
	var exists bool
	err := h.DB.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM issue_quality_gate_event
			WHERE issue_id = $1
			  AND gate_key = $2
			  AND from_status = $3
			  AND to_status = $4
		)
	`, issueID, gate.Key, gate.Transition.From, gate.Transition.To).Scan(&exists)
	return exists, err
}

func (h *Handler) skippedQualityGatesForTransition(ctx context.Context, issue db.Issue, toStatus string) ([]qualityGate, qualityGateProjectState, bool, error) {
	state, enabled, err := h.loadIssueQualityGateProjectState(ctx, issue)
	if err != nil || !enabled {
		return nil, state, enabled, err
	}
	var skipped []qualityGate
	for _, gate := range state.Config.Gates {
		if gate.Transition.To != toStatus {
			continue
		}
		exists, err := h.issueQualityGateEventExists(ctx, issue.ID, gate)
		if err != nil {
			return nil, state, enabled, err
		}
		if !exists {
			skipped = append(skipped, gate)
		}
	}
	return skipped, state, true, nil
}

func (h *Handler) agentMateriallyImplementedIssue(ctx context.Context, issue db.Issue, agentID string) (bool, error) {
	var exists bool
	err := h.DB.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM agent_task_queue
			WHERE issue_id = $1
			  AND agent_id = $2::uuid
			  AND status IN ('running', 'completed')
			  AND (started_at IS NOT NULL OR completed_at IS NOT NULL)
			  AND COALESCE(started_at, completed_at, created_at) < $3
		)
	`, issue.ID, agentID, issue.UpdatedAt).Scan(&exists)
	return exists, err
}

func (h *Handler) recordIssueQualityGateEvents(ctx context.Context, issue db.Issue, gates []qualityGate, fromStatus, toStatus, actorType, actorID string) {
	if len(gates) == 0 || !issue.ProjectID.Valid {
		return
	}
	var actorUUID pgtype.UUID
	if actorID != "" {
		actorUUID = parseUUID(actorID)
	}
	for _, gate := range gates {
		if _, err := h.DB.Exec(ctx, `
			INSERT INTO issue_quality_gate_event (
				workspace_id, project_id, issue_id, gate_key, gate_name,
				from_status, to_status, actor_type, actor_id
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		`, issue.WorkspaceID, issue.ProjectID, issue.ID, gate.Key, gate.Name, fromStatus, toStatus, actorType, actorUUID); err != nil {
			slog.Warn("record issue quality gate event failed", "issue_id", uuidToString(issue.ID), "gate_key", gate.Key, "error", err)
		}
	}
}

func qualityGateResponses(gates []qualityGate) []skippedQualityGateResponse {
	resp := make([]skippedQualityGateResponse, 0, len(gates))
	for _, gate := range gates {
		resp = append(resp, skippedQualityGateResponse{
			Key:        gate.Key,
			Name:       gate.Name,
			Transition: gate.Transition,
			NextActor:  gateRole(gate),
		})
	}
	return resp
}

func gateRole(gate qualityGate) string {
	if gate.RequiredRole != "" {
		return gate.RequiredRole
	}
	if gate.RequiredActorType != "" {
		return gate.RequiredActorType
	}
	return gate.Name
}

func (h *Handler) GetIssueQualityGates(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, id)
	if !ok {
		return
	}

	state, enabled, err := h.loadIssueQualityGateProjectState(r.Context(), issue)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load quality gate state")
		return
	}
	if !enabled {
		writeJSON(w, http.StatusOK, map[string]any{
			"enabled": false,
			"gates":   []any{},
		})
		return
	}

	rows, err := h.DB.Query(r.Context(), `
		SELECT gate_key, from_status, to_status, actor_type, actor_id, created_at
		FROM issue_quality_gate_event
		WHERE issue_id = $1
		ORDER BY created_at DESC
	`, issue.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load quality gate events")
		return
	}
	defer rows.Close()
	completed := map[string]map[string]any{}
	for rows.Next() {
		var key, fromStatus, toStatus, actorType string
		var actorID pgtype.UUID
		var createdAt pgtype.Timestamptz
		if err := rows.Scan(&key, &fromStatus, &toStatus, &actorType, &actorID, &createdAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read quality gate events")
			return
		}
		completedKey := qualityGateEventKey(key, fromStatus, toStatus)
		if _, ok := completed[completedKey]; !ok {
			completed[completedKey] = map[string]any{
				"actor_type": actorType,
				"actor_id":   uuidToPtr(actorID),
				"created_at": timestampToString(createdAt),
			}
		}
	}

	type gateState struct {
		Key               string                `json:"key"`
		Name              string                `json:"name"`
		Order             int                   `json:"order"`
		RequiredActorType string                `json:"required_actor_type,omitempty"`
		RequiredRole      string                `json:"required_role,omitempty"`
		Independent       bool                  `json:"independent"`
		Transition        qualityGateTransition `json:"transition"`
		Complete          bool                  `json:"complete"`
		Blocked           bool                  `json:"blocked"`
		Reason            string                `json:"reason,omitempty"`
		NextActor         string                `json:"next_actor,omitempty"`
		Event             map[string]any        `json:"event,omitempty"`
	}

	gates := make([]gateState, 0, len(state.Config.Gates))
	for _, gate := range state.Config.Gates {
		event, complete := completed[qualityGateEventKey(gate.Key, gate.Transition.From, gate.Transition.To)]
		gs := gateState{
			Key: gate.Key, Name: gate.Name, Order: gate.Order,
			RequiredActorType: gate.RequiredActorType,
			RequiredRole:      gate.RequiredRole,
			Independent:       gate.Independent,
			Transition:        gate.Transition,
			Complete:          complete,
			Event:             event,
		}
		if !complete && gate.Transition.From == issue.Status {
			gs.Blocked = true
			gs.NextActor = gateRole(gate)
			gs.Reason = fmt.Sprintf("%s must pass before moving from %s to %s", gateRole(gate), gate.Transition.From, gate.Transition.To)
		}
		gates = append(gates, gs)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":    true,
		"project_id": uuidToString(state.ProjectID),
		"issue_id":   uuidToString(issue.ID),
		"gates":      gates,
	})
}

func (h *Handler) OverrideIssueQualityGate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	prevIssue, ok := h.loadIssueForUser(w, r, id)
	if !ok {
		return
	}
	workspaceID := uuidToString(prevIssue.WorkspaceID)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	if actorType != "member" {
		writeError(w, http.StatusForbidden, "quality gate override requires a workspace owner or admin")
		return
	}
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}

	var req qualityGateOverrideRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Status = strings.TrimSpace(req.Status)
	req.Reason = strings.TrimSpace(req.Reason)
	if req.Status == "" {
		writeError(w, http.StatusBadRequest, "status is required")
		return
	}
	if !isValidIssueStatus(req.Status) {
		writeError(w, http.StatusBadRequest, "invalid status")
		return
	}
	if req.Reason == "" {
		writeError(w, http.StatusBadRequest, "override reason is required")
		return
	}
	if req.Status == prevIssue.Status {
		writeError(w, http.StatusBadRequest, "status is unchanged")
		return
	}

	skipped, _, enabled, err := h.skippedQualityGatesForTransition(r.Context(), prevIssue, req.Status)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load quality gate state")
		return
	}
	if !enabled || len(skipped) == 0 {
		writeError(w, http.StatusConflict, "no quality gate is blocking this transition; use normal issue status update")
		return
	}

	details, err := json.Marshal(map[string]any{
		"reason":        req.Reason,
		"from_status":   prevIssue.Status,
		"to_status":     req.Status,
		"skipped_gates": qualityGateResponses(skipped),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode quality gate override")
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to override quality gate")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	issue, err := qtx.UpdateIssue(r.Context(), db.UpdateIssueParams{
		ID:            prevIssue.ID,
		Status:        pgtype.Text{String: req.Status, Valid: true},
		AssigneeType:  prevIssue.AssigneeType,
		AssigneeID:    prevIssue.AssigneeID,
		StartDate:     prevIssue.StartDate,
		DueDate:       prevIssue.DueDate,
		ParentIssueID: prevIssue.ParentIssueID,
		ProjectID:     prevIssue.ProjectID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to override quality gate")
		return
	}

	var actorUUID pgtype.UUID
	if actorID != "" {
		actorUUID = parseUUID(actorID)
	}
	for _, gate := range skipped {
		if _, err := tx.Exec(r.Context(), `
			INSERT INTO issue_quality_gate_event (
				workspace_id, project_id, issue_id, gate_key, gate_name,
				from_status, to_status, actor_type, actor_id
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		`, issue.WorkspaceID, issue.ProjectID, issue.ID, gate.Key, gate.Name, gate.Transition.From, gate.Transition.To, actorType, actorUUID); err != nil {
			slog.Warn("record issue quality gate override event failed", "issue_id", uuidToString(issue.ID), "gate_key", gate.Key, "error", err)
			writeError(w, http.StatusInternalServerError, "audit log write failed; quality gate override rolled back")
			return
		}
	}
	if _, err := qtx.CreateActivity(r.Context(), db.CreateActivityParams{
		WorkspaceID: issue.WorkspaceID,
		IssueID:     issue.ID,
		ActorType:   pgtype.Text{String: actorType, Valid: true},
		ActorID:     actorUUID,
		Action:      "quality_gate_override",
		Details:     details,
	}); err != nil {
		slog.Warn("record quality gate override activity failed", "issue_id", uuidToString(issue.ID), "error", err)
		writeError(w, http.StatusInternalServerError, "audit log write failed; quality gate override rolled back")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to override quality gate")
		return
	}

	prefix := h.getIssuePrefix(r.Context(), issue.WorkspaceID)
	resp := issueToResponse(issue, prefix)

	h.publish(protocol.EventIssueUpdated, workspaceID, actorType, actorID, map[string]any{
		"issue":          resp,
		"status_changed": true,
		"prev_status":    prevIssue.Status,
	})

	if issue.Status == "cancelled" {
		h.TaskService.CancelTasksForIssue(r.Context(), issue.ID)
	}
	if prevIssue.Status == "backlog" && issue.Status != "done" && issue.Status != "cancelled" {
		if h.isAgentAssigneeReady(r.Context(), issue) {
			h.TaskService.EnqueueTaskForIssue(r.Context(), issue)
		}
		if h.isSquadLeaderReady(r.Context(), issue) {
			h.enqueueSquadLeaderTask(r.Context(), issue, pgtype.UUID{}, actorType, actorID)
		}
	}
	h.notifyParentOfChildDone(r.Context(), prevIssue, issue, actorType, actorID)
	h.advanceWorkflowOnStatusChange(r.Context(), prevIssue, issue, actorType, actorID)

	writeJSON(w, http.StatusOK, map[string]any{
		"issue": resp,
		"override": map[string]any{
			"reason":        req.Reason,
			"skipped_gates": qualityGateResponses(skipped),
		},
	})
}

func qualityGateEventKey(key, fromStatus, toStatus string) string {
	return key + "\x00" + fromStatus + "\x00" + toStatus
}
