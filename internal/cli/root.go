package cli

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/alcubie/delegator/internal/config"
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

// Root returns the command tree of dg. The program cmd/dg takes the directories
// and runs it, so each command has a test that needs no terminal.
//
// cobra says nothing itself about an error: cmd/dg writes each one, with the
// name of the program in front of it. cobra also keeps the usage back, because
// a person who wrote a title that dg refused does not want each command again.
func Root(dataDir, workDir string) *cobra.Command {
	mode := colourAuto
	// cfg is what the hook below read. The inbox needs the window of DONE and
	// the reconcile of each command needs the timeout, and a second Load in a
	// RunE could read a file that the person edited in between and give one
	// command two configs. Each command takes the address, because the hook
	// runs after this function has built the tree.
	var cfg config.Config
	var asJSON bool
	root := &cobra.Command{
		Use:           "dg",
		Short:         "Delegate tasks to an agent",
		Long:          "Delegate tasks to an agent to help you avoid overload from context switching.",
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          cobra.NoArgs,
		// Each command starts by loading the config, so the first command
		// a person runs leaves a file to open, and a file that Load refuses
		// stops the command with the key it names. cobra runs this before
		// the root and before each subcommand, as none has a hook of its
		// own.
		PersistentPreRunE: func(*cobra.Command, []string) error {
			if err := config.Init(); err != nil {
				return err
			}
			loaded, err := config.Load()
			if err != nil {
				return err
			}
			cfg = loaded
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			box, err := showInbox(dataDir, &cfg)
			if err != nil {
				return err
			}
			return writeValue(cmd.OutOrStdout(), box, asJSON, func(out io.Writer) {
				writeInbox(out, box.box, mode, box.now, box.done)
			})
		},
	}
	root.PersistentFlags().Var(&mode, "color",
		"colour the status: always, never or auto (a terminal only)")
	// --json is a flag of the root alone, and not a persistent one: dg show
	// has its own, and a subcommand that gains the flag from here would take
	// it and write text.
	root.Flags().BoolVar(&asJSON, "json", false,
		"write the inbox as one JSON object")
	root.AddCommand(ticketCommand(dataDir, workDir, &cfg))
	root.AddCommand(listCommand(dataDir, workDir, &cfg))
	root.AddCommand(searchCommand(dataDir, workDir, &cfg))
	root.AddCommand(showCommand(dataDir, workDir, &cfg))
	root.AddCommand(editCommand(dataDir, &cfg))
	root.AddCommand(moveCommand(dataDir, &cfg))
	root.AddCommand(dependCommand(dataDir, &cfg))
	root.AddCommand(finishCommand(dataDir, &cfg))
	root.AddCommand(acceptCommand(dataDir, workDir, &cfg))
	root.AddCommand(cancelCommand(dataDir, &cfg))
	root.AddCommand(restartCommand(dataDir, &cfg))
	root.AddCommand(chatCommand(dataDir, workDir, &cfg))
	root.AddCommand(runCommand(dataDir, &cfg))
	root.AddCommand(pauseCommand(dataDir, &cfg))
	root.AddCommand(startCommand(dataDir, &cfg))
	root.AddCommand(versionCommand())
	root.AddCommand(rpcCommand(dataDir, workDir))
	return root
}
