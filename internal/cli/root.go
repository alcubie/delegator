package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/alcubie/delegator/internal/config"
	"github.com/alcubie/delegator/internal/store"
	"github.com/spf13/cobra"
)

// DataDir returns the platform-specific delegator data directory.
func DataDir() (string, error) {
	return dataDir(runtime.GOOS)
}

// dataDir accepts a platform name for tests. XDG_DATA_HOME overrides all
// platforms; otherwise Windows uses LOCALAPPDATA and other systems use the
// XDG default.
func dataDir(goos string) (string, error) {
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
		return filepath.Join(dir, "delegator"), nil
	}
	if goos == "windows" {
		dir := os.Getenv("LOCALAPPDATA")
		if dir == "" {
			return "", errors.New("%LOCALAPPDATA% is not set")
		}
		return filepath.Join(dir, "delegator"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "delegator"), nil
}

// selectedDataDir validates and normalizes the directory named explicitly on
// the command line. A relative directory would make the same instance mean a
// different place to a detached supervisor running from another directory.
func selectedDataDir(dir string) (string, error) {
	if !filepath.IsAbs(dir) {
		return "", fmt.Errorf("--data-dir must be an absolute path")
	}
	return filepath.Clean(dir), nil
}

// staticDiscovery reports whether cmd only describes the command tree. Cobra
// handles --help before the hooks run, but its help command and the shell
// commands below completion do run the root's persistent hook. The hidden
// completion request is what a generated script invokes later.
func staticDiscovery(cmd *cobra.Command) bool {
	for current := cmd; current != nil; current = current.Parent() {
		switch current.Name() {
		case "help", "completion", cobra.ShellCompRequestCmd:
			return true
		}
	}
	return false
}

// Root builds dg's command tree with injectable I/O. Cobra suppresses its own
// error and usage output so cmd/dg can report each error once.
func Root(workDir string) *cobra.Command {
	mode := colourAuto
	// Unlike colour, the platform default can fail to resolve. Leave it empty
	// until after parsing so an explicit --data-dir can bypass that lookup.
	selectedDir := ""
	// Commands share a pointer to the settings snapshot populated by the
	// root hook after tree construction.
	var cfg config.Config
	root := &cobra.Command{
		Use:   "dg",
		Short: "Delegate tasks to coding agents",
		Long: "Delegator queues work for coding agents and keeps each task in its own Git " +
			"worktree. Run dg without a command to see the inbox and the state of the queue.",
		Example: `  dg
  dg ticket create "Add request tracing" --no-body
  dg ticket show 42`,
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          cobra.NoArgs,
		// Each command reads one settings snapshot before it does any work.
		// Reconciliation and the command then use the same values even when
		// another process changes a setting while the command is running.
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			if staticDiscovery(cmd) {
				return nil
			}
			var err error
			if cmd.Flags().Changed("data-dir") {
				selectedDir, err = selectedDataDir(selectedDir)
			} else if selectedDir == "" {
				selectedDir, err = DataDir()
			}
			if err != nil {
				return err
			}
			// version does not use instance state, and rpc loads settings for
			// the command inside each request rather than for the transport.
			if cmd.Name() == "version" || cmd.Name() == "rpc" {
				return nil
			}
			return store.With(selectedDir, func(s *store.Store) error {
				loaded, err := s.Settings()
				if err == nil {
					cfg = loaded
				}
				return err
			})
		},
		PersistentPostRun: func(cmd *cobra.Command, _ []string) {
			switch cmd.Name() {
			case "version", "rpc", "run", "telemetry-send":
				return
			}
			if cfg.Telemetry != nil && *cfg.Telemetry {
				triggerTelemetry(selectedDir)
			}
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			box, err := showInbox(selectedDir, &cfg)
			if err != nil {
				return err
			}
			return writeValue(cmd.OutOrStdout(), box, false, func(out io.Writer) {
				writeInbox(out, box.box, mode, box.now, box.done)
			})
		},
	}
	rpcOperationCommand("inbox", root)
	root.PersistentFlags().Var(&mode, "color",
		"when to colour status values: always, never, or auto (terminals only)")
	root.PersistentFlags().StringVar(&selectedDir, "data-dir", "",
		"store all Delegator data in this absolute directory (default: the platform data directory)")
	root.AddCommand(initCommand(&selectedDir, &cfg))
	root.AddCommand(ticketCommand(&selectedDir, workDir, &cfg))
	root.AddCommand(ticketCompatibilityCommand("dg ticket list", listCommand(&selectedDir, workDir, &cfg)))
	root.AddCommand(ticketCompatibilityCommand("dg ticket search", searchCommand(&selectedDir, workDir, &cfg)))
	root.AddCommand(ticketCompatibilityCommand("dg ticket show", showCommand(&selectedDir, workDir, &cfg)))
	root.AddCommand(ticketCompatibilityCommand("dg ticket map", mapCommand(&selectedDir, workDir, &cfg)))
	root.AddCommand(editCommand(&selectedDir, &cfg))
	root.AddCommand(moveCommand(&selectedDir, &cfg))
	root.AddCommand(dependCommand(&selectedDir, &cfg))
	root.AddCommand(finishCommand(&selectedDir, &cfg))
	root.AddCommand(acceptCommand(&selectedDir, workDir, &cfg))
	root.AddCommand(cancelCommand(&selectedDir, &cfg))
	root.AddCommand(restartCommand(&selectedDir, &cfg))
	root.AddCommand(chatCommand(&selectedDir, workDir, &cfg))
	root.AddCommand(runCommand(&selectedDir, &cfg))
	root.AddCommand(pauseCommand(&selectedDir, &cfg))
	root.AddCommand(startCommand(&selectedDir, &cfg))
	root.AddCommand(configCommand(&selectedDir, &cfg))
	root.AddCommand(agentsCommand(&selectedDir, &cfg))
	root.AddCommand(versionCommand())
	root.AddCommand(rpcCommand(&selectedDir, workDir))
	root.AddCommand(telemetryCommand(&selectedDir))
	return root
}
