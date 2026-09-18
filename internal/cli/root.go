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

// DataDir returns the directory that holds the database and the prose of each
// ticket, for the system the build is for.
func DataDir() (string, error) {
	return dataDir(runtime.GOOS)
}

// dataDir is DataDir with the system as a parameter, so that the tests can ask
// for a system that the build is not for. XDG_DATA_HOME names the directory on
// every system. A person who has not set that variable gets what their system
// asks for: LOCALAPPDATA on Windows, which has no XDG rule, and the directory
// of the XDG specification elsewhere.
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

// Root returns the command tree of dg. The program cmd/dg takes the working
// directory and runs it, so each command has a test that needs no terminal.
//
// cobra says nothing itself about an error: cmd/dg writes each one, with the
// name of the program in front of it. cobra also keeps the usage back, because
// a person who wrote a title that dg refused does not want each command again.
func Root(workDir string) *cobra.Command {
	mode := colourAuto
	// Unlike colour, the platform default can fail to resolve. Leave it empty
	// until after parsing so an explicit --data-dir can bypass that lookup.
	selectedDir := ""
	// cfg is the snapshot the hook below read. The inbox needs the window of
	// DONE and reconciliation needs the timeout. Each command takes the
	// address because the hook runs after this function has built the tree.
	var cfg config.Config
	root := &cobra.Command{
		Use:           "dg",
		Short:         "Delegate tasks to an agent",
		Long:          "Delegate tasks to an agent to help you avoid overload from context switching.",
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          cobra.NoArgs,
		// Each command reads one settings snapshot before it does any work.
		// Reconciliation and the command then use the same values even when
		// another process changes a setting while the command is running.
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
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
	root.PersistentFlags().Var(&mode, "color",
		"colour the status: always, never or auto (a terminal only)")
	root.PersistentFlags().StringVar(&selectedDir, "data-dir", "",
		"store all Delegator data in this absolute directory")
	root.AddCommand(ticketCommand(&selectedDir, workDir, &cfg))
	root.AddCommand(listCommand(&selectedDir, workDir, &cfg))
	root.AddCommand(searchCommand(&selectedDir, workDir, &cfg))
	root.AddCommand(showCommand(&selectedDir, workDir, &cfg))
	root.AddCommand(mapCommand(&selectedDir, workDir, &cfg))
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
	return root
}
