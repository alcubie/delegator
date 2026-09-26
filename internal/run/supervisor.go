package run

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/alcubie/delegator/internal/config"
	"github.com/alcubie/delegator/internal/store"
)

// Start claims the named ticket, creates its worktree and branch, then runs
// the configured agent. Output goes under runs/<id>; the session ID is saved
// on the ticket and the exit details on the run.
//
// Claiming first prevents another supervisor from using the same worktree.
// Before returning, any claimed ticket still running is marked failed: only
// dg finish makes work ready.
func Start(s *store.Store, id int64, cfg config.Config) error {
	ticket, err := s.Ticket(id)
	if err != nil {
		return err
	}
	runID, err := s.Claim(id, branch(id, ticket.Title))
	if err != nil {
		return err
	}
	return supervise(s, cfg, ticket, runID, cfg.DefaultAgent)
}

// Restart starts a failed ticket again. Restart writes the running state and
// the run row in the transaction that gives this supervisor the ticket, so a
// restarted ticket never waits behind the queue.
func Restart(s *store.Store, id int64, cfg config.Config) error {
	ticket, err := s.Ticket(id)
	if err != nil {
		return err
	}
	agent := cfg.DefaultAgent
	if ticket.Session != "" {
		prior, err := s.Run(id)
		if err != nil {
			return err
		}
		agent = prior.Agent
	}
	runID, err := s.Restart(id)
	if err != nil {
		return err
	}
	return supervise(s, cfg, ticket, runID, agent)
}

// StartNext claims and runs the first eligible queued ticket under cfg
// limits. Selection and claim are atomic, so concurrent supervisors cannot
// claim the same ticket.
//
// It returns false without error when no work can start, including when
// another supervisor took the available capacity. Callers should only trigger
// more work if this supervisor claimed a ticket.
func StartNext(s *store.Store, cfg config.Config) (bool, error) {
	ticket, runID, err := s.ClaimNext(cfg, func(t store.Ticket) string { return branch(t.ID, t.Title) })
	if errors.Is(err, store.ErrNoRoom) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, supervise(s, cfg, ticket, runID, cfg.DefaultAgent)
}

// noExitCode marks a run without a reported exit code, matching os/exec for a
// process terminated by a signal.
const noExitCode = -1

// runTimeout converts the configured limit to a duration. Tests replace it to
// avoid waiting whole minutes.
var runTimeout = config.Config.Timeout

// stopRun is the process-group stop used when the timer expires. A test runs
// Start inside its own process and replaces this before shortening the timer,
// because that process is not a detached supervisor.
var stopRun = Stop

type supervisorTimer struct {
	timer *time.Timer
	done  chan error
}

// startSupervisorTimer marks a run over and stops the process group when its
// limit expires. The state is written first because the supervisor belongs to
// the group being stopped and cannot report after the final signal.
func startSupervisorTimer(s *store.Store, runID int64, pid int, limit time.Duration, cancel context.CancelFunc) *supervisorTimer {
	watch := &supervisorTimer{}
	if limit <= 0 {
		return watch
	}
	watch.done = make(chan error, 1)
	watch.timer = time.AfterFunc(limit, func() {
		err := s.FailUnfinished(runID)
		cancel()
		err = errors.Join(err, stopRun(pid, StopGrace))
		watch.done <- err
	})
	return watch
}

// Close disarms a timer whose run ended in time, or waits for a timeout that
// has begun so that its database write is part of the result of the run.
func (t *supervisorTimer) Close() error {
	if t.timer == nil || t.timer.Stop() {
		return nil
	}
	return <-t.done
}

// supervise creates the claimed ticket's worktree and runs its agent. Updates
// use the claimed runID, since a restart may create a newer run. Setup
// failures close the run and mark the ticket failed so it does not keep
// occupying capacity.
func supervise(s *store.Store, cfg config.Config, ticket store.Ticket, runID int64, agent string) (err error) {
	dataDir := s.DataDir()
	id := ticket.ID
	// Any return without dg finish must fail the claimed ticket.
	defer func() { err = errors.Join(err, s.FailUnfinished(runID)) }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// A context deadline only asks ACP to stop; the timer also records the run and stops its whole process group.
	watch := startSupervisorTimer(s, runID, os.Getpid(), runTimeout(cfg), cancel)
	defer func() { err = errors.Join(err, watch.Close()) }()

	worktree, err := Worktree(dataDir, ticket)
	if err != nil {
		return errors.Join(err, s.ChangeStatus(id, store.Failed), s.EndRun(runID, noExitCode))
	}
	cacheDir, err := ProjectCache(dataDir, ticket.Project.ID)
	if err != nil {
		return err
	}

	log, err := openLog(dataDir, id)
	if err != nil {
		return err
	}
	defer log.Close()

	return superviseACP(ctx, s, agent, ticket, runID, worktree, cacheDir, log)
}

// logTime uses RFC 3339 with filename-safe separators and millisecond
// precision. Rapid restarts can occur within one second; distinct names avoid
// O_EXCL failures when creating their logs.
const logTime = "2006-01-02T15-04-05.000"

// openLog creates the log for one run below runs/<id>. Each run gets its own
// file, named for when it started, so a restart leaves the earlier log alone.
func openLog(dataDir string, id int64) (*os.File, error) {
	dir := filepath.Join(dataDir, "runs", strconv.FormatInt(id, 10))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	name := time.Now().UTC().Format(logTime) + ".log"
	return os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
}

// prompt tells the agent to read its ticket with dg show and report its
// commit with dg finish. It also gives concrete limits on repository reads to
// keep unnecessary files out of the agent's context.
func prompt(id int64, dataDir, cacheDir string) string {
	return fmt.Sprintf(`You are working on delegator ticket %[1]d, in this directory. It is a
git worktree on a branch of its own.

A project-scoped cache directory shared by concurrent tickets is writable at
%[3]q. If useful, use it only for caches that support concurrent access; keep
ticket-specific temporary artifacts in this worktree. Its contents are
disposable and are not removed with this worktree.

1. Run dg show %[1]d --data-dir %[2]q to read the ticket.
2. Do what the ticket asks.
3. Commit your work with git. The commit message is your report: say
   what you did and why, and name anything that did not go as the
   ticket said. If you changed nothing, commit with --allow-empty and
   say why in the message.
4. Run dg finish %[1]d <hash> --data-dir %[2]q with the hash of the commit you made.

How to read the repository:

- To find code, grep for the identifier and read the lines around what
  grep gives you. Do not cat a file to learn what is in it.
- Do not read a source file of over 200 lines whole, or a document of
  over 100 lines whole. Read the part you came for.
- Read a document only when the ticket needs a decision that the code
  does not hold. For what the code does, read the code.
`, id, dataDir, cacheDir)
}
