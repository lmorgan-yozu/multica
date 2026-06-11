package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

const (
	testHandoffIssueID   = "11111111-1111-1111-1111-111111111111"
	testFollowUpIssueID  = "22222222-2222-2222-2222-222222222222"
	testHandoffID        = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	testOtherHandoffID   = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	testHandoffWorkspace = "33333333-3333-3333-3333-333333333333"
)

func newIssueHandoffCreateTestCmd() *cobra.Command {
	c := &cobra.Command{Use: "create"}
	addStructuredHandoffTextFlags(c, "work-completed", "Work completed")
	addStructuredHandoffTextFlags(c, "work-remaining", "Work remaining")
	addStructuredHandoffTextFlags(c, "decisions-made", "Decisions made")
	addStructuredHandoffTextFlags(c, "uncertainties", "Uncertainties")
	c.Flags().StringSlice("follow-up", nil, "")
	c.Flags().String("task-id", "", "")
	c.Flags().String("next-assignee-type", "", "")
	c.Flags().String("next-assignee-id", "", "")
	c.Flags().String("workflow-run-id", "", "")
	c.Flags().String("workflow-step-id", "", "")
	c.Flags().String("output", "json", "")
	return c
}

func newIssueHandoffListTestCmd() *cobra.Command {
	c := &cobra.Command{Use: "list"}
	c.Flags().String("output", "json", "")
	c.Flags().Bool("full-id", false, "")
	return c
}

func newIssueHandoffShowTestCmd() *cobra.Command {
	c := &cobra.Command{Use: "show"}
	c.Flags().String("output", "json", "")
	return c
}

func issueHandoffTestServer(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(handler))
	t.Cleanup(srv.Close)
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", testHandoffWorkspace)
	t.Setenv("MULTICA_TOKEN", "test-token")
	return srv
}

func writeTestIssue(w http.ResponseWriter, id, identifier string) {
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":         id,
		"identifier": identifier,
		"title":      "Issue " + identifier,
	})
}

func writeTestHandoff(w http.ResponseWriter, id string) {
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":                  id,
		"workspace_id":        testHandoffWorkspace,
		"issue_id":            testHandoffIssueID,
		"work_completed":      "Implemented CLI support.",
		"work_remaining":      "Review and merge.",
		"decisions_made":      "Use issue-scoped commands.",
		"uncertainties":       "",
		"follow_up_issue_ids": []string{testFollowUpIssueID},
		"created_at":          "2026-06-11T18:00:00Z",
		"updated_at":          "2026-06-11T18:00:00Z",
	})
}

func writeTestHandoffList(w http.ResponseWriter) {
	_ = json.NewEncoder(w).Encode([]map[string]any{
		{
			"id":                  testHandoffID,
			"workspace_id":        testHandoffWorkspace,
			"issue_id":            testHandoffIssueID,
			"work_completed":      "Implemented CLI support.",
			"work_remaining":      "Review and merge.",
			"decisions_made":      "Use issue-scoped commands.",
			"uncertainties":       "",
			"follow_up_issue_ids": []string{testFollowUpIssueID},
			"created_at":          "2026-06-11T18:00:00Z",
			"updated_at":          "2026-06-11T18:00:00Z",
		},
		{
			"id":                  testOtherHandoffID,
			"workspace_id":        testHandoffWorkspace,
			"issue_id":            testHandoffIssueID,
			"work_completed":      "Second handoff.",
			"work_remaining":      "",
			"decisions_made":      "",
			"uncertainties":       "",
			"follow_up_issue_ids": []string{},
			"created_at":          "2026-06-11T19:00:00Z",
			"updated_at":          "2026-06-11T19:00:00Z",
		},
	})
}

func TestRunIssueHandoffCreateResolvesRefsAndPostsStructuredFields(t *testing.T) {
	var gotBody map[string]any
	var paths []string
	issueHandoffTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/ADA-22":
			writeTestIssue(w, testHandoffIssueID, "ADA-22")
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/ADA-23":
			writeTestIssue(w, testFollowUpIssueID, "ADA-23")
		case r.Method == http.MethodPost && r.URL.Path == "/api/issues/"+testHandoffIssueID+"/handoffs":
			if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
				t.Fatalf("decode create body: %v", err)
			}
			writeTestHandoff(w, testHandoffID)
		default:
			http.NotFound(w, r)
		}
	})

	cmd := newIssueHandoffCreateTestCmd()
	_ = cmd.Flags().Set("work-completed", " Done \\n with API ")
	_ = cmd.Flags().Set("work-remaining", "Review")
	_ = cmd.Flags().Set("decisions-made", "Issue-scoped CLI")
	_ = cmd.Flags().Set("follow-up", "ADA-23")
	_ = cmd.Flags().Set("output", "json")

	out, err := captureStdout(t, func() error {
		return runIssueHandoffCreate(cmd, []string{"ADA-22"})
	})
	if err != nil {
		t.Fatalf("runIssueHandoffCreate: %v\npaths: %v", err, paths)
	}
	if gotBody["work_completed"] != "Done \n with API" {
		t.Fatalf("work_completed body = %#v", gotBody["work_completed"])
	}
	if gotBody["work_remaining"] != "Review" || gotBody["decisions_made"] != "Issue-scoped CLI" {
		t.Fatalf("structured body = %#v", gotBody)
	}
	followUps, ok := gotBody["follow_up_issue_ids"].([]any)
	if !ok || len(followUps) != 1 || followUps[0] != testFollowUpIssueID {
		t.Fatalf("follow_up_issue_ids = %#v", gotBody["follow_up_issue_ids"])
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode stdout JSON: %v\n%s", err, out)
	}
	if got["id"] != testHandoffID {
		t.Fatalf("stdout id = %v, want %s", got["id"], testHandoffID)
	}
}

func TestRunIssueHandoffCreateReadsStructuredFieldFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "done.md")
	if err := os.WriteFile(path, []byte("Implemented from file.\n"), 0o600); err != nil {
		t.Fatalf("write test file: %v", err)
	}
	var gotBody map[string]any
	issueHandoffTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/ADA-22":
			writeTestIssue(w, testHandoffIssueID, "ADA-22")
		case r.Method == http.MethodPost && r.URL.Path == "/api/issues/"+testHandoffIssueID+"/handoffs":
			if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
				t.Fatalf("decode create body: %v", err)
			}
			writeTestHandoff(w, testHandoffID)
		default:
			http.NotFound(w, r)
		}
	})

	cmd := newIssueHandoffCreateTestCmd()
	_ = cmd.Flags().Set("work-completed-file", path)
	_, err := captureStdout(t, func() error {
		return runIssueHandoffCreate(cmd, []string{"ADA-22"})
	})
	if err != nil {
		t.Fatalf("runIssueHandoffCreate: %v", err)
	}
	if gotBody["work_completed"] != "Implemented from file." {
		t.Fatalf("work_completed body = %#v", gotBody["work_completed"])
	}
}

func TestRunIssueHandoffCreateRejectsEmptyStructuredContent(t *testing.T) {
	cmd := newIssueHandoffCreateTestCmd()
	err := runIssueHandoffCreate(cmd, []string{"ADA-22"})
	if err == nil {
		t.Fatal("runIssueHandoffCreate returned nil, want empty content error")
	}
	if !strings.Contains(err.Error(), "at least one structured content field is required") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunIssueHandoffCreateReportsInvalidFollowUpIssue(t *testing.T) {
	issueHandoffTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/ADA-22":
			writeTestIssue(w, testHandoffIssueID, "ADA-22")
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/NOPE-1":
			http.Error(w, "not found", http.StatusNotFound)
		default:
			http.NotFound(w, r)
		}
	})

	cmd := newIssueHandoffCreateTestCmd()
	_ = cmd.Flags().Set("work-completed", "done")
	_ = cmd.Flags().Set("follow-up", "NOPE-1")
	_, err := captureStdout(t, func() error {
		return runIssueHandoffCreate(cmd, []string{"ADA-22"})
	})
	if err == nil {
		t.Fatal("runIssueHandoffCreate returned nil, want follow-up resolution error")
	}
	if !strings.Contains(err.Error(), "resolve follow-up issue \"NOPE-1\"") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunIssueHandoffListPrintsJSONResponse(t *testing.T) {
	issueHandoffTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/ADA-22":
			writeTestIssue(w, testHandoffIssueID, "ADA-22")
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/"+testHandoffIssueID+"/handoffs":
			writeTestHandoffList(w)
		default:
			http.NotFound(w, r)
		}
	})

	cmd := newIssueHandoffListTestCmd()
	_ = cmd.Flags().Set("output", "json")
	out, err := captureStdout(t, func() error {
		return runIssueHandoffList(cmd, []string{"ADA-22"})
	})
	if err != nil {
		t.Fatalf("runIssueHandoffList: %v", err)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode stdout JSON: %v\n%s", err, out)
	}
	if len(got) != 2 || got[0]["id"] != testHandoffID {
		t.Fatalf("stdout = %#v", got)
	}
}

func TestRunIssueHandoffShowResolvesShortIDWithinIssue(t *testing.T) {
	var paths []string
	issueHandoffTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/ADA-22":
			writeTestIssue(w, testHandoffIssueID, "ADA-22")
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/"+testHandoffIssueID+"/handoffs":
			writeTestHandoffList(w)
		case r.Method == http.MethodGet && r.URL.Path == "/api/handoffs/"+testHandoffID:
			writeTestHandoff(w, testHandoffID)
		default:
			http.NotFound(w, r)
		}
	})

	cmd := newIssueHandoffShowTestCmd()
	_ = cmd.Flags().Set("output", "json")
	out, err := captureStdout(t, func() error {
		return runIssueHandoffShow(cmd, []string{"ADA-22", "aaaaaaaa"})
	})
	if err != nil {
		t.Fatalf("runIssueHandoffShow: %v\npaths: %v", err, paths)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode stdout JSON: %v\n%s", err, out)
	}
	if got["id"] != testHandoffID {
		t.Fatalf("stdout id = %v, want %s", got["id"], testHandoffID)
	}
}
