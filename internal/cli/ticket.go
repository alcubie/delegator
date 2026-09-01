package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// ticketCommand makes a ticket and shows its id. With no argument it opens the
// editor of the person, with one it takes the title, and with two it takes the
// title and the prose.
func ticketCommand(dataDir, workDir string) *cobra.Command {
	return &cobra.Command{
		Use:   "ticket [title] [body]",
		Short: "Add a ticket. With no arguments, it opens $EDITOR.",
		Args:  cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			var id int64
			var err error
			switch len(args) {
			case 0:
				id, err = TicketFromEditor(dataDir, workDir)
			case 1:
				id, err = Ticket(dataDir, workDir, args[0], "")
			case 2:
				id, err = Ticket(dataDir, workDir, args[0], args[1])
			default:
				return fmt.Errorf("dg ticket takes a title and a body, and got %d arguments", len(args))
			}
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), id)
			return nil
		},
	}
}
