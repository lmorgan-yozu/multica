package handler

import (
	"sort"
	"strconv"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// RoadmapProgress is a done/total rollup of leaf issues.
type RoadmapProgress struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}

// RoadmapEpic is one top-level node on the project roadmap. Epics are
// derived, not declared: any project issue whose parent is missing or
// outside the project is a roadmap node, with progress rolled up from its
// leaf descendants (a childless epic counts itself as its only leaf).
type RoadmapEpic struct {
	ID           string          `json:"id"`
	Identifier   string          `json:"identifier"`
	Number       int32           `json:"number"`
	Title        string          `json:"title"`
	Status       string          `json:"status"`
	Priority     string          `json:"priority"`
	MilestoneID  *string         `json:"milestone_id"`
	StartDate    *string         `json:"start_date"`
	DueDate      *string         `json:"due_date"`
	Position     float64         `json:"position"`
	Progress     RoadmapProgress `json:"progress"`
	BlockedCount int             `json:"blocked_count"`
	DependsOn    []string        `json:"depends_on"`
	ChildCount   int             `json:"child_count"`
}

// RoadmapMilestone groups epics under a named checkpoint with a target
// date. Progress aggregates the member epics' leaf rollups.
type RoadmapMilestone struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	TargetDate   *string         `json:"target_date"`
	Position     float64         `json:"position"`
	Progress     RoadmapProgress `json:"progress"`
	BlockedCount int             `json:"blocked_count"`
	EpicIDs      []string        `json:"epic_ids"`
	CreatedAt    string          `json:"created_at"`
	UpdatedAt    string          `json:"updated_at"`
}

// RoadmapResponse is the projection returned by GET /api/projects/{id}/roadmap
// and `multica project roadmap`. Epics are globally ordered (dependency
// order first, then due date / start date / position fallback); milestone
// epic_ids reuse that order.
type RoadmapResponse struct {
	ProjectID    string             `json:"project_id"`
	ProjectTitle string             `json:"project_title"`
	Milestones   []RoadmapMilestone `json:"milestones"`
	Epics        []RoadmapEpic      `json:"epics"`
	// CycleDetected is the read path's defence: the write path rejects
	// cycle-creating links, but pre-existing data must degrade to a stable
	// order with the affected issues flagged, never an unstable response.
	CycleDetected bool     `json:"cycle_detected"`
	CycleIssueIDs []string `json:"cycle_issue_ids,omitempty"`
}

// roadmapNode carries per-epic rollup state during projection.
type roadmapNode struct {
	row       db.ListProjectIssuesForRoadmapRow
	done      int
	total     int
	blocked   int
	dependsOn []string
}

// buildRoadmap derives the roadmap projection from a project's issues,
// milestones, and roadmap dependency links. Pure function — all inputs are
// already tenant-scoped by the caller.
func buildRoadmap(projectID, projectTitle, issuePrefix string,
	issues []db.ListProjectIssuesForRoadmapRow,
	milestones []db.Milestone,
	deps []db.ListProjectDependencyLinksRow,
) RoadmapResponse {
	resp := RoadmapResponse{
		ProjectID:    projectID,
		ProjectTitle: projectTitle,
		Milestones:   []RoadmapMilestone{},
		Epics:        []RoadmapEpic{},
	}

	inProject := make(map[pgtype.UUID]db.ListProjectIssuesForRoadmapRow, len(issues))
	for _, i := range issues {
		inProject[i.ID] = i
	}

	// children maps a parent to its direct children, but only for parents
	// inside the project — an issue whose parent lives elsewhere is treated
	// as top-level here rather than silently dropped.
	children := map[pgtype.UUID][]pgtype.UUID{}
	var epicIDs []pgtype.UUID
	for _, i := range issues {
		if i.ParentIssueID.Valid {
			if _, ok := inProject[i.ParentIssueID]; ok {
				children[i.ParentIssueID] = append(children[i.ParentIssueID], i.ID)
				continue
			}
		}
		epicIDs = append(epicIDs, i.ID)
	}

	nodes := make(map[pgtype.UUID]*roadmapNode, len(epicIDs))
	isEpic := make(map[pgtype.UUID]bool, len(epicIDs))
	for _, id := range epicIDs {
		nodes[id] = &roadmapNode{row: inProject[id]}
		isEpic[id] = true
	}

	// Leaf rollup per epic. Iterative DFS; a node with no in-project
	// children is a leaf, and a childless epic is its own single leaf.
	// Cancelled leaves are excluded from totals entirely.
	for _, id := range epicIDs {
		n := nodes[id]
		stack := []pgtype.UUID{id}
		for len(stack) > 0 {
			cur := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			kids := children[cur]
			if len(kids) > 0 {
				stack = append(stack, kids...)
				continue
			}
			switch inProject[cur].Status {
			case "cancelled":
			case "done":
				n.done++
				n.total++
			case "blocked":
				n.blocked++
				n.total++
			default:
				n.total++
			}
		}
	}

	// Dependency edges: only epic-to-epic links participate in roadmap
	// ordering. Links touching sub-issues are out of scope for this slice.
	dependsOn := map[pgtype.UUID][]pgtype.UUID{}
	for _, d := range deps {
		if isEpic[d.IssueID] && isEpic[d.DependsOnIssueID] && d.IssueID != d.DependsOnIssueID {
			dependsOn[d.IssueID] = append(dependsOn[d.IssueID], d.DependsOnIssueID)
		}
	}
	for id, ds := range dependsOn {
		seen := map[pgtype.UUID]bool{}
		for _, d := range ds {
			if !seen[d] {
				seen[d] = true
				nodes[id].dependsOn = append(nodes[id].dependsOn, uuidToString(d))
			}
		}
		sort.Strings(nodes[id].dependsOn)
	}

	ordered, cycleIDs := orderEpics(epicIDs, nodes, dependsOn)

	milestoneByID := make(map[pgtype.UUID]bool, len(milestones))
	for _, m := range milestones {
		milestoneByID[m.ID] = true
	}

	for _, id := range ordered {
		n := nodes[id]
		e := RoadmapEpic{
			ID:           uuidToString(id),
			Identifier:   issuePrefix + "-" + strconv.Itoa(int(n.row.Number)),
			Number:       n.row.Number,
			Title:        n.row.Title,
			Status:       n.row.Status,
			Priority:     n.row.Priority,
			StartDate:    dateToPtr(n.row.StartDate),
			DueDate:      dateToPtr(n.row.DueDate),
			Position:     n.row.Position,
			Progress:     RoadmapProgress{Done: n.done, Total: n.total},
			BlockedCount: n.blocked,
			DependsOn:    []string{},
			ChildCount:   len(children[id]),
		}
		if n.dependsOn != nil {
			e.DependsOn = n.dependsOn
		}
		// Only surface milestone_id when it points at one of this project's
		// milestones — a stale reference from a project move is dropped.
		if n.row.MilestoneID.Valid && milestoneByID[n.row.MilestoneID] {
			e.MilestoneID = uuidToPtr(n.row.MilestoneID)
		}
		resp.Epics = append(resp.Epics, e)
	}

	// Milestones arrive ordered (target_date, position, created_at) from
	// SQL; epic_ids follow the global epic order so grouping and ordering
	// agree.
	epicsByMilestone := map[pgtype.UUID][]pgtype.UUID{}
	for _, id := range ordered {
		row := nodes[id].row
		if row.MilestoneID.Valid && milestoneByID[row.MilestoneID] {
			epicsByMilestone[row.MilestoneID] = append(epicsByMilestone[row.MilestoneID], id)
		}
	}
	for _, m := range milestones {
		rm := RoadmapMilestone{
			ID:          uuidToString(m.ID),
			Name:        m.Name,
			Description: m.Description,
			TargetDate:  dateToPtr(m.TargetDate),
			Position:    m.Position,
			EpicIDs:     []string{},
			CreatedAt:   timestampToString(m.CreatedAt),
			UpdatedAt:   timestampToString(m.UpdatedAt),
		}
		for _, id := range epicsByMilestone[m.ID] {
			n := nodes[id]
			rm.EpicIDs = append(rm.EpicIDs, uuidToString(id))
			rm.Progress.Done += n.done
			rm.Progress.Total += n.total
			rm.BlockedCount += n.blocked
		}
		resp.Milestones = append(resp.Milestones, rm)
	}

	if len(cycleIDs) > 0 {
		resp.CycleDetected = true
		for _, id := range cycleIDs {
			resp.CycleIssueIDs = append(resp.CycleIssueIDs, uuidToString(id))
		}
	}
	return resp
}

// orderEpics runs Kahn's algorithm over the epic dependency graph with a
// deterministic tie-break (due date, start date, position, created_at, id)
// among ready nodes. Epics left with unmet dependencies — a cycle — are
// appended in tie-break order and reported, so the response order is always
// stable and complete.
func orderEpics(epicIDs []pgtype.UUID, nodes map[pgtype.UUID]*roadmapNode, dependsOn map[pgtype.UUID][]pgtype.UUID) (ordered, cycle []pgtype.UUID) {
	less := func(a, b pgtype.UUID) bool {
		ra, rb := nodes[a].row, nodes[b].row
		if c := compareDates(ra.DueDate, rb.DueDate); c != 0 {
			return c < 0
		}
		if c := compareDates(ra.StartDate, rb.StartDate); c != 0 {
			return c < 0
		}
		if ra.Position != rb.Position {
			return ra.Position < rb.Position
		}
		if !ra.CreatedAt.Time.Equal(rb.CreatedAt.Time) {
			return ra.CreatedAt.Time.Before(rb.CreatedAt.Time)
		}
		return uuidToString(a) < uuidToString(b)
	}

	indegree := make(map[pgtype.UUID]int, len(epicIDs))
	dependents := map[pgtype.UUID][]pgtype.UUID{}
	for _, id := range epicIDs {
		indegree[id] = 0
	}
	for id, ds := range dependsOn {
		seen := map[pgtype.UUID]bool{}
		for _, d := range ds {
			if !seen[d] {
				seen[d] = true
				indegree[id]++
				dependents[d] = append(dependents[d], id)
			}
		}
	}

	ready := make([]pgtype.UUID, 0, len(epicIDs))
	for _, id := range epicIDs {
		if indegree[id] == 0 {
			ready = append(ready, id)
		}
	}
	sort.Slice(ready, func(i, j int) bool { return less(ready[i], ready[j]) })

	ordered = make([]pgtype.UUID, 0, len(epicIDs))
	for len(ready) > 0 {
		next := ready[0]
		ready = ready[1:]
		ordered = append(ordered, next)
		changed := false
		for _, dep := range dependents[next] {
			indegree[dep]--
			if indegree[dep] == 0 {
				ready = append(ready, dep)
				changed = true
			}
		}
		if changed {
			sort.Slice(ready, func(i, j int) bool { return less(ready[i], ready[j]) })
		}
	}

	if len(ordered) < len(epicIDs) {
		var remaining []pgtype.UUID
		for _, id := range epicIDs {
			if indegree[id] > 0 {
				remaining = append(remaining, id)
			}
		}
		sort.Slice(remaining, func(i, j int) bool { return less(remaining[i], remaining[j]) })
		ordered = append(ordered, remaining...)
		cycle = remaining
	}
	return ordered, cycle
}

// compareDates orders valid dates ascending with NULLs last.
func compareDates(a, b pgtype.Date) int {
	switch {
	case a.Valid && b.Valid:
		if a.Time.Before(b.Time) {
			return -1
		}
		if a.Time.After(b.Time) {
			return 1
		}
		return 0
	case a.Valid:
		return -1
	case b.Valid:
		return 1
	default:
		return 0
	}
}
