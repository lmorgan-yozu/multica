package handler

import (
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Pure projection tests — no database required.

type rmIssueOpt func(*db.ListProjectIssuesForRoadmapRow)

func rmWithParent(parentID pgtype.UUID) rmIssueOpt {
	return func(r *db.ListProjectIssuesForRoadmapRow) { r.ParentIssueID = parentID }
}

func rmWithMilestone(milestoneID pgtype.UUID) rmIssueOpt {
	return func(r *db.ListProjectIssuesForRoadmapRow) { r.MilestoneID = milestoneID }
}

func rmWithDueDate(date string) rmIssueOpt {
	return func(r *db.ListProjectIssuesForRoadmapRow) { r.DueDate = mustDate(date) }
}

func rmWithPosition(pos float64) rmIssueOpt {
	return func(r *db.ListProjectIssuesForRoadmapRow) { r.Position = pos }
}

func mustDate(s string) pgtype.Date {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return pgtype.Date{Time: t, Valid: true}
}

var rmCreatedAt = pgtype.Timestamptz{Time: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), Valid: true}

func rmIssue(id pgtype.UUID, number int32, title, status string, opts ...rmIssueOpt) db.ListProjectIssuesForRoadmapRow {
	row := db.ListProjectIssuesForRoadmapRow{
		ID:        id,
		Number:    number,
		Title:     title,
		Status:    status,
		Priority:  "none",
		CreatedAt: rmCreatedAt,
	}
	for _, opt := range opts {
		opt(&row)
	}
	return row
}

func rmUUID(n byte) pgtype.UUID {
	var b [16]byte
	b[15] = n
	return pgtype.UUID{Bytes: b, Valid: true}
}

func rmDep(issueID, dependsOn pgtype.UUID) db.ListProjectDependencyLinksRow {
	return db.ListProjectDependencyLinksRow{IssueID: issueID, DependsOnIssueID: dependsOn}
}

func rmMilestone(id pgtype.UUID, name, targetDate string, position float64) db.Milestone {
	m := db.Milestone{
		ID:        id,
		Name:      name,
		Position:  position,
		CreatedAt: rmCreatedAt,
		UpdatedAt: rmCreatedAt,
	}
	if targetDate != "" {
		m.TargetDate = mustDate(targetDate)
	}
	return m
}

func epicOrder(t *testing.T, resp RoadmapResponse) []string {
	t.Helper()
	out := make([]string, 0, len(resp.Epics))
	for _, e := range resp.Epics {
		out = append(out, e.Title)
	}
	return out
}

func TestBuildRoadmapEmptyProject(t *testing.T) {
	resp := buildRoadmap("pid", "Empty", "ADA", nil, nil, nil)
	if resp.ProjectID != "pid" || resp.ProjectTitle != "Empty" {
		t.Fatalf("unexpected project fields: %+v", resp)
	}
	if resp.Epics == nil || len(resp.Epics) != 0 {
		t.Fatalf("expected empty non-nil epics, got %#v", resp.Epics)
	}
	if resp.Milestones == nil || len(resp.Milestones) != 0 {
		t.Fatalf("expected empty non-nil milestones, got %#v", resp.Milestones)
	}
	if resp.CycleDetected {
		t.Fatal("empty project must not report a cycle")
	}
}

func TestBuildRoadmapChildlessEpicIsItsOwnLeaf(t *testing.T) {
	issues := []db.ListProjectIssuesForRoadmapRow{
		rmIssue(rmUUID(1), 1, "Solo todo", "todo"),
		rmIssue(rmUUID(2), 2, "Solo done", "done"),
	}
	resp := buildRoadmap("pid", "P", "ADA", issues, nil, nil)
	if len(resp.Epics) != 2 {
		t.Fatalf("expected 2 epics, got %d", len(resp.Epics))
	}
	byTitle := map[string]RoadmapEpic{}
	for _, e := range resp.Epics {
		byTitle[e.Title] = e
	}
	if p := byTitle["Solo todo"].Progress; p != (RoadmapProgress{Done: 0, Total: 1}) {
		t.Fatalf("solo todo progress = %+v", p)
	}
	if p := byTitle["Solo done"].Progress; p != (RoadmapProgress{Done: 1, Total: 1}) {
		t.Fatalf("solo done progress = %+v", p)
	}
	if byTitle["Solo todo"].Identifier != "ADA-1" {
		t.Fatalf("identifier = %q", byTitle["Solo todo"].Identifier)
	}
}

func TestBuildRoadmapLeafRollupNestedTree(t *testing.T) {
	epic, childDone, childMid, grandDone, grandBlocked, cancelled :=
		rmUUID(1), rmUUID(2), rmUUID(3), rmUUID(4), rmUUID(5), rmUUID(6)
	issues := []db.ListProjectIssuesForRoadmapRow{
		rmIssue(epic, 1, "Epic", "in_progress"),
		rmIssue(childDone, 2, "Child done", "done", rmWithParent(epic)),
		// Mid node has children, so it is NOT a leaf and must not count.
		rmIssue(childMid, 3, "Child mid", "in_progress", rmWithParent(epic)),
		rmIssue(grandDone, 4, "Grandchild done", "done", rmWithParent(childMid)),
		rmIssue(grandBlocked, 5, "Grandchild blocked", "blocked", rmWithParent(childMid)),
		// Cancelled leaves are excluded from totals entirely.
		rmIssue(cancelled, 6, "Cancelled leaf", "cancelled", rmWithParent(epic)),
	}
	resp := buildRoadmap("pid", "P", "ADA", issues, nil, nil)
	if len(resp.Epics) != 1 {
		t.Fatalf("expected 1 epic, got %d: %v", len(resp.Epics), epicOrder(t, resp))
	}
	e := resp.Epics[0]
	if e.Progress != (RoadmapProgress{Done: 2, Total: 3}) {
		t.Fatalf("progress = %+v, want done 2 / total 3", e.Progress)
	}
	if e.BlockedCount != 1 {
		t.Fatalf("blocked count = %d, want 1", e.BlockedCount)
	}
	if e.ChildCount != 3 {
		t.Fatalf("child count = %d, want 3 direct children", e.ChildCount)
	}
}

func TestBuildRoadmapDanglingParentTreatedAsEpic(t *testing.T) {
	outsideParent := rmUUID(99) // not part of the project's issue set
	issues := []db.ListProjectIssuesForRoadmapRow{
		rmIssue(rmUUID(1), 1, "Adopted top-level", "todo", rmWithParent(outsideParent)),
	}
	resp := buildRoadmap("pid", "P", "ADA", issues, nil, nil)
	if len(resp.Epics) != 1 || resp.Epics[0].Title != "Adopted top-level" {
		t.Fatalf("expected dangling-parent issue promoted to epic, got %v", epicOrder(t, resp))
	}
}

func TestBuildRoadmapDependencyOrdering(t *testing.T) {
	x, y, z := rmUUID(1), rmUUID(2), rmUUID(3)
	// Positions deliberately reversed so position alone would give Z, Y, X.
	issues := []db.ListProjectIssuesForRoadmapRow{
		rmIssue(x, 1, "X", "todo", rmWithPosition(3)),
		rmIssue(y, 2, "Y", "todo", rmWithPosition(2)),
		rmIssue(z, 3, "Z", "todo", rmWithPosition(1)),
	}
	deps := []db.ListProjectDependencyLinksRow{
		rmDep(z, y), // Z depends on Y
		rmDep(y, x), // Y depends on X
	}
	resp := buildRoadmap("pid", "P", "ADA", issues, nil, deps)
	if got := epicOrder(t, resp); !reflect.DeepEqual(got, []string{"X", "Y", "Z"}) {
		t.Fatalf("order = %v, want X Y Z", got)
	}
	if resp.CycleDetected {
		t.Fatal("no cycle expected")
	}
	// depends_on ids surface on the dependent epic.
	if got := resp.Epics[2].DependsOn; !reflect.DeepEqual(got, []string{uuidToString(y)}) {
		t.Fatalf("Z depends_on = %v", got)
	}
}

func TestBuildRoadmapTieBreakDueDateThenPosition(t *testing.T) {
	a, b, c := rmUUID(1), rmUUID(2), rmUUID(3)
	issues := []db.ListProjectIssuesForRoadmapRow{
		// No dependencies: order falls to due date, then position.
		rmIssue(a, 1, "NoDueHighPos", "todo", rmWithPosition(5)),
		rmIssue(b, 2, "DueLate", "todo", rmWithDueDate("2026-07-01"), rmWithPosition(9)),
		rmIssue(c, 3, "DueSoon", "todo", rmWithDueDate("2026-06-15"), rmWithPosition(9)),
	}
	resp := buildRoadmap("pid", "P", "ADA", issues, nil, nil)
	if got := epicOrder(t, resp); !reflect.DeepEqual(got, []string{"DueSoon", "DueLate", "NoDueHighPos"}) {
		t.Fatalf("order = %v", got)
	}
}

func TestBuildRoadmapCycleFallback(t *testing.T) {
	a, b, c := rmUUID(1), rmUUID(2), rmUUID(3)
	issues := []db.ListProjectIssuesForRoadmapRow{
		rmIssue(a, 1, "A", "todo", rmWithPosition(1)),
		rmIssue(b, 2, "B", "todo", rmWithPosition(2)),
		rmIssue(c, 3, "Free", "todo", rmWithPosition(3)),
	}
	deps := []db.ListProjectDependencyLinksRow{
		rmDep(a, b),
		rmDep(b, a), // cycle A <-> B
	}
	resp := buildRoadmap("pid", "P", "ADA", issues, nil, deps)
	if !resp.CycleDetected {
		t.Fatal("expected cycle_detected")
	}
	if len(resp.Epics) != 3 {
		t.Fatalf("cycle must not drop epics: got %d", len(resp.Epics))
	}
	// Free has no unmet dependencies so it sorts ahead of the cycle members,
	// which are appended in deterministic tie-break order.
	if got := epicOrder(t, resp); !reflect.DeepEqual(got, []string{"Free", "A", "B"}) {
		t.Fatalf("order = %v", got)
	}
	wantCycle := []string{uuidToString(a), uuidToString(b)}
	if !reflect.DeepEqual(resp.CycleIssueIDs, wantCycle) {
		t.Fatalf("cycle ids = %v, want %v", resp.CycleIssueIDs, wantCycle)
	}
}

func TestBuildRoadmapMilestoneGroupingAndProgress(t *testing.T) {
	m1, m2 := rmUUID(10), rmUUID(11)
	otherProjectMilestone := rmUUID(12)
	e1, e2, e3, leaf := rmUUID(1), rmUUID(2), rmUUID(3), rmUUID(4)
	issues := []db.ListProjectIssuesForRoadmapRow{
		rmIssue(e1, 1, "E1", "done", rmWithMilestone(m1)),
		rmIssue(e2, 2, "E2", "in_progress", rmWithMilestone(m1)),
		rmIssue(leaf, 4, "E2 leaf", "blocked", rmWithParent(e2)),
		rmIssue(e3, 3, "E3", "todo", rmWithMilestone(otherProjectMilestone)),
	}
	milestones := []db.Milestone{
		rmMilestone(m1, "First", "2026-06-20", 0),
		rmMilestone(m2, "Second (empty)", "2026-07-20", 1),
	}
	resp := buildRoadmap("pid", "P", "ADA", issues, milestones, nil)
	if len(resp.Milestones) != 2 {
		t.Fatalf("expected 2 milestones, got %d", len(resp.Milestones))
	}
	first := resp.Milestones[0]
	if first.Name != "First" {
		t.Fatalf("milestone order wrong: %q first", first.Name)
	}
	// E1 contributes 1/1, E2 contributes 0/1 (single blocked leaf).
	if first.Progress != (RoadmapProgress{Done: 1, Total: 2}) {
		t.Fatalf("milestone progress = %+v", first.Progress)
	}
	if first.BlockedCount != 1 {
		t.Fatalf("milestone blocked = %d", first.BlockedCount)
	}
	if !reflect.DeepEqual(first.EpicIDs, []string{uuidToString(e1), uuidToString(e2)}) {
		t.Fatalf("milestone epic_ids = %v", first.EpicIDs)
	}
	second := resp.Milestones[1]
	if len(second.EpicIDs) != 0 || second.Progress.Total != 0 {
		t.Fatalf("empty milestone should have no epics/progress: %+v", second)
	}
	// A milestone_id pointing outside this project's milestones is dropped.
	for _, e := range resp.Epics {
		if e.Title == "E3" && e.MilestoneID != nil {
			t.Fatalf("E3 milestone_id should be nil, got %v", *e.MilestoneID)
		}
		if e.Title == "E1" && (e.MilestoneID == nil || *e.MilestoneID != uuidToString(m1)) {
			t.Fatalf("E1 milestone_id wrong: %v", e.MilestoneID)
		}
	}
}

func TestBuildRoadmapIgnoresNonEpicDependencyLinks(t *testing.T) {
	epicA, epicB, childOfB := rmUUID(1), rmUUID(2), rmUUID(3)
	issues := []db.ListProjectIssuesForRoadmapRow{
		rmIssue(epicA, 1, "A", "todo"),
		rmIssue(epicB, 2, "B", "todo"),
		rmIssue(childOfB, 3, "B child", "todo", rmWithParent(epicB)),
	}
	deps := []db.ListProjectDependencyLinksRow{
		rmDep(epicA, childOfB), // one end is not an epic — ignored
	}
	resp := buildRoadmap("pid", "P", "ADA", issues, nil, deps)
	for _, e := range resp.Epics {
		if len(e.DependsOn) != 0 {
			t.Fatalf("expected no epic-level depends_on, got %v on %s", e.DependsOn, e.Title)
		}
	}
	if resp.CycleDetected {
		t.Fatal("no cycle expected")
	}
}
