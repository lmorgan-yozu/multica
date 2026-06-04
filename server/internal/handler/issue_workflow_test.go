package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestStepShouldAdvance(t *testing.T) {
	const (
		agentA = "11111111-1111-1111-1111-111111111111"
		agentB = "22222222-2222-2222-2222-222222222222"
	)

	cases := []struct {
		name          string
		runState      string
		prevStatus    string
		newStatus     string
		advanceStatus string
		assigneeType  string
		assigneeID    string
		stepAgentID   string
		want          bool
	}{
		{
			name:     "fires on transition into advance_status while assigned to step agent",
			runState: "active", prevStatus: "in_progress", newStatus: "in_review",
			advanceStatus: "in_review", assigneeType: "agent", assigneeID: agentA, stepAgentID: agentA,
			want: true,
		},
		{
			name:     "no-op when status unchanged (re-save of same advance_status)",
			runState: "active", prevStatus: "in_review", newStatus: "in_review",
			advanceStatus: "in_review", assigneeType: "agent", assigneeID: agentA, stepAgentID: agentA,
			want: false,
		},
		{
			name:     "no-op when new status is not the step's advance_status",
			runState: "active", prevStatus: "todo", newStatus: "in_progress",
			advanceStatus: "in_review", assigneeType: "agent", assigneeID: agentA, stepAgentID: agentA,
			want: false,
		},
		{
			name:     "no-op when run is not active",
			runState: "completed", prevStatus: "in_progress", newStatus: "in_review",
			advanceStatus: "in_review", assigneeType: "agent", assigneeID: agentA, stepAgentID: agentA,
			want: false,
		},
		{
			name:     "no-op when issue reassigned away from the step's agent",
			runState: "active", prevStatus: "in_progress", newStatus: "in_review",
			advanceStatus: "in_review", assigneeType: "agent", assigneeID: agentB, stepAgentID: agentA,
			want: false,
		},
		{
			name:     "no-op when assignee is a human member",
			runState: "active", prevStatus: "in_progress", newStatus: "in_review",
			advanceStatus: "in_review", assigneeType: "member", assigneeID: agentA, stepAgentID: agentA,
			want: false,
		},
		{
			name:     "respects a custom advance_status",
			runState: "active", prevStatus: "in_progress", newStatus: "done",
			advanceStatus: "done", assigneeType: "agent", assigneeID: agentA, stepAgentID: agentA,
			want: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := stepShouldAdvance(tc.runState, tc.prevStatus, tc.newStatus, tc.advanceStatus, tc.assigneeType, tc.assigneeID, tc.stepAgentID)
			if got != tc.want {
				t.Fatalf("stepShouldAdvance = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNextStepIndex(t *testing.T) {
	cases := []struct {
		name    string
		orders  []int
		current int
		wantIdx int
		wantOk  bool
	}{
		{name: "middle step advances to next", orders: []int{1, 2, 3}, current: 1, wantIdx: 1, wantOk: true},
		{name: "last step is terminal", orders: []int{1, 2, 3}, current: 3, wantIdx: -1, wantOk: false},
		{name: "non-contiguous orders pick the next larger", orders: []int{10, 20, 30}, current: 10, wantIdx: 1, wantOk: true},
		{name: "unsorted input still finds the smallest larger order", orders: []int{30, 10, 20}, current: 10, wantIdx: 2, wantOk: true},
		{name: "single step is terminal", orders: []int{1}, current: 1, wantIdx: -1, wantOk: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			idx, ok := nextStepIndex(tc.orders, tc.current)
			if ok != tc.wantOk || idx != tc.wantIdx {
				t.Fatalf("nextStepIndex(%v, %d) = (%d, %v), want (%d, %v)", tc.orders, tc.current, idx, ok, tc.wantIdx, tc.wantOk)
			}
		})
	}
}

// TestWorkflowHandoffEndToEnd drives a two-step workflow (agent A → agent B)
// through the real UpdateIssue handler and asserts the advance engine
// reassigns, restatuses, comments, and finally opens a human review gate.
func TestWorkflowHandoffEndToEnd(t *testing.T) {
	ctx := context.Background()
	q := testHandler.Queries

	agentA := createHandlerTestAgent(t, "wf-agent-A "+time.Now().Format(time.RFC3339Nano), nil)
	agentB := createHandlerTestAgent(t, "wf-agent-B "+time.Now().Format(time.RFC3339Nano), nil)

	wf, err := q.CreateWorkflow(ctx, db.CreateWorkflowParams{
		WorkspaceID:   parseUUID(testWorkspaceID),
		Name:          "wf " + time.Now().Format(time.RFC3339Nano),
		Description:   "",
		CreatedByType: "member",
		CreatedByID:   parseUUID(testUserID),
	})
	if err != nil {
		t.Fatalf("create workflow: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM workflow WHERE id = $1`, uuidToString(wf.ID)) })

	stepA, err := q.CreateWorkflowStep(ctx, db.CreateWorkflowStepParams{
		WorkflowID: wf.ID, StepOrder: 1, AgentID: parseUUID(agentA),
		Name: "Backend", StartStatus: "todo", AdvanceStatus: "in_review",
	})
	if err != nil {
		t.Fatalf("create step A: %v", err)
	}
	stepB, err := q.CreateWorkflowStep(ctx, db.CreateWorkflowStepParams{
		WorkflowID: wf.ID, StepOrder: 2, AgentID: parseUUID(agentB),
		Name: "Frontend", StartStatus: "todo", AdvanceStatus: "in_review",
	})
	if err != nil {
		t.Fatalf("create step B: %v", err)
	}

	// Create an issue, assign it to agent A at in_progress, and bind it to the
	// workflow at step A.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "wf issue " + time.Now().Format(time.RFC3339Nano),
		"status": "todo",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create issue: %d %s", w.Code, w.Body.String())
	}
	var issue IssueResponse
	json.NewDecoder(w.Body).Decode(&issue)
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issue.ID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issue.ID)
	})
	if _, err := testPool.Exec(ctx,
		`UPDATE issue SET assignee_type='agent', assignee_id=$2, status='in_progress' WHERE id=$1`,
		issue.ID, agentA); err != nil {
		t.Fatalf("seed assignee: %v", err)
	}
	if _, err := q.CreateIssueWorkflowRun(ctx, db.CreateIssueWorkflowRunParams{
		IssueID: parseUUID(issue.ID), WorkflowID: wf.ID, CurrentStepID: stepA.ID,
	}); err != nil {
		t.Fatalf("bind run: %v", err)
	}

	// Agent A finishes: move the issue to in_review. This should advance to B.
	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/issues/"+issue.ID, map[string]any{"status": "in_review"})
	req = withURLParam(req, "id", issue.ID)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update->in_review: %d %s", w.Code, w.Body.String())
	}

	reloaded, err := q.GetIssue(ctx, parseUUID(issue.ID))
	if err != nil {
		t.Fatalf("reload issue: %v", err)
	}
	if got := uuidToString(reloaded.AssigneeID); got != agentB {
		t.Fatalf("expected reassignment to agent B (%s), got %s", agentB, got)
	}
	if reloaded.Status != "todo" {
		t.Fatalf("expected status reset to step B start_status 'todo', got %q", reloaded.Status)
	}
	run, err := q.GetIssueWorkflowRun(ctx, parseUUID(issue.ID))
	if err != nil {
		t.Fatalf("reload run: %v", err)
	}
	if uuidToString(run.CurrentStepID) != uuidToString(stepB.ID) {
		t.Fatalf("expected run to point at step B")
	}
	if run.State != "active" {
		t.Fatalf("expected run still active, got %q", run.State)
	}
	if !workflowCommentMentions(t, issue.ID, agentB) {
		t.Fatalf("expected a handoff comment mentioning agent B")
	}

	// Agent B finishes the final step: move to in_review again. The run should
	// complete and the issue should stay in_review as a human review gate.
	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/issues/"+issue.ID, map[string]any{"status": "in_review"})
	req = withURLParam(req, "id", issue.ID)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update->in_review (final): %d %s", w.Code, w.Body.String())
	}

	run, err = q.GetIssueWorkflowRun(ctx, parseUUID(issue.ID))
	if err != nil {
		t.Fatalf("reload run (final): %v", err)
	}
	if run.State != "completed" {
		t.Fatalf("expected run completed after final step, got %q", run.State)
	}
	final, err := q.GetIssue(ctx, parseUUID(issue.ID))
	if err != nil {
		t.Fatalf("reload issue (final): %v", err)
	}
	if final.Status != "in_review" {
		t.Fatalf("expected issue left at in_review (human gate), got %q", final.Status)
	}
	if uuidToString(final.AssigneeID) != agentB {
		t.Fatalf("expected issue still assigned to agent B at the gate")
	}
}

func workflowCommentMentions(t *testing.T, issueID, agentID string) bool {
	t.Helper()
	rows, err := testPool.Query(context.Background(),
		`SELECT content FROM comment WHERE issue_id = $1 AND author_type = 'system'`, issueID)
	if err != nil {
		t.Fatalf("load comments: %v", err)
	}
	defer rows.Close()
	needle := "mention://agent/" + agentID
	for rows.Next() {
		var content string
		if err := rows.Scan(&content); err != nil {
			t.Fatalf("scan comment: %v", err)
		}
		if strings.Contains(content, needle) {
			return true
		}
	}
	return false
}

// TestBindIssueWorkflowHandler covers the user-facing bind entry point: it
// should start the run at step 1, assign + restatus the issue, and reject a
// second bind with 409.
func TestBindIssueWorkflowHandler(t *testing.T) {
	ctx := context.Background()
	q := testHandler.Queries

	agentA := createHandlerTestAgent(t, "bind-agent-A "+time.Now().Format(time.RFC3339Nano), nil)
	wf, err := q.CreateWorkflow(ctx, db.CreateWorkflowParams{
		WorkspaceID: parseUUID(testWorkspaceID), Name: "bind wf " + time.Now().Format(time.RFC3339Nano),
		Description: "", CreatedByType: "member", CreatedByID: parseUUID(testUserID),
	})
	if err != nil {
		t.Fatalf("create workflow: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM workflow WHERE id = $1`, uuidToString(wf.ID)) })
	if _, err := q.CreateWorkflowStep(ctx, db.CreateWorkflowStepParams{
		WorkflowID: wf.ID, StepOrder: 1, AgentID: parseUUID(agentA),
		Name: "Backend", StartStatus: "todo", AdvanceStatus: "in_review",
	}); err != nil {
		t.Fatalf("create step: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title": "bind issue " + time.Now().Format(time.RFC3339Nano), "status": "backlog",
	})
	testHandler.CreateIssue(w, req)
	var issue IssueResponse
	json.NewDecoder(w.Body).Decode(&issue)
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issue.ID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issue.ID)
	})

	wfID := uuidToString(wf.ID)
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/workflows/"+wfID+"/bind", map[string]any{"issue_id": issue.ID})
	req = withURLParam(req, "id", wfID)
	testHandler.BindIssueWorkflow(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("bind: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	reloaded, err := q.GetIssue(ctx, parseUUID(issue.ID))
	if err != nil {
		t.Fatalf("reload issue: %v", err)
	}
	if uuidToString(reloaded.AssigneeID) != agentA {
		t.Fatalf("expected issue assigned to step 1 agent")
	}
	if reloaded.Status != "todo" {
		t.Fatalf("expected status set to step start_status 'todo', got %q", reloaded.Status)
	}

	// Second bind must conflict.
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/workflows/"+wfID+"/bind", map[string]any{"issue_id": issue.ID})
	req = withURLParam(req, "id", wfID)
	testHandler.BindIssueWorkflow(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("second bind: expected 409, got %d: %s", w.Code, w.Body.String())
	}
}
