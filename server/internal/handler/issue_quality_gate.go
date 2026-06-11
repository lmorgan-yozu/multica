package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
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

func normalizeProjectQualityGateConfig(raw json.RawMessage) []byte {
	if len(raw) == 0 || string(raw) == "null" {
		return []byte(`{}`)
	}
	return raw
}

func (h *Handler) enforceIssueQualityGates(ctx context.Context, issue db.Issue, fromStatus, toStatus, actorType, actorID string) ([]qualityGate, *qualityGateDecision, error) {
	state, ok, err := h.loadIssueQualityGateProjectState(ctx, issue)
	if err != nil || !ok {
		return nil, nil, err
	}

	var passed []qualityGate
	for _, gate := range state.Config.Gates {
		if gate.Transition.From != fromStatus || gate.Transition.To != toStatus {
			continue
		}
		if gate.Key == "" {
			gate.Key = gate.Name
		}
		if gate.Name == "" {
			gate.Name = gate.Key
		}
		if gate.RequiredActorType != "" && gate.RequiredActorType != actorType {
			return nil, &qualityGateDecision{
				Gate:   gate,
				Reason: fmt.Sprintf("missing quality gate %q: %s must be performed by %s", gate.Name, gateRole(gate), gate.RequiredActorType),
			}, nil
		}
		if gate.Independent && actorType == "agent" && actorID != "" {
			same, err := h.agentMateriallyImplementedIssue(ctx, issue.ID, actorID)
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

func (h *Handler) agentMateriallyImplementedIssue(ctx context.Context, issueID pgtype.UUID, agentID string) (bool, error) {
	var exists bool
	err := h.DB.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM agent_task_queue
			WHERE issue_id = $1
			  AND agent_id = $2::uuid
			  AND status IN ('running', 'completed')
			  AND (started_at IS NOT NULL OR completed_at IS NOT NULL)
		)
	`, issueID, agentID).Scan(&exists)
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
			continue
		}
	}
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
		SELECT gate_key, actor_type, actor_id, created_at
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
		var key, actorType string
		var actorID pgtype.UUID
		var createdAt pgtype.Timestamptz
		if err := rows.Scan(&key, &actorType, &actorID, &createdAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read quality gate events")
			return
		}
		if _, ok := completed[key]; !ok {
			completed[key] = map[string]any{
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
		event, complete := completed[gate.Key]
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
