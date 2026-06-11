package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func newUsageTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "usage"}
	cmd.Flags().String("output", "table", "")
	cmd.Flags().Int("days", 30, "")
	cmd.Flags().String("project", "", "")
	cmd.Flags().String("agent-id", "", "")
	return cmd
}

func TestRunUsagePassesWorkspaceFiltersAndPrintsJSON(t *testing.T) {
	var gotPaths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPaths = append(gotPaths, r.URL.RequestURI())
		switch r.URL.Path {
		case "/api/projects":
			if r.URL.Query().Get("workspace_id") != "ws-1" {
				t.Errorf("workspace_id query = %q, want ws-1", r.URL.Query().Get("workspace_id"))
			}
			json.NewEncoder(w).Encode(map[string]any{
				"projects": []map[string]any{{
					"id":    "11111111-1111-1111-1111-111111111111",
					"title": "Ada Bootstrapping",
				}},
			})
		case "/api/dashboard/usage/by-agent":
			if r.URL.Query().Get("days") != "7" {
				t.Errorf("days query = %q, want 7", r.URL.Query().Get("days"))
			}
			if r.URL.Query().Get("project_id") != "11111111-1111-1111-1111-111111111111" {
				t.Errorf("project_id query = %q, want project UUID", r.URL.Query().Get("project_id"))
			}
			if r.URL.Query().Get("agent_id") != "agent-uuid" {
				t.Errorf("agent_id query = %q, want agent-uuid", r.URL.Query().Get("agent_id"))
			}
			json.NewEncoder(w).Encode([]map[string]any{{
				"agent_id":           "agent-uuid",
				"model":              "gpt-test",
				"input_tokens":       100,
				"output_tokens":      200,
				"cache_read_tokens":  3,
				"cache_write_tokens": 4,
				"task_count":         2,
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newUsageTestCmd()
	_ = cmd.Flags().Set("output", "json")
	_ = cmd.Flags().Set("days", "7")
	_ = cmd.Flags().Set("project", "11111111")
	_ = cmd.Flags().Set("agent-id", "agent-uuid")
	out, err := captureStdout(t, func() error {
		return runUsage(cmd, nil)
	})
	if err != nil {
		t.Fatalf("runUsage: %v", err)
	}
	if want := []string{"/api/projects?workspace_id=ws-1", "/api/dashboard/usage/by-agent?agent_id=agent-uuid&days=7&project_id=11111111-1111-1111-1111-111111111111"}; fmt.Sprint(gotPaths) != fmt.Sprint(want) {
		t.Fatalf("paths = %v, want %v", gotPaths, want)
	}
	var payload []map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("decode JSON output: %v\n%s", err, out)
	}
	if len(payload) != 1 || payload[0]["agent_id"] != "agent-uuid" || payload[0]["task_count"] != float64(2) {
		t.Fatalf("unexpected usage payload: %#v", payload)
	}
}

func TestPrintUsageTableIncludesAggregateFields(t *testing.T) {
	rows := []map[string]any{{
		"agent_id":           "agent-uuid-123456",
		"model":              "gpt-test",
		"input_tokens":       float64(100),
		"output_tokens":      float64(200),
		"cache_read_tokens":  float64(3),
		"cache_write_tokens": float64(4),
		"task_count":         float64(2),
	}}

	out, err := captureStdout(t, func() error {
		printUsageTable(rows)
		return nil
	})
	if err != nil {
		t.Fatalf("capture table: %v", err)
	}
	for _, want := range []string{"AGENT", "MODEL", "INPUT", "OUTPUT", "CACHE READ", "CACHE WRITE", "TASKS", "agent-uu", "gpt-test", "100", "200", "3", "4", "2"} {
		if !strings.Contains(out, want) {
			t.Fatalf("table output missing %q:\n%s", want, out)
		}
	}
}
