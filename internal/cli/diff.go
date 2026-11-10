package cli

import (
	"errors"
	"os"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/alcubie/delegator/internal/config"
	"github.com/alcubie/delegator/internal/project"
	"github.com/alcubie/delegator/internal/run"
	"github.com/alcubie/delegator/internal/store"
)

// diffCommand opens the change of a ticket in the diff tool of the person and
// waits for it. With no ID, it selects the project's first ready ticket, as
// show and chat do.
func diffCommand(dataDir *string, workDir string, cfg *config.Config) *cobra.Command {
	var projectDir string
	cmd := &cobra.Command{
		Use:   "diff [id]",
		Short: "Open the change of a ticket in your diff tool.",
		Long: "Open the change of a ticket, from the default branch of its project to the work of " +
			"its branch, and wait for the tool. Git starts the tool that diff.tool names, and Delegator " +
			"holds no setting of its own for it. The exit status is the exit status of Git.\n\n" +
			"With no ID, open the change of the first ready ticket for the selected project.",
		Example: `  dg ticket diff 42
  dg ticket diff
  dg ticket diff --project ../api`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var target comparison
			err := withStore(*dataDir, cfg, func(s *store.Store) error {
				id, err := resolveTicketID(s, cfg, args, workDir, projectDir)
				if err != nil {
					return err
				}
				target, err = diffable(s, *dataDir, id)
				return err
			})
			if err != nil {
				return err
			}
			// Close the store before the tool, which the person
			// keeps open for as long as they read the change.
			return waitForExit(difftool(target))
		},
	}
	cmd.Flags().StringVar(&projectDir, "project", "",
		"select the project whose first ready ticket to open when ID is omitted (default: current working directory)")
	return rpcOperationCommand("diff", cmd)
}

// comparison is what the diff tool needs: the worktree that holds the work of
// a ticket, and the branch the change is measured from.
type comparison struct {
	worktree string
	base     string
}

// diffable rejects tickets whose change cannot be opened and returns what the
// tool needs for the rest. A ticket that is not ready, or that holds no
// change, gives a refusal, and any other error is a fault of the store or of
// Git. The status comes first, so a ticket that nothing can review costs no
// Git process. The checks have no side effect, so a caller that only reports
// them leaves nothing behind.
func diffable(s *store.Store, dataDir string, id int64) (comparison, error) {
	ticket, err := s.Ticket(id)
	if err != nil {
		return comparison{}, err
	}
	if !diffApplicable(ticket) {
		return comparison{}, refuse("ticket %d is %s: only a ready ticket holds a change to review",
			id, ticket.Status)
	}
	worktree := run.WorktreePath(dataDir, id)
	if _, err := os.Stat(worktree); err != nil {
		return comparison{}, refuse("the worktree of ticket %d is gone: %s", id, worktree)
	}
	base := ticket.Project.DefaultBranch
	if _, err := project.ResolveCommit(worktree, base); err != nil {
		if errors.Is(err, project.ErrUnknownCommit) {
			return comparison{}, refuse(
				"the default branch %s of the project does not resolve in the worktree of ticket %d: %s",
				base, id, worktree)
		}
		return comparison{}, err
	}
	above, err := project.CommitsAbove(worktree, base)
	if err != nil {
		return comparison{}, err
	}
	if above == 0 {
		return comparison{}, refuse("ticket %d holds no commit above %s, so there is no change to open", id, base)
	}
	return comparison{worktree: worktree, base: base}, nil
}

// diffAvailable reports whether diff can open the change of a ticket, and the
// reason beside it for when it cannot.
func diffAvailable(s *store.Store, dataDir string, id int64) (bool, string, error) {
	_, err := diffable(s, dataDir, id)
	return availability(err)
}

// diffApplicable reports whether a ticket in this status is one a person could
// review, which a refusal from diffAvailable does not separate from a change
// that exists and cannot be opened. Ready is the one status where a change
// waits for a person to accept it: a queued ticket has not run, a running one
// is still being written to, a failed one is restarted rather than reviewed,
// and a done or a cancelled ticket is closed.
func diffApplicable(ticket store.Ticket) bool {
	return ticket.Status == store.Ready
}

// difftool builds the Git command that opens the change. The flag -d gives
// the tool the whole change as two directories, and Git removes them when the
// tool stops, so the command must wait for it. project.Command takes away the
// Git variables of the caller, which a hook can set to a path that is wrong in
// another directory.
func difftool(target comparison) *exec.Cmd {
	cmd := project.Command(target.worktree, "difftool", "-d", target.base+"...HEAD")
	cmd.Dir = target.worktree
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd
}
