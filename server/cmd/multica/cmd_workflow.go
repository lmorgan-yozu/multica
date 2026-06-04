package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

var workflowCmd = &cobra.Command{
	Use:   "workflow",
	Short: "Manage handoff workflows (ordered, role-based agent chains)",
}

var workflowListCmd = &cobra.Command{
	Use:   "list",
	Short: "List workflows in the workspace",
	RunE:  runWorkflowList,
}

var workflowCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a workflow",
	RunE:  runWorkflowCreate,
}

var workflowShowCmd = &cobra.Command{
	Use:   "show <workflow-id>",
	Short: "Show a workflow and its ordered steps",
	Args:  cobra.ExactArgs(1),
	RunE:  runWorkflowShow,
}

var workflowAddStepCmd = &cobra.Command{
	Use:   "add-step <workflow-id>",
	Short: "Append a step (an agent) to a workflow",
	Args:  cobra.ExactArgs(1),
	RunE:  runWorkflowAddStep,
}

var workflowBindCmd = &cobra.Command{
	Use:   "bind <workflow-id>",
	Short: "Start a workflow on an issue (assigns + triggers the first step's agent)",
	Args:  cobra.ExactArgs(1),
	RunE:  runWorkflowBind,
}

func init() {
	workflowCmd.GroupID = groupCore
	workflowCmd.AddCommand(workflowListCmd)
	workflowCmd.AddCommand(workflowCreateCmd)
	workflowCmd.AddCommand(workflowShowCmd)
	workflowCmd.AddCommand(workflowAddStepCmd)
	workflowCmd.AddCommand(workflowBindCmd)

	workflowListCmd.Flags().String("output", "table", "Output format: table or json")

	workflowCreateCmd.Flags().String("name", "", "Workflow name (required)")
	workflowCreateCmd.Flags().String("description", "", "Workflow description")
	workflowCreateCmd.Flags().String("output", "json", "Output format: table or json")

	workflowShowCmd.Flags().String("output", "json", "Output format: table or json")

	workflowAddStepCmd.Flags().String("agent", "", "Agent for this step (name or ID) — required")
	workflowAddStepCmd.Flags().String("name", "", "Step name (e.g. Backend, Frontend, QA)")
	workflowAddStepCmd.Flags().String("start-status", "todo", "Status set on the issue when this step begins")
	workflowAddStepCmd.Flags().String("advance-status", "in_review", "Status that signals this step is complete")
	workflowAddStepCmd.Flags().String("output", "json", "Output format: table or json")

	workflowBindCmd.Flags().String("issue", "", "Issue (key or ID) to start the workflow on — required")
	workflowBindCmd.Flags().String("output", "json", "Output format: table or json")

	rootCmd.AddCommand(workflowCmd)
}

func runWorkflowList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if _, err := requireWorkspaceID(cmd); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var resp map[string]any
	if err := client.GetJSON(ctx, "/api/workflows", &resp); err != nil {
		return fmt.Errorf("list workflows: %w", err)
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, resp)
	}
	workflows, _ := resp["workflows"].([]any)
	if len(workflows) == 0 {
		fmt.Println("No workflows.")
		return nil
	}
	for _, w := range workflows {
		m, _ := w.(map[string]any)
		fmt.Printf("%s  %s\n", strVal(m, "id"), strVal(m, "name"))
	}
	return nil
}

func runWorkflowCreate(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if _, err := requireWorkspaceID(cmd); err != nil {
		return err
	}
	name, _ := cmd.Flags().GetString("name")
	if name == "" {
		return fmt.Errorf("--name is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	body := map[string]any{"name": name}
	if v, _ := cmd.Flags().GetString("description"); v != "" {
		body["description"] = v
	}
	var result map[string]any
	if err := client.PostJSON(ctx, "/api/workflows", body, &result); err != nil {
		return fmt.Errorf("create workflow: %w", err)
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	fmt.Printf("Workflow created: %s (%s)\n", strVal(result, "name"), strVal(result, "id"))
	return nil
}

func runWorkflowShow(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if _, err := requireWorkspaceID(cmd); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var resp map[string]any
	if err := client.GetJSON(ctx, "/api/workflows/"+args[0], &resp); err != nil {
		return fmt.Errorf("get workflow: %w", err)
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, resp)
	}
	wf, _ := resp["workflow"].(map[string]any)
	fmt.Printf("%s (%s)\n", strVal(wf, "name"), strVal(wf, "id"))
	steps, _ := resp["steps"].([]any)
	for _, s := range steps {
		m, _ := s.(map[string]any)
		fmt.Printf("  step %v: agent %s  [%s → %s]\n",
			m["step_order"], strVal(m, "agent_id"), strVal(m, "start_status"), strVal(m, "advance_status"))
	}
	return nil
}

func runWorkflowAddStep(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if _, err := requireWorkspaceID(cmd); err != nil {
		return err
	}
	agent, _ := cmd.Flags().GetString("agent")
	if agent == "" {
		return fmt.Errorf("--agent is required (agent name or ID)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	agentID, err := resolveAgent(ctx, client, agent)
	if err != nil {
		return fmt.Errorf("resolve agent: %w", err)
	}
	name, _ := cmd.Flags().GetString("name")
	startStatus, _ := cmd.Flags().GetString("start-status")
	advanceStatus, _ := cmd.Flags().GetString("advance-status")

	body := map[string]any{
		"agent_id":       agentID,
		"name":           name,
		"start_status":   startStatus,
		"advance_status": advanceStatus,
	}
	var result map[string]any
	if err := client.PostJSON(ctx, "/api/workflows/"+args[0]+"/steps", body, &result); err != nil {
		return fmt.Errorf("add step: %w", err)
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	fmt.Printf("Step %v added (agent %s)\n", result["step_order"], strVal(result, "agent_id"))
	return nil
}

func runWorkflowBind(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if _, err := requireWorkspaceID(cmd); err != nil {
		return err
	}
	issue, _ := cmd.Flags().GetString("issue")
	if issue == "" {
		return fmt.Errorf("--issue is required (issue key or ID)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	issueRef, err := resolveIssueRef(ctx, client, issue)
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}
	body := map[string]any{"issue_id": issueRef.ID}
	var result map[string]any
	if err := client.PostJSON(ctx, "/api/workflows/"+args[0]+"/bind", body, &result); err != nil {
		return fmt.Errorf("bind issue: %w", err)
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	fmt.Printf("Workflow started on issue %s (run %s)\n", issueRef.ID, strVal(result, "run_id"))
	return nil
}
