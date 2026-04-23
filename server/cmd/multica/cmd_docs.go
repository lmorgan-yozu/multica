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

// docsCmd is the entry point for the document library commands. Agents use
// these to browse and organise the documents produced during a project's
// lifetime; humans can invoke the same commands for parity.
var docsCmd = &cobra.Command{
	Use:   "docs",
	Short: "Work with the document library",
}

var docsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List documents in an issue or project library",
	Long: `List documents for an issue or project. Exactly one of --issue and
--project must be supplied.`,
	Example: `  # List documents attached to an issue
  $ multica docs list --issue abc123

  # List documents across every issue in a project
  $ multica docs list --project proj-xyz`,
	RunE: runDocsList,
}

var docsCurateCmd = &cobra.Command{
	Use:   "curate <attachment-id>",
	Short: "Set curation metadata (title, summary, category, tags, order) on a document",
	Long: `Update the curation fields for an attachment. Any flag you omit is
left untouched; this is the intended way for agents to organise the
growing set of documents produced during a project without trampling
earlier decisions.`,
	Example: `  # Categorise a doc and give it a human-friendly title
  $ multica docs curate 019dbb... --category Research --title "Competitive landscape"

  # Pin a doc and add tags
  $ multica docs curate 019dbb... --pinned --tag strategy --tag investor-pack

  # Archive a superseded doc
  $ multica docs curate 019dbb... --archived`,
	Args: exactArgs(1),
	RunE: runDocsCurate,
}

var docsSectionCmd = &cobra.Command{
	Use:   "section",
	Short: "Manage library sections (named groupings)",
}

var docsSectionCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new library section",
	Long: `Create a section at issue or project scope. Documents get placed
into sections via ` + "`multica docs section add-item`" + `.`,
	RunE: runDocsSectionCreate,
}

var docsSectionAddItemCmd = &cobra.Command{
	Use:   "add-item <section-id> <attachment-id>",
	Short: "Place a document into a library section",
	Args:  exactArgs(2),
	RunE:  runDocsSectionAddItem,
}

func init() {
	docsCmd.AddCommand(docsListCmd)
	docsCmd.AddCommand(docsCurateCmd)
	docsCmd.AddCommand(docsSectionCmd)
	docsSectionCmd.AddCommand(docsSectionCreateCmd)
	docsSectionCmd.AddCommand(docsSectionAddItemCmd)

	docsListCmd.Flags().String("issue", "", "Issue ID to list documents for")
	docsListCmd.Flags().String("project", "", "Project ID to list documents for")
	docsListCmd.Flags().String("output", "json", "Output format: json or table")

	docsCurateCmd.Flags().String("title", "", "Human-friendly title (overrides filename)")
	docsCurateCmd.Flags().String("summary", "", "Short description surfaced in lists")
	docsCurateCmd.Flags().String("category", "", `Category (e.g. "Research", "Architecture")`)
	docsCurateCmd.Flags().StringSlice("tag", nil, "Tag (repeatable); sets the full tag list when provided")
	docsCurateCmd.Flags().Float64("sort-order", 0, "Manual sort order within the category")
	docsCurateCmd.Flags().Bool("pinned", false, "Pin to the top of the library")
	docsCurateCmd.Flags().Bool("unpinned", false, "Unpin (clears the pinned flag)")
	docsCurateCmd.Flags().Bool("archived", false, "Archive (hide from default views)")
	docsCurateCmd.Flags().Bool("unarchived", false, "Unarchive (restore to default views)")

	docsSectionCreateCmd.Flags().String("name", "", "Section name (required)")
	docsSectionCreateCmd.Flags().String("description", "", "Optional section description")
	docsSectionCreateCmd.Flags().String("issue", "", "Issue ID to scope the section to")
	docsSectionCreateCmd.Flags().String("project", "", "Project ID to scope the section to")
	docsSectionCreateCmd.Flags().Float64("sort-order", 0, "Sort order within the library")

	_ = docsSectionCreateCmd.MarkFlagRequired("name")
}

func runDocsList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	issueID, _ := cmd.Flags().GetString("issue")
	projectID, _ := cmd.Flags().GetString("project")
	if issueID != "" && projectID != "" {
		return fmt.Errorf("provide --issue or --project, not both (omit both for workspace-wide)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var path string
	switch {
	case issueID != "":
		path = "/api/issues/" + issueID + "/library"
	case projectID != "":
		path = "/api/projects/" + projectID + "/library"
	default:
		path = "/api/library"
	}

	var result map[string]any
	if err := client.GetJSON(ctx, path, &result); err != nil {
		return fmt.Errorf("list documents: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		return printDocsTable(result)
	}
	return cli.PrintJSON(os.Stdout, result)
}

func printDocsTable(result map[string]any) error {
	docs, _ := result["documents"].([]any)
	headers := []string{"ATTACHMENT ID", "FILENAME", "CATEGORY", "TAGS", "PINNED"}
	rows := make([][]string, 0, len(docs))
	for _, d := range docs {
		doc, _ := d.(map[string]any)
		if doc == nil {
			continue
		}
		tags, _ := doc["tags"].([]any)
		tagStrs := make([]string, 0, len(tags))
		for _, t := range tags {
			if s, ok := t.(string); ok {
				tagStrs = append(tagStrs, s)
			}
		}
		pinned, _ := doc["pinned"].(bool)
		rows = append(rows, []string{
			truncateID(strVal(doc, "attachment_id")),
			strVal(doc, "filename"),
			strVal(doc, "category"),
			strings.Join(tagStrs, ","),
			boolStr(pinned),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func boolStr(b bool) string {
	if b {
		return "yes"
	}
	return ""
}

func runDocsCurate(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	attachmentID := args[0]
	payload := map[string]any{}

	if cmd.Flags().Changed("title") {
		v, _ := cmd.Flags().GetString("title")
		payload["title"] = v
	}
	if cmd.Flags().Changed("summary") {
		v, _ := cmd.Flags().GetString("summary")
		payload["summary"] = v
	}
	if cmd.Flags().Changed("category") {
		v, _ := cmd.Flags().GetString("category")
		payload["category"] = v
	}
	if cmd.Flags().Changed("tag") {
		tags, _ := cmd.Flags().GetStringSlice("tag")
		payload["tags"] = tags
	}
	if cmd.Flags().Changed("sort-order") {
		v, _ := cmd.Flags().GetFloat64("sort-order")
		payload["sort_order"] = v
	}
	if cmd.Flags().Changed("pinned") {
		payload["pinned"] = true
	} else if cmd.Flags().Changed("unpinned") {
		payload["pinned"] = false
	}
	if cmd.Flags().Changed("archived") {
		payload["archived"] = true
	} else if cmd.Flags().Changed("unarchived") {
		payload["archived"] = false
	}

	if len(payload) == 0 {
		return fmt.Errorf("no curation fields supplied; see --help for available flags")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var result map[string]any
	if err := client.PutJSON(ctx, "/api/documents/"+attachmentID+"/curation", payload, &result); err != nil {
		return fmt.Errorf("curate document: %w", err)
	}
	fmt.Fprintln(os.Stderr, "Curation saved for", attachmentID)
	return cli.PrintJSON(os.Stdout, result)
}

func runDocsSectionCreate(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	name, _ := cmd.Flags().GetString("name")
	description, _ := cmd.Flags().GetString("description")
	sortOrder, _ := cmd.Flags().GetFloat64("sort-order")
	issueID, _ := cmd.Flags().GetString("issue")
	projectID, _ := cmd.Flags().GetString("project")

	if (issueID == "") == (projectID == "") {
		return fmt.Errorf("exactly one of --issue or --project is required")
	}

	payload := map[string]any{
		"name":       name,
		"sort_order": sortOrder,
	}
	if description != "" {
		payload["description"] = description
	}
	if issueID != "" {
		payload["issue_id"] = issueID
	}
	if projectID != "" {
		payload["project_id"] = projectID
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/library/sections", payload, &result); err != nil {
		return fmt.Errorf("create section: %w", err)
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runDocsSectionAddItem(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	sectionID := args[0]
	attachmentID := args[1]

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/library/sections/"+sectionID+"/items",
		map[string]any{"attachment_id": attachmentID}, &result); err != nil {
		return fmt.Errorf("add section item: %w", err)
	}
	return cli.PrintJSON(os.Stdout, result)
}
