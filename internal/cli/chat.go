package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/alcubie/delegator/internal/config"
	"github.com/alcubie/delegator/internal/run"
	"github.com/alcubie/delegator/internal/store"
)

// ExitError carries the status of a program that dg started for the person and
// waited for. dg chat gives the terminal to the agent, so the status of dg is
// the status of the agent, and cmd/dg reads the code off this error. The
// program has already written whatever it had to say to the terminal, so
// nothing is written for it.
type ExitError struct{ Code int }

// Error names the status, for a caller that writes the error instead.
func (e ExitError) Error() string { return fmt.Sprintf("exit status %d", e.Code) }

// chat returns the command that continues a session: the argv of the adapter,
// in the worktree of the ticket, with the terminal of the person. It is a
// variable so that a test can see what dg chat would start without starting a
// program.
var chat = chatCmd

// chatCmd builds that command. The three streams are the terminal's, because
// the conversation is between the person and the agent and dg is only the
// program that started it.
func chatCmd(argv []string, dir string) *exec.Cmd {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd
}

// chatCommand returns the command dg chat. The line that continues a session
// is delegator's and not the person's: the table of agents knows which program
// to start and with which arguments, and delegator knows the worktree the
// conversation ran in and whether a run is on it now. A line typed by hand
// from the wrong directory starts a new conversation under the id of the old
// one, and the person cannot see that it did.
//
// With no id it continues the head of READY, as dg show and dg accept act on
// it: the ticket a person reads next is the ticket they have something to say
// to.
func chatCommand(dataDir, workDir string, cfg *config.Config) *cobra.Command {
	var projectDir string
	cmd := &cobra.Command{
		Use:   "chat [id]",
		Short: "Continue the session of a ticket in this terminal. Defaults to the first Ready ticket for the project.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var argv []string
			var worktree string
			err := withStore(dataDir, cfg, func(s *store.Store) error {
				id, err := resolveTicketID(s, cfg, args, workDir, projectDir)
				if err != nil {
					return err
				}
				argv, worktree, err = resumeOf(s, dataDir, id)
				return err
			})
			if err != nil {
				return err
			}
			// The start is outside the store, because the person is in that
			// conversation for as long as they want to be and no other
			// command can write while it is open.
			return waitForChat(chat(argv, worktree))
		},
	}
	cmd.Flags().StringVar(&projectDir, "project", "",
		"the directory of the project whose first ready ticket to continue.  Defaults to current working directory.")
	return cmd
}

// resumeOf returns the argv that continues the session of one ticket and the
// directory to start it in, and refuses each ticket that has no conversation
// to continue.
//
// The reconcile of every command has already marked failed each run whose
// supervisor is gone, so a ticket still in running here has an agent on that
// session now, and a second writer on one conversation is the fault this
// command exists to stop.
func resumeOf(s *store.Store, dataDir string, id int64) ([]string, string, error) {
	ticket, err := s.Ticket(id)
	if err != nil {
		return nil, "", err
	}
	if ticket.Status == store.Running {
		r, err := s.Run(id)
		if err != nil {
			return nil, "", err
		}
		return nil, "", fmt.Errorf(
			"ticket %d is running: process %d is on its session", id, r.PID)
	}
	if ticket.Session == "" {
		return nil, "", fmt.Errorf("ticket %d has no session: it has not run yet", id)
	}
	// The agent keeps the record of a conversation below the directory the
	// conversation ran in, so a session with no worktree is a session the
	// agent will not find, and it would make a new one under the old id.
	worktree := run.WorktreePath(dataDir, id)
	if _, err := os.Stat(worktree); err != nil {
		return nil, "", fmt.Errorf(
			"the worktree of ticket %d is gone: %s", id, worktree)
	}
	r, err := s.Run(id)
	if err != nil {
		return nil, "", err
	}
	agent, err := s.Agent(r.Agent)
	if err != nil {
		return nil, "", err
	}
	if len(agent.Resume) == 0 {
		return nil, "", fmt.Errorf("agent %q has no command that opens a session", r.Agent)
	}
	argv := make([]string, 0, len(agent.Resume))
	for _, arg := range agent.Resume {
		argv = append(argv, strings.ReplaceAll(arg, "{session}", ticket.Session))
	}
	return argv, worktree, nil
}

// waitForChat waits for the conversation to end and gives its status to the
// person: dg is the terminal of that program for as long as it runs, and a
// status of its own would say nothing the program did not already say.
func waitForChat(cmd *exec.Cmd) error {
	err := cmd.Run()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return ExitError{Code: exitErr.ExitCode()}
	}
	return err
}
