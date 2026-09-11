package cli

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/alcubie/delegator/internal/config"
	"github.com/alcubie/delegator/internal/run"
	"github.com/alcubie/delegator/internal/store"
)

// cancelCommand returns the command dg cancel. It takes the id of a ticket and
// no default: dg accept closes the head of READY because that is the ticket a
// person is reading, and a person who cancels is throwing work away and says
// which.
//
// The worktree stays. A run that a person stopped may hold work they want to
// read before it goes, and only dg accept removes a worktree, for work that is
// complete. Section 6.3 of TECHNICAL_DESIGN.md settles this.
func cancelCommand(dataDir string, cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "cancel <id>",
		Short: "Stop the work on a ticket and close it.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("%q is not the id of a ticket", args[0])
			}
			return withStore(dataDir, cfg, func(s *store.Store) error {
				return cancelTicket(s, id)
			})
		},
	}
}

// cancelTicket stops the run of a ticket, if it has one, and then closes the
// ticket.
//
// Only a ticket in running is signalled. The reconcile of every command has
// already marked failed each run it calls dead, so a ticket that is still in
// running here has a supervisor that answered signal 0, started after the boot
// and is inside the timeout. A run the reconcile called dead gets no signal at
// all: its process id can belong to a different program by then, and this
// writes the state alone.
//
// The signal goes before the write and outside its transaction. A supervisor
// that a signal ended writes nothing, so the end of the run comes from here,
// and the wait between the two signals is seconds, which is far too long to
// hold SQLite's writer lock.
func cancelTicket(s *store.Store, id int64) error {
	ticket, err := s.Ticket(id)
	if err != nil {
		return err
	}
	if ticket.Status == store.Running {
		held, err := s.Run(id)
		if err != nil {
			return err
		}
		if err := run.Stop(held.PID, run.StopGrace); err != nil {
			return err
		}
	}
	if err := s.Cancel(id); err != nil {
		return err
	}
	// The supervisor this stopped is the program that would have started the
	// next run as its own ended, so the command starts it in its place.
	return run.Next(s, launch)
}
