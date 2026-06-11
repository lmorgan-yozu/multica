package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var projectRoadmapCmd = &cobra.Command{
	Use:   "roadmap <project-id>",
	Short: "Show a project's roadmap: milestones, epics, progress, and dependencies",
	Long: `Show the derived roadmap for a project.

Epics are the project's top-level issues, ordered by dependency links first
and then by due date, start date, and position. Progress counts done vs
total leaf issues under each epic; milestones group epics and aggregate
their progress. The projection is read-only and always derived from the
live issue graph — there is nothing to hand-maintain.`,
	Example: `  # Human-readable scan
  multica project roadmap 4f8a21c3

  # Same projection as GET /api/projects/{id}/roadmap
  multica project roadmap "Ada Bootstrapping" --output json`,
	Args: exactArgs(1),
	RunE: runProjectRoadmap,
}

func init() {
	projectCmd.AddCommand(projectRoadmapCmd)
	projectRoadmapCmd.Flags().String("output", "table", "Output format: table or json")
}

func runProjectRoadmap(cmd *cobra.Command, args []string) error {
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

	var roadmap map[string]any
	if err := client.GetJSON(ctx, "/api/projects/"+projectRef.ID+"/roadmap", &roadmap); err != nil {
		return fmt.Errorf("get roadmap: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, roadmap)
	}

	epicsRaw, _ := roadmap["epics"].([]any)
	milestonesRaw, _ := roadmap["milestones"].([]any)

	// Map epic id -> identifier so dependency and grouping columns can show
	// routable keys instead of UUIDs.
	identifierByID := make(map[string]string, len(epicsRaw))
	for _, raw := range epicsRaw {
		if e, ok := raw.(map[string]any); ok {
			identifierByID[strVal(e, "id")] = strVal(e, "identifier")
		}
	}
	milestoneNameByID := make(map[string]string, len(milestonesRaw))
	for _, raw := range milestonesRaw {
		if m, ok := raw.(map[string]any); ok {
			milestoneNameByID[strVal(m, "id")] = strVal(m, "name")
		}
	}

	fmt.Fprintf(os.Stdout, "Project: %s\n", strVal(roadmap, "project_title"))

	if len(milestonesRaw) > 0 {
		fmt.Fprintln(os.Stdout)
		headers := []string{"MILESTONE", "TARGET", "PROGRESS", "BLOCKED", "EPICS"}
		rows := make([][]string, 0, len(milestonesRaw))
		for _, raw := range milestonesRaw {
			m, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			epicIDs, _ := m["epic_ids"].([]any)
			keys := make([]string, 0, len(epicIDs))
			for _, id := range epicIDs {
				if s, ok := id.(string); ok {
					keys = append(keys, identifierByID[s])
				}
			}
			rows = append(rows, []string{
				strVal(m, "name"),
				orDash(strVal(m, "target_date")),
				roadmapProgressCell(m),
				roadmapCountCell(m, "blocked_count"),
				orDash(strings.Join(keys, ", ")),
			})
		}
		cli.PrintTable(os.Stdout, headers, rows)
	}

	fmt.Fprintln(os.Stdout)
	headers := []string{"KEY", "TITLE", "STATUS", "PROGRESS", "BLOCKED", "DUE", "DEPENDS ON", "MILESTONE"}
	rows := make([][]string, 0, len(epicsRaw))
	for _, raw := range epicsRaw {
		e, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		dependsOn, _ := e["depends_on"].([]any)
		depKeys := make([]string, 0, len(dependsOn))
		for _, id := range dependsOn {
			if s, ok := id.(string); ok {
				depKeys = append(depKeys, identifierByID[s])
			}
		}
		milestoneName := ""
		if msID, ok := e["milestone_id"].(string); ok {
			milestoneName = milestoneNameByID[msID]
		}
		rows = append(rows, []string{
			strVal(e, "identifier"),
			truncateRunes(strVal(e, "title"), 60),
			strVal(e, "status"),
			roadmapProgressCell(e),
			roadmapCountCell(e, "blocked_count"),
			orDash(strVal(e, "due_date")),
			orDash(strings.Join(depKeys, ", ")),
			orDash(milestoneName),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)

	if detected, _ := roadmap["cycle_detected"].(bool); detected {
		fmt.Fprintln(os.Stderr, "Warning: dependency cycle detected; affected epics are listed last in fallback order (see cycle_issue_ids in --output json).")
	}
	return nil
}

func roadmapProgressCell(node map[string]any) string {
	progress, _ := node["progress"].(map[string]any)
	done, _ := progress["done"].(float64)
	total, _ := progress["total"].(float64)
	return fmt.Sprintf("%d/%d", int(done), int(total))
}

func roadmapCountCell(node map[string]any, key string) string {
	n, _ := node[key].(float64)
	if n == 0 {
		return "—"
	}
	return fmt.Sprintf("%d", int(n))
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func truncateRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max-1]) + "…"
}
