// The command that changes the title and the prose of a ticket that waits. The
// title is a column and the prose is a file, so the editor gets the two as one
// text, in the form that dg ticket with no arguments takes.

package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/alcubie/delegator/internal/config"
	"github.com/alcubie/delegator/internal/store"
)

// editCommand returns the command dg edit.
func editCommand(dataDir string, cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "edit <id>",
		Short: "Change the title and the prose of a queued ticket in $EDITOR.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("%q is not the id of a ticket", args[0])
			}
			return withStore(dataDir, cfg, func(s *store.Store) error {
				return editTicket(s, id)
			})
		},
	}
}

// editTicket opens the editor of the person on the title and the prose of a
// ticket, and writes back what the editor gave: the first line to the column
// title, and each line below it to the file of prose.
func editTicket(s *store.Store, id int64) error {
	ticket, err := s.Ticket(id)
	if err != nil {
		return err
	}

	if ticket.Status != store.Queued {
		return refuseEdit(ticket)
	}

	path := proseFile(s.DataDir(), id)
	prose, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	before := ticket.Title + "\n\n" + string(prose)
	after, err := fromEditor(before)
	if err != nil {
		return err
	}

	// The person changed nothing, so nothing is written. The file is the file
	// of the person, and a write that puts back what was read is still a write
	// of text that delegator holds in memory.
	if after == before {
		return nil
	}

	title, body := splitTitle(after)
	if title == "" {
		return ErrNoTitle
	}
	if err := s.SetTitle(id, title); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(body), filePerm)
}

// refuseEdit returns the error for a ticket that dg edit cannot change. A run
// reads the ticket when it starts, so every state below queued belongs to a run
// that has already read it, and a change the agent cannot see leaves a report
// that answers a ticket which is not there any more. dg revise is the command
// that gives the work again with new prose.
func refuseEdit(ticket store.Ticket) error {
	if ticket.Status == store.Running {
		return fmt.Errorf("ticket %d is running: use dg revise after the run to change it", ticket.ID)
	}
	return fmt.Errorf("ticket %d is %s: only a ticket in the queue can be changed", ticket.ID, ticket.Status)
}
