package main

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/kkroid/onellm-router/internal/usage"
	"github.com/spf13/cobra"
)

func statsCmd() *cobra.Command {
	return newStatsCmd("", time.Now)
}

func newStatsCmd(baseDir string, now func() time.Time) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show token usage statistics",
	}
	cmd.AddCommand(statsPeriodCmd(usage.PeriodDay, "[YYYY-MM-DD]", baseDir, now))
	cmd.AddCommand(statsPeriodCmd(usage.PeriodWeek, "[YYYY-Www]", baseDir, now))
	cmd.AddCommand(statsPeriodCmd(usage.PeriodMonth, "[YYYY-MM]", baseDir, now))
	return cmd
}

func statsPeriodCmd(period usage.Period, argument string, baseDir string, now func() time.Time) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   string(period) + " " + argument,
		Short: "Show usage for a UTC " + string(period),
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			value := ""
			if len(args) == 1 {
				value = args[0]
			}
			selected, err := usage.ParseRange(period, value, now())
			if err != nil {
				return err
			}
			result, err := usage.AggregateDir(baseDir, selected)
			if err != nil {
				return err
			}
			if asJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
			}
			return writeStatsTable(cmd.OutOrStdout(), result)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return cmd
}

func writeStatsTable(output io.Writer, result usage.StatsResult) error {
	if _, err := fmt.Fprintf(output, "Period: %s %s (UTC)\nMalformed lines: %d\n", result.Range.Period, result.Range.Label, result.MalformedLines); err != nil {
		return err
	}
	if len(result.Groups) == 0 {
		_, err := fmt.Fprintln(output, "No usage records.")
		return err
	}

	writer := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(writer, "PROVIDER\tREQUESTED MODEL\tUPSTREAM MODEL\tINPUT\tOUTPUT\tCACHE READ\tCACHE WRITE\tREASONING\tUNKNOWN INPUT\tUNKNOWN OUTPUT\tUNKNOWN CACHE READ\tUNKNOWN CACHE WRITE\tUNKNOWN REASONING"); err != nil {
		return err
	}
	for _, group := range result.Groups {
		if _, err := fmt.Fprintf(writer, "%s\t%s\t%s\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\n",
			group.Provider, group.RequestedModel, group.UpstreamModel,
			group.Tokens.Input, group.Tokens.Output, group.Tokens.CacheRead,
			group.Tokens.CacheWrite, group.Tokens.Reasoning,
			group.UnknownTokens.Input, group.UnknownTokens.Output, group.UnknownTokens.CacheRead,
			group.UnknownTokens.CacheWrite, group.UnknownTokens.Reasoning,
		); err != nil {
			return err
		}
	}
	return writer.Flush()
}
