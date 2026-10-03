package cli

import (
	"os"
	"os/exec"
	"time"

	"github.com/alcubie/delegator/internal/analytics"
	"github.com/alcubie/delegator/internal/run"
	"github.com/alcubie/delegator/internal/store"
	"github.com/spf13/cobra"
)

var sendTelemetry = analytics.Send

var launchTelemetry = func(dataDir string) *exec.Cmd {
	exe, err := os.Executable()
	if err != nil {
		exe = "dg"
	}
	return exec.Command(exe, "telemetry-send", "--data-dir", dataDir)
}

func attemptTelemetry(s *store.Store) {
	_ = sendTelemetry(s, time.Now(), Version)
}

func triggerTelemetry(dataDir string) {
	if dataDir != "" {
		_ = run.Detach(launchTelemetry(dataDir))
	}
}

// telemetryCommand is the short-lived detached sender entry point. It is
// hidden because consent and inspection use init/config rather than a separate
// public telemetry interface.
func telemetryCommand(dataDir *string) *cobra.Command {
	return &cobra.Command{
		Use: "telemetry-send", Hidden: true, Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return store.With(*dataDir, func(s *store.Store) error {
				attemptTelemetry(s)
				return nil
			})
		},
	}
}
