package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// multica issue milestone {set|clear} and multica issue dependency
// {add|remove} — the roadmap write verbs on the issue side (ADA-57). They
// wrap PUT /api/issues/{id}/milestone and POST/DELETE
// /api/issues/{id}/dependencies; actor contract: any authenticated
// workspace member or agent on issues in their workspace. The server
// enforces same-project membership for both link kinds and rejects
// dependency cycles at write time.

var issueMilestoneCmd = &cobra.Command{
	Use:   "milestone",
	Short: "Assign an issue (epic) to a roadmap milestone",
}

var issueMilestoneSetCmd = &cobra.Command{
	Use:   "set <issue-id> <milestone-id>",
	Short: "Put an issue in a milestone (must belong to the issue's project)",
	Example: `  multica issue milestone set ADA-54 aaaa1111
  multica issue milestone set ADA-54 aaaa1111 --output json`,
	Args: exactArgs(2),
	RunE: runIssueMilestoneSet,
}

var issueMilestoneClearCmd = &cobra.Command{
	Use:     "clear <issue-id>",
	Short:   "Remove an issue from its milestone",
	Example: `  multica issue milestone clear ADA-54`,
	Args:    exactArgs(1),
	RunE:    runIssueMilestoneClear,
}

var issueDependencyCmd = &cobra.Command{
	Use:   "dependency",
	Short: "Manage roadmap depends-on links between issues",
}

var issueDependencyAddCmd = &cobra.Command{
	Use:   "add <issue-id> <depends-on-issue-id>",
	Short: "Record that the first issue depends on the second landing first",
	Long: `Record a roadmap dependency: <issue-id> depends on <depends-on-issue-id>.

Both issues must belong to the same project. Links that would close a
cycle are rejected. Dependency links between top-level issues (epics)
drive roadmap ordering.`,
	Example: `  # ADA-57 depends on ADA-54 (ADA-54 must land first)
  multica issue dependency add ADA-57 ADA-54`,
	Args: exactArgs(2),
	RunE: runIssueDependencyAdd,
}

var issueDependencyRemoveCmd = &cobra.Command{
	Use:     "remove <issue-id> <depends-on-issue-id>",
	Short:   "Remove a roadmap depends-on link (idempotent)",
	Example: `  multica issue dependency remove ADA-57 ADA-54`,
	Args:    exactArgs(2),
	RunE:    runIssueDependencyRemove,
}

func addIssueMilestoneOutputFlag(cmd *cobra.Command) {
	cmd.Flags().String("output", "json", "Output format: table or json")
}

func addIssueDependencyOutputFlag(cmd *cobra.Command) {
	cmd.Flags().String("output", "json", "Output format: table or json")
}

func init() {
	issueCmd.AddCommand(issueMilestoneCmd)
	issueMilestoneCmd.AddCommand(issueMilestoneSetCmd)
	issueMilestoneCmd.AddCommand(issueMilestoneClearCmd)
	addIssueMilestoneOutputFlag(issueMilestoneSetCmd)
	addIssueMilestoneOutputFlag(issueMilestoneClearCmd)

	issueCmd.AddCommand(issueDependencyCmd)
	issueDependencyCmd.AddCommand(issueDependencyAddCmd)
	issueDependencyCmd.AddCommand(issueDependencyRemoveCmd)
	addIssueDependencyOutputFlag(issueDependencyAddCmd)
	addIssueDependencyOutputFlag(issueDependencyRemoveCmd)
}

func runIssueMilestoneSet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	issueRef, err := resolveIssueRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}
	// A full milestone UUID goes straight through; a prefix resolves
	// against the issue's project roadmap (the only milestone read path).
	milestoneInput := strings.TrimSpace(args[1])
	var milestoneRef resolvedID
	if uuidRegexp.MatchString(milestoneInput) {
		milestoneRef = resolvedID{ID: milestoneInput, Display: milestoneInput}
	} else {
		projectID, err := fetchIssueProjectID(ctx, client, issueRef.ID)
		if err != nil {
			return fmt.Errorf("resolve milestone: %w", err)
		}
		milestoneRef, err = resolveMilestoneID(ctx, client, projectID, milestoneInput)
		if err != nil {
			return fmt.Errorf("resolve milestone: %w", err)
		}
	}

	body := map[string]any{"milestone_id": milestoneRef.ID}
	var issue map[string]any
	if err := client.PutJSON(ctx, "/api/issues/"+issueRef.ID+"/milestone", body, &issue); err != nil {
		return fmt.Errorf("set issue milestone: %w", err)
	}
	return printIssueMilestoneResult(cmd, issue)
}

func runIssueMilestoneClear(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	issueRef, err := resolveIssueRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}

	body := map[string]any{"milestone_id": nil}
	var issue map[string]any
	if err := client.PutJSON(ctx, "/api/issues/"+issueRef.ID+"/milestone", body, &issue); err != nil {
		return fmt.Errorf("clear issue milestone: %w", err)
	}
	return printIssueMilestoneResult(cmd, issue)
}

// fetchIssueProjectID loads the issue and returns its project id, erroring
// when the issue is not in a project (roadmap verbs need one).
func fetchIssueProjectID(ctx context.Context, client *cli.APIClient, issueID string) (string, error) {
	var issue map[string]any
	if err := client.GetJSON(ctx, "/api/issues/"+issueID, &issue); err != nil {
		return "", err
	}
	projectID := strVal(issue, "project_id")
	if projectID == "" {
		return "", fmt.Errorf("issue %s is not in a project", issueDisplayKey(issue))
	}
	return projectID, nil
}

func printIssueMilestoneResult(cmd *cobra.Command, issue map[string]any) error {
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, issue)
	}
	headers := []string{"KEY", "TITLE", "MILESTONE"}
	milestoneID, _ := issue["milestone_id"].(string)
	rows := [][]string{{
		issueDisplayKey(issue),
		truncateRunes(strVal(issue, "title"), 60),
		orDash(milestoneID),
	}}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runIssueDependencyAdd(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	issueRef, err := resolveIssueRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}
	dependsOnRef, err := resolveIssueRef(ctx, client, args[1])
	if err != nil {
		return fmt.Errorf("resolve depends-on issue: %w", err)
	}

	body := map[string]any{"depends_on_issue_id": dependsOnRef.ID}
	var link map[string]any
	if err := client.PostJSON(ctx, "/api/issues/"+issueRef.ID+"/dependencies", body, &link); err != nil {
		return fmt.Errorf("add dependency: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, link)
	}
	headers := []string{"ISSUE", "DEPENDS ON", "TYPE"}
	rows := [][]string{{issueRef.Display, dependsOnRef.Display, strVal(link, "type")}}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runIssueDependencyRemove(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	issueRef, err := resolveIssueRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}
	dependsOnRef, err := resolveIssueRef(ctx, client, args[1])
	if err != nil {
		return fmt.Errorf("resolve depends-on issue: %w", err)
	}

	if err := client.DeleteJSON(ctx, "/api/issues/"+issueRef.ID+"/dependencies/"+dependsOnRef.ID); err != nil {
		return fmt.Errorf("remove dependency: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Dependency %s -> %s removed.\n", issueRef.Display, dependsOnRef.Display)
	return nil
}
