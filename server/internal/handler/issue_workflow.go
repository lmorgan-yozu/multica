package handler

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// advanceWorkflowOnStatusChange drives role-based handoff workflows. When an
// issue that is bound to a workflow (has an issue_workflow_run row) reaches the
// current step's advance_status, this advances the run to the next step:
// reassigning the issue to the next agent, posting a structured handoff system
// comment, and triggering the next agent. The final step instead leaves the
// issue at advance_status as a human review gate and marks the run completed.
//
// This mirrors notifyParentOfChildDone deliberately: it is a best-effort hook
// fired after a successful status update, it inserts system comments directly
// via db.Queries (bypassing the generic comment trigger), it dedupes agent
// enqueues with HasPendingTaskForIssueAndAgent, and it swallows errors —
// failing a handoff must never roll back the user's status change.
//
// The whole feature is opt-in: issues without a workflow run return at the
// first guard, so existing behaviour is untouched.
func (h *Handler) advanceWorkflowOnStatusChange(ctx context.Context, prev, issue db.Issue, actorType, actorID string) {
	run, err := h.Queries.GetIssueWorkflowRun(ctx, issue.ID)
	if err != nil {
		return // no workflow run for this issue — opt-in no-op
	}
	if run.State != "active" {
		return
	}

	step, err := h.Queries.GetWorkflowStep(ctx, run.CurrentStepID)
	if err != nil {
		slog.Warn("workflow advance: load current step failed",
			"error", err, "issue_id", uuidToString(issue.ID), "run_id", uuidToString(run.ID))
		return
	}

	if !stepShouldAdvance(run.State, prev.Status, issue.Status,
		step.AdvanceStatus, assigneeTypeString(issue), uuidToString(issue.AssigneeID), uuidToString(step.AgentID)) {
		return
	}

	steps, err := h.Queries.ListWorkflowSteps(ctx, run.WorkflowID)
	if err != nil {
		slog.Warn("workflow advance: list steps failed",
			"error", err, "workflow_id", uuidToString(run.WorkflowID))
		return
	}

	orders := make([]int, len(steps))
	for i, s := range steps {
		orders[i] = int(s.StepOrder)
	}
	nextIdx, ok := nextStepIndex(orders, int(step.StepOrder))
	if !ok {
		h.completeWorkflowRun(ctx, run, issue)
		return
	}
	h.advanceToStep(ctx, run, issue, step, steps[nextIdx])
}

// completeWorkflowRun marks the run finished and posts a human-review-gate
// system comment. The issue is left at advance_status (typically in_review),
// which is the point at which a human takes over per the vision.
func (h *Handler) completeWorkflowRun(ctx context.Context, run db.IssueWorkflowRun, issue db.Issue) {
	if err := h.Queries.SetIssueWorkflowRunState(ctx, db.SetIssueWorkflowRunStateParams{
		ID:    run.ID,
		State: "completed",
	}); err != nil {
		slog.Warn("workflow advance: mark completed failed",
			"error", err, "run_id", uuidToString(run.ID))
		return
	}
	content := "Workflow complete — the final step is done. Ready for human review."
	h.postWorkflowSystemComment(ctx, issue, content)
}

// advanceToStep reassigns the issue to the next step's agent, posts a handoff
// comment that @mentions the next agent, and triggers that agent's run.
func (h *Handler) advanceToStep(ctx context.Context, run db.IssueWorkflowRun, issue db.Issue, prevStep, nextStep db.WorkflowStep) {
	if err := h.Queries.SetIssueWorkflowRunStep(ctx, db.SetIssueWorkflowRunStepParams{
		ID:            run.ID,
		CurrentStepID: nextStep.ID,
	}); err != nil {
		slog.Warn("workflow advance: point run at next step failed",
			"error", err, "run_id", uuidToString(run.ID))
		return
	}

	// Cancel any still-queued/dispatched task for the outgoing step's agent
	// before reassigning. We reassign via a narrow query (not UpdateIssue), so
	// UpdateIssue's own CancelTasksForIssue-on-assignee-change never runs here;
	// without this, a stale task for the previous agent could execute against
	// an issue that has already moved on to the next step.
	if err := h.TaskService.CancelTasksForIssue(ctx, issue.ID); err != nil {
		slog.Warn("workflow advance: cancel outgoing tasks failed",
			"error", err, "issue_id", uuidToString(issue.ID))
	}

	updated, err := h.Queries.AssignIssueToWorkflowStep(ctx, db.AssignIssueToWorkflowStepParams{
		ID:         issue.ID,
		AssigneeID: nextStep.AgentID,
		Status:     nextStep.StartStatus,
	})
	if err != nil {
		slog.Warn("workflow advance: reassign issue failed",
			"error", err, "issue_id", uuidToString(issue.ID), "next_agent_id", uuidToString(nextStep.AgentID))
		return
	}

	// Publish so connected clients see the reassignment + status change.
	prefix := h.getIssuePrefix(ctx, updated.WorkspaceID)
	h.publish(protocol.EventIssueUpdated, uuidToString(updated.WorkspaceID), "system", "", map[string]any{
		"issue":              issueToResponse(updated, prefix),
		"assignee_changed":   true,
		"status_changed":     true,
		"prev_assignee_type": textToPtr(issue.AssigneeType),
		"prev_assignee_id":   uuidToPtr(issue.AssigneeID),
		"prev_status":        issue.Status,
	})

	// Record the structured handoff before posting the comment: the handoff
	// row is the source of truth for the transition; the system comment is
	// only the timeline notification.
	h.recordWorkflowHandoff(ctx, run, updated, prevStep, nextStep)

	mention := h.buildAgentMention(ctx, updated.WorkspaceID, nextStep.AgentID)
	content := fmt.Sprintf(
		"%sWorkflow handoff: %s is done. You're up next for %s. The full context is in this issue's thread and history.",
		mention, stepLabel(prevStep), stepLabel(nextStep),
	)
	comment := h.postWorkflowSystemComment(ctx, updated, content)

	// Trigger the next agent, mirroring the child-done dispatch guards.
	h.triggerWorkflowAgent(ctx, updated, nextStep.AgentID, comment)
}

// recordWorkflowHandoff writes the structured handoff record for a workflow
// advancement (see migration 118_issue_handoff). Like everything else on the
// advance path it is best-effort: errors are logged and swallowed, never
// failing the user's status change.
//
// Two shapes, so the issue's latest handoff stays the richest one:
//   - The outgoing agent already wrote its own handoff for this stint (the
//     issue's latest handoff is authored by the outgoing step's agent and not
//     yet linked to any workflow run): adopt it — stamp the run/step linkage
//     and the workflow's routing decision onto that record rather than
//     inserting a thin duplicate that would shadow it as "latest".
//   - Otherwise: insert a synthesized record carrying the facts the workflow
//     knows — author (outgoing step's agent), next assignee (next step's
//     agent), run/step linkage, and the outgoing agent's task where available.
func (h *Handler) recordWorkflowHandoff(ctx context.Context, run db.IssueWorkflowRun, issue db.Issue, prevStep, nextStep db.WorkflowStep) {
	taskID, err := h.Queries.GetLatestTaskIDForIssueAndAgent(ctx, db.GetLatestTaskIDForIssueAndAgentParams{
		IssueID: issue.ID,
		AgentID: prevStep.AgentID,
	})
	if err != nil {
		taskID = pgtype.UUID{Valid: false} // no task linkage — fine
	}

	nextAssigneeType := pgtype.Text{String: "agent", Valid: true}

	latest, err := h.Queries.GetLatestIssueHandoff(ctx, db.GetLatestIssueHandoffParams{
		WorkspaceID: issue.WorkspaceID,
		IssueID:     issue.ID,
	})
	if err == nil && !latest.WorkflowRunID.Valid &&
		latest.AuthorType.Valid && latest.AuthorType.String == "agent" &&
		latest.AuthorID == prevStep.AgentID {
		_, linkErr := h.Queries.LinkIssueHandoffToWorkflow(ctx, db.LinkIssueHandoffToWorkflowParams{
			ID:               latest.ID,
			WorkflowRunID:    run.ID,
			WorkflowStepID:   prevStep.ID,
			NextAssigneeType: nextAssigneeType,
			NextAssigneeID:   nextStep.AgentID,
			TaskID:           taskID,
		})
		if linkErr == nil {
			return
		}
		slog.Warn("workflow advance: link agent handoff to workflow failed, inserting synthesized record",
			"error", linkErr, "handoff_id", uuidToString(latest.ID), "run_id", uuidToString(run.ID))
	}

	if _, err := h.Queries.CreateIssueHandoff(ctx, db.CreateIssueHandoffParams{
		WorkspaceID:      issue.WorkspaceID,
		IssueID:          issue.ID,
		TaskID:           taskID,
		AuthorType:       pgtype.Text{String: "agent", Valid: true},
		AuthorID:         prevStep.AgentID,
		NextAssigneeType: nextAssigneeType,
		NextAssigneeID:   nextStep.AgentID,
		WorkflowRunID:    run.ID,
		WorkflowStepID:   prevStep.ID,
		WorkCompleted:    fmt.Sprintf("%s is done — the issue reached %q and the workflow advanced.", stepLabel(prevStep), prevStep.AdvanceStatus),
		WorkRemaining:    fmt.Sprintf("%s is next. The work itself is defined by the issue description and comment thread.", stepLabel(nextStep)),
		DecisionsMade:    "",
		Uncertainties:    "",
	}); err != nil {
		slog.Warn("workflow advance: create handoff record failed",
			"error", err, "issue_id", uuidToString(issue.ID), "run_id", uuidToString(run.ID))
	}
}

// triggerWorkflowAgent enqueues a task for the next step's agent, applying the
// same readiness + idempotency guards the child-done path uses.
func (h *Handler) triggerWorkflowAgent(ctx context.Context, issue db.Issue, agentID pgtype.UUID, triggerComment db.Comment) {
	agent, err := h.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{
		ID:          agentID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil || !agent.RuntimeID.Valid || agent.ArchivedAt.Valid {
		return
	}

	hasPending, err := h.Queries.HasPendingTaskForIssueAndAgent(ctx, db.HasPendingTaskForIssueAndAgentParams{
		IssueID: issue.ID,
		AgentID: agentID,
	})
	if err != nil || hasPending {
		return
	}

	triggerID := pgtype.UUID{Valid: false}
	if triggerComment.ID.Valid {
		triggerID = triggerComment.ID
	}
	if _, err := h.TaskService.EnqueueTaskForMention(ctx, issue, agentID, triggerID); err != nil {
		slog.Warn("workflow advance: enqueue next agent task failed",
			"error", err, "issue_id", uuidToString(issue.ID), "agent_id", uuidToString(agentID))
	}
}

// postWorkflowSystemComment inserts a top-level system comment and publishes
// it, mirroring the child-done notification's direct-insert approach so the
// generic comment trigger path is bypassed.
func (h *Handler) postWorkflowSystemComment(ctx context.Context, issue db.Issue, content string) db.Comment {
	comment, err := h.Queries.CreateComment(ctx, db.CreateCommentParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
		AuthorType:  "system",
		AuthorID:    pgtype.UUID{Valid: true},
		Content:     content,
		Type:        "system",
		ParentID:    pgtype.UUID{Valid: false},
	})
	if err != nil {
		slog.Warn("workflow advance: create system comment failed",
			"error", err, "issue_id", uuidToString(issue.ID))
		return db.Comment{}
	}
	h.publish(protocol.EventCommentCreated, uuidToString(issue.WorkspaceID), "system", "", map[string]any{
		"comment":             commentToResponse(comment, nil, nil),
		"issue_title":         issue.Title,
		"issue_assignee_type": textToPtr(issue.AssigneeType),
		"issue_assignee_id":   uuidToPtr(issue.AssigneeID),
		"issue_status":        issue.Status,
	})
	return comment
}

// buildAgentMention returns a "[@Name](mention://agent/<id>) " prefix for the
// given agent, or "" if the agent can't be resolved.
func (h *Handler) buildAgentMention(ctx context.Context, workspaceID, agentID pgtype.UUID) string {
	agent, err := h.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{
		ID:          agentID,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		return ""
	}
	return fmt.Sprintf("[@%s](mention://agent/%s) ", sanitizeMentionLabel(agent.Name), uuidToString(agentID))
}

// stepLabel renders a human-readable label for a workflow step.
func stepLabel(step db.WorkflowStep) string {
	if step.Name != "" {
		return fmt.Sprintf("step %d (%s)", step.StepOrder, step.Name)
	}
	return fmt.Sprintf("step %d", step.StepOrder)
}

func assigneeTypeString(issue db.Issue) string {
	if issue.AssigneeType.Valid {
		return issue.AssigneeType.String
	}
	return ""
}

// stepShouldAdvance reports whether a status transition on a workflow-bound
// issue should advance the workflow. It fires only on the exact transition
// INTO the step's advance_status (idempotent — re-saving the same status does
// nothing), only while the run is active, and only while the issue is still
// assigned to the step's own agent (so a human reassigning mid-flight, or an
// unrelated status change, does not advance the chain).
func stepShouldAdvance(runState, prevStatus, newStatus, advanceStatus, assigneeType, assigneeID, stepAgentID string) bool {
	if runState != "active" {
		return false
	}
	if assigneeType != "agent" || assigneeID == "" || assigneeID != stepAgentID {
		return false
	}
	if newStatus != advanceStatus {
		return false
	}
	if prevStatus == advanceStatus {
		return false // already there; not a transition
	}
	return true
}

// nextStepIndex returns the index into orders of the step that follows the
// current step_order — the smallest order strictly greater than current.
// ok=false means the current step is the last one (workflow terminal). orders
// need not be sorted.
func nextStepIndex(orders []int, current int) (int, bool) {
	bestIdx := -1
	bestOrder := 0
	for i, o := range orders {
		if o <= current {
			continue
		}
		if bestIdx == -1 || o < bestOrder {
			bestIdx = i
			bestOrder = o
		}
	}
	return bestIdx, bestIdx != -1
}
