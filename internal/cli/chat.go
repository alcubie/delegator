package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
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
			"interactive command. With no ID, continue the first ready ticket for the selected project.\n\n" +
			"Moving the session to the background from within chat may cause issues, and is best avoided. " +
			"For example, Claude Code's /background command ends the session and starts a new process. " +
			"This causes Delegator to show that the ticket has failed while it is still actually running. " +
			"However, when the run has completed successfully, the ticket will move to the Ready bucket.",
		Example: `  dg ticket chat 42
  dg ticket chat
  dg ticket chat --project ../api`,
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
			interactive.Env = chatEnvironment(os.Environ(), cacheDir, os.Getpid())
			return waitForChat(interactive)
		},
	}
	cmd.Flags().StringVar(&projectDir, "project", "",
		"select the project whose first ready ticket to continue when ID is omitted (default: current working directory)")
	return rpcOperationCommand("chat", cmd)
}

// refusal is a reason that a ticket cannot continue its conversation. chat
// returns it as the error of the command and show reports it in its result, so
// both name the same reason.
type refusal struct{ reason string }

// Error gives the reason, for a caller that writes the error.
func (r refusal) Error() string { return r.reason }

// refuse builds one refusal.
func refuse(format string, args ...any) error {
	return refusal{reason: fmt.Sprintf(format, args...)}
}

// resumption is what continuing a conversation needs: the session the agent
// reported, the worktree its run worked in, the command of the agent that
// opens a session, and the project whose cache that command names.
type resumption struct {
	session  string
	worktree string
	resume   []string
	project  int64
}

// resumable rejects tickets without a resumable conversation and returns what
// the resume command needs for the rest. Running tickets are refused to avoid
// two agents writing the same session; reconciliation has already identified
// stale runs. A ticket that cannot continue gives a refusal, and any other
// error is a fault of the store. The checks have no side effect, so a caller
// that only reports them leaves nothing behind.
func resumable(s *store.Store, dataDir string, id int64) (resumption, error) {
	ticket, err := s.Ticket(id)
	if err != nil {
		return resumption{}, err
	}
	if ticket.Status == store.Running {
		r, err := s.Run(id)
		if err != nil {
			return resumption{}, err
		}
		return resumption{}, refuse("ticket %d is running: process %d is on its session", id, r.PID)
	}
	if ticket.Session == "" {
		return resumption{}, noSession(s, dataDir, id)
	}
	// Require the original worktree: agents may locate session history by
	// working directory.
	worktree := run.WorktreePath(dataDir, id)
	if _, err := os.Stat(worktree); err != nil {
		return resumption{}, refuse("the worktree of ticket %d is gone: %s", id, worktree)
	}
	r, err := s.Run(id)
	if err != nil {
		return resumption{}, err
	}
	agent, err := s.Agent(r.Agent)
	if err != nil {
		return resumption{}, err
	}
	if len(agent.Resume) == 0 {
		return resumption{}, refuse("agent %q has no command that opens a session", r.Agent)
	}
	return resumption{
		session:  ticket.Session,
		worktree: worktree,
		resume:   agent.Resume,
		project:  ticket.Project.ID,
	}, nil
}

// chatAvailable reports whether chat can continue a ticket's conversation,
// and the reason beside it for when it cannot.
func chatAvailable(s *store.Store, dataDir string, id int64) (bool, string, error) {
	_, err := resumable(s, dataDir, id)
	var refused refusal
	if errors.As(err, &refused) {
		return false, refused.reason, nil
	}
	if err != nil {
		return false, "", err
	}
	return true, "", nil
}

// resumeOf returns resume arguments, the worktree, and the project cache of a
// ticket whose conversation can continue.
func resumeOf(s *store.Store, dataDir string, id int64) ([]string, string, string, error) {
	target, err := resumable(s, dataDir, id)
	if err != nil {
		return nil, "", "", err
	}
	cacheDir, err := run.ProjectCache(dataDir, target.project)
	if err != nil {
		return nil, "", "", fmt.Errorf("prepare the project cache: %w", err)
	}
	argv := make([]string, 0, len(target.resume))
	for _, arg := range target.resume {
		arg = strings.ReplaceAll(arg, "{session}", target.session)
		argv = append(argv, strings.ReplaceAll(arg, "{project_cache}", cacheDir))
	}
	return argv, target.worktree, cacheDir, nil
}

// noSession explains a ticket that holds no conversation to continue. A run
// that ended before its agent opened a session wrote the reason to its log, so
// name that log instead of reporting a ticket that has never run.
func noSession(s *store.Store, dataDir string, id int64) error {
	r, err := s.Run(id)
	if errors.Is(err, store.ErrNoRun) {
		return refuse("ticket %d has no session: it has not run yet", id)
	}
	if err != nil {
		return err
	}
	if log, ok := run.LatestLog(dataDir, id); ok {
		return refuse("ticket %d has no session: run %d ended before the agent opened one\n"+
			"View the run log for more details: %s", id, r.ID, log)
	}
	return refuse("ticket %d has no session: run %d ended before the agent opened one", id, r.ID)
}

// chatEnvironment sets the project cache and chat PID variables on the
// agent's environment, replacing any inherited values of either so a chat
// started from inside another chat does not keep its parent's.
func chatEnvironment(environ []string, cacheDir string, pid int) []string {
	environ = withoutEnv(environ, run.ProjectCacheEnvironment)
	environ = withoutEnv(environ, run.ChatPIDEnvironment)
	return append(environ,
		run.ProjectCacheEnvironment+"="+cacheDir,
		run.ChatPIDEnvironment+"="+strconv.Itoa(pid))
}

// withoutEnv returns environ with any entry for key removed.
func withoutEnv(environ []string, key string) []string {
	prefix := key + "="
	result := make([]string, 0, len(environ))
	for _, entry := range environ {
		if !strings.HasPrefix(entry, prefix) {
			result = append(result, entry)
		}
	}
	return result
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
