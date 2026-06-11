package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

// ADA-44 (session-cap resilience, slice 1): when a task fails because the
// provider capped it — usage/session quota or capacity/rate limit — record
// when work can resume so slice 2's scheduler sweep can re-enqueue it, and
// tell the issue what happened in plain language instead of a raw provider
// error dump. Slice 1 records only; nothing here re-enqueues or re-routes.

// Defaults for the workspace settings key `session_cap_resilience`. The
// feature is on by default (row-presence opt-in would force every existing
// workspace to opt in to a pure-observability column; defaulting on matches
// the auto-link precedent in workspaceAutoLinkPRsEnabled).
const (
	capResumeDefaultBackoffMinutes = 60
	capResumeDefaultMaxAttempts    = 3
)

// sessionCapResilienceSettings is the parsed form of the workspace
// `settings.session_cap_resilience` JSONB key:
//
//	{"enabled": bool, "default_backoff_minutes": int, "max_resume_attempts": int}
//
// MaxResumeAttempts is consumed by slice 2's re-enqueue sweep; the shape is
// defined now so workspaces can pre-configure it.
type sessionCapResilienceSettings struct {
	Enabled               bool
	DefaultBackoffMinutes int
	MaxResumeAttempts     int
}

// parseSessionCapResilience reads the session_cap_resilience key out of a
// workspace settings JSONB blob, falling back to defaults for anything
// missing, malformed, or out of range. Mirrors the pointer-field pattern of
// handler.workspaceAutoLinkPRsEnabled so "absent" and "explicitly false"
// stay distinguishable.
func parseSessionCapResilience(raw []byte) sessionCapResilienceSettings {
	out := sessionCapResilienceSettings{
		Enabled:               true,
		DefaultBackoffMinutes: capResumeDefaultBackoffMinutes,
		MaxResumeAttempts:     capResumeDefaultMaxAttempts,
	}
	if len(raw) == 0 {
		return out
	}
	var s struct {
		SessionCapResilience *struct {
			Enabled               *bool `json:"enabled"`
			DefaultBackoffMinutes *int  `json:"default_backoff_minutes"`
			MaxResumeAttempts     *int  `json:"max_resume_attempts"`
		} `json:"session_cap_resilience"`
	}
	if err := json.Unmarshal(raw, &s); err != nil || s.SessionCapResilience == nil {
		return out
	}
	cfg := s.SessionCapResilience
	if cfg.Enabled != nil {
		out.Enabled = *cfg.Enabled
	}
	if cfg.DefaultBackoffMinutes != nil && *cfg.DefaultBackoffMinutes > 0 {
		out.DefaultBackoffMinutes = *cfg.DefaultBackoffMinutes
	}
	if cfg.MaxResumeAttempts != nil && *cfg.MaxResumeAttempts >= 0 {
		out.MaxResumeAttempts = *cfg.MaxResumeAttempts
	}
	return out
}

// isCapFailureReason reports whether the classified failure_reason is one of
// the two cap-class reasons. These two ARE the cap class — the taxonomy in
// pkg/taskfailure is kept in lock-step with the MUL-1949 SQL backfill and
// must not grow a new reason for this feature.
func isCapFailureReason(reason string) bool {
	switch taskfailure.Reason(reason) {
	case taskfailure.ReasonAgentProviderQuotaLimit,
		taskfailure.ReasonAgentProviderCapacityOrRateLimit:
		return true
	default:
		return false
	}
}

// capResumeTime computes the resume schedule for a capped failure: the
// provider's parsed reset time when the error text carries an unambiguous
// one, otherwise now + the configured default backoff. The result is never
// earlier than now, and (per AC2) never earlier than a parsed reset time —
// a stale past timestamp clamps to now, which trivially satisfies both.
// The second return reports whether a provider reset time was used.
func capResumeTime(errMsg string, now time.Time, cfg sessionCapResilienceSettings) (time.Time, bool) {
	if reset, ok := taskfailure.ParseResetAt(errMsg, now); ok {
		if reset.Before(now) {
			return now, true
		}
		return reset, true
	}
	return now.Add(time.Duration(cfg.DefaultBackoffMinutes) * time.Minute), false
}

// maybeScheduleCapResume runs the slice-1 cap bookkeeping for a
// freshly-failed task: persist resume_at and post one plain-language system
// comment on the linked issue. Returns true when it handled the failure —
// FailTask then skips the generic raw-error comment so a capped attempt
// produces exactly one system comment (AC3) rather than a provider error
// dump plus a cap notice.
//
// Best-effort by contract (same stance as the hooks in
// handler/issue_workflow.go): the failure write has already committed, and
// nothing here may block or fail it. Every early return below is a
// deliberate "behave exactly as today" path:
//   - non-cap reasons, no linked issue (AC4),
//   - autopilot tasks — the autopilot scheduler owns its own cadence, same
//     rule as MaybeRetryFailedTask (AC6),
//   - workspace opted out via session_cap_resilience.enabled=false (AC5).
func (s *TaskService) maybeScheduleCapResume(ctx context.Context, task db.AgentTaskQueue, reason, errMsg string) bool {
	if !isCapFailureReason(reason) || !task.IssueID.Valid || task.AutopilotRunID.Valid {
		return false
	}
	issue, err := s.Queries.GetIssue(ctx, task.IssueID)
	if err != nil {
		slog.Warn("cap resume: load issue failed",
			"task_id", util.UUIDToString(task.ID),
			"issue_id", util.UUIDToString(task.IssueID),
			"error", err)
		return false
	}
	ws, err := s.Queries.GetWorkspace(ctx, issue.WorkspaceID)
	if err != nil {
		slog.Warn("cap resume: load workspace failed",
			"task_id", util.UUIDToString(task.ID),
			"workspace_id", util.UUIDToString(issue.WorkspaceID),
			"error", err)
		return false
	}
	cfg := parseSessionCapResilience(ws.Settings)
	if !cfg.Enabled {
		return false
	}

	now := time.Now()
	resumeAt, parsedReset := capResumeTime(errMsg, now, cfg)
	if err := s.Queries.SetAgentTaskResumeAt(ctx, db.SetAgentTaskResumeAtParams{
		ID:       task.ID,
		ResumeAt: pgtype.Timestamptz{Time: resumeAt, Valid: true},
	}); err != nil {
		// Without a persisted resume_at there is nothing for slice 2 to act
		// on — fall back to today's behaviour (raw-error comment) rather
		// than posting a "resuming at T" promise we didn't record.
		slog.Warn("cap resume: persist resume_at failed",
			"task_id", util.UUIDToString(task.ID),
			"error", err)
		return false
	}

	slog.Info("cap resume scheduled",
		"task_id", util.UUIDToString(task.ID),
		"issue_id", util.UUIDToString(task.IssueID),
		"failure_reason", reason,
		"resume_at", resumeAt.UTC().Format(time.RFC3339),
		"parsed_reset", parsedReset)

	s.postCapResumeComment(ctx, issue, task, reason, resumeAt, parsedReset)
	return true
}

// postCapResumeComment writes the single system comment for a capped
// attempt: which agent/model hit a cap, when work resumes, no human action
// needed. author_type='system' mirrors handler.postWorkflowSystemComment —
// the cap is a platform observation, not something the agent said.
func (s *TaskService) postCapResumeComment(ctx context.Context, issue db.Issue, task db.AgentTaskQueue, reason string, resumeAt time.Time, parsedReset bool) {
	agentLabel := "The assigned agent"
	if agent, err := s.Queries.GetAgent(ctx, task.AgentID); err == nil {
		agentLabel = agent.Name
		if agent.Model.Valid && agent.Model.String != "" {
			agentLabel = fmt.Sprintf("%s (%s)", agent.Name, agent.Model.String)
		}
	}

	capKind := "usage cap"
	if taskfailure.Reason(reason) == taskfailure.ReasonAgentProviderCapacityOrRateLimit {
		capKind = "capacity/rate limit"
	}
	when := resumeAt.UTC().Format("2006-01-02 15:04 UTC")
	source := "provider-reported reset time"
	if !parsedReset {
		source = "default backoff"
	}
	content := fmt.Sprintf(
		"%s hit a provider %s. Work is scheduled to resume at %s (%s). No human action needed.",
		agentLabel, capKind, when, source,
	)

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
		slog.Warn("cap resume: create system comment failed",
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
