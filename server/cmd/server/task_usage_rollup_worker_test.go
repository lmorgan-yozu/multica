package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

type fakeTaskUsageRollupDB struct {
	mu      sync.Mutex
	queries []string
	err     error
}

func (f *fakeTaskUsageRollupDB) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	f.mu.Lock()
	f.queries = append(f.queries, sql)
	f.mu.Unlock()
	return fakeTaskUsageRollupRow{err: f.err}
}

func (f *fakeTaskUsageRollupDB) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.queries)
}

func (f *fakeTaskUsageRollupDB) lastQuery() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.queries) == 0 {
		return ""
	}
	return f.queries[len(f.queries)-1]
}

type fakeTaskUsageRollupRow struct {
	err error
}

func (r fakeTaskUsageRollupRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) == 1 {
		if p, ok := dest[0].(*int64); ok {
			*p = 3
		}
	}
	return nil
}

func TestTickTaskUsageHourlyRollupCallsDatabaseFunction(t *testing.T) {
	db := &fakeTaskUsageRollupDB{}

	tickTaskUsageHourlyRollup(context.Background(), db)

	if db.count() != 1 {
		t.Fatalf("expected one rollup query, got %d", db.count())
	}
	if !strings.Contains(db.lastQuery(), "rollup_task_usage_hourly()") {
		t.Fatalf("expected rollup function query, got %q", db.lastQuery())
	}
}

func TestTickTaskUsageHourlyRollupSwallowsDatabaseError(t *testing.T) {
	db := &fakeTaskUsageRollupDB{err: errors.New("database unavailable")}

	tickTaskUsageHourlyRollup(context.Background(), db)

	if db.count() != 1 {
		t.Fatalf("expected one rollup query on error, got %d", db.count())
	}
}

func TestRunTaskUsageHourlyRollupStopsOnContextCancel(t *testing.T) {
	db := &fakeTaskUsageRollupDB{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		runTaskUsageHourlyRollup(ctx, db, time.Hour)
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("rollup worker did not stop after context cancellation")
	}
}

func TestRunTaskUsageHourlyRollupRunsImmediately(t *testing.T) {
	db := &fakeTaskUsageRollupDB{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		runTaskUsageHourlyRollup(ctx, db, time.Hour)
		close(done)
	}()

	deadline := time.After(time.Second)
	for db.count() == 0 {
		select {
		case <-deadline:
			cancel()
			t.Fatal("rollup worker did not run before first ticker interval")
		default:
			time.Sleep(time.Millisecond)
		}
	}

	cancel()
	<-done
}
