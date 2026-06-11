package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIssueHandoffCreateListRead(t *testing.T) {
	issueID := createHandoffTestIssue(t, "Handoff source")
	followUpID := createHandoffTestIssue(t, "Handoff follow-up")
	agentID := createHandlerTestAgent(t, "Handoff next agent", []byte(`{}`))

	body := map[string]any{
		"work_completed":     "Implemented the API contract.",
		"work_remaining":     "Wire the UI.",
		"decisions_made":     "Follow-up issues are stored as ids.",
		"uncertainties":      "No CLI shape agreed yet.",
		"next_assignee_type": "agent",
		"next_assignee_id":   agentID,
		"follow_up_issue_ids": []string{
			followUpID,
		},
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/"+issueID+"/handoffs", body)
	req = withURLParam(req, "id", issueID)
	testHandler.CreateHandoff(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateHandoff: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var created HandoffResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if created.IssueID != issueID {
		t.Fatalf("issue_id = %q, want %q", created.IssueID, issueID)
	}
	if created.WorkspaceID != testWorkspaceID {
		t.Fatalf("workspace_id = %q, want %q", created.WorkspaceID, testWorkspaceID)
	}
	if created.AuthorType == nil || *created.AuthorType != "member" || created.AuthorID == nil || *created.AuthorID != testUserID {
		t.Fatalf("author = %v/%v, want member/%s", created.AuthorType, created.AuthorID, testUserID)
	}
	if created.NextAssigneeType == nil || *created.NextAssigneeType != "agent" {
		t.Fatalf("next_assignee_type = %v, want agent", created.NextAssigneeType)
	}
	if created.NextAssigneeID == nil || *created.NextAssigneeID != agentID {
		t.Fatalf("next_assignee_id = %v, want %s", created.NextAssigneeID, agentID)
	}
	if len(created.FollowUpIssueIDs) != 1 || created.FollowUpIssueIDs[0] != followUpID {
		t.Fatalf("follow_up_issue_ids = %+v, want [%s]", created.FollowUpIssueIDs, followUpID)
	}
	if created.WorkCompleted != "Implemented the API contract." || created.WorkRemaining != "Wire the UI." {
		t.Fatalf("structured content not preserved: %+v", created)
	}

	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/issues/"+issueID+"/handoffs", nil)
	req = withURLParam(req, "id", issueID)
	testHandler.ListHandoffs(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListHandoffs: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var listed []HandoffResponse
	if err := json.NewDecoder(w.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("listed handoffs = %+v, want created id %s", listed, created.ID)
	}

	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/handoffs/"+created.ID, nil)
	req = withURLParam(req, "id", created.ID)
	testHandler.GetHandoff(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetHandoff: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got HandoffResponse
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if got.ID != created.ID || len(got.FollowUpIssueIDs) != 1 || got.FollowUpIssueIDs[0] != followUpID {
		t.Fatalf("read handoff mismatch: %+v", got)
	}
}

func TestCreateHandoffValidation(t *testing.T) {
	issueID := createHandoffTestIssue(t, "Handoff validation")
	otherWorkspaceIssueID := createHandoffOtherWorkspaceIssue(t)

	tests := []struct {
		name string
		body map[string]any
	}{
		{
			name: "missing structured content",
			body: map[string]any{},
		},
		{
			name: "next assignee type without id",
			body: map[string]any{
				"work_completed":     "done",
				"next_assignee_type": "agent",
			},
		},
		{
			name: "invalid next assignee type",
			body: map[string]any{
				"work_completed":     "done",
				"next_assignee_type": "robot",
				"next_assignee_id":   testUserID,
			},
		},
		{
			name: "follow-up issue outside workspace",
			body: map[string]any{
				"work_completed":      "done",
				"follow_up_issue_ids": []string{otherWorkspaceIssueID},
			},
		},
		{
			name: "malformed follow-up issue id",
			body: map[string]any{
				"work_completed":      "done",
				"follow_up_issue_ids": []string{"not-a-uuid"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := newRequest("POST", "/api/issues/"+issueID+"/handoffs", tc.body)
			req = withURLParam(req, "id", issueID)
			testHandler.CreateHandoff(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestHandoffWorkspaceIsolation(t *testing.T) {
	otherIssueID := createHandoffOtherWorkspaceIssue(t)

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/issues/"+otherIssueID+"/handoffs", nil)
	req = withURLParam(req, "id", otherIssueID)
	testHandler.ListHandoffs(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("ListHandoffs outside workspace: expected 404, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/issues/"+otherIssueID+"/handoffs", map[string]any{"work_completed": "done"})
	req = withURLParam(req, "id", otherIssueID)
	testHandler.CreateHandoff(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("CreateHandoff outside workspace: expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func createHandoffTestIssue(t *testing.T, title string) string {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":    title,
		"status":   "todo",
		"priority": "medium",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("createHandoffTestIssue: %d %s", w.Code, w.Body.String())
	}
	var issue IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&issue); err != nil {
		t.Fatalf("decode issue: %v", err)
	}
	t.Cleanup(func() {
		req := newRequest("DELETE", "/api/issues/"+issue.ID, nil)
		req = withURLParam(req, "id", issue.ID)
		testHandler.DeleteIssue(httptest.NewRecorder(), req)
	})
	return issue.ID
}

func createHandoffOtherWorkspaceIssue(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	var workspaceID, userID, issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Handoff Other User', 'handoff-other-' || gen_random_uuid() || '@multica.ai')
		RETURNING id
	`).Scan(&userID); err != nil {
		t.Fatalf("create other user: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, issue_prefix)
		VALUES ('Handoff Other Workspace', 'handoff-other-' || gen_random_uuid(), 'HOF')
		RETURNING id
	`).Scan(&workspaceID); err != nil {
		t.Fatalf("create other workspace: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, workspaceID, userID); err != nil {
		t.Fatalf("create other member: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, number, position)
		VALUES ($1, 'Other workspace handoff issue', 'todo', 'medium', 'member', $2, 1, 0)
		RETURNING id
	`, workspaceID, userID).Scan(&issueID); err != nil {
		t.Fatalf("create other issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID)
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
	})
	return issueID
}
