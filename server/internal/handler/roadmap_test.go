package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Integration tests for the roadmap endpoints against the real test
// database. Pure projection logic is covered in roadmap_projection_test.go;
// these tests focus on tenant scoping, validation paths, and the wiring
// between fixtures and the projection.

func createRoadmapTestProject(t *testing.T, workspaceID, title string) string {
	t.Helper()
	var projectID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO project (workspace_id, title, status)
		VALUES ($1, $2, 'in_progress')
		RETURNING id
	`, workspaceID, title).Scan(&projectID); err != nil {
		t.Fatalf("failed to create test project: %v", err)
	}
	t.Cleanup(func() {
		// Issues hold project_id with ON DELETE SET NULL, so remove them
		// first to keep the shared workspace clean; milestones cascade.
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE project_id = $1`, projectID)
		testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID)
	})
	return projectID
}

type roadmapTestIssue struct {
	title    string
	status   string
	parentID string
	dueDate  string
	position float64
}

func createRoadmapTestIssue(t *testing.T, workspaceID, projectID string, spec roadmapTestIssue) string {
	t.Helper()
	var parent any
	if spec.parentID != "" {
		parent = spec.parentID
	}
	var due any
	if spec.dueDate != "" {
		due = spec.dueDate
	}
	var issueID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO issue (workspace_id, project_id, title, status, priority,
			creator_type, creator_id, parent_issue_id, due_date, position, number)
		VALUES ($1, $2, $3, $4, 'none', 'member', $5, $6, $7, $8,
			(SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1))
		RETURNING id
	`, workspaceID, projectID, spec.title, spec.status, testUserID, parent, due, spec.position).Scan(&issueID); err != nil {
		t.Fatalf("failed to create test issue %q: %v", spec.title, err)
	}
	return issueID
}

func getRoadmap(t *testing.T, projectID string) (*httptest.ResponseRecorder, RoadmapResponse) {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/projects/"+projectID+"/roadmap", nil)
	req = withURLParam(req, "id", projectID)
	testHandler.GetProjectRoadmap(w, req)
	var resp RoadmapResponse
	if w.Code == http.StatusOK {
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode roadmap response: %v", err)
		}
	}
	return w, resp
}

func createMilestoneViaAPI(t *testing.T, projectID string, body map[string]any) MilestoneResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects/"+projectID+"/milestones", body)
	req = withURLParam(req, "id", projectID)
	testHandler.CreateMilestone(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create milestone failed: %d %s", w.Code, w.Body.String())
	}
	var resp MilestoneResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode milestone response: %v", err)
	}
	return resp
}

func setIssueMilestoneViaAPI(t *testing.T, issueID string, milestoneID *string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/issues/"+issueID+"/milestone", map[string]any{"milestone_id": milestoneID})
	req = withURLParam(req, "id", issueID)
	testHandler.SetIssueMilestone(w, req)
	return w
}

func addDependencyViaAPI(t *testing.T, issueID, dependsOnID string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/"+issueID+"/dependencies", map[string]any{"depends_on_issue_id": dependsOnID})
	req = withURLParam(req, "id", issueID)
	testHandler.CreateIssueDependency(w, req)
	return w
}

func waitForIssueUpdatedEvent(t *testing.T, ch <-chan events.Event) map[string]any {
	t.Helper()
	select {
	case e := <-ch:
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			t.Fatalf("issue:updated payload type = %T, want map[string]any", e.Payload)
		}
		return payload
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive issue:updated event")
	}
	return nil
}

func TestGetProjectRoadmapEmptyProject(t *testing.T) {
	projectID := createRoadmapTestProject(t, testWorkspaceID, "Roadmap Empty")
	w, resp := getRoadmap(t, projectID)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(resp.Epics) != 0 || len(resp.Milestones) != 0 || resp.CycleDetected {
		t.Fatalf("expected empty roadmap, got %+v", resp)
	}
	if resp.ProjectTitle != "Roadmap Empty" {
		t.Fatalf("project title = %q", resp.ProjectTitle)
	}
}

func TestIssueListSurfacesMilestoneID(t *testing.T) {
	projectID := createRoadmapTestProject(t, testWorkspaceID, "Roadmap List Milestone")
	milestone := createMilestoneViaAPI(t, projectID, map[string]any{"name": "Alpha"})
	issueID := createRoadmapTestIssue(t, testWorkspaceID, projectID, roadmapTestIssue{title: "Milestoned issue", status: "todo"})
	if w := setIssueMilestoneViaAPI(t, issueID, &milestone.ID); w.Code != http.StatusOK {
		t.Fatalf("SetIssueMilestone: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/issues?workspace_id="+testWorkspaceID+"&project_id="+projectID, nil)
	testHandler.ListIssues(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListIssues: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var listResp struct {
		Issues []IssueResponse `json:"issues"`
	}
	if err := json.NewDecoder(w.Body).Decode(&listResp); err != nil {
		t.Fatalf("decode ListIssues: %v", err)
	}
	if len(listResp.Issues) != 1 {
		t.Fatalf("ListIssues: expected 1 issue, got %d", len(listResp.Issues))
	}
	if got := listResp.Issues[0].MilestoneID; got == nil || *got != milestone.ID {
		t.Fatalf("ListIssues milestone_id = %v, want %s", got, milestone.ID)
	}

	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/issues/grouped?workspace_id="+testWorkspaceID+"&project_id="+projectID, nil)
	testHandler.ListGroupedIssues(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListGroupedIssues: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var grouped GroupedIssuesResponse
	if err := json.NewDecoder(w.Body).Decode(&grouped); err != nil {
		t.Fatalf("decode ListGroupedIssues: %v", err)
	}
	if len(grouped.Groups) != 1 || len(grouped.Groups[0].Issues) != 1 {
		t.Fatalf("ListGroupedIssues: expected one group with one issue, got %+v", grouped.Groups)
	}
	if got := grouped.Groups[0].Issues[0].MilestoneID; got == nil || *got != milestone.ID {
		t.Fatalf("ListGroupedIssues milestone_id = %v, want %s", got, milestone.ID)
	}
}

func TestRoadmapIssueWritesPublishIssueUpdated(t *testing.T) {
	projectID := createRoadmapTestProject(t, testWorkspaceID, "Roadmap Event Writes")
	milestone := createMilestoneViaAPI(t, projectID, map[string]any{"name": "Beta"})
	issueA := createRoadmapTestIssue(t, testWorkspaceID, projectID, roadmapTestIssue{title: "A", status: "todo"})
	issueB := createRoadmapTestIssue(t, testWorkspaceID, projectID, roadmapTestIssue{title: "B", status: "todo"})

	gotEvents := make(chan events.Event, 4)
	testHandler.Bus.Subscribe(protocol.EventIssueUpdated, func(e events.Event) {
		select {
		case gotEvents <- e:
		default:
		}
	})

	if w := setIssueMilestoneViaAPI(t, issueA, &milestone.ID); w.Code != http.StatusOK {
		t.Fatalf("SetIssueMilestone: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	payload := waitForIssueUpdatedEvent(t, gotEvents)
	if payload["milestone_changed"] != true {
		t.Fatalf("SetIssueMilestone event milestone_changed = %v, want true", payload["milestone_changed"])
	}
	if issue, ok := payload["issue"].(IssueResponse); !ok || issue.ID != issueA || issue.MilestoneID == nil || *issue.MilestoneID != milestone.ID {
		t.Fatalf("SetIssueMilestone event issue = %#v, want updated issue %s with milestone %s", payload["issue"], issueA, milestone.ID)
	}

	if w := addDependencyViaAPI(t, issueA, issueB); w.Code != http.StatusCreated {
		t.Fatalf("CreateIssueDependency: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	payload = waitForIssueUpdatedEvent(t, gotEvents)
	if payload["dependencies_changed"] != true {
		t.Fatalf("CreateIssueDependency event dependencies_changed = %v, want true", payload["dependencies_changed"])
	}

	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/issues/"+issueA+"/dependencies/"+issueB, nil)
	req = withURLParam(req, "id", issueA)
	req = withURLParam(req, "dependsOnId", issueB)
	testHandler.DeleteIssueDependency(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteIssueDependency: expected 204, got %d: %s", w.Code, w.Body.String())
	}
	payload = waitForIssueUpdatedEvent(t, gotEvents)
	if payload["dependencies_changed"] != true {
		t.Fatalf("DeleteIssueDependency event dependencies_changed = %v, want true", payload["dependencies_changed"])
	}
}

func TestGetProjectRoadmapWorkspaceIsolation(t *testing.T) {
	// A project in another workspace must 404 for this workspace's caller,
	// exactly like GetProject.
	var otherWorkspaceID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ('Roadmap Other WS', 'roadmap-other-ws', '', 'ROW')
		RETURNING id
	`).Scan(&otherWorkspaceID); err != nil {
		t.Fatalf("failed to create other workspace: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, otherWorkspaceID)
	})
	projectID := createRoadmapTestProject(t, otherWorkspaceID, "Foreign Project")

	w, _ := getRoadmap(t, projectID)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-workspace roadmap, got %d", w.Code)
	}

	// Milestone writes are scoped the same way.
	mw := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects/"+projectID+"/milestones", map[string]any{"name": "Nope"})
	req = withURLParam(req, "id", projectID)
	testHandler.CreateMilestone(mw, req)
	if mw.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-workspace milestone create, got %d", mw.Code)
	}
}

func TestGetProjectRoadmapRollupAndBlockedFlags(t *testing.T) {
	projectID := createRoadmapTestProject(t, testWorkspaceID, "Roadmap Rollup")
	epic := createRoadmapTestIssue(t, testWorkspaceID, projectID, roadmapTestIssue{title: "Epic", status: "in_progress"})
	createRoadmapTestIssue(t, testWorkspaceID, projectID, roadmapTestIssue{title: "Done leaf", status: "done", parentID: epic})
	createRoadmapTestIssue(t, testWorkspaceID, projectID, roadmapTestIssue{title: "Blocked leaf", status: "blocked", parentID: epic})
	createRoadmapTestIssue(t, testWorkspaceID, projectID, roadmapTestIssue{title: "Cancelled leaf", status: "cancelled", parentID: epic})
	solo := createRoadmapTestIssue(t, testWorkspaceID, projectID, roadmapTestIssue{title: "Solo", status: "todo"})

	w, resp := getRoadmap(t, projectID)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(resp.Epics) != 2 {
		t.Fatalf("expected 2 epics, got %d", len(resp.Epics))
	}
	byID := map[string]RoadmapEpic{}
	for _, e := range resp.Epics {
		byID[e.ID] = e
	}
	e := byID[epic]
	if e.Progress != (RoadmapProgress{Done: 1, Total: 2}) {
		t.Fatalf("epic progress = %+v, want 1/2 (cancelled excluded)", e.Progress)
	}
	if e.BlockedCount != 1 {
		t.Fatalf("epic blocked = %d, want 1", e.BlockedCount)
	}
	if e.ChildCount != 3 {
		t.Fatalf("epic child count = %d, want 3", e.ChildCount)
	}
	if s := byID[solo]; s.Progress != (RoadmapProgress{Done: 0, Total: 1}) {
		t.Fatalf("solo progress = %+v, want 0/1", s.Progress)
	}
	if e.Identifier == "" || e.Identifier[0] == '-' {
		t.Fatalf("epic identifier missing prefix: %q", e.Identifier)
	}
}

func TestRoadmapMilestoneLifecycleAndGrouping(t *testing.T) {
	projectID := createRoadmapTestProject(t, testWorkspaceID, "Roadmap Milestones")
	epicA := createRoadmapTestIssue(t, testWorkspaceID, projectID, roadmapTestIssue{title: "A", status: "done"})
	epicB := createRoadmapTestIssue(t, testWorkspaceID, projectID, roadmapTestIssue{title: "B", status: "todo"})

	m := createMilestoneViaAPI(t, projectID, map[string]any{"name": "Demo ready", "target_date": "2026-07-01"})
	if m.TargetDate == nil || *m.TargetDate != "2026-07-01" {
		t.Fatalf("milestone target_date = %v", m.TargetDate)
	}

	for _, issueID := range []string{epicA, epicB} {
		if w := setIssueMilestoneViaAPI(t, issueID, &m.ID); w.Code != http.StatusOK {
			t.Fatalf("set milestone on %s failed: %d %s", issueID, w.Code, w.Body.String())
		}
	}

	_, resp := getRoadmap(t, projectID)
	if len(resp.Milestones) != 1 {
		t.Fatalf("expected 1 milestone, got %d", len(resp.Milestones))
	}
	ms := resp.Milestones[0]
	if ms.Progress != (RoadmapProgress{Done: 1, Total: 2}) {
		t.Fatalf("milestone progress = %+v", ms.Progress)
	}
	if len(ms.EpicIDs) != 2 {
		t.Fatalf("milestone epic_ids = %v", ms.EpicIDs)
	}

	// Clearing the assignment removes the epic from the group.
	if w := setIssueMilestoneViaAPI(t, epicB, nil); w.Code != http.StatusOK {
		t.Fatalf("clear milestone failed: %d %s", w.Code, w.Body.String())
	}
	_, resp = getRoadmap(t, projectID)
	if got := len(resp.Milestones[0].EpicIDs); got != 1 {
		t.Fatalf("after clear, epic_ids len = %d, want 1", got)
	}

	// Update changes name and clears the target date with explicit null.
	uw := httptest.NewRecorder()
	req := newRequest("PUT", "/api/projects/"+projectID+"/milestones/"+m.ID, map[string]any{
		"name": "Renamed", "target_date": nil,
	})
	req = withURLParams(req, "id", projectID, "milestoneId", m.ID)
	testHandler.UpdateMilestone(uw, req)
	if uw.Code != http.StatusOK {
		t.Fatalf("update milestone failed: %d %s", uw.Code, uw.Body.String())
	}
	var updated MilestoneResponse
	json.NewDecoder(uw.Body).Decode(&updated)
	if updated.Name != "Renamed" || updated.TargetDate != nil {
		t.Fatalf("update result = %+v", updated)
	}

	// Delete leaves the issue ungrouped (FK SET NULL), not deleted.
	dw := httptest.NewRecorder()
	req = newRequest("DELETE", "/api/projects/"+projectID+"/milestones/"+m.ID, nil)
	req = withURLParams(req, "id", projectID, "milestoneId", m.ID)
	testHandler.DeleteMilestone(dw, req)
	if dw.Code != http.StatusNoContent {
		t.Fatalf("delete milestone failed: %d %s", dw.Code, dw.Body.String())
	}
	_, resp = getRoadmap(t, projectID)
	if len(resp.Milestones) != 0 {
		t.Fatalf("milestone should be gone, got %d", len(resp.Milestones))
	}
	if len(resp.Epics) != 2 {
		t.Fatalf("epics must survive milestone delete, got %d", len(resp.Epics))
	}
}

func TestSetIssueMilestoneRejectsOtherProject(t *testing.T) {
	projectA := createRoadmapTestProject(t, testWorkspaceID, "Roadmap Proj A")
	projectB := createRoadmapTestProject(t, testWorkspaceID, "Roadmap Proj B")
	issueA := createRoadmapTestIssue(t, testWorkspaceID, projectA, roadmapTestIssue{title: "A1", status: "todo"})
	mB := createMilestoneViaAPI(t, projectB, map[string]any{"name": "B milestone"})

	w := setIssueMilestoneViaAPI(t, issueA, &mB.ID)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for cross-project milestone, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRoadmapDependencyOrderingEndToEnd(t *testing.T) {
	projectID := createRoadmapTestProject(t, testWorkspaceID, "Roadmap Deps")
	// Positions reversed so only the dependency links can produce X, Y, Z.
	x := createRoadmapTestIssue(t, testWorkspaceID, projectID, roadmapTestIssue{title: "X", status: "todo", position: 3})
	y := createRoadmapTestIssue(t, testWorkspaceID, projectID, roadmapTestIssue{title: "Y", status: "todo", position: 2})
	z := createRoadmapTestIssue(t, testWorkspaceID, projectID, roadmapTestIssue{title: "Z", status: "todo", position: 1})

	if w := addDependencyViaAPI(t, z, y); w.Code != http.StatusCreated {
		t.Fatalf("add dep z->y failed: %d %s", w.Code, w.Body.String())
	}
	if w := addDependencyViaAPI(t, y, x); w.Code != http.StatusCreated {
		t.Fatalf("add dep y->x failed: %d %s", w.Code, w.Body.String())
	}
	// Idempotent re-create.
	if w := addDependencyViaAPI(t, y, x); w.Code != http.StatusCreated {
		t.Fatalf("repeat dep create should be idempotent 201, got %d: %s", w.Code, w.Body.String())
	}

	_, resp := getRoadmap(t, projectID)
	var titles []string
	for _, e := range resp.Epics {
		titles = append(titles, e.Title)
	}
	if len(titles) != 3 || titles[0] != "X" || titles[1] != "Y" || titles[2] != "Z" {
		t.Fatalf("dependency order = %v, want [X Y Z]", titles)
	}
	if resp.CycleDetected {
		t.Fatal("no cycle expected")
	}
	if len(resp.Epics[2].DependsOn) != 1 || resp.Epics[2].DependsOn[0] != y {
		t.Fatalf("Z depends_on = %v, want [%s]", resp.Epics[2].DependsOn, y)
	}

	// Deleting a link is idempotent and removes the edge.
	dw := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/issues/"+z+"/dependencies/"+y, nil)
	req = withURLParams(req, "id", z, "dependsOnId", y)
	testHandler.DeleteIssueDependency(dw, req)
	if dw.Code != http.StatusNoContent {
		t.Fatalf("delete dependency failed: %d", dw.Code)
	}
	_, resp = getRoadmap(t, projectID)
	for _, e := range resp.Epics {
		if e.ID == z && len(e.DependsOn) != 0 {
			t.Fatalf("Z should have no deps after delete, got %v", e.DependsOn)
		}
	}
}

func TestCreateIssueDependencyValidation(t *testing.T) {
	projectA := createRoadmapTestProject(t, testWorkspaceID, "Roadmap Val A")
	projectB := createRoadmapTestProject(t, testWorkspaceID, "Roadmap Val B")
	a1 := createRoadmapTestIssue(t, testWorkspaceID, projectA, roadmapTestIssue{title: "A1", status: "todo"})
	a2 := createRoadmapTestIssue(t, testWorkspaceID, projectA, roadmapTestIssue{title: "A2", status: "todo"})
	a3 := createRoadmapTestIssue(t, testWorkspaceID, projectA, roadmapTestIssue{title: "A3", status: "todo"})
	b1 := createRoadmapTestIssue(t, testWorkspaceID, projectB, roadmapTestIssue{title: "B1", status: "todo"})

	// Self-dependency.
	if w := addDependencyViaAPI(t, a1, a1); w.Code != http.StatusBadRequest {
		t.Fatalf("self dependency: expected 400, got %d", w.Code)
	}
	// Cross-project.
	if w := addDependencyViaAPI(t, a1, b1); w.Code != http.StatusBadRequest {
		t.Fatalf("cross-project dependency: expected 400, got %d: %s", w.Code, w.Body.String())
	}
	// Direct cycle: a1 -> a2, then a2 -> a1 must be rejected.
	if w := addDependencyViaAPI(t, a1, a2); w.Code != http.StatusCreated {
		t.Fatalf("a1->a2 failed: %d", w.Code)
	}
	if w := addDependencyViaAPI(t, a2, a1); w.Code != http.StatusBadRequest {
		t.Fatalf("direct cycle: expected 400, got %d: %s", w.Code, w.Body.String())
	}
	// Transitive cycle: a2 -> a3, then a3 -> a1 must be rejected.
	if w := addDependencyViaAPI(t, a2, a3); w.Code != http.StatusCreated {
		t.Fatalf("a2->a3 failed: %d", w.Code)
	}
	if w := addDependencyViaAPI(t, a3, a1); w.Code != http.StatusBadRequest {
		t.Fatalf("transitive cycle: expected 400, got %d: %s", w.Code, w.Body.String())
	}
	// Missing target.
	if w := addDependencyViaAPI(t, a1, "00000000-0000-0000-0000-00000000dead"); w.Code != http.StatusNotFound {
		t.Fatalf("missing target: expected 404, got %d", w.Code)
	}
}
