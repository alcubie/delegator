package cli

import (
	"fmt"
	"os"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/alcubie/delegator/internal/config"
	"github.com/alcubie/delegator/internal/run"
	"github.com/alcubie/delegator/internal/store"
)

// rerunCommand marks a ready ticket running again, for the agent inside dg
// ticket chat to run before it changes anything. It is hidden: a person
// never runs it directly, and it does not start an agent or a supervisor,
// since dg ticket chat already holds the session.
func rerunCommand(dataDir *string, cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:    "rerun <id>",
		Short:  "Mark a ticket running again, from inside dg ticket chat.",
		Hidden: true,
		Long: "Mark a ready ticket running again. The agent inside dg ticket chat runs this " +
			"before it changes anything, so the ticket does not stay ready while it works. " +
			"It refuses outside dg ticket chat.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := ticketArg(args[0])
			if err != nil {
				return err
			}
			pid, err := chatPID()
			if err != nil {
				return err
			}
			return withStore(*dataDir, cfg, func(s *store.Store) error {
				ticket, err := s.Ticket(id)
				if err != nil {
					return err
				}
				if ticket.Status != store.Ready {
					return fmt.Errorf("%w: the ticket is %s, and only a ready ticket reruns",
						store.ErrInvalidTicketStateChange, ticket.Status)
				}
				prior, err := s.Run(id)
				if err != nil {
					return err
				}
				_, err = s.Rerun(id, prior.AgentID, pid)
				return err
			})
		},
	}
	return rpcOperationCommand("rerun", cmd)
}

// chatPID reads the PID dg ticket chat recorded on the agent's environment,
// refusing when rerun did not start inside one.
func chatPID() (int, error) {
	value := os.Getenv(run.ChatPIDEnvironment)
	if value == "" {
		return 0, fmt.Errorf("dg ticket rerun only works inside dg ticket chat")
	}
	pid, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s is not a valid PID: %q", run.ChatPIDEnvironment, value)
	}
	return pid, nil
}
