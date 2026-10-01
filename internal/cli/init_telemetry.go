package cli

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/alcubie/delegator/internal/config"
	"github.com/alcubie/delegator/internal/store"
	"github.com/spf13/cobra"
)

// initTelemetry runs before agent discovery so consent does not depend on
// successful agent setup. Scripted selection never submits the UI default.
func initTelemetry(cmd *cobra.Command, s *store.Store, cfg *config.Config, options initOptions, interactive bool) (bool, error) {
	choice := options.telemetry
	proceed := true
	if choice == nil && cfg.Telemetry == nil && interactive {
		var err error
		choice, proceed, err = promptTelemetry(cmd.InOrStdin(), cmd.OutOrStdout())
		if err != nil {
			return false, err
		}
	}
	if choice != nil {
		if err := setSetting(s, "telemetry", strconv.FormatBool(*choice)); err != nil {
			return false, err
		}
		cfg.Telemetry = choice
	}
	changeTo := "true"
	if cfg.Telemetry != nil && *cfg.Telemetry {
		changeTo = "false"
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Telemetry: %s. Change with dg config set telemetry %s.\n", settingText(settingValue(*cfg, "telemetry")), changeTo)
	return proceed, nil
}

func promptTelemetry(in io.Reader, out io.Writer) (*bool, bool, error) {
	fmt.Fprintln(out, "Sends counts, settings, agents used, and identifiers—not ticket text or code.")
	fmt.Fprintln(out, "Details: https://alcubi.ai/delegator/privacy/")
	for {
		fmt.Fprint(out, "Share telemetry to help improve Delegator? [Y/n] (s to skip, q to cancel): ")
		// Read only through submission; buffering here could swallow agent
		// answers. EOF, even after a partial answer, is not confirmation.
		var answer strings.Builder
		var next [1]byte
		for {
			_, err := io.ReadFull(in, next[:])
			if err == io.EOF || (err == nil && (next[0] == 3 || next[0] == 4 || next[0] == 27)) {
				fmt.Fprintln(out)
				return nil, false, nil
			}
			if err != nil {
				return nil, false, err
			}
			if next[0] == '\n' {
				break
			}
			answer.WriteByte(next[0])
		}
		switch strings.ToLower(strings.TrimSpace(answer.String())) {
		case "", "y", "yes":
			choice := true
			return &choice, true, nil
		case "n", "no":
			choice := false
			return &choice, true, nil
		case "s", "skip":
			return nil, true, nil
		case "q", "cancel":
			return nil, false, nil
		default:
			fmt.Fprintln(out, "Please answer yes, no, s to skip, or q to cancel.")
		}
	}
}
