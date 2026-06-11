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
	assertJSONEqual(t, w.Body.Bytes(), `{"updated":0}`)

	var status string
	if err := testPool.QueryRow(context.Background(), `SELECT status FROM issue WHERE id = $1`, issueID).Scan(&status); err != nil {
		t.Fatalf("load issue status: %v", err)
	}
	if status != "in_review" {
		t.Fatalf("batch update bypassed gate, status = %q", status)
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
		VALUES ($1, $2, $3, 'medium', 'member', $4, $5, nextval('issue_number_seq'))
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
