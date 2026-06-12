package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/teamtmpl"
)

// createTeamTemplateTestAgent inserts an agent with real instructions and a
// custom_env secret — the secret exists to prove export never carries it.
func createTeamTemplateTestAgent(t *testing.T, name, instructions string) string {
	t.Helper()

	var agentID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id,
			instructions, custom_env, custom_args, mcp_config, model
		)
		VALUES ($1, $2, 'role description', 'cloud', '{}'::jsonb, $3, 'private', 3, $4,
		        $5, '{"GITHUB_TOKEN":"tmpl-test-secret-value"}'::jsonb, '[]'::jsonb, NULL, 'claude-fable-5')
		RETURNING id
	`, testWorkspaceID, name, handlerTestRuntimeID(t), testUserID, instructions).Scan(&agentID); err != nil {
		t.Fatalf("failed to create team-template test agent: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})
	return agentID
}

func createTeamTemplateTestSkill(t *testing.T, name, content string) string {
	t.Helper()

	var skillID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO skill (workspace_id, name, description, content, config, created_by)
		VALUES ($1, $2, 'a transferable skill', $3, '{}'::jsonb, $4)
		RETURNING id
	`, testWorkspaceID, name, content, testUserID).Scan(&skillID); err != nil {
		t.Fatalf("failed to create team-template test skill: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO skill_file (skill_id, path, content)
		VALUES ($1, 'references/notes.md', 'supporting file body')
	`, skillID); err != nil {
		t.Fatalf("failed to create skill file: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM skill WHERE id = $1`, skillID)
	})
	return skillID
}

func bindSkill(t *testing.T, agentID, skillID string) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO agent_skill (agent_id, skill_id) VALUES ($1, $2) ON CONFLICT DO NOTHING
	`, agentID, skillID); err != nil {
		t.Fatalf("failed to bind skill: %v", err)
	}
}

func cleanupTeamTemplate(t *testing.T, name string) {
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM team_template WHERE workspace_id = $1 AND name = $2`, testWorkspaceID, name)
	})
}

func exportRequestBody(name string, mutate func(map[string]any)) map[string]any {
	body := map[string]any{"name": name}
	if mutate != nil {
		mutate(body)
	}
	return body
}

func doExport(t *testing.T, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	req := newRequest(http.MethodPost, "/api/team-templates/export", body)
	w := httptest.NewRecorder()
	testHandler.ExportTeamTemplate(w, req)
	return w
}

func TestExportTeamTemplateHappyPath(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	leadID := createTeamTemplateTestAgent(t, "TT Tech Lead",
		"You own technical direction. Clone https://github.com/acme/source-repo and follow its conventions.")
	engID := createTeamTemplateTestAgent(t, "TT Backend Engineer",
		"Implement well-specified Go changes.")
	packSkillID := createTeamTemplateTestSkill(t, "tt-api-design", "# API design rules")
	localSkillID := createTeamTemplateTestSkill(t, "tt-project-only", "# project-specific lore")
	bindSkill(t, leadID, packSkillID)
	bindSkill(t, leadID, localSkillID) // NOT in skill pack -> binding must drop

	// Squad with both agents plus a human member (humans never export).
	var squadID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id, instructions)
		VALUES ($1, 'TT Delivery Squad', 'squad desc', $2, $3, 'work as a team')
		RETURNING id
	`, testWorkspaceID, leadID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("failed to create squad: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID) })
	for _, m := range []struct{ typ, id, role string }{
		{"agent", leadID, "leader"},
		{"agent", engID, "engineer"},
		{"member", testUserID, "owner"},
	} {
		if _, err := testPool.Exec(context.Background(), `
			INSERT INTO squad_member (squad_id, member_type, member_id, role) VALUES ($1, $2, $3, $4)
		`, squadID, m.typ, m.id, m.role); err != nil {
			t.Fatalf("failed to add squad member: %v", err)
		}
	}

	var workflowID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO workflow (workspace_id, name, description, created_by_type, created_by_id)
		VALUES ($1, 'TT Build chain', '', 'member', $2)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&workflowID); err != nil {
		t.Fatalf("failed to create workflow: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM workflow WHERE id = $1`, workflowID) })
	for i, agentID := range []string{engID, leadID} {
		if _, err := testPool.Exec(context.Background(), `
			INSERT INTO workflow_step (workflow_id, step_order, agent_id, name, start_status, advance_status)
			VALUES ($1, $2, $3, $4, 'todo', 'in_review')
		`, workflowID, i+1, agentID, fmt.Sprintf("step-%d", i+1)); err != nil {
			t.Fatalf("failed to create workflow step: %v", err)
		}
	}

	var autopilotID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO autopilot (workspace_id, title, description, assignee_type, assignee_id, status, execution_mode, issue_title_template, created_by_type, created_by_id)
		VALUES ($1, 'TT Wave manager', 'tick', 'agent', $2, 'active', 'run_only', 'Tick {{date}}', 'member', $3)
		RETURNING id
	`, testWorkspaceID, leadID, testUserID).Scan(&autopilotID); err != nil {
		t.Fatalf("failed to create autopilot: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, autopilotID) })
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO autopilot_trigger (autopilot_id, kind, enabled, cron_expression, timezone, webhook_token)
		VALUES ($1, 'schedule', true, '0 * * * *', 'Europe/London', NULL),
		       ($1, 'webhook', true, NULL, NULL, 'whk_super_secret_token_value')
	`, autopilotID); err != nil {
		t.Fatalf("failed to create triggers: %v", err)
	}

	cleanupTeamTemplate(t, "TT Export Happy")
	w := doExport(t, exportRequestBody("TT Export Happy", func(b map[string]any) {
		b["agent_ids"] = []string{leadID, engID}
		b["squad_id"] = squadID
		b["workflow_ids"] = []string{workflowID}
		b["autopilot_ids"] = []string{autopilotID}
		b["skill_ids"] = []string{packSkillID}
		b["parameters"] = []map[string]any{
			{"key": "repo_url", "label": "Repository URL", "required": true},
		}
		b["substitutions"] = []map[string]any{
			{"find": "https://github.com/acme/source-repo", "param_key": "repo_url"},
		}
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp ExportTeamTemplateResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Version.Version != 1 {
		t.Errorf("expected version 1, got %d", resp.Version.Version)
	}

	var m teamtmpl.Manifest
	if err := json.Unmarshal(resp.Version.Manifest, &m); err != nil {
		t.Fatalf("failed to decode manifest: %v", err)
	}
	if err := m.Validate(); err != nil {
		t.Errorf("stored manifest does not validate: %v", err)
	}

	if m.Provenance.SourceWorkspaceID != testWorkspaceID {
		t.Errorf("provenance source workspace = %q, want %q", m.Provenance.SourceWorkspaceID, testWorkspaceID)
	}
	if m.Provenance.ExportedBy != testUserID {
		t.Errorf("provenance exported_by = %q, want %q", m.Provenance.ExportedBy, testUserID)
	}

	raw := string(resp.Version.Manifest)
	// Sanitisation: secrets and instance wiring must never appear.
	for _, banned := range []string{"tmpl-test-secret-value", "whk_super_secret_token_value", "custom_env", "runtime_id", "webhook_token"} {
		if strings.Contains(raw, banned) {
			t.Errorf("manifest contains banned content %q", banned)
		}
	}

	if len(m.Roles) != 2 {
		t.Fatalf("expected 2 roles, got %d", len(m.Roles))
	}
	var lead *teamtmpl.Role
	for i := range m.Roles {
		if m.Roles[i].Key == "tt-tech-lead" {
			lead = &m.Roles[i]
		}
	}
	if lead == nil {
		t.Fatalf("missing tt-tech-lead role; got %+v", m.Roles)
	}
	// Parameterisation replaced the source repo URL.
	if !strings.Contains(lead.Instructions, "{{param.repo_url}}") || strings.Contains(lead.Instructions, "acme/source-repo") {
		t.Errorf("substitution not applied to instructions: %q", lead.Instructions)
	}
	// Binding filter: only the explicit skill pack travels.
	if len(lead.Skills) != 1 || lead.Skills[0] != "tt-api-design" {
		t.Errorf("expected only the packed skill binding, got %v", lead.Skills)
	}
	if len(m.Skills) != 1 || m.Skills[0].Name != "tt-api-design" || len(m.Skills[0].Files) != 1 {
		t.Errorf("unexpected skill pack: %+v", m.Skills)
	}

	if m.Squad == nil || m.Squad.LeaderRole != "tt-tech-lead" {
		t.Fatalf("unexpected squad: %+v", m.Squad)
	}
	if len(m.Squad.Members) != 2 {
		t.Errorf("human squad members must be dropped; got %+v", m.Squad.Members)
	}

	if len(m.Workflows) != 1 || len(m.Workflows[0].Steps) != 2 {
		t.Fatalf("unexpected workflows: %+v", m.Workflows)
	}
	if m.Workflows[0].Steps[0].Role != "tt-backend-engineer" {
		t.Errorf("workflow step 1 should map to role key, got %q", m.Workflows[0].Steps[0].Role)
	}

	if len(m.Autopilots) != 1 {
		t.Fatalf("unexpected autopilots: %+v", m.Autopilots)
	}
	if m.Autopilots[0].AssigneeRole != "tt-tech-lead" {
		t.Errorf("autopilot assignee should map to role key, got %q", m.Autopilots[0].AssigneeRole)
	}
	if len(m.Autopilots[0].Triggers) != 2 {
		t.Errorf("expected 2 triggers, got %+v", m.Autopilots[0].Triggers)
	}

	// Appending a second version via template_id.
	w2 := doExport(t, exportRequestBody("", func(b map[string]any) {
		b["template_id"] = resp.Template.ID
		b["agent_ids"] = []string{leadID}
		b["notes"] = "v2: lead only"
	}))
	if w2.Code != http.StatusCreated {
		t.Fatalf("expected 201 for v2, got %d: %s", w2.Code, w2.Body.String())
	}
	var resp2 ExportTeamTemplateResponse
	if err := json.Unmarshal(w2.Body.Bytes(), &resp2); err != nil {
		t.Fatal(err)
	}
	if resp2.Version.Version != 2 || resp2.Template.ID != resp.Template.ID {
		t.Errorf("expected version 2 on same template, got v%d on %s", resp2.Version.Version, resp2.Template.ID)
	}

	// Read the stored version back through the version endpoint.
	req := newRequest(http.MethodGet, "/api/team-templates/"+resp.Template.ID+"/versions/1", nil)
	req = withURLParam(req, "id", resp.Template.ID)
	req = withURLParam(req, "version", "1")
	w3 := httptest.NewRecorder()
	testHandler.GetTeamTemplateVersion(w3, req)
	if w3.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w3.Code, w3.Body.String())
	}
	if !strings.Contains(w3.Body.String(), "tt-tech-lead") {
		t.Errorf("stored manifest missing role key")
	}
}

func TestExportTeamTemplateLintBlocksAutonomyLanguage(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createTeamTemplateTestAgent(t, "TT Autonomous Lead",
		"The team has standing authorisation to act in autonomous mode without per-issue human sign-off (ADA-1).")

	cleanupTeamTemplate(t, "TT Autonomy Block")
	w := doExport(t, exportRequestBody("TT Autonomy Block", func(b map[string]any) {
		b["agent_ids"] = []string{agentID}
	}))
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
	}
	var resp exportLintFailureResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode lint response: %v", err)
	}
	if len(resp.Findings) == 0 {
		t.Fatal("expected lint findings")
	}
	for _, f := range resp.Findings {
		if f.Rule != "autonomy_language" {
			t.Errorf("unexpected rule %q", f.Rule)
		}
	}

	// Nothing must be persisted on a blocked export.
	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM team_template WHERE workspace_id = $1 AND name = 'TT Autonomy Block'`,
		testWorkspaceID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("blocked export persisted a template")
	}
}

func TestExportTeamTemplateLintBlocksBlockedSkill(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createTeamTemplateTestAgent(t, "TT Clean Lead", "Lead the work.")
	// The ADA-1 autonomy grant itself must never export, whatever it says.
	skillID := createTeamTemplateTestSkill(t, "ada-house-rules", "perfectly innocent-looking content")

	cleanupTeamTemplate(t, "TT Blocked Skill")
	w := doExport(t, exportRequestBody("TT Blocked Skill", func(b map[string]any) {
		b["agent_ids"] = []string{agentID}
		b["skill_ids"] = []string{skillID}
	}))
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "blocked_skill") {
		t.Errorf("expected blocked_skill finding: %s", w.Body.String())
	}
}

func TestExportTeamTemplateRejectsDanglingReferences(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	inID := createTeamTemplateTestAgent(t, "TT Included Agent", "Included.")
	outID := createTeamTemplateTestAgent(t, "TT Excluded Agent", "Excluded.")

	var workflowID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO workflow (workspace_id, name, description, created_by_type, created_by_id)
		VALUES ($1, 'TT Dangling chain', '', 'member', $2)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&workflowID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM workflow WHERE id = $1`, workflowID) })
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO workflow_step (workflow_id, step_order, agent_id, name, start_status, advance_status)
		VALUES ($1, 1, $2, 'step', 'todo', 'in_review')
	`, workflowID, outID); err != nil {
		t.Fatal(err)
	}

	cleanupTeamTemplate(t, "TT Dangling")
	w := doExport(t, exportRequestBody("TT Dangling", func(b map[string]any) {
		b["agent_ids"] = []string{inID}
		b["workflow_ids"] = []string{workflowID}
	}))
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "outside agent_ids") {
		t.Errorf("expected dangling-reference error: %s", w.Body.String())
	}
}

func TestTeamTemplateEndpointsRefuseAgentActors(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createTeamTemplateTestAgent(t, "TT Sneaky Agent", "Try to export the team.")
	taskID := createHandlerTestTaskForAgent(t, agentID)

	body := exportRequestBody("TT Agent Denied", func(b map[string]any) {
		b["agent_ids"] = []string{agentID}
	})
	req := newRequest(http.MethodPost, "/api/team-templates/export", body)
	req.Header.Set("X-Agent-ID", agentID)
	req.Header.Set("X-Task-ID", taskID)
	w := httptest.NewRecorder()
	testHandler.ExportTeamTemplate(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for agent actor, got %d: %s", w.Code, w.Body.String())
	}

	req = newRequest(http.MethodGet, "/api/team-templates", nil)
	req.Header.Set("X-Agent-ID", agentID)
	req.Header.Set("X-Task-ID", taskID)
	w = httptest.NewRecorder()
	testHandler.ListTeamTemplates(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for agent actor on list, got %d: %s", w.Code, w.Body.String())
	}
}

func TestExportTeamTemplateValidation(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	// Missing agent_ids.
	w := doExport(t, exportRequestBody("TT No Agents", nil))
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing agent_ids, got %d", w.Code)
	}

	// Missing name and template_id.
	agentID := createTeamTemplateTestAgent(t, "TT Validation Agent", "Work.")
	w = doExport(t, exportRequestBody("", func(b map[string]any) {
		b["agent_ids"] = []string{agentID}
	}))
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing name, got %d", w.Code)
	}

	// Undeclared substitution parameter.
	cleanupTeamTemplate(t, "TT Bad Sub")
	w = doExport(t, exportRequestBody("TT Bad Sub", func(b map[string]any) {
		b["agent_ids"] = []string{agentID}
		b["substitutions"] = []map[string]any{{"find": "x", "param_key": "ghost"}}
	}))
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for undeclared substitution param, got %d: %s", w.Code, w.Body.String())
	}

	// Duplicate template name -> 409.
	cleanupTeamTemplate(t, "TT Dup Name")
	first := doExport(t, exportRequestBody("TT Dup Name", func(b map[string]any) {
		b["agent_ids"] = []string{agentID}
	}))
	if first.Code != http.StatusCreated {
		t.Fatalf("setup export failed: %d %s", first.Code, first.Body.String())
	}
	dup := doExport(t, exportRequestBody("TT Dup Name", func(b map[string]any) {
		b["agent_ids"] = []string{agentID}
	}))
	if dup.Code != http.StatusConflict {
		t.Errorf("expected 409 for duplicate name, got %d", dup.Code)
	}
}
