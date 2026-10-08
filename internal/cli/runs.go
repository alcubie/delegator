package cli

import (
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/alcubie/delegator/internal/store"
)

type runJSON struct {
	ID       int64      `json:"id"`
	Agent    *string    `json:"agent"`
	Model    *string    `json:"model"`
	Started  *time.Time `json:"started"`
	Ended    *time.Time `json:"ended"`
	ExitCode *int       `json:"exit_code"`
}

type runsJSON struct {
	TicketID int64     `json:"ticket_id"`
	Runs     []runJSON `json:"runs"`
}

func runsResultSchema() map[string]any {
	nullableString := map[string]any{"type": []string{"string", "null"}}
	timestamp := map[string]any{
		"type":   []string{"string", "null"},
		"format": "date-time",
	}
	run := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":        map[string]any{"type": "integer", "minimum": 1},
			"agent":     nullableString,
			"model":     nullableString,
			"started":   timestamp,
			"ended":     timestamp,
			"exit_code": map[string]any{"type": []string{"integer", "null"}},
		},
		"required":             []string{"id", "agent", "model", "started", "ended", "exit_code"},
		"additionalProperties": true,
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ticket_id": map[string]any{"type": "integer", "minimum": 1},
			"runs": map[string]any{
				"type":  "array",
				"items": run,
			},
		},
		"required":             []string{"ticket_id", "runs"},
		"additionalProperties": true,
	}
}

func runsCommand(dataDir *string) *cobra.Command {
	return rpcOperationCommand("ticket.runs", &cobra.Command{
		Use:   "runs <id>",
		Short: "Show the recorded runs of a ticket.",
		Long: "Show every recorded run of one ticket, newest first. Missing recorded " +
			"values are shown as dashes in terminal output and null in structured output.",
		Example: `  dg ticket runs 42`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := ticketArg(args[0])
			if err != nil {
				return err
			}
			if id < 1 {
				return fmt.Errorf("ticket id must be positive, got %d", id)
			}
			return store.With(*dataDir, func(s *store.Store) error {
				runs, err := s.Runs(id)
				if err != nil {
					return err
				}
				value := runsValue(id, runs)
				return writeValue(cmd.OutOrStdout(), value, false, func(out io.Writer) {
					writeRuns(out, value)
				})
			})
		},
	})
}

func runsValue(ticketID int64, runs []store.Run) runsJSON {
	value := runsJSON{TicketID: ticketID, Runs: make([]runJSON, 0, len(runs))}
	for _, run := range runs {
		row := runJSON{
			ID:      run.ID,
			Agent:   nullable(run.Agent),
			Started: nullableTime(run.StartedAt),
			Ended:   nullableTime(run.EndedAt),
		}
		if run.ModelID.Valid {
			row.Model = nullable(run.Model)
		}
		if run.ExitCode.Valid {
			exitCode := run.ExitCode.V
			row.ExitCode = &exitCode
		}
		value.Runs = append(value.Runs, row)
	}
	return value
}

func writeRuns(out io.Writer, value runsJSON) {
	if len(value.Runs) == 0 {
		fmt.Fprintf(out, "No runs recorded for ticket #%d.\n", value.TicketID)
		return
	}
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "RUN\tAGENT\tMODEL\tSTARTED\tENDED\tEXIT CODE")
	for _, run := range value.Runs {
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\n", run.ID,
			stringValue(run.Agent), stringValue(run.Model), timeValue(run.Started),
			timeValue(run.Ended), intValue(run.ExitCode))
	}
	w.Flush()
}

func stringValue(value *string) string {
	if value == nil {
		return "-"
	}
	return *value
}

func timeValue(value *time.Time) string {
	if value == nil {
		return "-"
	}
	return value.Format(time.RFC3339)
}

func intValue(value *int) string {
	if value == nil {
		return "-"
	}
	return fmt.Sprint(*value)
}
