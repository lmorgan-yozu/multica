package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
)

const taskUsageHourlyRollupInterval = 5 * time.Minute

type taskUsageHourlyRollupDB interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func runTaskUsageHourlyRollup(ctx context.Context, db taskUsageHourlyRollupDB, interval time.Duration) {
	if interval <= 0 {
		interval = taskUsageHourlyRollupInterval
	}

	tickTaskUsageHourlyRollup(ctx, db)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tickTaskUsageHourlyRollup(ctx, db)
		}
	}
}

func tickTaskUsageHourlyRollup(ctx context.Context, db taskUsageHourlyRollupDB) {
	var rows int64
	if err := db.QueryRow(ctx, `SELECT rollup_task_usage_hourly()`).Scan(&rows); err != nil {
		slog.Warn("task usage hourly rollup failed", "error", err)
		return
	}
	if rows > 0 {
		slog.Info("task usage hourly rollup advanced", "rows", rows)
	}
}
