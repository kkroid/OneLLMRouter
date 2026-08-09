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
	if _, err := fmt.Fprintln(writer, "PROVIDER\tREQUESTED MODEL\tUPSTREAM MODEL\tATTEMPTS\tRETRY ATTEMPTS\tRETRIED REQUESTS\tREQUESTS\tSUCCESS\tERROR\tCANCELLED\tUNKNOWN\tUNKNOWN USAGE\tINPUT\tOUTPUT\tCACHE READ\tCACHE WRITE\tREASONING\tUNKNOWN INPUT\tUNKNOWN OUTPUT\tUNKNOWN CACHE READ\tUNKNOWN CACHE WRITE\tUNKNOWN REASONING\tSUCCESSFUL REQUESTS\tSUCCESS UNKNOWN USAGE\tSUCCESS INPUT\tSUCCESS OUTPUT\tSUCCESS CACHE READ\tSUCCESS CACHE WRITE\tSUCCESS REASONING\tSUCCESS UNKNOWN INPUT\tSUCCESS UNKNOWN OUTPUT\tSUCCESS UNKNOWN CACHE READ\tSUCCESS UNKNOWN CACHE WRITE\tSUCCESS UNKNOWN REASONING"); err != nil {
		return err
	}
	for _, group := range result.Groups {
		all := group.AllAttempts
		outcomes := group.RequestOutcomes
		successful := group.SuccessfulRequestUsage
		if _, err := fmt.Fprintf(writer, "%s\t%s\t%s\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\n",
			group.Provider, group.RequestedModel, group.UpstreamModel,
			all.Attempts, all.RetryAttempts, all.RetriedRequests,
			outcomes.Requests, outcomes.Success, outcomes.Error, outcomes.Cancelled, outcomes.Unknown,
			all.UnknownRecords,
			all.Tokens.Input, all.Tokens.Output, all.Tokens.CacheRead, all.Tokens.CacheWrite, all.Tokens.Reasoning,
			all.UnknownTokens.Input, all.UnknownTokens.Output, all.UnknownTokens.CacheRead,
			all.UnknownTokens.CacheWrite, all.UnknownTokens.Reasoning,
			successful.Requests, successful.UnknownRecords,
			successful.Tokens.Input, successful.Tokens.Output, successful.Tokens.CacheRead,
			successful.Tokens.CacheWrite, successful.Tokens.Reasoning,
			successful.UnknownTokens.Input, successful.UnknownTokens.Output, successful.UnknownTokens.CacheRead,
			successful.UnknownTokens.CacheWrite, successful.UnknownTokens.Reasoning,
		); err != nil {
			return err
		}
	}
	return writer.Flush()
}
