package main

// `multica team-template` — list, inspect, and export team templates
// (ADA-30). Export takes its request as a JSON file (--file) because the
// selection (agents, skills, workflows, autopilots, parameters,
// substitutions) is structured; convenience flags cover only name and
// notes. The endpoint is human owner/admin only — agent tokens get 403.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var teamTemplateCmd = &cobra.Command{
	Use:     "team-template",
	Aliases: []string{"team-templates"},
	Short:   "Work with team templates (export a delivery team for reuse on new engagements)",
}

var teamTemplateListCmd = &cobra.Command{
	Use:   "list",
	Short: "List team templates in the workspace",
	RunE:  runTeamTemplateList,
}

var teamTemplateGetCmd = &cobra.Command{
	Use:   "get <template-id>",
	Short: "Get a team template and its version history",
	Args:  exactArgs(1),
	RunE:  runTeamTemplateGet,
}

var teamTemplateVersionCmd = &cobra.Command{
	Use:   "version <template-id> <version>",
	Short: "Get one template version including the full manifest",
	Args:  exactArgs(2),
	RunE:  runTeamTemplateVersion,
}

var teamTemplateExportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export a team template from this workspace",
	Long: `Export a team template from this workspace.

The export request is supplied as JSON via --file. Shape:

  {
    "name": "Yozu delivery team",
    "description": "...",
    "agent_ids": ["<uuid>", ...],
    "squad_id": "<uuid>",
    "workflow_ids": ["<uuid>", ...],
    "autopilot_ids": ["<uuid>", ...],
    "skill_ids": ["<uuid>", ...],
    "parameters": [{"key": "repo_url", "label": "Repository URL", "required": true}],
    "substitutions": [{"find": "https://github.com/acme/app", "param_key": "repo_url"}]
  }

Set "template_id" instead of "name" to append a new version to an
existing template. Export fails with structured findings when the
selection carries autonomy language, a blocked skill, or a secret.`,
	RunE: runTeamTemplateExport,
}

func init() {
	teamTemplateListCmd.Flags().String("output", "table", "Output format: table or json")
	teamTemplateGetCmd.Flags().String("output", "json", "Output format: table or json")
	teamTemplateVersionCmd.Flags().String("output", "json", "Output format: json")
	teamTemplateExportCmd.Flags().String("file", "", "Path to the JSON export request (required)")
	teamTemplateExportCmd.Flags().String("name", "", "Template name (overrides the request file)")
	teamTemplateExportCmd.Flags().String("notes", "", "Version notes (overrides the request file)")
	teamTemplateExportCmd.Flags().String("output", "json", "Output format: json")
	_ = teamTemplateExportCmd.MarkFlagRequired("file")

	teamTemplateCmd.AddCommand(teamTemplateListCmd, teamTemplateGetCmd, teamTemplateVersionCmd, teamTemplateExportCmd)
}

func runTeamTemplateList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var templates []map[string]any
	if err := client.GetJSON(ctx, "/api/team-templates", &templates); err != nil {
		return fmt.Errorf("list team templates: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, templates)
	}

	headers := []string{"ID", "NAME", "LATEST_VERSION", "DESCRIPTION", "UPDATED_AT"}
	rows := make([][]string, 0, len(templates))
	for _, t := range templates {
		latest := ""
		if v, ok := t["latest_version"].(float64); ok {
			latest = strconv.Itoa(int(v))
		}
		rows = append(rows, []string{
			strVal(t, "id"),
			strVal(t, "name"),
			latest,
			strVal(t, "description"),
			strVal(t, "updated_at"),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runTeamTemplateGet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var tmpl map[string]any
	if err := client.GetJSON(ctx, "/api/team-templates/"+args[0], &tmpl); err != nil {
		return fmt.Errorf("get team template: %w", err)
	}
	return cli.PrintJSON(os.Stdout, tmpl)
}

func runTeamTemplateVersion(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if _, err := strconv.Atoi(args[1]); err != nil {
		return fmt.Errorf("version must be a number, got %q", args[1])
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var version map[string]any
	if err := client.GetJSON(ctx, "/api/team-templates/"+args[0]+"/versions/"+args[1], &version); err != nil {
		return fmt.Errorf("get template version: %w", err)
	}
	return cli.PrintJSON(os.Stdout, version)
}

func runTeamTemplateExport(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	file, _ := cmd.Flags().GetString("file")
	raw, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("read export request: %w", err)
	}
	var req map[string]any
	if err := json.Unmarshal(raw, &req); err != nil {
		return fmt.Errorf("export request is not valid JSON: %w", err)
	}
	if name, _ := cmd.Flags().GetString("name"); name != "" {
		req["name"] = name
	}
	if notes, _ := cmd.Flags().GetString("notes"); notes != "" {
		req["notes"] = notes
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var resp map[string]any
	if err := client.PostJSON(ctx, "/api/team-templates/export", req, &resp); err != nil {
		return fmt.Errorf("export team template: %w", err)
	}
	return cli.PrintJSON(os.Stdout, resp)
}
