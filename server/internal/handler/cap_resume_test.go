package handler

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestCapFailureRecordsResumeSchedule covers ADA-44 slice 1 end-to-end at
// the service layer with a real database: a task failing on a provider cap
// gets resume_at stamped and exactly one plain-language system comment;
// everything outside the cap class behaves exactly as before.
//
// Subtests share the fixture workspace and run sequentially — the disabled
// case mutates workspace settings and restores them via t.Cleanup.
func TestCapFailureRecordsResumeSchedule(t *testing.T) {
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

	mkIssue := func(t *testing.T, title string) string {
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO issue (workspace_id, title, creator_id, creator_type, number)
			VALUES (
				$1, $2, $3, 'member',
				(SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1)
			)
			RETURNING id
		`, testWorkspaceID, title, testUserID).Scan(&id); err != nil {
			t.Fatalf("insert issue: %v", err)
		}
		t.Cleanup(func() {
			testPool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id = $1`, id)
			testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, id)
		})
		return id
	}

	mkRunningTask := func(t *testing.T, issueID string, autopilotRunID any) string {
		var taskID string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (agent_id, issue_id, runtime_id, status, priority, started_at, autopilot_run_id)
			VALUES ($1, $2, $3, 'running', 0, now(), $4)
			RETURNING id
		`, agentID, issueID, runtimeID, autopilotRunID).Scan(&taskID); err != nil {
			t.Fatalf("insert task: %v", err)
		}
		t.Cleanup(func() {
			testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
		})
		return taskID
	}

	readTask := func(t *testing.T, taskID string) (resumeAt *time.Time, failureReason string) {
		if err := testPool.QueryRow(ctx, `
			SELECT resume_at, COALESCE(failure_reason, '') FROM agent_task_queue WHERE id = $1
		`, taskID).Scan(&resumeAt, &failureReason); err != nil {
			t.Fatalf("read task: %v", err)
		}
		return resumeAt, failureReason
	}

	countComments := func(t *testing.T, issueID, authorType string) int {
		var n int
		if err := testPool.QueryRow(ctx, `
			SELECT COUNT(*) FROM comment WHERE issue_id = $1 AND author_type = $2
		`, issueID, authorType).Scan(&n); err != nil {
			t.Fatalf("count comments: %v", err)
		}
		return n
	}

	failTask := func(t *testing.T, taskID, errMsg string) {
		t.Helper()
		// failureReason "" exercises the production path: the server
		// classifies the raw error text itself.
		if _, err := testHandler.TaskService.FailTask(ctx, parseUUID(taskID), errMsg, "", "", ""); err != nil {
			t.Fatalf("FailTask: %v", err)
		}
	}

	t.Run("quota cap with parseable duration uses provider reset", func(t *testing.T) {
		issueID := mkIssue(t, "cap quota parseable")
		taskID := mkRunningTask(t, issueID, nil)

		before := time.Now()
		failTask(t, taskID, "You've hit your usage limit, retry after 30 minutes")

		resumeAt, reason := readTask(t, taskID)
		if reason != "agent_error.provider_quota_limit" {
			t.Errorf("failure_reason = %q, want agent_error.provider_quota_limit", reason)
		}
		if resumeAt == nil {
			t.Fatal("resume_at is NULL, want ~now+30m")
		}
		lo, hi := before.Add(29*time.Minute), time.Now().Add(31*time.Minute)
		if resumeAt.Before(lo) || resumeAt.After(hi) {
			t.Errorf("resume_at = %v, want within [%v, %v]", resumeAt, lo, hi)
		}
		if got := countComments(t, issueID, "system"); got != 1 {
			t.Errorf("system comments = %d, want exactly 1 (AC3)", got)
		}
		if got := countComments(t, issueID, "agent"); got != 0 {
			t.Errorf("agent comments = %d, want 0 — cap comment replaces the raw error dump", got)
		}
		var content string
		if err := testPool.QueryRow(ctx, `
			SELECT content FROM comment WHERE issue_id = $1 AND author_type = 'system'
		`, issueID).Scan(&content); err != nil {
			t.Fatalf("read comment: %v", err)
		}
		for _, want := range []string{"No human action needed", "resume"} {
			if !containsFold(content, want) {
				t.Errorf("cap comment %q missing %q", content, want)
			}
		}
	})

	t.Run("rate limit cap without parseable time uses default backoff", func(t *testing.T) {
		issueID := mkIssue(t, "cap rate limit backoff")
		taskID := mkRunningTask(t, issueID, nil)

		before := time.Now()
		failTask(t, taskID, "API Error: 429 Too Many Requests")

		resumeAt, reason := readTask(t, taskID)
		if reason != "agent_error.provider_capacity_or_rate_limit" {
			t.Errorf("failure_reason = %q, want agent_error.provider_capacity_or_rate_limit", reason)
		}
		if resumeAt == nil {
			t.Fatal("resume_at is NULL, want ~now+60m (default backoff)")
		}
		lo, hi := before.Add(59*time.Minute), time.Now().Add(61*time.Minute)
		if resumeAt.Before(lo) || resumeAt.After(hi) {
			t.Errorf("resume_at = %v, want within [%v, %v]", resumeAt, lo, hi)
		}
		if got := countComments(t, issueID, "system"); got != 1 {
			t.Errorf("system comments = %d, want exactly 1", got)
		}
	})

	t.Run("non-cap failure is byte-for-byte unaffected", func(t *testing.T) {
		issueID := mkIssue(t, "non-cap failure")
		taskID := mkRunningTask(t, issueID, nil)

		failTask(t, taskID, "agent process exited with exit status 1")

		resumeAt, reason := readTask(t, taskID)
		if reason != "agent_error.process_failure" {
			t.Errorf("failure_reason = %q, want agent_error.process_failure", reason)
		}
		if resumeAt != nil {
			t.Errorf("resume_at = %v, want NULL for non-cap failures (AC4)", resumeAt)
		}
		if got := countComments(t, issueID, "system"); got != 0 {
			t.Errorf("system comments = %d, want 0 for non-cap failures", got)
		}
		// Existing behaviour: the raw error is posted as an agent-authored
		// system-type comment.
		if got := countComments(t, issueID, "agent"); got != 1 {
			t.Errorf("agent comments = %d, want 1 (existing raw-error comment)", got)
		}
	})

	t.Run("workspace opt-out behaves exactly as today", func(t *testing.T) {
		var prevSettings []byte
		if err := testPool.QueryRow(ctx, `
			SELECT settings FROM workspace WHERE id = $1
		`, testWorkspaceID).Scan(&prevSettings); err != nil {
			t.Fatalf("read settings: %v", err)
		}
		if _, err := testPool.Exec(ctx, `
			UPDATE workspace
			SET settings = settings || '{"session_cap_resilience": {"enabled": false}}'::jsonb
			WHERE id = $1
		`, testWorkspaceID); err != nil {
			t.Fatalf("disable feature: %v", err)
		}
		t.Cleanup(func() {
			testPool.Exec(context.Background(), `UPDATE workspace SET settings = $2 WHERE id = $1`, testWorkspaceID, prevSettings)
		})

		issueID := mkIssue(t, "cap while disabled")
		taskID := mkRunningTask(t, issueID, nil)

		failTask(t, taskID, "You've hit your usage limit, retry after 30 minutes")

		resumeAt, _ := readTask(t, taskID)
		if resumeAt != nil {
			t.Errorf("resume_at = %v, want NULL when feature disabled (AC5)", resumeAt)
		}
		if got := countComments(t, issueID, "system"); got != 0 {
			t.Errorf("system comments = %d, want 0 when feature disabled", got)
		}
		if got := countComments(t, issueID, "agent"); got != 1 {
			t.Errorf("agent comments = %d, want 1 (existing raw-error comment)", got)
		}
	})

	t.Run("autopilot tasks never get a resume schedule", func(t *testing.T) {
		var autopilotID string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO autopilot (workspace_id, title, assignee_id, execution_mode, created_by_type, created_by_id)
			VALUES ($1, 'cap-resume-ap', $2, 'run_only', 'member', $3)
			RETURNING id
		`, testWorkspaceID, agentID, testUserID).Scan(&autopilotID); err != nil {
			t.Fatalf("create autopilot: %v", err)
		}
		t.Cleanup(func() {
			testPool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, autopilotID)
		})
		var runID string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO autopilot_run (autopilot_id, source, status)
			VALUES ($1, 'manual', 'running')
			RETURNING id
		`, autopilotID).Scan(&runID); err != nil {
			t.Fatalf("create autopilot_run: %v", err)
		}

		issueID := mkIssue(t, "cap on autopilot task")
		taskID := mkRunningTask(t, issueID, runID)

		failTask(t, taskID, "You've hit your usage limit, retry after 30 minutes")

		resumeAt, _ := readTask(t, taskID)
		if resumeAt != nil {
			t.Errorf("resume_at = %v, want NULL for autopilot tasks (AC6)", resumeAt)
		}
		if got := countComments(t, issueID, "system"); got != 0 {
			t.Errorf("system comments = %d, want 0 for autopilot tasks", got)
		}
	})
}

// containsFold is a case-insensitive substring check for comment content
// assertions.
func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}
