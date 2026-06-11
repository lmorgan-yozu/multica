package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var usageCmd = &cobra.Command{
	Use:   "usage",
	Short: "Show workspace token usage aggregates",
	RunE:  runUsage,
}

func init() {
	usageCmd.Flags().String("output", "table", "Output format: table or json")
	usageCmd.Flags().Int("days", 30, "Number of days of usage data to retrieve (max 365)")
	usageCmd.Flags().String("project", "", "Filter by project ID")
	usageCmd.Flags().String("agent-id", "", "Filter by agent UUID")
}

func runUsage(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if client.WorkspaceID == "" {
		if _, err := requireWorkspaceID(cmd); err != nil {
			return err
		}
	}

	params := url.Values{}
	if days, _ := cmd.Flags().GetInt("days"); days > 0 {
		params.Set("days", fmt.Sprintf("%d", days))
	}
	if v, _ := cmd.Flags().GetString("project"); v != "" {
		project, err := resolveProjectID(ctx, client, v)
		if err != nil {
			return err
		}
		params.Set("project_id", project.ID)
	}
	if v, _ := cmd.Flags().GetString("agent-id"); v != "" {
		params.Set("agent_id", v)
	}

	path := "/api/dashboard/usage/by-agent"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var rows []map[string]any
	if err := client.GetJSON(ctx, path, &rows); err != nil {
		return fmt.Errorf("get usage: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, rows)
	}

	printUsageTable(rows)
	return nil
}

func printUsageTable(rows []map[string]any) {
	headers := []string{"AGENT", "MODEL", "INPUT", "OUTPUT", "CACHE READ", "CACHE WRITE", "TASKS"}
	tableRows := make([][]string, 0, len(rows))
	for _, row := range rows {
		tableRows = append(tableRows, []string{
			truncateID(strVal(row, "agent_id")),
			strVal(row, "model"),
			strVal(row, "input_tokens"),
			strVal(row, "output_tokens"),
			strVal(row, "cache_read_tokens"),
			strVal(row, "cache_write_tokens"),
			strVal(row, "task_count"),
		})
	}
	cli.PrintTable(os.Stdout, headers, tableRows)
}
