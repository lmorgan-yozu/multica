package handler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestIssueLoopBrakeDetectorTriggersWithoutMemberProgress(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("handler test fixture unavailable")
	}
	ctx := context.Background()
	issue := createLoopBrakeTestIssue(t, "loop brake threshold")
	task := seedLoopBrakeTask(t, issue.ID, "completed", 1_000, 500)
	seedLoopBrakeTask(t, issue.ID, "completed", 1_100, 600)
	seedLoopBrakeTask(t, issue.ID, "queued", 0, 0)
	seedLoopBrakeTask(t, issue.ID, "running", 0, 0)
	seedLoopBrakeComment(t, issue.ID, "agent", parseUUID(testUserID), "agent output is loop exhaust")
	seedLoopBrakeComment(t, issue.ID, "system", parseUUID(testWorkspaceID), "system output is loop exhaust")
	upsertLoopBrakeTestConfig(t, 2, 2_000)

	brake, err := testHandler.TaskService.EvaluateIssueLoopBrakeForTask(ctx, task)
	if err != nil {
		t.Fatalf("EvaluateIssueLoopBrakeForTask: %v", err)
	}
	if brake == nil || brake.State != "active" {
		t.Fatalf("expected active brake, got %#v", brake)
	}

	evidence := brake.Evidence
	if got := int(evidence["run_count"].(float64)); got != 4 {
		t.Fatalf("run_count = %d, want 4", got)
	}
	if got := int(evidence["total_tokens"].(float64)); got != 3200 {
		t.Fatalf("total_tokens = %d, want 3200", got)
	}
	if got := int(evidence["existing_queued_tasks"].(float64)); got != 1 {
		t.Fatalf("existing_queued_tasks = %d, want 1", got)
	}
	if got := int(evidence["existing_running_tasks"].(float64)); got != 1 {
		t.Fatalf("existing_running_tasks = %d, want 1", got)
	}

	// Already-running tasks may finish after the brake is active. Re-evaluation
	// must not overwrite the original trigger evidence on the live brake row.
	laterTask := seedLoopBrakeTask(t, issue.ID, "completed", 9_000, 9_000)
	laterBrake, err := testHandler.TaskService.EvaluateIssueLoopBrakeForTask(ctx, laterTask)
	if err != nil {
		t.Fatalf("EvaluateIssueLoopBrakeForTask with active brake: %v", err)
	}
	if laterBrake == nil || laterBrake.TriggeredByTaskID == nil || *laterBrake.TriggeredByTaskID != *brake.TriggeredByTaskID {
		t.Fatalf("expected existing active brake to be returned without re-triggering, got %#v", laterBrake)
	}
	if got := int(laterBrake.Evidence["total_tokens"].(float64)); got != 3200 {
		t.Fatalf("active brake evidence was overwritten: total_tokens = %d, want 3200", got)
	}
}

func TestIssueLoopBrakeDetectorSkipsWhenMemberCommentShowsProgress(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("handler test fixture unavailable")
	}
	ctx := context.Background()
	issue := createLoopBrakeTestIssue(t, "loop brake member progress")
	task := seedLoopBrakeTask(t, issue.ID, "completed", 1_000, 500)
	seedLoopBrakeTask(t, issue.ID, "completed", 1_100, 600)
	seedLoopBrakeComment(t, issue.ID, "member", parseUUID(testUserID), "human progress signal")
	upsertLoopBrakeTestConfig(t, 2, 2_000)

	brake, err := testHandler.TaskService.EvaluateIssueLoopBrakeForTask(ctx, task)
	if err != nil {
		t.Fatalf("EvaluateIssueLoopBrakeForTask: %v", err)
	}
	if brake != nil {
		t.Fatalf("expected no brake when member progress exists, got %#v", brake)
	}
}

func TestIssueLoopBrakeDetectorSkipsWhenStatusChangeShowsProgress(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("handler test fixture unavailable")
	}
	ctx := context.Background()
	issue := createLoopBrakeTestIssue(t, "loop brake status progress")
	task := seedLoopBrakeTask(t, issue.ID, "completed", 1_000, 500)
	seedLoopBrakeTask(t, issue.ID, "completed", 1_100, 600)
	seedLoopBrakeStatusChange(t, issue.ID)
	upsertLoopBrakeTestConfig(t, 2, 2_000)

	brake, err := testHandler.TaskService.EvaluateIssueLoopBrakeForTask(ctx, task)
	if err != nil {
		t.Fatalf("EvaluateIssueLoopBrakeForTask: %v", err)
	}
	if brake != nil {
		t.Fatalf("expected no brake when status progress exists, got %#v", brake)
	}
}

func TestIssueLoopBrakeBlocksOnlyPausedIssueAndClearUnpauses(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("handler test fixture unavailable")
	}
	ctx := context.Background()
	paused := createLoopBrakeTestIssue(t, "loop brake paused issue")
	other := createLoopBrakeTestIssue(t, "loop brake other issue")
	trigger := seedLoopBrakeTask(t, paused.ID, "completed", 1_000, 500)
	seedLoopBrakeTask(t, paused.ID, "completed", 1_100, 600)
	upsertLoopBrakeTestConfig(t, 2, 2_000)

	if _, err := testHandler.TaskService.EvaluateIssueLoopBrakeForTask(ctx, trigger); err != nil {
		t.Fatalf("EvaluateIssueLoopBrakeForTask: %v", err)
	}
	if _, err := testHandler.TaskService.EnqueueTaskForIssue(ctx, paused); !errors.Is(err, service.ErrIssueBrakeActive) {
		t.Fatalf("paused issue enqueue error = %v, want ErrIssueBrakeActive", err)
	}
	if _, err := testHandler.TaskService.EnqueueTaskForMention(ctx, paused, paused.AssigneeID, pgtype.UUID{}); !errors.Is(err, service.ErrIssueBrakeActive) {
		t.Fatalf("paused issue mention enqueue error = %v, want ErrIssueBrakeActive", err)
	}
	if _, err := testHandler.TaskService.EnqueueTaskForIssue(ctx, other); err != nil {
		t.Fatalf("other issue should remain claimable: %v", err)
	}

	cleared, err := testHandler.TaskService.ClearIssueLoopBrake(ctx, paused.ID, "member", parseUUID(testUserID), "reviewed")
	if err != nil {
		t.Fatalf("ClearIssueLoopBrake: %v", err)
	}
	if cleared == nil || cleared.State != "cleared" {
		t.Fatalf("expected cleared brake, got %#v", cleared)
	}
	if _, err := testHandler.TaskService.EnqueueTaskForIssue(ctx, paused); err != nil {
		t.Fatalf("cleared issue should enqueue again: %v", err)
	}
}

func upsertLoopBrakeTestConfig(t *testing.T, minRuns int32, minTokens int64) {
	t.Helper()
	if _, err := testHandler.TaskService.UpsertIssueLoopBrakeConfig(context.Background(), service.UpsertIssueLoopBrakeConfigParams{
		WorkspaceID:    parseUUID(testWorkspaceID),
		Enabled:        true,
		WindowMinutes:  60,
		MinRunCount:    minRuns,
		MinTotalTokens: minTokens,
	}); err != nil {
		t.Fatalf("upsert loop brake config: %v", err)
	}
}

func createLoopBrakeTestIssue(t *testing.T, title string) db.Issue {
	t.Helper()
	ctx := context.Background()
	var agentID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("load test agent: %v", err)
	}
	var issueID pgtype.UUID
	if err := testPool.QueryRow(ctx, `
INSERT INTO issue (workspace_id, title, status, priority, assignee_type, assignee_id, creator_type, creator_id)
VALUES ($1, $2, 'in_progress', 'medium', 'agent', $3, 'member', $4)
RETURNING id`, parseUUID(testWorkspaceID), title, parseUUID(agentID), parseUUID(testUserID)).Scan(&issueID); err != nil {
		t.Fatalf("create loop brake test issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})
	issue, err := testHandler.Queries.GetIssue(ctx, issueID)
	if err != nil {
		t.Fatalf("load loop brake test issue: %v", err)
	}
	return issue
}

func seedLoopBrakeTask(t *testing.T, issueID pgtype.UUID, status string, inputTokens, outputTokens int64) db.AgentTaskQueue {
	t.Helper()
	ctx := context.Background()
	var agentID, runtimeID pgtype.UUID
	if err := testPool.QueryRow(ctx, `
SELECT id, runtime_id FROM agent
WHERE workspace_id = $1
ORDER BY created_at ASC
LIMIT 1`, testWorkspaceID).Scan(&agentID, &runtimeID); err != nil {
		t.Fatalf("load test agent/runtime: %v", err)
	}
	now := time.Now().UTC()
	var taskID pgtype.UUID
	if err := testPool.QueryRow(ctx, `
INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at, completed_at, created_at)
VALUES ($1, $2, $3, $4, 0, $5, $6, $7)
RETURNING id`, agentID, runtimeID, issueID, status, now.Add(-2*time.Minute), nullableTime(status, now.Add(-time.Minute)), now.Add(-3*time.Minute)).Scan(&taskID); err != nil {
		t.Fatalf("seed loop brake task: %v", err)
	}
	if inputTokens+outputTokens > 0 {
		if _, err := testPool.Exec(ctx, `
INSERT INTO task_usage (task_id, provider, model, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, created_at)
VALUES ($1, 'test', 'loop-brake', $2, $3, 0, 0, $4)`, taskID, inputTokens, outputTokens, now.Add(-30*time.Second)); err != nil {
			t.Fatalf("seed loop brake task usage: %v", err)
		}
	}
	task, err := testHandler.Queries.GetAgentTask(ctx, taskID)
	if err != nil {
		t.Fatalf("load loop brake test task: %v", err)
	}
	return task
}

func seedLoopBrakeComment(t *testing.T, issueID pgtype.UUID, authorType string, authorID pgtype.UUID, content string) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(), `
INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type)
VALUES ($1, $2, $3, $4, $5, 'comment')`, issueID, parseUUID(testWorkspaceID), authorType, authorID, content); err != nil {
		t.Fatalf("seed loop brake comment: %v", err)
	}
}

func seedLoopBrakeStatusChange(t *testing.T, issueID pgtype.UUID) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(), `
INSERT INTO activity_log (workspace_id, issue_id, actor_type, actor_id, action, details)
VALUES ($1, $2, 'member', $3, 'status_changed', '{"from":"todo","to":"in_progress"}'::jsonb)`,
		parseUUID(testWorkspaceID), issueID, parseUUID(testUserID)); err != nil {
		t.Fatalf("seed loop brake status change: %v", err)
	}
}

func nullableTime(status string, ts time.Time) any {
	if status == "completed" || status == "failed" || status == "cancelled" {
		return ts
	}
	return nil
}
