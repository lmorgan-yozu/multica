package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var ErrIssueBrakeActive = errors.New("issue loop brake is active")

type IssueLoopBrakeConfig struct {
	ID                  string  `json:"id"`
	WorkspaceID         string  `json:"workspace_id"`
	ProjectID           *string `json:"project_id,omitempty"`
	Enabled             bool    `json:"enabled"`
	WindowMinutes       int32   `json:"window_minutes"`
	MinRunCount         int32   `json:"min_run_count"`
	MinTotalTokens      int64   `json:"min_total_tokens"`
	MinEstimatedCostUSD float64 `json:"min_estimated_cost_usd"`
	CreatedAt           string  `json:"created_at"`
	UpdatedAt           string  `json:"updated_at"`
}

type UpsertIssueLoopBrakeConfigParams struct {
	WorkspaceID         pgtype.UUID
	ProjectID           pgtype.UUID
	Enabled             bool
	WindowMinutes       int32
	MinRunCount         int32
	MinTotalTokens      int64
	MinEstimatedCostUSD float64
}

type IssueLoopBrakeEvidence struct {
	WindowStartedAt        time.Time `json:"window_started_at"`
	WindowEndedAt          time.Time `json:"window_ended_at"`
	RunCount               int32     `json:"run_count"`
	TotalInputTokens       int64     `json:"total_input_tokens"`
	TotalOutputTokens      int64     `json:"total_output_tokens"`
	TotalCacheReadTokens   int64     `json:"total_cache_read_tokens"`
	TotalCacheWriteTokens  int64     `json:"total_cache_write_tokens"`
	TotalTokens            int64     `json:"total_tokens"`
	EstimatedCostUSD       float64   `json:"estimated_cost_usd"`
	MissingProgressSignals []string  `json:"missing_progress_signals"`
	ExistingQueuedTasks    int32     `json:"existing_queued_tasks"`
	ExistingRunningTasks   int32     `json:"existing_running_tasks"`
}

type IssueLoopBrake struct {
	IssueID           string         `json:"issue_id"`
	WorkspaceID       string         `json:"workspace_id"`
	ProjectID         *string        `json:"project_id,omitempty"`
	State             string         `json:"state"`
	Reason            string         `json:"reason"`
	Evidence          map[string]any `json:"evidence"`
	WindowStartedAt   string         `json:"window_started_at"`
	WindowEndedAt     string         `json:"window_ended_at"`
	TriggeredAt       string         `json:"triggered_at"`
	TriggeredByTaskID *string        `json:"triggered_by_task_id,omitempty"`
	ClearedAt         *string        `json:"cleared_at,omitempty"`
	ClearedByType     string         `json:"cleared_by_type,omitempty"`
	ClearedByID       *string        `json:"cleared_by_id,omitempty"`
	ClearReason       string         `json:"clear_reason,omitempty"`
}

func (s *TaskService) UpsertIssueLoopBrakeConfig(ctx context.Context, p UpsertIssueLoopBrakeConfigParams) (IssueLoopBrakeConfig, error) {
	if p.WindowMinutes <= 0 {
		return IssueLoopBrakeConfig{}, fmt.Errorf("window_minutes must be positive")
	}
	if p.MinRunCount <= 1 {
		return IssueLoopBrakeConfig{}, fmt.Errorf("min_run_count must be greater than 1")
	}
	if p.MinTotalTokens < 0 || p.MinEstimatedCostUSD < 0 {
		return IssueLoopBrakeConfig{}, fmt.Errorf("thresholds must be non-negative")
	}

	if !p.ProjectID.Valid {
		row := s.rawDB().QueryRow(ctx, `
INSERT INTO issue_loop_brake_config (
	workspace_id, project_id, enabled, window_minutes, min_run_count,
	min_total_tokens, min_estimated_cost_usd, updated_at
)
VALUES ($1, NULL, $2, $3, $4, $5, $6, now())
ON CONFLICT (workspace_id) WHERE project_id IS NULL DO UPDATE SET
	enabled = EXCLUDED.enabled,
	window_minutes = EXCLUDED.window_minutes,
	min_run_count = EXCLUDED.min_run_count,
	min_total_tokens = EXCLUDED.min_total_tokens,
	min_estimated_cost_usd = EXCLUDED.min_estimated_cost_usd,
	updated_at = now()
RETURNING id, workspace_id, project_id, enabled, window_minutes, min_run_count,
	min_total_tokens, min_estimated_cost_usd, created_at, updated_at`,
			p.WorkspaceID, p.Enabled, p.WindowMinutes, p.MinRunCount,
			p.MinTotalTokens, p.MinEstimatedCostUSD)
		return scanLoopBrakeConfig(row)
	}

	row := s.rawDB().QueryRow(ctx, `
INSERT INTO issue_loop_brake_config (
	workspace_id, project_id, enabled, window_minutes, min_run_count,
	min_total_tokens, min_estimated_cost_usd, updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, now())
ON CONFLICT (workspace_id, project_id) DO UPDATE SET
	enabled = EXCLUDED.enabled,
	window_minutes = EXCLUDED.window_minutes,
	min_run_count = EXCLUDED.min_run_count,
	min_total_tokens = EXCLUDED.min_total_tokens,
	min_estimated_cost_usd = EXCLUDED.min_estimated_cost_usd,
	updated_at = now()
RETURNING id, workspace_id, project_id, enabled, window_minutes, min_run_count,
	min_total_tokens, min_estimated_cost_usd, created_at, updated_at`,
		p.WorkspaceID, nullableUUID(p.ProjectID), p.Enabled, p.WindowMinutes, p.MinRunCount,
		p.MinTotalTokens, p.MinEstimatedCostUSD)
	return scanLoopBrakeConfig(row)
}

func (s *TaskService) GetIssueLoopBrakeConfig(ctx context.Context, workspaceID, projectID pgtype.UUID) (*IssueLoopBrakeConfig, error) {
	row := s.rawDB().QueryRow(ctx, `
SELECT id, workspace_id, project_id, enabled, window_minutes, min_run_count,
	min_total_tokens, min_estimated_cost_usd, created_at, updated_at
FROM issue_loop_brake_config
WHERE workspace_id = $1
  AND (($2::uuid IS NOT NULL AND project_id = $2) OR ($2::uuid IS NULL AND project_id IS NULL))
LIMIT 1`, workspaceID, nullableUUID(projectID))
	cfg, err := scanLoopBrakeConfig(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &cfg, err
}

func (s *TaskService) GetActiveIssueLoopBrake(ctx context.Context, issueID pgtype.UUID) (*IssueLoopBrake, error) {
	row := s.rawDB().QueryRow(ctx, `
SELECT issue_id, workspace_id, project_id, state, reason, evidence,
	window_started_at, window_ended_at, triggered_at, triggered_by_task_id,
	cleared_at, cleared_by_type, cleared_by_id, clear_reason
FROM issue_loop_brake
WHERE issue_id = $1 AND state = 'active'`, issueID)
	brake, err := scanLoopBrake(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &brake, err
}

func (s *TaskService) ClearIssueLoopBrake(ctx context.Context, issueID pgtype.UUID, actorType string, actorID pgtype.UUID, reason string) (*IssueLoopBrake, error) {
	if strings.TrimSpace(reason) == "" {
		reason = "cleared manually"
	}
	var brake IssueLoopBrake
	err := s.runInRawTx(ctx, func(tx db.DBTX) error {
		row := tx.QueryRow(ctx, `
UPDATE issue_loop_brake
SET state = 'cleared',
    cleared_at = now(),
    cleared_by_type = $2,
    cleared_by_id = $3,
    clear_reason = $4,
    updated_at = now()
WHERE issue_id = $1 AND state = 'active'
RETURNING issue_id, workspace_id, project_id, state, reason, evidence,
	window_started_at, window_ended_at, triggered_at, triggered_by_task_id,
	cleared_at, cleared_by_type, cleared_by_id, clear_reason`, issueID, actorType, nullableUUID(actorID), reason)
		scanned, err := scanLoopBrake(row)
		if err != nil {
			return err
		}
		brake = scanned
		_, err = tx.Exec(ctx, `
INSERT INTO issue_loop_brake_audit (issue_id, workspace_id, actor_type, actor_id, action, reason, evidence)
VALUES ($1, $2, $3, $4, 'cleared', $5, $6)`,
			issueID, util.MustParseUUID(brake.WorkspaceID), actorType, nullableUUID(actorID), reason, brake.Evidence)
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &brake, nil
}

func (s *TaskService) EnsureIssueBrakeAllowsEnqueue(ctx context.Context, issueID pgtype.UUID) error {
	if !issueID.Valid {
		return nil
	}
	active, err := s.GetActiveIssueLoopBrake(ctx, issueID)
	if err != nil {
		return fmt.Errorf("check issue loop brake: %w", err)
	}
	if active != nil {
		return ErrIssueBrakeActive
	}
	return nil
}

func (s *TaskService) EvaluateIssueLoopBrakeForTask(ctx context.Context, task db.AgentTaskQueue) (*IssueLoopBrake, error) {
	if !task.IssueID.Valid {
		return nil, nil
	}
	cfg, issue, err := s.activeLoopBrakeConfigForIssue(ctx, task.IssueID)
	if err != nil || cfg == nil {
		return nil, err
	}
	windowEnd := time.Now().UTC()
	windowStart := windowEnd.Add(-time.Duration(cfg.WindowMinutes) * time.Minute)
	evidence, shouldBrake, err := s.issueLoopBrakeEvidence(ctx, issue.ID, windowStart, windowEnd, *cfg)
	if err != nil || !shouldBrake {
		return nil, err
	}
	return s.triggerIssueLoopBrake(ctx, issue, task.ID, evidence)
}

func (s *TaskService) activeLoopBrakeConfigForIssue(ctx context.Context, issueID pgtype.UUID) (*IssueLoopBrakeConfig, db.Issue, error) {
	issue, err := s.Queries.GetIssue(ctx, issueID)
	if err != nil {
		return nil, db.Issue{}, fmt.Errorf("load issue for loop brake: %w", err)
	}
	var row pgx.Row
	if issue.ProjectID.Valid {
		row = s.rawDB().QueryRow(ctx, `
SELECT id, workspace_id, project_id, enabled, window_minutes, min_run_count,
	min_total_tokens, min_estimated_cost_usd, created_at, updated_at
FROM issue_loop_brake_config
WHERE workspace_id = $1 AND enabled
  AND (project_id = $2 OR project_id IS NULL)
ORDER BY project_id IS NULL
LIMIT 1`, issue.WorkspaceID, issue.ProjectID)
	} else {
		row = s.rawDB().QueryRow(ctx, `
SELECT id, workspace_id, project_id, enabled, window_minutes, min_run_count,
	min_total_tokens, min_estimated_cost_usd, created_at, updated_at
FROM issue_loop_brake_config
WHERE workspace_id = $1 AND project_id IS NULL AND enabled
LIMIT 1`, issue.WorkspaceID)
	}
	cfg, err := scanLoopBrakeConfig(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, issue, nil
	}
	if err != nil {
		return nil, issue, err
	}
	return &cfg, issue, nil
}

func (s *TaskService) issueLoopBrakeEvidence(ctx context.Context, issueID pgtype.UUID, windowStart, windowEnd time.Time, cfg IssueLoopBrakeConfig) (IssueLoopBrakeEvidence, bool, error) {
	var ev IssueLoopBrakeEvidence
	ev.WindowStartedAt = windowStart
	ev.WindowEndedAt = windowEnd
	err := s.rawDB().QueryRow(ctx, `
SELECT
	COUNT(DISTINCT atq.id)::int,
	COALESCE(SUM(tu.input_tokens), 0)::bigint,
	COALESCE(SUM(tu.output_tokens), 0)::bigint,
	COALESCE(SUM(tu.cache_read_tokens), 0)::bigint,
	COALESCE(SUM(tu.cache_write_tokens), 0)::bigint,
	COUNT(*) FILTER (WHERE atq.status = 'queued')::int,
	COUNT(*) FILTER (WHERE atq.status IN ('dispatched', 'running', 'waiting_local_directory'))::int
FROM agent_task_queue atq
LEFT JOIN task_usage tu ON tu.task_id = atq.id
WHERE atq.issue_id = $1
  AND COALESCE(tu.created_at, atq.completed_at, atq.started_at, atq.created_at) >= $2
  AND COALESCE(tu.created_at, atq.completed_at, atq.started_at, atq.created_at) < $3`,
		issueID, windowStart, windowEnd).Scan(
		&ev.RunCount,
		&ev.TotalInputTokens,
		&ev.TotalOutputTokens,
		&ev.TotalCacheReadTokens,
		&ev.TotalCacheWriteTokens,
		&ev.ExistingQueuedTasks,
		&ev.ExistingRunningTasks,
	)
	if err != nil {
		return ev, false, err
	}
	ev.TotalTokens = ev.TotalInputTokens + ev.TotalOutputTokens + ev.TotalCacheReadTokens + ev.TotalCacheWriteTokens

	rows, err := s.rawDB().Query(ctx, `
SELECT tu.provider, tu.model,
	COALESCE(SUM(tu.input_tokens), 0)::bigint,
	COALESCE(SUM(tu.output_tokens), 0)::bigint,
	COALESCE(SUM(tu.cache_read_tokens), 0)::bigint,
	COALESCE(SUM(tu.cache_write_tokens), 0)::bigint
FROM agent_task_queue atq
JOIN task_usage tu ON tu.task_id = atq.id
WHERE atq.issue_id = $1 AND tu.created_at >= $2 AND tu.created_at < $3
GROUP BY tu.provider, tu.model`, issueID, windowStart, windowEnd)
	if err != nil {
		return ev, false, err
	}
	defer rows.Close()
	for rows.Next() {
		var provider, model string
		var in, out, cacheRead, cacheWrite int64
		if err := rows.Scan(&provider, &model, &in, &out, &cacheRead, &cacheWrite); err != nil {
			return ev, false, err
		}
		price, ok := metrics.PriceForModelAlias(provider + ":" + model)
		if !ok {
			price, ok = metrics.PriceForModelAlias(model)
		}
		if !ok {
			continue
		}
		ev.EstimatedCostUSD += costForTokens(in, price.InputPerM)
		ev.EstimatedCostUSD += costForTokens(out, price.OutputPerM)
		ev.EstimatedCostUSD += costForTokens(cacheRead, price.CacheReadPerM)
		ev.EstimatedCostUSD += costForTokens(cacheWrite, price.CacheWritePerM)
	}
	if err := rows.Err(); err != nil {
		return ev, false, err
	}
	ev.EstimatedCostUSD = math.Round(ev.EstimatedCostUSD*1_000_000) / 1_000_000

	if ev.RunCount < cfg.MinRunCount {
		return ev, false, nil
	}
	if cfg.MinTotalTokens > 0 && ev.TotalTokens < cfg.MinTotalTokens {
		return ev, false, nil
	}
	if cfg.MinEstimatedCostUSD > 0 && ev.EstimatedCostUSD < cfg.MinEstimatedCostUSD {
		return ev, false, nil
	}
	progress, err := s.issueProgressSignalsSince(ctx, issueID, windowStart)
	if err != nil {
		return ev, false, err
	}
	if len(progress) > 0 {
		return ev, false, nil
	}
	ev.MissingProgressSignals = []string{"issue_status_or_field_update", "comment_or_handoff", "attachment"}
	return ev, true, nil
}

func (s *TaskService) issueProgressSignalsSince(ctx context.Context, issueID pgtype.UUID, since time.Time) ([]string, error) {
	signals := []string{}
	var issueUpdated bool
	if err := s.rawDB().QueryRow(ctx, `SELECT updated_at >= $2 FROM issue WHERE id = $1`, issueID, since).Scan(&issueUpdated); err != nil {
		return nil, err
	}
	if issueUpdated {
		signals = append(signals, "issue_status_or_field_update")
	}
	var commentCount int
	if err := s.rawDB().QueryRow(ctx, `SELECT count(*) FROM comment WHERE issue_id = $1 AND created_at >= $2`, issueID, since).Scan(&commentCount); err != nil {
		return nil, err
	}
	if commentCount > 0 {
		signals = append(signals, "comment_or_handoff")
	}
	var attachmentCount int
	if err := s.rawDB().QueryRow(ctx, `SELECT count(*) FROM attachment WHERE issue_id = $1 AND created_at >= $2`, issueID, since).Scan(&attachmentCount); err != nil {
		return nil, err
	}
	if attachmentCount > 0 {
		signals = append(signals, "attachment")
	}
	return signals, nil
}

func (s *TaskService) triggerIssueLoopBrake(ctx context.Context, issue db.Issue, taskID pgtype.UUID, ev IssueLoopBrakeEvidence) (*IssueLoopBrake, error) {
	evidenceBytes, err := json.Marshal(ev)
	if err != nil {
		return nil, err
	}
	var evidence map[string]any
	if err := json.Unmarshal(evidenceBytes, &evidence); err != nil {
		return nil, err
	}
	reason := fmt.Sprintf("loop detector paused new agent runs: %d runs and %d tokens since %s without progress signals", ev.RunCount, ev.TotalTokens, ev.WindowStartedAt.Format(time.RFC3339))

	var brake IssueLoopBrake
	err = s.runInRawTx(ctx, func(tx db.DBTX) error {
		row := tx.QueryRow(ctx, `
INSERT INTO issue_loop_brake (
	issue_id, workspace_id, project_id, state, reason, evidence,
	window_started_at, window_ended_at, triggered_by_task_id, updated_at
)
VALUES ($1, $2, $3, 'active', $4, $5, $6, $7, $8, now())
ON CONFLICT (issue_id) DO UPDATE SET
	state = 'active',
	reason = EXCLUDED.reason,
	evidence = EXCLUDED.evidence,
	window_started_at = EXCLUDED.window_started_at,
	window_ended_at = EXCLUDED.window_ended_at,
	triggered_at = now(),
	triggered_by_task_id = EXCLUDED.triggered_by_task_id,
	cleared_at = NULL,
	cleared_by_type = NULL,
	cleared_by_id = NULL,
	clear_reason = NULL,
	updated_at = now()
RETURNING issue_id, workspace_id, project_id, state, reason, evidence,
	window_started_at, window_ended_at, triggered_at, triggered_by_task_id,
	cleared_at, cleared_by_type, cleared_by_id, clear_reason`,
			issue.ID, issue.WorkspaceID, nullableUUID(issue.ProjectID), reason, evidence, ev.WindowStartedAt, ev.WindowEndedAt, nullableUUID(taskID))
		scanned, err := scanLoopBrake(row)
		if err != nil {
			return err
		}
		brake = scanned
		if _, err := tx.Exec(ctx, `
INSERT INTO issue_loop_brake_audit (issue_id, workspace_id, actor_type, actor_id, action, reason, evidence)
VALUES ($1, $2, 'system', NULL, 'triggered', $3, $4)`, issue.ID, issue.WorkspaceID, reason, evidence); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type)
VALUES ($1, $2, 'system', $2, $3, 'system')`, issue.ID, issue.WorkspaceID, reason)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &brake, nil
}

func scanLoopBrakeConfig(row pgx.Row) (IssueLoopBrakeConfig, error) {
	var cfg IssueLoopBrakeConfig
	var id, workspaceID, projectID pgtype.UUID
	var minCost pgtype.Numeric
	var createdAt, updatedAt pgtype.Timestamptz
	if err := row.Scan(&id, &workspaceID, &projectID, &cfg.Enabled, &cfg.WindowMinutes, &cfg.MinRunCount, &cfg.MinTotalTokens, &minCost, &createdAt, &updatedAt); err != nil {
		return cfg, err
	}
	cfg.ID = util.UUIDToString(id)
	cfg.WorkspaceID = util.UUIDToString(workspaceID)
	cfg.ProjectID = uuidPtr(projectID)
	floatValue, _ := minCost.Float64Value()
	if floatValue.Valid {
		cfg.MinEstimatedCostUSD = floatValue.Float64
	}
	cfg.CreatedAt = createdAt.Time.Format(time.RFC3339)
	cfg.UpdatedAt = updatedAt.Time.Format(time.RFC3339)
	return cfg, nil
}

func scanLoopBrake(row pgx.Row) (IssueLoopBrake, error) {
	var b IssueLoopBrake
	var issueID, workspaceID, projectID, taskID, clearedByID pgtype.UUID
	var evidenceBytes []byte
	var windowStartedAt, windowEndedAt, triggeredAt, clearedAt pgtype.Timestamptz
	var clearedByType, clearReason pgtype.Text
	if err := row.Scan(&issueID, &workspaceID, &projectID, &b.State, &b.Reason, &evidenceBytes, &windowStartedAt, &windowEndedAt, &triggeredAt, &taskID, &clearedAt, &clearedByType, &clearedByID, &clearReason); err != nil {
		return b, err
	}
	b.IssueID = util.UUIDToString(issueID)
	b.WorkspaceID = util.UUIDToString(workspaceID)
	b.ProjectID = uuidPtr(projectID)
	b.TriggeredByTaskID = uuidPtr(taskID)
	b.ClearedByID = uuidPtr(clearedByID)
	b.WindowStartedAt = windowStartedAt.Time.Format(time.RFC3339)
	b.WindowEndedAt = windowEndedAt.Time.Format(time.RFC3339)
	b.TriggeredAt = triggeredAt.Time.Format(time.RFC3339)
	if clearedAt.Valid {
		v := clearedAt.Time.Format(time.RFC3339)
		b.ClearedAt = &v
	}
	if clearedByType.Valid {
		b.ClearedByType = clearedByType.String
	}
	if clearReason.Valid {
		b.ClearReason = clearReason.String
	}
	_ = json.Unmarshal(evidenceBytes, &b.Evidence)
	if b.Evidence == nil {
		b.Evidence = map[string]any{}
	}
	return b, nil
}

func nullableUUID(id pgtype.UUID) any {
	if !id.Valid {
		return nil
	}
	return id
}

func uuidPtr(id pgtype.UUID) *string {
	if !id.Valid {
		return nil
	}
	v := util.UUIDToString(id)
	return &v
}

func costForTokens(tokens int64, pricePerM float64) float64 {
	if tokens <= 0 || pricePerM <= 0 {
		return 0
	}
	return float64(tokens) * pricePerM / 1_000_000
}

func (s *TaskService) rawDB() db.DBTX {
	if s.RawDB != nil {
		return s.RawDB
	}
	return queryOnlyDB{s.Queries}
}

func (s *TaskService) runInRawTx(ctx context.Context, fn func(db.DBTX) error) error {
	if s.TxStarter == nil {
		return fn(s.rawDB())
	}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type queryOnlyDB struct {
	queries *db.Queries
}

func (q queryOnlyDB) Exec(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("raw db executor unavailable")
}

func (q queryOnlyDB) Query(context.Context, string, ...interface{}) (pgx.Rows, error) {
	return nil, errors.New("raw db executor unavailable")
}

func (q queryOnlyDB) QueryRow(context.Context, string, ...interface{}) pgx.Row {
	return errorRow{err: errors.New("raw db executor unavailable")}
}

type errorRow struct {
	err error
}

func (r errorRow) Scan(...interface{}) error {
	return r.err
}
