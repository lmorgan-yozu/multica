package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestRunIssueSpendJSONUsesSharedSpendAPI(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_TOKEN", "test-token")
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")

	var spendPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/issues/ADA-16":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "issue-123", "identifier": "ADA-16"})
		case r.URL.Path == "/api/spend":
			spendPath = r.URL.RawQuery
			_ = json.NewEncoder(w).Encode(map[string]any{
				"group_by": "task",
				"rows": []map[string]any{{
					"task_id":            "task-1",
					"provider":           "openai",
					"model":              "gpt-5.4-mini",
					"input_tokens":       1000,
					"output_tokens":      2000,
					"estimated_cost_usd": 0.001,
					"usage_complete":     true,
				}},
				"usage_complete": true,
			})
		default:
			t.Fatalf("unexpected request: %s", r.URL.String())
		}
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)

	cmd := &cobra.Command{Use: "spend"}
	cmd.Flags().String("output", "json", "")
	cmd.Flags().String("from", "", "")
	cmd.Flags().String("to", "", "")
	cmd.Flags().String("tz", "", "")
	_ = cmd.Flags().Set("output", "json")

	out, err := captureStdout(t, func() error {
		return runIssueSpend(cmd, []string{"ADA-16"})
	})
	if err != nil {
		t.Fatalf("runIssueSpend: %v", err)
	}
	if !strings.Contains(spendPath, "group_by=task") || !strings.Contains(spendPath, "issue_id=issue-123") {
		t.Fatalf("spend query = %q", spendPath)
	}
	if !strings.Contains(out, `"estimated_cost_usd": 0.001`) {
		t.Fatalf("json output missing cost: %s", out)
	}
}

func TestRunSpendDailyTableShowsCostAndIncompleteUsage(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_TOKEN", "test-token")
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")

	var spendPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/spend" {
			t.Fatalf("unexpected request: %s", r.URL.String())
		}
		spendPath = r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(map[string]any{
			"group_by": "day",
			"rows": []map[string]any{{
				"date":               "2026-06-10",
				"provider":           "anthropic",
				"model":              "claude-sonnet-4.6",
				"input_tokens":       1000000,
				"output_tokens":      2000000,
				"estimated_cost_usd": 33.0,
				"usage_complete":     false,
			}},
			"usage_complete": false,
		})
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)

	cmd := &cobra.Command{Use: "daily"}
	cmd.Flags().String("agent", "", "")
	cmd.Flags().String("project", "", "")
	cmd.Flags().String("from", "", "")
	cmd.Flags().String("to", "", "")
	cmd.Flags().String("tz", "", "")
	cmd.Flags().String("output", "table", "")
	_ = cmd.Flags().Set("agent", "agent-123")

	out, err := captureStdout(t, func() error {
		return runSpendDaily(cmd, nil)
	})
	if err != nil {
		t.Fatalf("runSpendDaily: %v", err)
	}
	if !strings.Contains(spendPath, "group_by=day") || !strings.Contains(spendPath, "agent_id=agent-123") {
		t.Fatalf("spend query = %q", spendPath)
	}
	if !strings.Contains(out, "COST_USD") || !strings.Contains(out, "33.000000") || !strings.Contains(out, "incomplete") {
		t.Fatalf("table output missing spend fields: %s", out)
	}
}
