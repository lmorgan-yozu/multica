package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

// SweepDueCapResumes promotes due cap-resume schedules into queued retry tasks.
// Each task is consumed in its own transaction before any side effects run, so
// racing sweepers either see one owner or no work.
func (s *TaskService) SweepDueCapResumes(ctx context.Context, maxPerTick int32) int {
	if maxPerTick <= 0 {
		return 0
	}
	candidates, err := s.Queries.ListDueCapResumeTasks(ctx, maxPerTick)
	if err != nil {
		slog.Warn("cap resume sweep: list due tasks failed", "error", err)
		return 0
	}
	resumed := 0
	for _, candidate := range candidates {
		if s.processDueCapResume(ctx, candidate.ID) {
			resumed++
		}
	}
	return resumed
}

func (s *TaskService) processDueCapResume(ctx context.Context, taskID pgtype.UUID) bool {
	var outcome capResumeOutcome
	err := s.runInTx(ctx, func(qtx *db.Queries) error {
		task, err := qtx.ConsumeDueCapResumeTask(ctx, taskID)
		if err != nil {
			if err == pgx.ErrNoRows {
				return nil
			}
			return fmt.Errorf("consume cap resume task: %w", err)
		}
		outcome.parent = task
		if !task.IssueID.Valid {
			outcome.skipReason = "no issue"
			return nil
		}
		if task.AutopilotRunID.Valid {
			outcome.skipReason = "autopilot task"
			return nil
		}
		issue, err := qtx.GetIssue(ctx, task.IssueID)
		if err != nil {
			return fmt.Errorf("load issue: %w", err)
		}
		outcome.issue = issue
		if issue.Status == "cancelled" || issue.Status == "done" || issue.Status == "blocked" {
			outcome.skipReason = "issue terminal"
			return nil
		}
		if issue.AssigneeType.String != "agent" || !issue.AssigneeID.Valid || issue.AssigneeID.Bytes != task.AgentID.Bytes {
			outcome.skipReason = "issue reassigned"
			return nil
		}
		ws, err := qtx.GetWorkspace(ctx, issue.WorkspaceID)
		if err != nil {
			return fmt.Errorf("load workspace: %w", err)
		}
		cfg := parseSessionCapResilience(ws.Settings)
		if !cfg.Enabled {
			outcome.skipReason = "disabled"
			return nil
		}
		outcome.maxAttempts = cfg.MaxResumeAttempts
		attempts, err := qtx.CountConsecutiveCapFailuresInTaskChain(ctx, task.ID)
		if err != nil {
			return fmt.Errorf("count cap failures: %w", err)
		}
		outcome.attempt = int(attempts)
		if cfg.MaxResumeAttempts >= 0 && outcome.attempt > cfg.MaxResumeAttempts {
			updated, err := qtx.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
				ID:          issue.ID,
				Status:      "blocked",
				WorkspaceID: issue.WorkspaceID,
			})
			if err != nil {
				return fmt.Errorf("block issue: %w", err)
			}
			outcome.issue = updated
			outcome.blocked = true
			return nil
		}
		hasActive, err := qtx.HasActiveTaskForIssueAndAgent(ctx, db.HasActiveTaskForIssueAndAgentParams{
			IssueID: task.IssueID,
			AgentID: task.AgentID,
		})
		if err != nil {
			return fmt.Errorf("check active task: %w", err)
		}
		if hasActive {
			outcome.skipReason = "active task already exists"
			return nil
		}
		child, err := qtx.CreateRetryTask(ctx, task.ID)
		if err != nil {
			return fmt.Errorf("create retry task: %w", err)
		}
		outcome.child = &child
		return nil
	})
	if err != nil {
		slog.Warn("cap resume sweep: process task failed",
			"task_id", util.UUIDToString(taskID),
			"error", err)
		return false
	}
	outcome.publish(ctx, s)
	return outcome.child != nil
}

type capResumeOutcome struct {
	parent      db.AgentTaskQueue
	child       *db.AgentTaskQueue
	issue       db.Issue
	attempt     int
	maxAttempts int
	blocked     bool
	skipReason  string
}

func (o capResumeOutcome) publish(ctx context.Context, s *TaskService) {
	if o.parent.ID.Valid && o.skipReason != "" {
		slog.Info("cap resume skipped",
			"task_id", util.UUIDToString(o.parent.ID),
			"issue_id", util.UUIDToString(o.parent.IssueID),
			"reason", o.skipReason)
	}
	if o.child != nil {
		slog.Info("cap resume enqueued",
			"parent_task_id", util.UUIDToString(o.parent.ID),
			"child_task_id", util.UUIDToString(o.child.ID),
			"attempt", o.attempt,
			"max_attempts", o.maxAttempts)
		s.broadcastTaskEvent(ctx, protocol.EventTaskQueued, *o.child)
		s.NotifyTaskEnqueued(ctx, *o.child)
		s.postCapResumeSweepComment(ctx, o.issue, o.parent, o.attempt, o.maxAttempts, false)
		return
	}
	if o.blocked {
		s.broadcastIssueUpdated(o.issue)
		s.postCapResumeSweepComment(ctx, o.issue, o.parent, o.attempt, o.maxAttempts, true)
	}
}

func (s *TaskService) postCapResumeSweepComment(ctx context.Context, issue db.Issue, task db.AgentTaskQueue, attempt, maxAttempts int, blocked bool) {
	if !issue.ID.Valid {
		return
	}
	agentLabel := "The assigned agent"
	if agent, err := s.Queries.GetAgent(ctx, task.AgentID); err == nil {
		agentLabel = agent.Name
		if agent.Model.Valid && agent.Model.String != "" {
			agentLabel = fmt.Sprintf("%s (%s)", agent.Name, agent.Model.String)
		}
	}

	capKind := "usage cap"
	reason := ""
	if task.FailureReason.Valid {
		reason = task.FailureReason.String
	}
	if taskfailure.Reason(reason) == taskfailure.ReasonAgentProviderCapacityOrRateLimit {
		capKind = "capacity/rate limit"
	}

	content := fmt.Sprintf(
		"%s is resuming automatically after a provider %s (resume attempt %d of %d).",
		agentLabel, capKind, attempt, maxAttempts,
	)
	if blocked {
		content = fmt.Sprintf(
			"%s hit repeated provider caps (%d resume attempts, latest %s). The issue is now blocked so the history is explicit.",
			agentLabel, maxAttempts, time.Now().UTC().Format("2006-01-02 15:04 UTC"),
		)
	}

	comment, err := s.Queries.CreateComment(ctx, db.CreateCommentParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
		AuthorType:  "system",
		AuthorID:    pgtype.UUID{Valid: true},
		Content:     content,
		Type:        "system",
		ParentID:    pgtype.UUID{Valid: false},
	})
	if err != nil {
		slog.Warn("cap resume sweep: create system comment failed",
			"task_id", util.UUIDToString(task.ID),
			"issue_id", util.UUIDToString(issue.ID),
			"error", err)
		return
	}
	s.Bus.Publish(events.Event{
		Type:        protocol.EventCommentCreated,
		WorkspaceID: util.UUIDToString(issue.WorkspaceID),
		ActorType:   "system",
		Payload: map[string]any{
			"comment": map[string]any{
				"id":          util.UUIDToString(comment.ID),
				"issue_id":    util.UUIDToString(comment.IssueID),
				"author_type": comment.AuthorType,
				"author_id":   util.UUIDToString(comment.AuthorID),
				"content":     comment.Content,
				"type":        comment.Type,
				"parent_id":   util.UUIDToPtr(comment.ParentID),
				"created_at":  comment.CreatedAt.Time.Format("2006-01-02T15:04:05Z"),
			},
			"issue_title":  issue.Title,
			"issue_status": issue.Status,
		},
	})
}
