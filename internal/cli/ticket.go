package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/alcubie/delegator/internal/project"
	"github.com/alcubie/delegator/internal/store"
)

// filePerm is the permission of a ticket's prose file. A ticket can hold
// private data, so only the person who made it can read it.
const filePerm = 0o600

// ErrNoTitle shows that a ticket has no title. The inbox shows the title, and
// the branch of a run takes its name from it, so a ticket with no title is a
// ticket that a person cannot find again.
var ErrNoTitle = errors.New("the ticket must have a title")

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

// Ticket makes a ticket for the project that holds workDir, and puts it at the
// end of the queue. It returns the id. The body is the prose of the ticket, and
// it can be empty.
func Ticket(dataDir, workDir, title, body string) (int64, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return 0, ErrNoTitle
	}

	root, err := project.Root(workDir)
	if err != nil {
		return 0, err
	}
	branch, err := project.DefaultBranch(root)
	if err != nil {
		return 0, err
	}

	var id int64
	err = store.With(dataDir, func(s *store.Store) error {

		projectID, err := s.ProjectID(root, branch)
		if err != nil {
			return err
		}
		id, err = s.AddTicket(projectID, title)
		if err != nil {
			return err
		}

		// The person owns the prose after this write. Only dg revise adds to the
		// file, and no command writes it again from what it holds in memory.
		if err := os.WriteFile(proseFile(dataDir, id), []byte(body), filePerm); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return id, nil
}
