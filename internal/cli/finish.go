package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/alcubie/delegator/internal/config"
	"github.com/alcubie/delegator/internal/project"
	"github.com/alcubie/delegator/internal/store"
	"github.com/spf13/cobra"
)

// claudeSessionEnv is the variable Claude Code exports to the commands its
// tools run. A session sent to the background continues as a fork under a new
// ID, and exports that one.
const claudeSessionEnv = "CLAUDE_CODE_SESSION_ID"

// errCommitConflicts refuses a commit that no longer merges into the default
// branch. The agent is still in its session when it runs finish, so the
// message is the one that tells it what to do about the conflict.
var errCommitConflicts = errors.New("the commit conflicts with the default branch")

func refuseConflict(ticket store.Ticket) error {
	into := ticket.Project.DefaultBranch
	return fmt.Errorf("%w %s: merge %s into %s, correct the conflicts, run the checks of the project again, "+
		"commit, and run finish with the new commit; do not rebase, because a rebase changes commits the "+
		"person has possibly reviewed",
		errCommitConflicts, into, into, ticket.Branch)
}

// callingSession returns the session of the Claude Code session that is
// running finish, when the ticket's latest run is a claude run. It returns the
// empty string otherwise, which leaves the stored session alone: a person
// finishing a ticket by hand from a Claude session of their own must not
// replace the session of a run by another agent.
func callingSession(s *store.Store, ticket store.Ticket) (string, error) {
	session := os.Getenv(claudeSessionEnv)
	if session == "" {
		return "", nil
	}
	run, err := s.Run(ticket.ID)
	if errors.Is(err, store.ErrNoRun) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if run.Agent != "claude" {
		return "", nil
	}
	return session, nil
}

// finishCommand returns a fresh finish command whose example uses commandPath.
func finishCommand(dataDir *string, cfg *config.Config, commandPath string) *cobra.Command {
	return rpcOperationCommand("finish", &cobra.Command{
		Use:   "finish <id> <commit>",
		Short: "Record the commit of a ticket and mark it Ready.",
		Long: "Record the ticket branch commit that contains the completed work and mark the " +
			"ticket ready for review. The commit must belong to the ticket's branch and must still merge " +
			"into the project's default branch without conflicts. When it conflicts, merge the default " +
			"branch into the ticket branch, correct the conflicts, commit, and finish with the new commit.",
		Example: fmt.Sprintf("  %s 42 4f3c2b1", commandPath),
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := ticketArg(args[0])
			if err != nil {
				return err
			}
			return withStore(*dataDir, cfg, func(s *store.Store) error {
				ticket, err := s.Ticket(id)
				if err != nil {
					return err
				}
				// Report the state before consulting Git, without passing an
				// unvalidated commit to a ticket that cannot be finished.
				if ticket.Status != store.Running && ticket.Status != store.Failed && ticket.Status != store.Ready {
					return fmt.Errorf("%w: %s to %s",
						store.ErrInvalidTicketStateChange, ticket.Status, store.Ready)
				}
				commit, err := project.CommitOnBranch(ticket.Project.Path, args[1], ticket.Branch)
				if err != nil {
					return err
				}
				clean, err := project.MergesCleanly(ticket.Project.Path, commit, ticket.Project.DefaultBranch)
				if err != nil {
					return err
				}
				if !clean {
					return refuseConflict(ticket)
				}
				session, err := callingSession(s, ticket)
				if err != nil {
					return err
				}
				return s.FinishTicket(id, commit, session)
			})
		},
	})
}
