package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

var issueHandoffCmd = &cobra.Command{
	Use:   "handoff",
	Short: "Create and inspect structured issue handoffs",
}

var issueHandoffCreateCmd = &cobra.Command{
	Use:   "create <issue-id>",
	Short: "Create a structured handoff for an issue",
	Long: `Create a structured handoff for an issue. At least one structured
content field is required: work completed, work remaining, decisions made, or
uncertainties/blockers.`,
	Example: `  multica issue handoff create ADA-22 \
    --work-completed-stdin \
    --work-remaining-file remaining.md \
    --follow-up ADA-23 \
    --output json`,
	Args: exactArgs(1),
	RunE: runIssueHandoffCreate,
}

var issueHandoffListCmd = &cobra.Command{
	Use:   "list <issue-id>",
	Short: "List structured handoffs for an issue",
	Args:  exactArgs(1),
	RunE:  runIssueHandoffList,
}

var issueHandoffShowCmd = &cobra.Command{
	Use:     "show <issue-id> <handoff-id>",
	Aliases: []string{"read"},
	Short:   "Show a structured handoff",
	Args:    exactArgs(2),
	RunE:    runIssueHandoffShow,
}

func init() {
	issueHandoffCmd.AddCommand(issueHandoffCreateCmd)
	issueHandoffCmd.AddCommand(issueHandoffListCmd)
	issueHandoffCmd.AddCommand(issueHandoffShowCmd)
	issueCmd.AddCommand(issueHandoffCmd)

	addStructuredHandoffTextFlags(issueHandoffCreateCmd, "work-completed", "Work completed")
	addStructuredHandoffTextFlags(issueHandoffCreateCmd, "work-remaining", "Work remaining")
	addStructuredHandoffTextFlags(issueHandoffCreateCmd, "decisions-made", "Decisions made")
	addStructuredHandoffTextFlags(issueHandoffCreateCmd, "uncertainties", "Uncertainties or blockers")
	issueHandoffCreateCmd.Flags().StringSlice("follow-up", nil, "Follow-up issue key/ID (repeatable)")
	issueHandoffCreateCmd.Flags().String("task-id", "", "Task UUID this handoff belongs to")
	issueHandoffCreateCmd.Flags().String("next-assignee-type", "", "Next assignee type: agent, member, or squad")
	issueHandoffCreateCmd.Flags().String("next-assignee-id", "", "Next assignee UUID")
	issueHandoffCreateCmd.Flags().String("workflow-run-id", "", "Workflow run UUID")
	issueHandoffCreateCmd.Flags().String("workflow-step-id", "", "Workflow step UUID")
	issueHandoffCreateCmd.Flags().String("output", "json", "Output format: table or json")

	issueHandoffListCmd.Flags().String("output", "table", "Output format: table or json")
	issueHandoffListCmd.Flags().Bool("full-id", false, "Show full handoff UUIDs in table output")

	issueHandoffShowCmd.Flags().String("output", "json", "Output format: table or json")
}

func addStructuredHandoffTextFlags(cmd *cobra.Command, name, label string) {
	cmd.Flags().String(name, "", label+" (decodes \\n, \\r, \\t, \\\\; use stdin/file for multi-line content)")
	cmd.Flags().Bool(name+"-stdin", false, "Read "+strings.ToLower(label)+" from stdin")
	cmd.Flags().String(name+"-file", "", "Read "+strings.ToLower(label)+" from a UTF-8 file")
}

func runIssueHandoffCreate(cmd *cobra.Command, args []string) error {
	body := map[string]any{}
	if err := addHandoffTextField(cmd, body, "work-completed", "work_completed"); err != nil {
		return err
	}
	if err := addHandoffTextField(cmd, body, "work-remaining", "work_remaining"); err != nil {
		return err
	}
	if err := addHandoffTextField(cmd, body, "decisions-made", "decisions_made"); err != nil {
		return err
	}
	if err := addHandoffTextField(cmd, body, "uncertainties", "uncertainties"); err != nil {
		return err
	}
	if !hasNonEmptyHandoffContent(body) {
		return fmt.Errorf("at least one structured content field is required (--work-completed, --work-remaining, --decisions-made, or --uncertainties)")
	}

	if err := addOptionalHandoffUUID(body, cmd, "task-id", "task_id"); err != nil {
		return err
	}
	if err := addOptionalHandoffUUID(body, cmd, "next-assignee-id", "next_assignee_id"); err != nil {
		return err
	}
	if err := addOptionalHandoffUUID(body, cmd, "workflow-run-id", "workflow_run_id"); err != nil {
		return err
	}
	if err := addOptionalHandoffUUID(body, cmd, "workflow-step-id", "workflow_step_id"); err != nil {
		return err
	}
	if nextType, _ := cmd.Flags().GetString("next-assignee-type"); strings.TrimSpace(nextType) != "" {
		body["next_assignee_type"] = strings.TrimSpace(nextType)
	}

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

	followUps, _ := cmd.Flags().GetStringSlice("follow-up")
	if len(followUps) > 0 {
		ids := make([]string, 0, len(followUps))
		for _, input := range followUps {
			ref, err := resolveIssueRef(ctx, client, input)
			if err != nil {
				return fmt.Errorf("resolve follow-up issue %q: %w", input, err)
			}
			ids = append(ids, ref.ID)
		}
		body["follow_up_issue_ids"] = ids
	}

	var result map[string]any
	path := "/api/issues/" + url.PathEscape(issueRef.ID) + "/handoffs"
	if err := client.PostJSON(ctx, path, body, &result); err != nil {
		return fmt.Errorf("create handoff: %w", err)
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	printHandoffTable([]map[string]any{result}, false, issueRef.Display)
	return nil
}

func addHandoffTextField(cmd *cobra.Command, body map[string]any, flagName, jsonName string) error {
	value, ok, err := resolveTextFlag(cmd, flagName)
	if err != nil {
		return err
	}
	if ok {
		body[jsonName] = strings.TrimSpace(value)
	}
	return nil
}

func addOptionalHandoffUUID(body map[string]any, cmd *cobra.Command, flagName, jsonName string) error {
	value, _ := cmd.Flags().GetString(flagName)
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if !uuidRegexp.MatchString(value) {
		return fmt.Errorf("--%s must be a full UUID", flagName)
	}
	body[jsonName] = value
	return nil
}

func hasNonEmptyHandoffContent(body map[string]any) bool {
	for _, key := range []string{"work_completed", "work_remaining", "decisions_made", "uncertainties"} {
		if value, _ := body[key].(string); strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

func runIssueHandoffList(cmd *cobra.Command, args []string) error {
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

	handoffs, err := listIssueHandoffs(ctx, client, issueRef.ID)
	if err != nil {
		return err
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, handoffs)
	}
	fullID, _ := cmd.Flags().GetBool("full-id")
	printHandoffTable(handoffs, fullID, issueRef.Display)
	return nil
}

func runIssueHandoffShow(cmd *cobra.Command, args []string) error {
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
	handoffID, err := resolveIssueHandoffID(ctx, client, issueRef.ID, args[1])
	if err != nil {
		return err
	}

	var result map[string]any
	if err := client.GetJSON(ctx, "/api/handoffs/"+url.PathEscape(handoffID), &result); err != nil {
		return fmt.Errorf("get handoff: %w", err)
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	printHandoffTable([]map[string]any{result}, true, issueRef.Display)
	return nil
}

func listIssueHandoffs(ctx context.Context, client *cli.APIClient, issueID string) ([]map[string]any, error) {
	var raw []any
	if err := client.GetJSON(ctx, "/api/issues/"+url.PathEscape(issueID)+"/handoffs", &raw); err != nil {
		return nil, fmt.Errorf("list handoffs: %w", err)
	}
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out, nil
}

func resolveIssueHandoffID(ctx context.Context, client *cli.APIClient, issueID, input string) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", fmt.Errorf("handoff id is required")
	}
	if uuidRegexp.MatchString(input) {
		return input, nil
	}
	prefix, err := normalizeUUIDPrefix(input)
	if err != nil {
		return "", fmt.Errorf("resolve handoff: %w", err)
	}
	handoffs, err := listIssueHandoffs(ctx, client, issueID)
	if err != nil {
		return "", err
	}
	matches := make([]string, 0, 1)
	for _, handoff := range handoffs {
		id := strVal(handoff, "id")
		if strings.HasPrefix(compactUUID(id), prefix) {
			matches = append(matches, id)
		}
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no handoff found matching id prefix %q on issue", input)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("ambiguous handoff id prefix %q; use more characters or run `multica issue handoff list <issue> --full-id`", input)
	}
}

func printHandoffTable(handoffs []map[string]any, fullID bool, issueDisplay string) {
	headers := []string{"ID", "ISSUE", "CREATED", "DONE", "REMAINING", "DECISIONS", "UNCERTAINTIES"}
	rows := make([][]string, 0, len(handoffs))
	for _, handoff := range handoffs {
		id := strVal(handoff, "id")
		rows = append(rows, []string{
			displayID(id, fullID),
			issueDisplay,
			formatHandoffCreatedAt(strVal(handoff, "created_at")),
			truncateRunes(strVal(handoff, "work_completed"), 60),
			truncateRunes(strVal(handoff, "work_remaining"), 60),
			truncateRunes(strVal(handoff, "decisions_made"), 60),
			truncateRunes(strVal(handoff, "uncertainties"), 60),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
}

func formatHandoffCreatedAt(value string) string {
	if len(value) >= len("2006-01-02T15:04") {
		return value[:len("2006-01-02T15:04")]
	}
	return value
}

func truncateRunes(value string, max int) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), "\n", " ")
	if utf8.RuneCountInString(value) <= max {
		return value
	}
	runes := []rune(value)
	if max <= 3 {
		return string(runes[:max])
	}
	return string(runes[:max-3]) + "..."
}
