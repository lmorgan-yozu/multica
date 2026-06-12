package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"
)

const (
	milestoneTestProjectID = "11111111-2222-3333-4444-555555555555"
	milestoneTestID        = "aaaaaaaa-0000-0000-0000-000000000001"
)

// milestoneTestServer records the last write request so tests can assert the
// exact body the CLI sends — agents depend on this contract.
type milestoneTestServer struct {
	srv *httptest.Server

	mu         sync.Mutex
	lastMethod string
	lastPath   string
	lastBody   map[string]any
}

func milestoneJSON() map[string]any {
	return map[string]any{
		"id":          milestoneTestID,
		"project_id":  milestoneTestProjectID,
		"name":        "Demo ready",
		"description": "",
		"target_date": "2026-07-01",
		"position":    float64(0),
		"created_at":  "2026-06-12T00:00:00Z",
		"updated_at":  "2026-06-12T00:00:00Z",
	}
}

func startMilestoneTestServer(t *testing.T) *milestoneTestServer {
	t.Helper()
	ts := &milestoneTestServer{}
	ts.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recordWrite := func() {
			ts.mu.Lock()
			defer ts.mu.Unlock()
			ts.lastMethod = r.Method
			ts.lastPath = r.URL.Path
			ts.lastBody = nil
			if r.Body != nil {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
					ts.lastBody = body
				}
			}
		}
		switch {
		case r.Method == "POST" && r.URL.Path == "/api/projects/"+milestoneTestProjectID+"/milestones":
			recordWrite()
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(milestoneJSON())
		case r.Method == "PUT" && r.URL.Path == "/api/projects/"+milestoneTestProjectID+"/milestones/"+milestoneTestID:
			recordWrite()
			json.NewEncoder(w).Encode(milestoneJSON())
		case r.Method == "DELETE" && r.URL.Path == "/api/projects/"+milestoneTestProjectID+"/milestones/"+milestoneTestID:
			recordWrite()
			w.WriteHeader(http.StatusNoContent)
		case r.Method == "GET" && r.URL.Path == "/api/projects/"+milestoneTestProjectID+"/roadmap":
			json.NewEncoder(w).Encode(map[string]any{
				"project_id":    milestoneTestProjectID,
				"project_title": "Ada Bootstrapping",
				"milestones": []map[string]any{
					{
						"id":            milestoneTestID,
						"name":          "Demo ready",
						"target_date":   "2026-07-01",
						"position":      float64(0),
						"progress":      map[string]any{"done": float64(1), "total": float64(3)},
						"blocked_count": float64(1),
						"epic_ids":      []any{"bbbbbbbb-0000-0000-0000-000000000001"},
					},
				},
				"epics":          []map[string]any{},
				"cycle_detected": false,
			})
		case r.Method == "GET" && r.URL.Path == "/api/projects":
			json.NewEncoder(w).Encode(map[string]any{
				"projects": []map[string]any{{"id": milestoneTestProjectID, "title": "Ada Bootstrapping"}},
				"total":    1,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(ts.srv.Close)
	return ts
}

func (ts *milestoneTestServer) setEnv(t *testing.T) {
	t.Helper()
	t.Setenv("MULTICA_SERVER_URL", ts.srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")
}

func captureMilestoneStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	err := fn()
	_ = w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)
	return string(out), err
}

func newMilestoneTestCmd(flags func(*cobra.Command)) *cobra.Command {
	cmd := &cobra.Command{Use: "test"}
	flags(cmd)
	return cmd
}

func TestRunProjectMilestoneAddJSON(t *testing.T) {
	ts := startMilestoneTestServer(t)
	ts.setEnv(t)

	cmd := newMilestoneTestCmd(addProjectMilestoneAddFlags)
	_ = cmd.Flags().Set("name", "Demo ready")
	_ = cmd.Flags().Set("target-date", "2026-07-01")

	out, err := captureMilestoneStdout(t, func() error {
		return runProjectMilestoneAdd(cmd, []string{milestoneTestProjectID})
	})
	if err != nil {
		t.Fatalf("runProjectMilestoneAdd: %v", err)
	}

	if ts.lastBody["name"] != "Demo ready" || ts.lastBody["target_date"] != "2026-07-01" {
		t.Fatalf("request body = %v", ts.lastBody)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("decode JSON output: %v\n%s", err, out)
	}
	if payload["id"] != milestoneTestID || payload["name"] != "Demo ready" {
		t.Fatalf("payload = %v", payload)
	}
}

func TestRunProjectMilestoneAddRequiresName(t *testing.T) {
	ts := startMilestoneTestServer(t)
	ts.setEnv(t)

	cmd := newMilestoneTestCmd(addProjectMilestoneAddFlags)
	err := runProjectMilestoneAdd(cmd, []string{milestoneTestProjectID})
	if err == nil || !strings.Contains(err.Error(), "--name") {
		t.Fatalf("expected --name required error, got %v", err)
	}
}

func TestRunProjectMilestoneListTable(t *testing.T) {
	ts := startMilestoneTestServer(t)
	ts.setEnv(t)

	cmd := newMilestoneTestCmd(addProjectMilestoneListFlags)
	out, err := captureMilestoneStdout(t, func() error {
		return runProjectMilestoneList(cmd, []string{milestoneTestProjectID})
	})
	if err != nil {
		t.Fatalf("runProjectMilestoneList: %v", err)
	}
	for _, want := range []string{"NAME", "Demo ready", "2026-07-01", "1/3"} {
		if !strings.Contains(out, want) {
			t.Fatalf("table output missing %q:\n%s", want, out)
		}
	}
}

// Update resolves a milestone id prefix via the project roadmap and sends
// only the changed fields; --target-date "" must send an explicit clear.
func TestRunProjectMilestoneUpdatePrefixAndClearDate(t *testing.T) {
	ts := startMilestoneTestServer(t)
	ts.setEnv(t)

	cmd := newMilestoneTestCmd(addProjectMilestoneUpdateFlags)
	_ = cmd.Flags().Set("target-date", "")
	_ = cmd.Flags().Set("position", "2.5")

	_, err := captureMilestoneStdout(t, func() error {
		return runProjectMilestoneUpdate(cmd, []string{milestoneTestProjectID, "aaaaaaaa"})
	})
	if err != nil {
		t.Fatalf("runProjectMilestoneUpdate: %v", err)
	}

	if ts.lastMethod != "PUT" || !strings.HasSuffix(ts.lastPath, "/milestones/"+milestoneTestID) {
		t.Fatalf("last request = %s %s", ts.lastMethod, ts.lastPath)
	}
	if v, present := ts.lastBody["target_date"]; !present || v != "" {
		t.Fatalf("target_date should be sent as explicit clear, body = %v", ts.lastBody)
	}
	if _, present := ts.lastBody["name"]; present {
		t.Fatalf("unchanged name must not be sent, body = %v", ts.lastBody)
	}
	if ts.lastBody["position"] != 2.5 {
		t.Fatalf("position = %v", ts.lastBody["position"])
	}
}

func TestRunProjectMilestoneRemove(t *testing.T) {
	ts := startMilestoneTestServer(t)
	ts.setEnv(t)

	cmd := newMilestoneTestCmd(addProjectMilestoneRemoveFlags)
	_ = cmd.Flags().Set("output", "json")
	out, err := captureMilestoneStdout(t, func() error {
		return runProjectMilestoneRemove(cmd, []string{milestoneTestProjectID, milestoneTestID})
	})
	if err != nil {
		t.Fatalf("runProjectMilestoneRemove: %v", err)
	}
	if ts.lastMethod != "DELETE" || !strings.HasSuffix(ts.lastPath, "/milestones/"+milestoneTestID) {
		t.Fatalf("last request = %s %s", ts.lastMethod, ts.lastPath)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("decode JSON output: %v\n%s", err, out)
	}
	if payload["removed"] != true || payload["milestone_id"] != milestoneTestID {
		t.Fatalf("payload = %v", payload)
	}
}
