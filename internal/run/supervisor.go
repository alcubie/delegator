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

// Start runs the ticket with the given id: it claims the ticket for this run,
// creates the worktree and its branch, then runs the agent in the worktree and
// waits for it to exit. What the agent writes goes to a log below runs/<id>,
// the session id it reports goes on the ticket, and the time it exits and its
// exit code go on the row of the run.
//
// The claim comes first, and it refuses a ticket that is not queued. A ticket
// in running is one that a different supervisor holds, and this one stops
// rather than run a second agent on the same worktree.
//
// A ticket the run claimed and did not finish is failed before Start returns.
// dg finish is the only thing that makes a ticket ready, so a run that reached
// its end with the ticket still in running gave no report, whatever ended it.
//
// cfg is the config of the person, which says which agent the run starts.
func Start(s *store.Store, id int64, cfg config.Config) error {
	ticket, err := s.Ticket(id)
	if err != nil {
		return err
	}
	runID, err := s.Claim(id, branch(id, ticket.Title))
	if err != nil {
		return err
	}
	return supervise(s, cfg, ticket, runID)
}

// Restart starts a failed ticket again. Restart writes the running state and
// the run row in the transaction that gives this supervisor the ticket, so a
// restarted ticket never waits behind the queue.
func Restart(s *store.Store, id int64, cfg config.Config) error {
	ticket, err := s.Ticket(id)
	if err != nil {
		return err
	}
	runID, err := s.Restart(id)
	if err != nil {
		return err
	}
	return supervise(s, cfg, ticket, runID)
}

// StartNext claims the first ticket of the queue for this run and works it,
// the way Start works the ticket a person named. The read of the queue and the
// claim are one transaction, so two supervisors that a trigger started at the
// same time take two different tickets, or one takes a ticket and the other
// finds none.
//
// A supervisor with nothing to claim stops and gives no error. The slot that
// was free when the trigger counted it can be taken by the time this one reads
// the queue, and that is the ordinary end of the second supervisor.
//
// The bool it returns says whether it claimed a ticket. A supervisor that
// claimed nothing is the wrong one to start the next, because nothing has
// changed in the queue since the trigger that started it counted the slots, so
// the caller launches no supervisor for a false.
//
// cfg is the config of the person, which the claim counts the slots against.
func StartNext(s *store.Store, cfg config.Config) (bool, error) {
	ticket, runID, err := s.ClaimNext(cfg, func(t store.Ticket) string { return branch(t.ID, t.Title) })
	if errors.Is(err, store.ErrNoRoom) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, supervise(s, cfg, ticket, runID)
}

// noExitCode is the exit code of a run that ended with no process of its own
// to give one. os/exec gives the same for a process that a signal ended.
const noExitCode = -1

// runTimeout is how long a run may take, from the config. It is a variable so
// that a test can shorten it: the config gives the time in minutes, and no
// test can wait one.
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

// supervise works the ticket that the caller has claimed: it makes the
// worktree and runs the agent in it. runID is the run that the claim wrote,
// and everything this function puts on the row of a run names it, because the
// last run of a ticket is not always the one this supervisor holds.
//
// A worktree that git will not make ends the run before it starts. The ticket
// is claimed by then, so this marks it failed and ends the run: a ticket left
// in running would hold the queue with no supervisor working on it.
func supervise(s *store.Store, cfg config.Config, ticket store.Ticket, runID int64) (err error) {
	dataDir := s.DataDir()
	id := ticket.ID
	// The caller has claimed the ticket, so this run holds it, and every way
	// out of the function below is a way out with no report.
	defer func() { err = errors.Join(err, s.FailUnfinished(runID)) }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	watch := startSupervisorTimer(s, runID, os.Getpid(), runTimeout(cfg), cancel)
	defer func() { err = errors.Join(err, watch.Close()) }()

	worktree, err := Worktree(dataDir, ticket)
	if err != nil {
		return errors.Join(err, s.ChangeStatus(id, store.Failed), s.EndRun(runID, noExitCode))
	}

	log, err := openLog(dataDir, id)
	if err != nil {
		return err
	}
	defer log.Close()

	return superviseACP(ctx, s, cfg, id, runID, worktree, log)
}

// logTime is the layout of a log's name. It is RFC 3339 with the colons
// replaced, because a colon is not a safe character in a file name on every
// system, and it still sorts by time.
//
// It keeps the milliseconds. A restart can claim a ticket in the same second
// that its last run ended, and two runs that took the same name would give
// the second one a file that O_EXCL refuses, which ends the run before the
// agent starts.
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

// prompt returns the first message to the agent for one ticket. It names the
// two commands the agent uses: dg show gives it the ticket, so the prompt does
// not repeat the prose, and dg finish ends the run.
//
// It also says how to read the repository. An agent that starts a run knows
// nothing of the code and finds it by cat, and every file it reads that way
// stays in the context and is sent again with each later call of the run. The
// rules name the reading to avoid rather than the principle behind it, because
// an agent that is told to read with care still cats the file.
func prompt(id int64) string {
	return fmt.Sprintf(`You are working on delegator ticket %[1]d, in this directory. It is a
git worktree on a branch of its own.

1. Run "dg show %[1]d" to read the ticket.
2. Do what the ticket asks.
3. Commit your work with git. The commit message is your report: say
   what you did and why, and name anything that did not go as the
   ticket said. If you changed nothing, commit with --allow-empty and
   say why in the message.
4. Run "dg finish %[1]d <hash>" with the hash of the commit you made.

How to read the repository:

- To find code, grep for the identifier and read the lines around what
  grep gives you. Do not cat a file to learn what is in it.
- Do not read a source file of over 200 lines whole, or a document of
  over 100 lines whole. Read the part you came for.
- Read a document only when the ticket needs a decision that the code
  does not hold. For what the code does, read the code.
`, id)
}
