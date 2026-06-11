package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

const roadmapTestProjectID = "11111111-2222-3333-4444-555555555555"

func newProjectRoadmapTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "roadmap"}
	cmd.Flags().String("output", "table", "")
	return cmd
}

func roadmapTestPayload() map[string]any {
	return map[string]any{
		"project_id":    roadmapTestProjectID,
		"project_title": "Ada Bootstrapping",
		"milestones": []map[string]any{
			{
				"id":            "aaaaaaaa-0000-0000-0000-000000000001",
				"name":          "Demo ready",
				"description":   "",
				"target_date":   "2026-07-01",
				"position":      float64(0),
				"progress":      map[string]any{"done": float64(1), "total": float64(3)},
				"blocked_count": float64(1),
				"epic_ids":      []any{"bbbbbbbb-0000-0000-0000-000000000001"},
				"created_at":    "2026-06-12T00:00:00Z",
				"updated_at":    "2026-06-12T00:00:00Z",
			},
		},
		"epics": []map[string]any{
			{
				"id":            "bbbbbbbb-0000-0000-0000-000000000001",
				"identifier":    "ADA-54",
				"number":        float64(54),
				"title":         "Epic: Project Roadmap",
				"status":        "in_progress",
				"priority":      "high",
				"milestone_id":  "aaaaaaaa-0000-0000-0000-000000000001",
				"start_date":    nil,
				"due_date":      "2026-06-30",
				"position":      float64(0),
				"progress":      map[string]any{"done": float64(1), "total": float64(3)},
				"blocked_count": float64(1),
				"depends_on":    []any{"bbbbbbbb-0000-0000-0000-000000000002"},
				"child_count":   float64(3),
			},
			{
				"id":            "bbbbbbbb-0000-0000-0000-000000000002",
				"identifier":    "ADA-3",
				"number":        float64(3),
				"title":         "Spend visibility",
				"status":        "done",
				"priority":      "high",
				"milestone_id":  nil,
				"start_date":    nil,
				"due_date":      nil,
				"position":      float64(1),
				"progress":      map[string]any{"done": float64(2), "total": float64(2)},
				"blocked_count": float64(0),
				"depends_on":    []any{},
				"child_count":   float64(2),
			},
		},
		"cycle_detected": false,
	}
}

func startRoadmapTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/api/projects/"+roadmapTestProjectID+"/roadmap":
			json.NewEncoder(w).Encode(roadmapTestPayload())
		case r.Method == "GET" && r.URL.Path == "/api/projects":
			json.NewEncoder(w).Encode(map[string]any{
				"projects": []map[string]any{{"id": roadmapTestProjectID, "title": "Ada Bootstrapping"}},
				"total":    1,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func captureRoadmapStdout(t *testing.T, cmd *cobra.Command) (string, error) {
	t.Helper()
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	err := runProjectRoadmap(cmd, []string{roadmapTestProjectID})
	_ = w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)
	return string(out), err
}

func TestRunProjectRoadmapJSONOutput(t *testing.T) {
	srv := startRoadmapTestServer(t)
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newProjectRoadmapTestCmd()
	_ = cmd.Flags().Set("output", "json")
	out, err := captureRoadmapStdout(t, cmd)
	if err != nil {
		t.Fatalf("runProjectRoadmap: %v", err)
	}

	// stdout must be exactly the API projection — agents pipe this.
	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("decode JSON output: %v\n%s", err, out)
	}
	if payload["project_title"] != "Ada Bootstrapping" {
		t.Fatalf("project_title = %v", payload["project_title"])
	}
	epics, _ := payload["epics"].([]any)
	if len(epics) != 2 {
		t.Fatalf("epics len = %d", len(epics))
	}
	first, _ := epics[0].(map[string]any)
	if first["identifier"] != "ADA-54" {
		t.Fatalf("first epic = %v", first["identifier"])
	}
	if _, ok := payload["cycle_detected"].(bool); !ok {
		t.Fatalf("cycle_detected missing: %v", payload)
	}
}

func TestRunProjectRoadmapTableOutput(t *testing.T) {
	srv := startRoadmapTestServer(t)
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newProjectRoadmapTestCmd()
	out, err := captureRoadmapStdout(t, cmd)
	if err != nil {
		t.Fatalf("runProjectRoadmap: %v", err)
	}

	for _, want := range []string{
		"Project: Ada Bootstrapping",
		"MILESTONE", "Demo ready", "2026-07-01", "1/3",
		"KEY", "ADA-54", "Epic: Project Roadmap", "in_progress",
		// Dependency column shows the routable key of the other epic.
		"ADA-3",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("table output missing %q:\n%s", want, out)
		}
	}
}
