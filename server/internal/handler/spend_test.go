package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGetSpendReport_IssueTaskRowsExposeCostAndMissingUsage(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var runtimeID, agentID string
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM agent_runtime WHERE workspace_id = $1 LIMIT 1
	`, testWorkspaceID).Scan(&runtimeID); err != nil {
		t.Fatalf("fetch runtime: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1
	`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("fetch agent: %v", err)
	}

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_id, creator_type, number)
		VALUES (
			$1, 'spend issue', $2, 'member',
			(SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1)
		)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("insert issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID) })

	usageAt := time.Now().UTC().Add(-2 * time.Hour)
	var knownTaskID, missingTaskID string
	for i, dest := range []*string{&knownTaskID, &missingTaskID} {
		if err := testPool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (agent_id, issue_id, runtime_id, status, started_at, completed_at, created_at, session_id)
			VALUES ($1, $2, $3, 'completed', $4, $5, $4, $6)
			RETURNING id
		`, agentID, issueID, runtimeID, usageAt.Add(time.Duration(i)*time.Minute), usageAt.Add(time.Duration(i+1)*time.Minute), "session-spend").Scan(dest); err != nil {
			t.Fatalf("insert task %d: %v", i, err)
		}
		t.Cleanup(func(id string) func() {
			return func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, id) }
		}(*dest))
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO task_usage (task_id, provider, model, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, created_at)
		VALUES ($1, 'anthropic', 'claude-sonnet-4.6', 1000000, 2000000, 500000, 100000, $2)
	`, knownTaskID, usageAt); err != nil {
		t.Fatalf("insert task_usage: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/spend?group_by=task&issue_id="+issueID, nil)
	testHandler.GetSpendReport(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var report SpendReportResponse
	if err := json.NewDecoder(w.Body).Decode(&report); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if report.GroupBy != "task" {
		t.Fatalf("group_by = %q, want task", report.GroupBy)
	}
	if report.TaskCount != 2 || report.MissingUsageTaskCount != 1 || report.UsageComplete {
		t.Fatalf("summary task/missing/complete = %d/%d/%v, want 2/1/false", report.TaskCount, report.MissingUsageTaskCount, report.UsageComplete)
	}
	if report.EstimatedCostUSD == nil || *report.EstimatedCostUSD <= 0 {
		t.Fatalf("expected positive estimated cost, got %#v", report.EstimatedCostUSD)
	}
	var sawKnown, sawMissing bool
	for _, row := range report.Rows {
		if row.TaskID != nil && *row.TaskID == knownTaskID {
			sawKnown = true
			if row.Provider != "anthropic" || row.Model != "claude-sonnet-4.6" {
				t.Fatalf("known row provider/model = %s/%s", row.Provider, row.Model)
			}
			if row.SessionID == nil || *row.SessionID != "session-spend" {
				t.Fatalf("known row session_id = %#v", row.SessionID)
			}
			if row.EstimatedCostUSD == nil || !row.CostComplete || !row.UsageComplete {
				t.Fatalf("known row cost/complete = %#v/%v/%v", row.EstimatedCostUSD, row.CostComplete, row.UsageComplete)
			}
		}
		if row.TaskID != nil && *row.TaskID == missingTaskID {
			sawMissing = true
			if row.UsageComplete || row.CostComplete || row.MissingUsageTaskCount != 1 {
				t.Fatalf("missing row complete/missing = %v/%v/%d", row.UsageComplete, row.CostComplete, row.MissingUsageTaskCount)
			}
		}
	}
	if !sawKnown || !sawMissing {
		t.Fatalf("expected rows for known=%s and missing=%s, got %+v", knownTaskID, missingTaskID, report.Rows)
	}
}

func TestGetSpendReport_DayFilterAndAgentGrouping(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var runtimeID, agentID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent_runtime WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&runtimeID); err != nil {
		t.Fatalf("fetch runtime: %v", err)
	}
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("fetch agent: %v", err)
	}

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_id, creator_type, number)
		VALUES ($1, 'spend day issue', $2, 'member', (SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1))
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("insert issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID) })

	usageAt := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, issue_id, runtime_id, status, started_at, completed_at, created_at)
		VALUES ($1, $2, $3, 'completed', $4, $4, $4)
		RETURNING id
	`, agentID, issueID, runtimeID, usageAt).Scan(&taskID); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })
	if _, err := testPool.Exec(ctx, `
		INSERT INTO task_usage (task_id, provider, model, input_tokens, output_tokens, created_at)
		VALUES ($1, 'openai', 'gpt-5.4-mini', 1000, 2000, $2)
	`, taskID, usageAt); err != nil {
		t.Fatalf("insert usage: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/spend?group_by=day&agent_id="+agentID+"&from=2026-06-10&to=2026-06-10&tz=UTC", nil)
	testHandler.GetSpendReport(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var report SpendReportResponse
	if err := json.NewDecoder(w.Body).Decode(&report); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(report.Rows) != 1 {
		t.Fatalf("expected one day row, got %+v", report.Rows)
	}
	if report.Rows[0].Date == nil || *report.Rows[0].Date != "2026-06-10" {
		t.Fatalf("date = %#v, want 2026-06-10", report.Rows[0].Date)
	}
	if report.TaskCount != 1 || report.TotalInputTokens != 1000 || report.TotalOutputTokens != 2000 {
		t.Fatalf("summary = tasks %d input %d output %d", report.TaskCount, report.TotalInputTokens, report.TotalOutputTokens)
	}
}

func TestGetSpendReport_IssueGroupingAggregatesKnownUsage(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var runtimeID, agentID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent_runtime WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&runtimeID); err != nil {
		t.Fatalf("fetch runtime: %v", err)
	}
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("fetch agent: %v", err)
	}

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_id, creator_type, number)
		VALUES ($1, 'spend grouped issue', $2, 'member', (SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1))
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("insert issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID) })

	usageAt := time.Date(2026, 6, 11, 9, 0, 0, 0, time.UTC)
	for i := 0; i < 2; i++ {
		var taskID string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (agent_id, issue_id, runtime_id, status, started_at, completed_at, created_at)
			VALUES ($1, $2, $3, 'completed', $4, $4, $4)
			RETURNING id
		`, agentID, issueID, runtimeID, usageAt.Add(time.Duration(i)*time.Minute)).Scan(&taskID); err != nil {
			t.Fatalf("insert task %d: %v", i, err)
		}
		t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })
		if _, err := testPool.Exec(ctx, `
			INSERT INTO task_usage (task_id, provider, model, input_tokens, output_tokens, created_at)
			VALUES ($1, 'openai', 'gpt-5.4-mini', $2, $3, $4)
		`, taskID, int64(1000*(i+1)), int64(2000*(i+1)), usageAt.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatalf("insert usage %d: %v", i, err)
		}
	}

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/spend?group_by=issue&issue_id="+issueID, nil)
	testHandler.GetSpendReport(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var report SpendReportResponse
	if err := json.NewDecoder(w.Body).Decode(&report); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if report.GroupBy != "issue" {
		t.Fatalf("group_by = %q, want issue", report.GroupBy)
	}
	if len(report.Rows) != 1 {
		t.Fatalf("expected one issue row, got %+v", report.Rows)
	}
	row := report.Rows[0]
	if row.IssueID == nil || *row.IssueID != issueID {
		t.Fatalf("issue_id = %#v, want %s", row.IssueID, issueID)
	}
	if row.TaskCount != 2 || row.MissingUsageTaskCount != 0 || !row.UsageComplete {
		t.Fatalf("row task/missing/complete = %d/%d/%v, want 2/0/true", row.TaskCount, row.MissingUsageTaskCount, row.UsageComplete)
	}
	if row.InputTokens != 3000 || row.OutputTokens != 6000 {
		t.Fatalf("row tokens = %d/%d, want 3000/6000", row.InputTokens, row.OutputTokens)
	}
	if report.TaskCount != 2 || report.TotalInputTokens != 3000 || report.TotalOutputTokens != 6000 || !report.UsageComplete {
		t.Fatalf("summary = tasks %d input %d output %d complete %v", report.TaskCount, report.TotalInputTokens, report.TotalOutputTokens, report.UsageComplete)
	}
}

func TestGetSpendReport_AgentGroupingExposesAgentUsage(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var runtimeID, agentID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent_runtime WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&runtimeID); err != nil {
		t.Fatalf("fetch runtime: %v", err)
	}
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("fetch agent: %v", err)
	}

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_id, creator_type, number)
		VALUES ($1, 'spend agent issue', $2, 'member', (SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1))
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("insert issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID) })

	usageAt := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, issue_id, runtime_id, status, started_at, completed_at, created_at)
		VALUES ($1, $2, $3, 'completed', $4, $4, $4)
		RETURNING id
	`, agentID, issueID, runtimeID, usageAt).Scan(&taskID); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })
	if _, err := testPool.Exec(ctx, `
		INSERT INTO task_usage (task_id, provider, model, input_tokens, output_tokens, created_at)
		VALUES ($1, 'openai', 'gpt-5.4-mini', 4000, 5000, $2)
	`, taskID, usageAt); err != nil {
		t.Fatalf("insert usage: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/spend?group_by=agent&agent_id="+agentID+"&issue_id="+issueID, nil)
	testHandler.GetSpendReport(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var report SpendReportResponse
	if err := json.NewDecoder(w.Body).Decode(&report); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if report.GroupBy != "agent" {
		t.Fatalf("group_by = %q, want agent", report.GroupBy)
	}
	if len(report.Rows) != 1 {
		t.Fatalf("expected one agent row, got %+v", report.Rows)
	}
	row := report.Rows[0]
	if row.AgentID == nil || *row.AgentID != agentID {
		t.Fatalf("agent_id = %#v, want %s", row.AgentID, agentID)
	}
	if row.TaskCount != 1 || row.MissingUsageTaskCount != 0 || !row.UsageComplete {
		t.Fatalf("row task/missing/complete = %d/%d/%v, want 1/0/true", row.TaskCount, row.MissingUsageTaskCount, row.UsageComplete)
	}
	if row.InputTokens != 4000 || row.OutputTokens != 5000 {
		t.Fatalf("row tokens = %d/%d, want 4000/5000", row.InputTokens, row.OutputTokens)
	}
	if report.TaskCount != 1 || report.MissingUsageTaskCount != 0 || !report.UsageComplete {
		t.Fatalf("summary task/missing/complete = %d/%d/%v, want 1/0/true", report.TaskCount, report.MissingUsageTaskCount, report.UsageComplete)
	}
}
