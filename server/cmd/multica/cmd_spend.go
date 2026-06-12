package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var spendCmd = &cobra.Command{
	Use:   "spend",
	Short: "Inspect workspace spend",
}

var spendDailyCmd = &cobra.Command{
	Use:   "daily",
	Short: "Show daily workspace or agent spend",
	RunE:  runSpendDaily,
}

var issueSpendCmd = &cobra.Command{
	Use:   "spend <issue-id>",
	Short: "Show spend for an issue",
	Args:  exactArgs(1),
	RunE:  runIssueSpend,
}

func init() {
	spendCmd.AddCommand(spendDailyCmd)
	issueCmd.AddCommand(issueSpendCmd)

	spendDailyCmd.Flags().String("agent", "", "Agent ID")
	spendDailyCmd.Flags().String("project", "", "Project ID")
	spendDailyCmd.Flags().String("from", "", "Start date (YYYY-MM-DD)")
	spendDailyCmd.Flags().String("to", "", "End date (YYYY-MM-DD)")
	spendDailyCmd.Flags().String("tz", "", "Viewing timezone")
	spendDailyCmd.Flags().String("output", "table", "Output format: table or json")

	issueSpendCmd.Flags().String("from", "", "Start date (YYYY-MM-DD)")
	issueSpendCmd.Flags().String("to", "", "End date (YYYY-MM-DD)")
	issueSpendCmd.Flags().String("tz", "", "Viewing timezone")
	issueSpendCmd.Flags().String("output", "table", "Output format: table or json")
}

func runIssueSpend(cmd *cobra.Command, args []string) error {
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

	params := spendBaseParams(cmd, "task")
	params.Set("issue_id", issueRef.ID)
	report, err := fetchSpendReport(ctx, client, params)
	if err != nil {
		return fmt.Errorf("get issue spend: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, report)
	}
	printSpendRows([]string{"TASK", "SESSION", "MODEL", "INPUT", "OUTPUT", "COST_USD", "USAGE"}, reportRows(report, "task"))
	return nil
}

func runSpendDaily(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	params := spendBaseParams(cmd, "day")
	if agent, _ := cmd.Flags().GetString("agent"); agent != "" {
		params.Set("agent_id", agent)
	}
	if project, _ := cmd.Flags().GetString("project"); project != "" {
		params.Set("project_id", project)
	}

	report, err := fetchSpendReport(ctx, client, params)
	if err != nil {
		return fmt.Errorf("get daily spend: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, report)
	}
	printSpendRows([]string{"DATE", "MODEL", "INPUT", "OUTPUT", "COST_USD", "USAGE"}, reportRows(report, "day"))
	return nil
}

func spendBaseParams(cmd *cobra.Command, groupBy string) url.Values {
	params := url.Values{}
	params.Set("group_by", groupBy)
	for _, name := range []string{"from", "to", "tz"} {
		if value, _ := cmd.Flags().GetString(name); value != "" {
			params.Set(name, value)
		}
	}
	return params
}

func fetchSpendReport(ctx context.Context, client *cli.APIClient, params url.Values) (map[string]any, error) {
	var report map[string]any
	path := "/api/spend"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}
	if err := client.GetJSON(ctx, path, &report); err != nil {
		return nil, err
	}
	return report, nil
}

func reportRows(report map[string]any, groupBy string) [][]string {
	rawRows, _ := report["rows"].([]any)
	rows := make([][]string, 0, len(rawRows))
	for _, raw := range rawRows {
		row, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		key := strVal(row, groupBy+"_id")
		if groupBy == "day" {
			key = strVal(row, "date")
		}
		model := strVal(row, "model")
		if provider := strVal(row, "provider"); provider != "" && model != "" {
			model = provider + "/" + model
		}
		usage := "complete"
		if v, ok := row["usage_complete"].(bool); ok && !v {
			usage = "incomplete"
		}
		values := []string{
			key,
			model,
			strVal(row, "input_tokens"),
			strVal(row, "output_tokens"),
			formatSpendCost(row["estimated_cost_usd"]),
			usage,
		}
		if groupBy == "task" {
			values = append([]string{key, strVal(row, "session_id")}, values[1:]...)
		}
		rows = append(rows, values)
	}
	return rows
}

func printSpendRows(headers []string, rows [][]string) {
	cli.PrintTable(os.Stdout, headers, rows)
}

func formatSpendCost(v any) string {
	switch n := v.(type) {
	case float64:
		return strconv.FormatFloat(n, 'f', 6, 64)
	case float32:
		return strconv.FormatFloat(float64(n), 'f', 6, 64)
	default:
		return "unknown"
	}
}
