package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestIssueQualityGateOptInAndIndependentApproval(t *testing.T) {
	ctx := context.Background()
	implementer := createHandlerTestAgent(t, "gate-impl "+time.Now().Format(time.RFC3339Nano), nil)
	reviewer := createHandlerTestAgent(t, "gate-reviewer "+time.Now().Format(time.RFC3339Nano), nil)

	projectID := createQualityGateProject(t, `{
		"gates": [{
			"key": "code_review",
			"name": "Code review",
			"order": 1,
			"required_actor_type": "agent",
			"required_role": "Code Reviewer",
			"independent": true,
			"transition": {"from": "in_review", "to": "done"}
		}]
	}`)
	issueID := createQualityGateIssue(t, projectID, "in_review")
	seedCompletedTask(t, issueID, implementer)

	// Same-agent approval is rejected because the configured gate requires an
	// independent reviewer.
	taskID := createHandlerTestTaskForAgentOnIssue(t, implementer, issueID)
	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/issues/"+issueID, map[string]any{"status": "done"})
	req = withURLParam(req, "id", issueID)
	req.Header.Set("X-Agent-ID", implementer)
	req.Header.Set("X-Task-ID", taskID)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("same-agent approval: expected 409, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Code Reviewer") || !strings.Contains(w.Body.String(), "independent") {
		t.Fatalf("same-agent rejection should explain the required independent gate, got %s", w.Body.String())
	}

	// A different agent can pass the configured transition gate.
	reviewerTask := createHandlerTestTaskForAgentOnIssue(t, reviewer, issueID)
	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/issues/"+issueID, map[string]any{"status": "done"})
	req = withURLParam(req, "id", issueID)
	req.Header.Set("X-Agent-ID", reviewer)
	req.Header.Set("X-Task-ID", reviewerTask)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("independent approval: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var count int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM issue_quality_gate_event
		WHERE issue_id = $1 AND gate_key = 'code_review' AND actor_type = 'agent' AND actor_id = $2
	`, issueID, reviewer).Scan(&count); err != nil {
		t.Fatalf("count quality gate event: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected one recorded gate event for reviewer, got %d", count)
	}
}

func TestIssueQualityGateNoConfigPreservesExistingBehaviour(t *testing.T) {
	projectID := createQualityGateProject(t, `{}`)
	issueID := createQualityGateIssue(t, projectID, "in_review")

	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/issues/"+issueID, map[string]any{"status": "done"})
	req = withURLParam(req, "id", issueID)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ungated project update: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestIssueQualityGateRejectsMissingRequiredActor(t *testing.T) {
	projectID := createQualityGateProject(t, `{
		"gates": [{
			"key": "code_review",
			"name": "Code review",
			"order": 1,
			"required_actor_type": "agent",
			"required_role": "Code Reviewer",
			"transition": {"from": "in_review", "to": "done"}
		}]
	}`)
	issueID := createQualityGateIssue(t, projectID, "in_review")

	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/issues/"+issueID, map[string]any{"status": "done"})
	req = withURLParam(req, "id", issueID)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("member approval: expected 409, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Code Reviewer") || !strings.Contains(w.Body.String(), "agent") {
		t.Fatalf("missing gate rejection should explain required actor, got %s", w.Body.String())
	}
}

func TestIssueQualityGateOwnerOverrideRequiresReason(t *testing.T) {
	projectID := createQualityGateProject(t, `{
		"gates": [{
			"key": "code_review",
			"name": "Code review",
			"order": 1,
			"required_actor_type": "agent",
			"required_role": "Code Reviewer",
			"transition": {"from": "in_review", "to": "done"}
		}]
	}`)
	issueID := createQualityGateIssue(t, projectID, "in_review")

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/"+issueID+"/quality-gates/override", map[string]any{"status": "done"})
	req = withURLParam(req, "id", issueID)
	testHandler.OverrideIssueQualityGate(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("override without reason: expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "reason") {
		t.Fatalf("missing reason error should be actionable, got %s", w.Body.String())
	}
}

func TestIssueQualityGateOwnerOverrideRecordsAuditEvent(t *testing.T) {
	projectID := createQualityGateProject(t, `{
		"gates": [{
			"key": "code_review",
			"name": "Code review",
			"order": 1,
			"required_actor_type": "agent",
			"required_role": "Code Reviewer",
			"transition": {"from": "in_review", "to": "done"}
		}]
	}`)
	issueID := createQualityGateIssue(t, projectID, "in_review")

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/"+issueID+"/quality-gates/override", map[string]any{
		"status": "done",
		"reason": "release is blocked and owner accepted the risk",
	})
	req = withURLParam(req, "id", issueID)
	testHandler.OverrideIssueQualityGate(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("owner override: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Issue struct {
			Status string `json:"status"`
		} `json:"issue"`
		Override struct {
			Reason       string `json:"reason"`
			SkippedGates []struct {
				Key       string `json:"key"`
				Name      string `json:"name"`
				NextActor string `json:"next_actor"`
			} `json:"skipped_gates"`
		} `json:"override"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode override response: %v", err)
	}
	if resp.Issue.Status != "done" || resp.Override.Reason == "" || len(resp.Override.SkippedGates) != 1 {
		t.Fatalf("override response should include issue and skipped gate, got %+v", resp)
	}
	if resp.Override.SkippedGates[0].Key != "code_review" || resp.Override.SkippedGates[0].NextActor != "Code Reviewer" {
		t.Fatalf("skipped gate should name reviewer gate, got %+v", resp.Override.SkippedGates[0])
	}

	var gateEvents int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM issue_quality_gate_event
		WHERE issue_id = $1 AND gate_key = 'code_review' AND actor_type = 'member' AND actor_id = $2
	`, issueID, testUserID).Scan(&gateEvents); err != nil {
		t.Fatalf("count override gate event: %v", err)
	}
	if gateEvents != 1 {
		t.Fatalf("expected one override gate event, got %d", gateEvents)
	}

	var details string
	if err := testPool.QueryRow(context.Background(), `
		SELECT details::text
		FROM activity_log
		WHERE issue_id = $1 AND action = 'quality_gate_override'
		ORDER BY created_at DESC
		LIMIT 1
	`, issueID).Scan(&details); err != nil {
		t.Fatalf("load override activity: %v", err)
	}
	for _, want := range []string{"release is blocked", "code_review", "Code review", "in_review", "done"} {
		if !strings.Contains(details, want) {
			t.Fatalf("override activity details missing %q: %s", want, details)
		}
	}
}

func TestIssueQualityGateOverrideRejectsUnauthorisedActors(t *testing.T) {
	projectID := createQualityGateProject(t, `{
		"gates": [{
			"key": "code_review",
			"name": "Code review",
			"order": 1,
			"required_actor_type": "agent",
			"required_role": "Code Reviewer",
			"transition": {"from": "in_review", "to": "done"}
		}]
	}`)
	memberID := createQualityGateMember(t, "member")

	t.Run("plain member", func(t *testing.T) {
		issueID := createQualityGateIssue(t, projectID, "in_review")
		w := httptest.NewRecorder()
		req := newRequestAs(memberID, "POST", "/api/issues/"+issueID+"/quality-gates/override", map[string]any{
			"status": "done",
			"reason": "trying without owner role",
		})
		req = withURLParam(req, "id", issueID)
		testHandler.OverrideIssueQualityGate(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("member override: expected 403, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("agent task actor", func(t *testing.T) {
		issueID := createQualityGateIssue(t, projectID, "in_review")
		agentID := createHandlerTestAgent(t, "gate override agent "+time.Now().Format(time.RFC3339Nano), nil)
		taskID := createHandlerTestTaskForAgentOnIssue(t, agentID, issueID)
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/issues/"+issueID+"/quality-gates/override", map[string]any{
			"status": "done",
			"reason": "agent should not override",
		})
		req = withURLParam(req, "id", issueID)
		req.Header.Set("X-Agent-ID", agentID)
		req.Header.Set("X-Task-ID", taskID)
		testHandler.OverrideIssueQualityGate(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("agent override: expected 403, got %d: %s", w.Code, w.Body.String())
		}
	})
}

func TestIssueQualityGateBatchUpdateUsesSameEnforcement(t *testing.T) {
	projectID := createQualityGateProject(t, `{
		"gates": [{
			"key": "code_review",
			"name": "Code review",
			"order": 1,
			"required_actor_type": "agent",
			"required_role": "Code Reviewer",
			"transition": {"from": "in_review", "to": "done"}
		}]
	}`)
	issueID := createQualityGateIssue(t, projectID, "in_review")

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/batch-update", map[string]any{
		"issue_ids": []string{issueID},
		"updates":   map[string]any{"status": "done"},
	})
	testHandler.BatchUpdateIssues(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("batch update: expected stable 200 response, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Updated int `json:"updated"`
		Skipped []struct {
			IssueID string `json:"issue_id"`
			Reason  string `json:"reason"`
		} `json:"skipped"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode batch response: %v", err)
	}
	if resp.Updated != 0 || len(resp.Skipped) != 1 {
		t.Fatalf("expected one explained skip, got %s", w.Body.String())
	}
	if resp.Skipped[0].IssueID != issueID || !strings.Contains(resp.Skipped[0].Reason, "Code Reviewer") {
		t.Fatalf("batch skip should explain required reviewer, got %+v", resp.Skipped[0])
	}

	var status string
	if err := testPool.QueryRow(context.Background(), `SELECT status FROM issue WHERE id = $1`, issueID).Scan(&status); err != nil {
		t.Fatalf("load issue status: %v", err)
	}
	if status != "in_review" {
		t.Fatalf("batch update bypassed gate, status = %q", status)
	}
}

func TestIssueQualityGateRejectsTwoStepBypassIntoProtectedStatus(t *testing.T) {
	projectID := createQualityGateProject(t, `{
		"gates": [{
			"key": "code_review",
			"name": "Code review",
			"order": 1,
			"required_actor_type": "agent",
			"required_role": "Code Reviewer",
			"transition": {"from": "in_review", "to": "done"}
		}]
	}`)
	issueID := createQualityGateIssue(t, projectID, "in_review")

	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/issues/"+issueID, map[string]any{"status": "in_progress"})
	req = withURLParam(req, "id", issueID)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("setup move out of review: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/issues/"+issueID, map[string]any{"status": "done"})
	req = withURLParam(req, "id", issueID)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("bypass into done: expected 409, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Code Reviewer") || !strings.Contains(w.Body.String(), "done") {
		t.Fatalf("bypass rejection should explain protected gate, got %s", w.Body.String())
	}
}

func TestProjectQualityGateConfigRejectsInvalidShape(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects", map[string]any{
		"title":               "invalid gate project",
		"quality_gate_config": map[string]any{"gates": 42},
	})
	testHandler.CreateProject(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("create invalid gate config: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	projectID := createQualityGateProject(t, `{}`)
	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/projects/"+projectID, map[string]any{
		"quality_gate_config": []any{"not", "an", "object"},
	})
	req = withURLParam(req, "id", projectID)
	testHandler.UpdateProject(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("update invalid gate config: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetIssueQualityGatesReportsConfiguredState(t *testing.T) {
	projectID := createQualityGateProject(t, `{
		"gates": [{
			"key": "code_review",
			"name": "Code review",
			"order": 1,
			"required_actor_type": "agent",
			"required_role": "Code Reviewer",
			"transition": {"from": "in_review", "to": "done"}
		}]
	}`)
	issueID := createQualityGateIssue(t, projectID, "in_review")

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/issues/"+issueID+"/quality-gates", nil)
	req = withURLParam(req, "id", issueID)
	testHandler.GetIssueQualityGates(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("gate state: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Enabled bool `json:"enabled"`
		Gates   []struct {
			Key       string `json:"key"`
			Complete  bool   `json:"complete"`
			Blocked   bool   `json:"blocked"`
			Reason    string `json:"reason"`
			NextActor string `json:"next_actor"`
		} `json:"gates"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode gate state: %v", err)
	}
	if !resp.Enabled || len(resp.Gates) != 1 {
		t.Fatalf("expected one enabled gate, got %+v", resp)
	}
	if !resp.Gates[0].Blocked || !strings.Contains(resp.Gates[0].Reason, "Code Reviewer") {
		t.Fatalf("expected blocked gate state explaining next reviewer, got %+v", resp.Gates[0])
	}
}

func createQualityGateProject(t *testing.T, config string) string {
	t.Helper()
	var projectID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO project (workspace_id, title, description, status, priority, quality_gate_config)
		VALUES ($1, $2, '', 'in_progress', 'medium', $3::jsonb)
		RETURNING id
	`, testWorkspaceID, "gate project "+time.Now().Format(time.RFC3339Nano), config).Scan(&projectID); err != nil {
		t.Fatalf("create gate project: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID)
	})
	return projectID
}

func createQualityGateIssue(t *testing.T, projectID, status string) string {
	t.Helper()
	var issueID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, project_id, number)
		VALUES ($1, $2, $3, 'medium', 'member', $4, $5,
		        (SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1))
		RETURNING id
	`, testWorkspaceID, "gate issue "+time.Now().Format(time.RFC3339Nano), status, testUserID, projectID).Scan(&issueID); err != nil {
		t.Fatalf("create gate issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})
	return issueID
}

func seedCompletedTask(t *testing.T, issueID, agentID string) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at, completed_at)
		VALUES ($1, $2, $3, 'completed', 0, now() - interval '2 minutes', now() - interval '1 minute')
	`, agentID, handlerTestRuntimeID(t), issueID); err != nil {
		t.Fatalf("seed completed task: %v", err)
	}
}

func createQualityGateMember(t *testing.T, role string) string {
	t.Helper()
	var userID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO "user" (name, email)
		VALUES ($1, $2)
		RETURNING id
	`, "gate "+role+" "+time.Now().Format(time.RFC3339Nano), "gate-"+role+"-"+time.Now().Format("20060102150405.000000000")+"@example.com").Scan(&userID); err != nil {
		t.Fatalf("create gate user: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, $3)
	`, testWorkspaceID, userID, role); err != nil {
		t.Fatalf("create gate member: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, userID)
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
	})
	return userID
}
