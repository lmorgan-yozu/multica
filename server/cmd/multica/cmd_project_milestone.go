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

// multica project milestone {list|add|update|remove} — roadmap milestone CRUD
// (ADA-57). Wraps the /api/projects/{id}/milestones endpoints; actor
// contract: any authenticated workspace member or agent, same reach as
// GetProject. Milestones group a project's epics on the roadmap; the
// projection itself stays derived (see docs/adr/0001-roadmap-data-model.md).

var projectMilestoneCmd = &cobra.Command{
	Use:   "milestone",
	Short: "Manage a project's roadmap milestones",
}

var projectMilestoneListCmd = &cobra.Command{
	Use:   "list <project-id>",
	Short: "List a project's milestones with target dates and rolled-up progress",
	Example: `  multica project milestone list 4f8a21c3
  multica project milestone list 4f8a21c3 --output json`,
	Args: exactArgs(1),
	RunE: runProjectMilestoneList,
}

var projectMilestoneAddCmd = &cobra.Command{
	Use:   "add <project-id>",
	Short: "Create a milestone on a project",
	Example: `  multica project milestone add 4f8a21c3 --name "Demo ready" --target-date 2026-07-01
  multica project milestone add 4f8a21c3 --name "Phase 2" --description "Post-demo hardening" --position 10`,
	Args: exactArgs(1),
	RunE: runProjectMilestoneAdd,
}

var projectMilestoneUpdateCmd = &cobra.Command{
	Use:   "update <project-id> <milestone-id>",
	Short: "Update a milestone's name, description, target date, or position",
	Long: `Update a milestone. Only the flags you pass are changed.

Pass --target-date "" to clear the target date.`,
	Example: `  multica project milestone update 4f8a21c3 aaaa1111 --target-date 2026-08-01
  multica project milestone update 4f8a21c3 aaaa1111 --name "Demo ready (rev)"
  multica project milestone update 4f8a21c3 aaaa1111 --target-date ""`,
	Args: exactArgs(2),
	RunE: runProjectMilestoneUpdate,
}

var projectMilestoneRemoveCmd = &cobra.Command{
	Use:     "remove <project-id> <milestone-id>",
	Short:   "Delete a milestone (issues in it become ungrouped, they are not deleted)",
	Example: `  multica project milestone remove 4f8a21c3 aaaa1111`,
	Args:    exactArgs(2),
	RunE:    runProjectMilestoneRemove,
}

func addProjectMilestoneListFlags(cmd *cobra.Command) {
	cmd.Flags().String("output", "table", "Output format: table or json")
	cmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")
}

func addProjectMilestoneAddFlags(cmd *cobra.Command) {
	cmd.Flags().String("name", "", "Milestone name (required)")
	cmd.Flags().String("description", "", "Milestone description")
	cmd.Flags().String("target-date", "", "Target date (YYYY-MM-DD)")
	cmd.Flags().Float64("position", 0, "Sort position among the project's milestones")
	cmd.Flags().String("output", "json", "Output format: table or json")
}

func addProjectMilestoneUpdateFlags(cmd *cobra.Command) {
	cmd.Flags().String("name", "", "New milestone name")
	cmd.Flags().String("description", "", "New milestone description")
	cmd.Flags().String("target-date", "", `New target date (YYYY-MM-DD); pass "" to clear`)
	cmd.Flags().Float64("position", 0, "New sort position")
	cmd.Flags().String("output", "json", "Output format: table or json")
}

func addProjectMilestoneRemoveFlags(cmd *cobra.Command) {
	cmd.Flags().String("output", "table", "Output format: table or json")
}

func init() {
	projectCmd.AddCommand(projectMilestoneCmd)
	projectMilestoneCmd.AddCommand(projectMilestoneListCmd)
	projectMilestoneCmd.AddCommand(projectMilestoneAddCmd)
	projectMilestoneCmd.AddCommand(projectMilestoneUpdateCmd)
	projectMilestoneCmd.AddCommand(projectMilestoneRemoveCmd)

	addProjectMilestoneListFlags(projectMilestoneListCmd)
	addProjectMilestoneAddFlags(projectMilestoneAddCmd)
	addProjectMilestoneUpdateFlags(projectMilestoneUpdateCmd)
	addProjectMilestoneRemoveFlags(projectMilestoneRemoveCmd)
}

// resolveMilestoneID accepts a full UUID or a unique UUID prefix; prefixes
// resolve against the project's roadmap milestones (the only milestone read
// path — there is no standalone list endpoint).
func resolveMilestoneID(ctx context.Context, client *cli.APIClient, projectID, input string) (resolvedID, error) {
	fetch := func(ctx context.Context, client *cli.APIClient) ([]idCandidate, error) {
		milestones, err := fetchProjectRoadmapMilestones(ctx, client, projectID)
		if err != nil {
			return nil, err
		}
		candidates := make([]idCandidate, 0, len(milestones))
		for _, m := range milestones {
			candidates = append(candidates, idCandidate{
				ID:      strVal(m, "id"),
				Display: strVal(m, "name"),
				Detail:  strVal(m, "target_date"),
			})
		}
		return candidates, nil
	}
	return resolveIDByPrefix(ctx, client, "milestone", input, fetch)
}

func fetchProjectRoadmapMilestones(ctx context.Context, client *cli.APIClient, projectID string) ([]map[string]any, error) {
	var roadmap map[string]any
	if err := client.GetJSON(ctx, "/api/projects/"+projectID+"/roadmap", &roadmap); err != nil {
		return nil, err
	}
	raw, _ := roadmap["milestones"].([]any)
	milestones := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if m, ok := item.(map[string]any); ok {
			milestones = append(milestones, m)
		}
	}
	return milestones, nil
}

func runProjectMilestoneList(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	projectRef, err := resolveProjectID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve project: %w", err)
	}
	milestones, err := fetchProjectRoadmapMilestones(ctx, client, projectRef.ID)
	if err != nil {
		return fmt.Errorf("list milestones: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, milestones)
	}
	fullID, _ := cmd.Flags().GetBool("full-id")
	headers := []string{"ID", "NAME", "TARGET", "PROGRESS", "BLOCKED", "EPICS"}
	rows := make([][]string, 0, len(milestones))
	for _, m := range milestones {
		epicIDs, _ := m["epic_ids"].([]any)
		rows = append(rows, []string{
			displayID(strVal(m, "id"), fullID),
			truncateRunes(strVal(m, "name"), 60),
			orDash(strVal(m, "target_date")),
			roadmapProgressCell(m),
			roadmapCountCell(m, "blocked_count"),
			fmt.Sprintf("%d", len(epicIDs)),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runProjectMilestoneAdd(cmd *cobra.Command, args []string) error {
	name, _ := cmd.Flags().GetString("name")
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("--name is required")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	projectRef, err := resolveProjectID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve project: %w", err)
	}

	body := map[string]any{"name": name}
	if description, _ := cmd.Flags().GetString("description"); cmd.Flags().Changed("description") {
		body["description"] = description
	}
	if targetDate, _ := cmd.Flags().GetString("target-date"); cmd.Flags().Changed("target-date") {
		body["target_date"] = targetDate
	}
	if position, _ := cmd.Flags().GetFloat64("position"); cmd.Flags().Changed("position") {
		body["position"] = position
	}

	var milestone map[string]any
	if err := client.PostJSON(ctx, "/api/projects/"+projectRef.ID+"/milestones", body, &milestone); err != nil {
		return fmt.Errorf("create milestone: %w", err)
	}
	return printMilestoneResult(cmd, milestone)
}

func runProjectMilestoneUpdate(cmd *cobra.Command, args []string) error {
	body := map[string]any{}
	if name, _ := cmd.Flags().GetString("name"); cmd.Flags().Changed("name") {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("--name cannot be empty")
		}
		body["name"] = name
	}
	if description, _ := cmd.Flags().GetString("description"); cmd.Flags().Changed("description") {
		body["description"] = description
	}
	if targetDate, _ := cmd.Flags().GetString("target-date"); cmd.Flags().Changed("target-date") {
		// Empty string is an explicit clear; the server keys off field
		// presence, so unchanged dates must stay out of the body entirely.
		body["target_date"] = targetDate
	}
	if position, _ := cmd.Flags().GetFloat64("position"); cmd.Flags().Changed("position") {
		body["position"] = position
	}
	if len(body) == 0 {
		return fmt.Errorf("nothing to update: pass at least one of --name, --description, --target-date, --position")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	projectRef, err := resolveProjectID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve project: %w", err)
	}
	milestoneRef, err := resolveMilestoneID(ctx, client, projectRef.ID, args[1])
	if err != nil {
		return fmt.Errorf("resolve milestone: %w", err)
	}

	var milestone map[string]any
	if err := client.PutJSON(ctx, "/api/projects/"+projectRef.ID+"/milestones/"+milestoneRef.ID, body, &milestone); err != nil {
		return fmt.Errorf("update milestone: %w", err)
	}
	return printMilestoneResult(cmd, milestone)
}

func runProjectMilestoneRemove(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	projectRef, err := resolveProjectID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve project: %w", err)
	}
	milestoneRef, err := resolveMilestoneID(ctx, client, projectRef.ID, args[1])
	if err != nil {
		return fmt.Errorf("resolve milestone: %w", err)
	}

	if err := client.DeleteJSON(ctx, "/api/projects/"+projectRef.ID+"/milestones/"+milestoneRef.ID); err != nil {
		return fmt.Errorf("remove milestone: %w", err)
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, map[string]any{
			"removed":      true,
			"project_id":   projectRef.ID,
			"milestone_id": milestoneRef.ID,
		})
	}
	fmt.Fprintf(os.Stderr, "Milestone %s removed from project %s.\n", milestoneRef.Display, projectRef.Display)
	return nil
}

func printMilestoneResult(cmd *cobra.Command, milestone map[string]any) error {
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, milestone)
	}
	headers := []string{"ID", "NAME", "TARGET", "POSITION"}
	rows := [][]string{{
		strVal(milestone, "id"),
		strVal(milestone, "name"),
		orDash(strVal(milestone, "target_date")),
		fmt.Sprintf("%g", floatVal(milestone, "position")),
	}}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}
