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

func setInitTelemetry(s *store.Store, cfg *config.Config, choice *bool) error {
	if choice != nil {
		if err := setSetting(s, "telemetry", strconv.FormatBool(*choice)); err != nil {
			return err
		}
		cfg.Telemetry = choice
	}
	return nil
}

func initTelemetry(cmd *cobra.Command, s *store.Store, cfg *config.Config, interactive bool) error {
	if cfg.Telemetry == nil && interactive {
		choice, err := promptTelemetry(cmd.InOrStdin(), cmd.OutOrStdout())
		if err != nil {
			return err
		}
		if err := setInitTelemetry(s, cfg, choice); err != nil {
			return err
		}
	}
	return nil
}

func promptTelemetry(in io.Reader, out io.Writer) (*bool, error) {
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Would you like to share your usage data to help improve Delegator?")
	fmt.Fprintln(out, "This will only share number of actions taken and configuration information. Your specific tickets and files will never be shared.")
	fmt.Fprintln(out, "See https://alcubi.ai/delegator/privacy/ for details.")
	for {
		fmt.Fprint(out, "Share data? [Y/n] ")
		// Read only through submission; buffering here could swallow agent
		// answers. EOF, even after a partial answer, is not confirmation.
		var answer strings.Builder
		var next [1]byte
		for {
			_, err := io.ReadFull(in, next[:])
			if err == io.EOF || (err == nil && (next[0] == 3 || next[0] == 4 || next[0] == 27)) {
				fmt.Fprintln(out)
				return nil, nil
			}
			if err != nil {
				return nil, err
			}
			if next[0] == '\n' {
				break
			}
			answer.WriteByte(next[0])
		}
		switch strings.ToLower(strings.TrimSpace(answer.String())) {
		case "", "y", "yes":
			choice := true
			return &choice, nil
		case "n", "no":
			choice := false
			return &choice, nil
		default:
			fmt.Fprintln(out, "Please answer yes or no.")
		}
	}
}
