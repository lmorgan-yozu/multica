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
	issueRoadmapTestProjectID   = "11111111-2222-3333-4444-555555555555"
	issueRoadmapTestIssueID     = "cccccccc-0000-0000-0000-000000000001"
	issueRoadmapTestDependsOnID = "cccccccc-0000-0000-0000-000000000002"
	issueRoadmapTestMilestoneID = "aaaaaaaa-0000-0000-0000-000000000001"
)

type issueRoadmapTestServer struct {
	srv *httptest.Server

	mu         sync.Mutex
	lastMethod string
	lastPath   string
	lastRaw    []byte
}

func issueRoadmapIssueJSON(id, identifier string) map[string]any {
	return map[string]any{
		"id":           id,
		"identifier":   identifier,
		"title":        "Some issue",
		"status":       "in_progress",
		"project_id":   issueRoadmapTestProjectID,
		"workspace_id": "ws-1",
	}
}

func startIssueRoadmapTestServer(t *testing.T) *issueRoadmapTestServer {
	t.Helper()
	ts := &issueRoadmapTestServer{}
	ts.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recordWrite := func() {
			ts.mu.Lock()
			defer ts.mu.Unlock()
			ts.lastMethod = r.Method
			ts.lastPath = r.URL.Path
			ts.lastRaw, _ = io.ReadAll(r.Body)
		}
		switch {
		case r.Method == "GET" && r.URL.Path == "/api/issues/ADA-57":
			json.NewEncoder(w).Encode(issueRoadmapIssueJSON(issueRoadmapTestIssueID, "ADA-57"))
		case r.Method == "GET" && r.URL.Path == "/api/issues/ADA-54":
			json.NewEncoder(w).Encode(issueRoadmapIssueJSON(issueRoadmapTestDependsOnID, "ADA-54"))
		case r.Method == "GET" && r.URL.Path == "/api/issues/"+issueRoadmapTestIssueID:
			json.NewEncoder(w).Encode(issueRoadmapIssueJSON(issueRoadmapTestIssueID, "ADA-57"))
		case r.Method == "PUT" && r.URL.Path == "/api/issues/"+issueRoadmapTestIssueID+"/milestone":
			recordWrite()
			issue := issueRoadmapIssueJSON(issueRoadmapTestIssueID, "ADA-57")
			issue["milestone_id"] = issueRoadmapTestMilestoneID
			json.NewEncoder(w).Encode(issue)
		case r.Method == "POST" && r.URL.Path == "/api/issues/"+issueRoadmapTestIssueID+"/dependencies":
			recordWrite()
			var body map[string]any
			if err := json.Unmarshal(ts.lastRaw, &body); err == nil && body["depends_on_issue_id"] == issueRoadmapTestIssueID {
				http.Error(w, "self dependency is not allowed", http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]any{
				"issue_id":            issueRoadmapTestIssueID,
				"depends_on_issue_id": issueRoadmapTestDependsOnID,
				"type":                "blocked_by",
				"created_at":          "2026-06-12T00:00:00Z",
			})
		case r.Method == "DELETE" && r.URL.Path == "/api/issues/"+issueRoadmapTestIssueID+"/dependencies/"+issueRoadmapTestDependsOnID:
			recordWrite()
			w.WriteHeader(http.StatusNoContent)
		case r.Method == "GET" && r.URL.Path == "/api/projects/"+issueRoadmapTestProjectID+"/roadmap":
			json.NewEncoder(w).Encode(map[string]any{
				"project_id":    issueRoadmapTestProjectID,
				"project_title": "Ada Bootstrapping",
				"milestones": []map[string]any{
					{"id": issueRoadmapTestMilestoneID, "name": "Demo ready"},
				},
				"epics":          []map[string]any{},
				"cycle_detected": false,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(ts.srv.Close)
	return ts
}

func (ts *issueRoadmapTestServer) setEnv(t *testing.T) {
	t.Helper()
	t.Setenv("MULTICA_SERVER_URL", ts.srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")
}

func (ts *issueRoadmapTestServer) lastBody(t *testing.T) map[string]any {
	t.Helper()
	ts.mu.Lock()
	defer ts.mu.Unlock()
	var body map[string]any
	if len(ts.lastRaw) > 0 {
		if err := json.Unmarshal(ts.lastRaw, &body); err != nil {
			t.Fatalf("decode last request body: %v\n%s", err, ts.lastRaw)
		}
	}
	return body
}

func captureIssueRoadmapStdout(t *testing.T, fn func() error) (string, error) {
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

func newIssueRoadmapTestCmd(flags func(*cobra.Command)) *cobra.Command {
	cmd := &cobra.Command{Use: "test"}
	flags(cmd)
	return cmd
}

// Issue refs are routable keys; a milestone id prefix resolves through the
// issue's project roadmap.
func TestRunIssueMilestoneSetResolvesKeyAndPrefix(t *testing.T) {
	ts := startIssueRoadmapTestServer(t)
	ts.setEnv(t)

	cmd := newIssueRoadmapTestCmd(addIssueMilestoneSetFlags)
	_ = cmd.Flags().Set("milestone", "aaaaaaaa")
	out, err := captureIssueRoadmapStdout(t, func() error {
		return runIssueMilestoneSet(cmd, []string{"ADA-57"})
	})
	if err != nil {
		t.Fatalf("runIssueMilestoneSet: %v", err)
	}

	body := ts.lastBody(t)
	if body["milestone_id"] != issueRoadmapTestMilestoneID {
		t.Fatalf("milestone_id = %v", body["milestone_id"])
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("decode JSON output: %v\n%s", err, out)
	}
	if payload["milestone_id"] != issueRoadmapTestMilestoneID {
		t.Fatalf("payload milestone_id = %v", payload["milestone_id"])
	}
}

func TestRunIssueMilestoneSetClearSendsNull(t *testing.T) {
	ts := startIssueRoadmapTestServer(t)
	ts.setEnv(t)

	cmd := newIssueRoadmapTestCmd(addIssueMilestoneSetFlags)
	_ = cmd.Flags().Set("clear", "true")
	_, err := captureIssueRoadmapStdout(t, func() error {
		return runIssueMilestoneSet(cmd, []string{"ADA-57"})
	})
	if err != nil {
		t.Fatalf("runIssueMilestoneSet: %v", err)
	}

	ts.mu.Lock()
	raw := string(ts.lastRaw)
	ts.mu.Unlock()
	body := ts.lastBody(t)
	if v, present := body["milestone_id"]; !present || v != nil {
		t.Fatalf("expected explicit milestone_id null, body = %s", raw)
	}
}

func TestRunIssueMilestoneSetValidatesFlagChoice(t *testing.T) {
	cmd := newIssueRoadmapTestCmd(addIssueMilestoneSetFlags)
	if err := runIssueMilestoneSet(cmd, []string{"ADA-57"}); err == nil || !strings.Contains(err.Error(), "--milestone or --clear") {
		t.Fatalf("expected missing flag error, got %v", err)
	}

	cmd = newIssueRoadmapTestCmd(addIssueMilestoneSetFlags)
	_ = cmd.Flags().Set("milestone", "aaaaaaaa")
	_ = cmd.Flags().Set("clear", "true")
	if err := runIssueMilestoneSet(cmd, []string{"ADA-57"}); err == nil || !strings.Contains(err.Error(), "either --milestone or --clear") {
		t.Fatalf("expected mutually exclusive flag error, got %v", err)
	}
}

func TestRunIssueMilestoneSetTableOutput(t *testing.T) {
	ts := startIssueRoadmapTestServer(t)
	ts.setEnv(t)

	cmd := newIssueRoadmapTestCmd(addIssueMilestoneSetFlags)
	_ = cmd.Flags().Set("milestone", issueRoadmapTestMilestoneID)
	_ = cmd.Flags().Set("output", "table")
	out, err := captureIssueRoadmapStdout(t, func() error {
		return runIssueMilestoneSet(cmd, []string{"ADA-57"})
	})
	if err != nil {
		t.Fatalf("runIssueMilestoneSet: %v", err)
	}
	for _, want := range []string{"KEY", "ADA-57", issueRoadmapTestMilestoneID} {
		if !strings.Contains(out, want) {
			t.Fatalf("table output missing %q:\n%s", want, out)
		}
	}
}

func TestRunIssueDependencyAddResolvesKeys(t *testing.T) {
	ts := startIssueRoadmapTestServer(t)
	ts.setEnv(t)

	cmd := newIssueRoadmapTestCmd(addIssueDependencyFlags)
	_ = cmd.Flags().Set("depends-on", "ADA-54")
	out, err := captureIssueRoadmapStdout(t, func() error {
		return runIssueDependencyAdd(cmd, []string{"ADA-57"})
	})
	if err != nil {
		t.Fatalf("runIssueDependencyAdd: %v", err)
	}

	body := ts.lastBody(t)
	if body["depends_on_issue_id"] != issueRoadmapTestDependsOnID {
		t.Fatalf("depends_on_issue_id = %v", body["depends_on_issue_id"])
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("decode JSON output: %v\n%s", err, out)
	}
	if payload["issue_id"] != issueRoadmapTestIssueID {
		t.Fatalf("payload = %v", payload)
	}
}

func TestRunIssueDependencyAddTableOutput(t *testing.T) {
	ts := startIssueRoadmapTestServer(t)
	ts.setEnv(t)

	cmd := newIssueRoadmapTestCmd(addIssueDependencyFlags)
	_ = cmd.Flags().Set("depends-on", "ADA-54")
	_ = cmd.Flags().Set("output", "table")
	out, err := captureIssueRoadmapStdout(t, func() error {
		return runIssueDependencyAdd(cmd, []string{"ADA-57"})
	})
	if err != nil {
		t.Fatalf("runIssueDependencyAdd: %v", err)
	}
	for _, want := range []string{"ISSUE", "DEPENDS ON", "ADA-57", "ADA-54", "blocked_by"} {
		if !strings.Contains(out, want) {
			t.Fatalf("table output missing %q:\n%s", want, out)
		}
	}
}

func TestRunIssueDependencyAddRequiresDependsOn(t *testing.T) {
	cmd := newIssueRoadmapTestCmd(addIssueDependencyFlags)
	if err := runIssueDependencyAdd(cmd, []string{"ADA-57"}); err == nil || !strings.Contains(err.Error(), "--depends-on") {
		t.Fatalf("expected --depends-on required error, got %v", err)
	}
}

func TestRunIssueDependencyAddPropagatesAPIError(t *testing.T) {
	ts := startIssueRoadmapTestServer(t)
	ts.setEnv(t)

	cmd := newIssueRoadmapTestCmd(addIssueDependencyFlags)
	_ = cmd.Flags().Set("depends-on", "ADA-57")
	err := runIssueDependencyAdd(cmd, []string{"ADA-57"})
	if err == nil || !strings.Contains(err.Error(), "add dependency") || !strings.Contains(err.Error(), "self dependency is not allowed") {
		t.Fatalf("expected API validation error, got %v", err)
	}
}

func TestRunIssueDependencyRemove(t *testing.T) {
	ts := startIssueRoadmapTestServer(t)
	ts.setEnv(t)

	cmd := newIssueRoadmapTestCmd(addIssueDependencyFlags)
	_ = cmd.Flags().Set("depends-on", "ADA-54")
	out, err := captureIssueRoadmapStdout(t, func() error {
		return runIssueDependencyRemove(cmd, []string{"ADA-57"})
	})
	if err != nil {
		t.Fatalf("runIssueDependencyRemove: %v", err)
	}
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.lastMethod != "DELETE" || !strings.HasSuffix(ts.lastPath, "/dependencies/"+issueRoadmapTestDependsOnID) {
		t.Fatalf("last request = %s %s", ts.lastMethod, ts.lastPath)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("decode JSON output: %v\n%s", err, out)
	}
	if payload["removed"] != true || payload["depends_on_issue_id"] != issueRoadmapTestDependsOnID {
		t.Fatalf("payload = %v", payload)
	}
}
