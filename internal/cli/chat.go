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

// ExitError carries the exit status of an interactive agent. cmd/dg returns
// that status without printing another error because the agent already wrote
// to the terminal.
type ExitError struct{ Code int }

// Error names the status, for a caller that writes the error instead.
func (e ExitError) Error() string { return fmt.Sprintf("exit status %d", e.Code) }

// chat builds the session-resume command. Tests replace it to inspect
// launches without starting an agent.
var chat = chatCmd

// chatCmd connects the resume command directly to the user's terminal.
func chatCmd(argv []string, dir string) *exec.Cmd {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd
}

// chatCommand resumes a ticket conversation using the registered agent
// command and original worktree. With no ID, it selects the project's first
// ready ticket, as show and accept do.
func chatCommand(dataDir *string, workDir string, cfg *config.Config) *cobra.Command {
	var projectDir string
	cmd := &cobra.Command{
		Use:   "chat [id]",
		Short: "Continue a ticket's agent session.",
		Long: "Continue a ticket's existing agent session in its worktree and wait for the " +
			"interactive command. With no ID, continue the first ready ticket for the selected project.",
		Example: `  dg chat 42
  dg chat
  dg chat --project ../api`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var argv []string
			var worktree, cacheDir string
			err := withStore(*dataDir, cfg, func(s *store.Store) error {
				id, err := resolveTicketID(s, cfg, args, workDir, projectDir)
				if err != nil {
					return err
				}
				argv, worktree, cacheDir, err = resumeOf(s, *dataDir, id)
				return err
			})
			if err != nil {
				return err
			}
			// Close the store before the interactive
			// conversation, which may run indefinitely.
			interactive := chat(argv, worktree)
			interactive.Env = cacheEnvironment(os.Environ(), cacheDir)
			return waitForChat(interactive)
		},
	}
	cmd.Flags().StringVar(&projectDir, "project", "",
		"select the project whose first ready ticket to continue when ID is omitted (default: current working directory)")
	return rpcOperationCommand("chat", cmd)
}

// resumeOf returns resume arguments, the worktree, and the project cache,
// rejecting tickets without a resumable conversation. Running tickets are
// refused to avoid two agents writing the same session; reconciliation has
// already identified stale runs.
func resumeOf(s *store.Store, dataDir string, id int64) ([]string, string, string, error) {
	ticket, err := s.Ticket(id)
	if err != nil {
		return nil, "", "", err
	}
	if ticket.Status == store.Running {
		r, err := s.Run(id)
		if err != nil {
			return nil, "", "", err
		}
		return nil, "", "", fmt.Errorf(
			"ticket %d is running: process %d is on its session", id, r.PID)
	}
	if ticket.Session == "" {
		return nil, "", "", fmt.Errorf("ticket %d has no session: it has not run yet", id)
	}
	// Require the original worktree: agents may locate session history by
	// working directory.
	worktree := run.WorktreePath(dataDir, id)
	if _, err := os.Stat(worktree); err != nil {
		return nil, "", "", fmt.Errorf(
			"the worktree of ticket %d is gone: %s", id, worktree)
	}
	r, err := s.Run(id)
	if err != nil {
		return nil, "", "", err
	}
	agent, err := s.Agent(r.Agent)
	if err != nil {
		return nil, "", "", err
	}
	if len(agent.Resume) == 0 {
		return nil, "", "", fmt.Errorf("agent %q has no command that opens a session", r.Agent)
	}
	cacheDir, err := run.ProjectCache(dataDir, ticket.Project.ID)
	if err != nil {
		return nil, "", "", fmt.Errorf("prepare the project cache: %w", err)
	}
	argv := make([]string, 0, len(agent.Resume))
	for _, arg := range agent.Resume {
		arg = strings.ReplaceAll(arg, "{session}", ticket.Session)
		argv = append(argv, strings.ReplaceAll(arg, "{project_cache}", cacheDir))
	}
	return argv, worktree, cacheDir, nil
}

func cacheEnvironment(environ []string, cacheDir string) []string {
	prefix := run.ProjectCacheEnvironment + "="
	result := make([]string, 0, len(environ)+1)
	for _, entry := range environ {
		if !strings.HasPrefix(entry, prefix) {
			result = append(result, entry)
		}
	}
	return append(result, prefix+cacheDir)
}

// waitForChat waits for the interactive agent and propagates its exit status.
func waitForChat(cmd *exec.Cmd) error {
	err := cmd.Run()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return ExitError{Code: exitErr.ExitCode()}
	}
	return err
}
