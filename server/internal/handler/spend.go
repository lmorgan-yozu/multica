package handler

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/util"
)

type SpendRowResponse struct {
	GroupBy               string   `json:"group_by"`
	IssueID               *string  `json:"issue_id,omitempty"`
	TaskID                *string  `json:"task_id,omitempty"`
	SessionID             *string  `json:"session_id,omitempty"`
	AgentID               *string  `json:"agent_id,omitempty"`
	Date                  *string  `json:"date,omitempty"`
	Provider              string   `json:"provider,omitempty"`
	Model                 string   `json:"model,omitempty"`
	InputTokens           int64    `json:"input_tokens"`
	OutputTokens          int64    `json:"output_tokens"`
	CacheReadTokens       int64    `json:"cache_read_tokens"`
	CacheWriteTokens      int64    `json:"cache_write_tokens"`
	TaskCount             int32    `json:"task_count"`
	MissingUsageTaskCount int32    `json:"missing_usage_task_count"`
	UsageComplete         bool     `json:"usage_complete"`
	EstimatedCostUSD      *float64 `json:"estimated_cost_usd,omitempty"`
	CostComplete          bool     `json:"cost_complete"`
}

type SpendReportResponse struct {
	GroupBy               string             `json:"group_by"`
	Rows                  []SpendRowResponse `json:"rows"`
	TotalInputTokens      int64              `json:"total_input_tokens"`
	TotalOutputTokens     int64              `json:"total_output_tokens"`
	TotalCacheReadTokens  int64              `json:"total_cache_read_tokens"`
	TotalCacheWriteTokens int64              `json:"total_cache_write_tokens"`
	TaskCount             int32              `json:"task_count"`
	MissingUsageTaskCount int32              `json:"missing_usage_task_count"`
	UsageComplete         bool               `json:"usage_complete"`
	EstimatedCostUSD      *float64           `json:"estimated_cost_usd,omitempty"`
	CostComplete          bool               `json:"cost_complete"`
}

type spendFilters struct {
	workspaceID pgtype.UUID
	groupBy     string
	tz          string
	issueID     pgtype.UUID
	agentID     pgtype.UUID
	projectID   pgtype.UUID
	from        pgtype.Timestamptz
	to          pgtype.Timestamptz
}

func (h *Handler) GetSpendReport(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}

	filters, ok := h.parseSpendFilters(w, r, workspaceID)
	if !ok {
		return
	}

	resp, err := h.listSpendReport(r.Context(), filters)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list spend")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) parseSpendFilters(w http.ResponseWriter, r *http.Request, workspaceID string) (spendFilters, bool) {
	q := r.URL.Query()
	groupBy := strings.TrimSpace(q.Get("group_by"))
	if groupBy == "" {
		groupBy = "day"
	}
	switch groupBy {
	case "task", "issue", "agent", "day":
	default:
		writeError(w, http.StatusBadRequest, "invalid group_by")
		return spendFilters{}, false
	}

	parseOptionalUUID := func(name string) (pgtype.UUID, bool) {
		raw := strings.TrimSpace(q.Get(name))
		if raw == "" {
			return pgtype.UUID{}, true
		}
		u, err := util.ParseUUID(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid "+name)
			return pgtype.UUID{}, false
		}
		return u, true
	}

	issueID, ok := parseOptionalUUID("issue_id")
	if !ok {
		return spendFilters{}, false
	}
	agentID, ok := parseOptionalUUID("agent_id")
	if !ok {
		return spendFilters{}, false
	}
	projectID, ok := parseOptionalUUID("project_id")
	if !ok {
		return spendFilters{}, false
	}

	tz := h.resolveViewingTZ(r)
	parseDateBound := func(name string, end bool) (pgtype.Timestamptz, bool) {
		raw := strings.TrimSpace(q.Get(name))
		if raw == "" {
			return pgtype.Timestamptz{}, true
		}
		loc, err := time.LoadLocation(tz)
		if err != nil {
			loc = time.UTC
		}
		day, err := time.ParseInLocation("2006-01-02", raw, loc)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid "+name)
			return pgtype.Timestamptz{}, false
		}
		if end {
			day = day.AddDate(0, 0, 1)
		}
		return pgtype.Timestamptz{Time: day.UTC(), Valid: true}, true
	}
	from, ok := parseDateBound("from", false)
	if !ok {
		return spendFilters{}, false
	}
	to, ok := parseDateBound("to", true)
	if !ok {
		return spendFilters{}, false
	}

	return spendFilters{
		workspaceID: parseUUID(workspaceID),
		groupBy:     groupBy,
		tz:          tz,
		issueID:     issueID,
		agentID:     agentID,
		projectID:   projectID,
		from:        from,
		to:          to,
	}, true
}

func (h *Handler) listSpendReport(ctx context.Context, filters spendFilters) (SpendReportResponse, error) {
	groupSelect, groupBySQL, orderBySQL := spendGroupSQL(filters.groupBy)
	sql := fmt.Sprintf(`
WITH scoped_tasks AS (
	SELECT
		atq.id AS task_id,
		atq.session_id,
		atq.issue_id,
		atq.agent_id,
		i.project_id,
		COALESCE(tu.created_at, atq.completed_at, atq.started_at, atq.created_at) AS usage_at,
		tu.provider,
		tu.model,
		COALESCE(tu.input_tokens, 0)::bigint AS input_tokens,
		COALESCE(tu.output_tokens, 0)::bigint AS output_tokens,
		COALESCE(tu.cache_read_tokens, 0)::bigint AS cache_read_tokens,
		COALESCE(tu.cache_write_tokens, 0)::bigint AS cache_write_tokens,
		(tu.task_id IS NOT NULL) AS has_usage
	FROM agent_task_queue atq
	JOIN agent a ON a.id = atq.agent_id
	LEFT JOIN issue i ON i.id = atq.issue_id
	LEFT JOIN task_usage tu ON tu.task_id = atq.id
	WHERE a.workspace_id = $1
	  AND ($2::uuid IS NULL OR atq.issue_id = $2)
	  AND ($3::uuid IS NULL OR atq.agent_id = $3)
	  AND ($4::uuid IS NULL OR i.project_id = $4)
	  AND ($5::timestamptz IS NULL OR COALESCE(tu.created_at, atq.completed_at, atq.started_at, atq.created_at) >= $5)
	  AND ($6::timestamptz IS NULL OR COALESCE(tu.created_at, atq.completed_at, atq.started_at, atq.created_at) < $6)
)
SELECT
	%s,
	provider,
	model,
	SUM(input_tokens)::bigint AS input_tokens,
	SUM(output_tokens)::bigint AS output_tokens,
	SUM(cache_read_tokens)::bigint AS cache_read_tokens,
	SUM(cache_write_tokens)::bigint AS cache_write_tokens,
	COUNT(DISTINCT task_id)::int AS task_count,
	COUNT(DISTINCT task_id) FILTER (WHERE NOT has_usage)::int AS missing_usage_task_count
FROM scoped_tasks
GROUP BY %s, provider, model
ORDER BY %s, provider NULLS LAST, model NULLS LAST`, groupSelect, groupBySQL, orderBySQL)

	args := []any{filters.workspaceID, filters.issueID, filters.agentID, filters.projectID, filters.from, filters.to}
	if filters.groupBy == "day" {
		args = append(args, filters.tz)
	}
	rows, err := h.DB.Query(ctx, sql, args...)
	if err != nil {
		return SpendReportResponse{}, err
	}
	defer rows.Close()

	resp := SpendReportResponse{GroupBy: filters.groupBy, UsageComplete: true, CostComplete: true}
	seenTasks := map[string]struct{}{}
	seenMissingTasks := map[string]struct{}{}
	for rows.Next() {
		row, taskIDs, missingTaskIDs, err := scanSpendRow(rows, filters.groupBy)
		if err != nil {
			return SpendReportResponse{}, err
		}
		applySpendCost(&row)
		resp.Rows = append(resp.Rows, row)
		resp.TotalInputTokens += row.InputTokens
		resp.TotalOutputTokens += row.OutputTokens
		resp.TotalCacheReadTokens += row.CacheReadTokens
		resp.TotalCacheWriteTokens += row.CacheWriteTokens
		for _, id := range taskIDs {
			seenTasks[id] = struct{}{}
		}
		for _, id := range missingTaskIDs {
			seenMissingTasks[id] = struct{}{}
		}
		if !row.UsageComplete {
			resp.UsageComplete = false
		}
		if !row.CostComplete {
			resp.CostComplete = false
		}
		if row.EstimatedCostUSD != nil {
			if resp.EstimatedCostUSD == nil {
				zero := 0.0
				resp.EstimatedCostUSD = &zero
			}
			*resp.EstimatedCostUSD += *row.EstimatedCostUSD
		}
	}
	if err := rows.Err(); err != nil {
		return SpendReportResponse{}, err
	}
	resp.TaskCount = int32(len(seenTasks))
	resp.MissingUsageTaskCount = int32(len(seenMissingTasks))
	if resp.MissingUsageTaskCount > 0 {
		resp.UsageComplete = false
		resp.CostComplete = false
	}
	return resp, nil
}

func spendGroupSQL(groupBy string) (string, string, string) {
	switch groupBy {
	case "task":
		return "task_id::text AS group_task_id, session_id::text AS group_session_id, NULL::text AS group_issue_id, NULL::text AS group_agent_id, NULL::text AS group_date, ARRAY_AGG(DISTINCT task_id::text) AS task_ids, ARRAY_AGG(DISTINCT task_id::text) FILTER (WHERE NOT has_usage) AS missing_task_ids", "task_id, session_id", "group_task_id"
	case "issue":
		return "NULL::text AS group_task_id, NULL::text AS group_session_id, issue_id::text AS group_issue_id, NULL::text AS group_agent_id, NULL::text AS group_date, ARRAY_AGG(DISTINCT task_id::text) AS task_ids, ARRAY_AGG(DISTINCT task_id::text) FILTER (WHERE NOT has_usage) AS missing_task_ids", "issue_id", "group_issue_id"
	case "agent":
		return "NULL::text AS group_task_id, NULL::text AS group_session_id, NULL::text AS group_issue_id, agent_id::text AS group_agent_id, NULL::text AS group_date, ARRAY_AGG(DISTINCT task_id::text) AS task_ids, ARRAY_AGG(DISTINCT task_id::text) FILTER (WHERE NOT has_usage) AS missing_task_ids", "agent_id", "group_agent_id"
	default:
		return "NULL::text AS group_task_id, NULL::text AS group_session_id, NULL::text AS group_issue_id, NULL::text AS group_agent_id, DATE(usage_at AT TIME ZONE $7::text)::text AS group_date, ARRAY_AGG(DISTINCT task_id::text) AS task_ids, ARRAY_AGG(DISTINCT task_id::text) FILTER (WHERE NOT has_usage) AS missing_task_ids", "DATE(usage_at AT TIME ZONE $7::text)", "group_date DESC"
	}
}

func scanSpendRow(rows pgx.Rows, groupBy string) (SpendRowResponse, []string, []string, error) {
	var taskID, sessionID, issueID, agentID, date, provider, model pgtype.Text
	var taskIDs, missingTaskIDs []string
	row := SpendRowResponse{GroupBy: groupBy}
	err := rows.Scan(&taskID, &sessionID, &issueID, &agentID, &date, &taskIDs, &missingTaskIDs, &provider, &model, &row.InputTokens, &row.OutputTokens, &row.CacheReadTokens, &row.CacheWriteTokens, &row.TaskCount, &row.MissingUsageTaskCount)
	if err != nil {
		return SpendRowResponse{}, nil, nil, err
	}
	row.TaskID = textToPtr(taskID)
	row.SessionID = textToPtr(sessionID)
	row.IssueID = textToPtr(issueID)
	row.AgentID = textToPtr(agentID)
	row.Date = textToPtr(date)
	if provider.Valid {
		row.Provider = provider.String
	}
	if model.Valid {
		row.Model = model.String
	}
	row.UsageComplete = row.MissingUsageTaskCount == 0
	row.CostComplete = row.UsageComplete
	return row, taskIDs, missingTaskIDs, nil
}

func applySpendCost(row *SpendRowResponse) {
	if row.Model == "" {
		row.CostComplete = false
		return
	}
	price, ok := metrics.PriceForModelAlias(row.Provider + ":" + row.Model)
	if !ok {
		price, ok = metrics.PriceForModelAlias(row.Model)
	}
	if !ok {
		row.CostComplete = false
		return
	}
	cost := spendTokenCost(row.InputTokens, price.InputPerM) +
		spendTokenCost(row.OutputTokens, price.OutputPerM) +
		spendTokenCost(row.CacheReadTokens, price.CacheReadPerM) +
		spendTokenCost(row.CacheWriteTokens, price.CacheWritePerM)
	row.EstimatedCostUSD = &cost
}

func spendTokenCost(tokens int64, pricePerM float64) float64 {
	if tokens <= 0 || pricePerM <= 0 {
		return 0
	}
	return float64(tokens) * pricePerM / 1_000_000
}
