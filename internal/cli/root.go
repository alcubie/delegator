package cli

import (
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

// DataDir returns the directory that holds the database and the prose of each
// ticket. XDG_DATA_HOME names it, and a person who has not set that variable
// gets the directory that the XDG specification asks for.
func DataDir() (string, error) {
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
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
	root := &cobra.Command{
		Use:           "dg",
		Short:         "Delegate tasks to an agent",
		Long:          "Delegate tasks to an agent to help you avoid overload from context switching.",
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return showInbox(cmd.OutOrStdout(), dataDir)
		},
	}
	root.AddCommand(ticketCommand(dataDir, workDir))
	root.AddCommand(showCommand(dataDir))
	root.AddCommand(moveCommand(dataDir))
	root.AddCommand(finishCommand(dataDir))
	root.AddCommand(acceptCommand(dataDir))
	root.AddCommand(runCommand(dataDir))
	root.AddCommand(pauseCommand(dataDir))
	return root
}
