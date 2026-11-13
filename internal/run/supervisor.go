package run

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	agent, err := s.Agent(cfg.DefaultAgent)
	if err != nil {
		return err
	}
	runID, err := s.Claim(id, branch(id, ticket.Title), agent.ID)
	if err != nil {
		return err
	}
	return supervise(s, cfg, ticket, runID, agent, cfg.DefaultModel, prompt, "")
}

// sessionAgent returns the agent and the model of a run that continues the
// work of a ticket. A stored session keeps the agent and the recorded model of
// the run that saved it, even where the defaults have changed, and an unknown
// prior model leaves the loaded session unchanged. A ticket without a session
// has none to continue, so a new session starts in the existing worktree with
// the current defaults.
func sessionAgent(s *store.Store, ticket store.Ticket, cfg config.Config) (store.Agent, string, sql.Null[int64], error) {
	if ticket.Session == "" {
		agent, err := s.Agent(cfg.DefaultAgent)
		return agent, cfg.DefaultModel, sql.Null[int64]{}, err
	}
	prior, err := s.Run(ticket.ID)
	if err != nil {
		return store.Agent{}, "", sql.Null[int64]{}, err
	}
	agent, err := s.AgentByID(prior.AgentID)
	if err != nil {
		return store.Agent{}, "", sql.Null[int64]{}, err
	}
	return agent, prior.Model, prior.ModelID, nil
}

// cacheLifetime is how long after its last use Claude keeps the prompt cache
// of a session, less five minutes of margin.
const cacheLifetime = 55 * time.Minute

var now = time.Now

// Restart starts a failed ticket again. Restart writes the running state and
// the run row in the transaction that gives this supervisor the ticket, so a
// restarted ticket never waits behind the queue. A nonempty modelOverride
// selects and records a model for this run before session setup.
func Restart(s *store.Store, id int64, cfg config.Config, modelOverride string) error {
	ticket, err := s.Ticket(id)
	if err != nil {
		return err
	}
	agent, model, modelID, err := sessionAgent(s, ticket, cfg)
	if err != nil {
		return err
	}
	text, note := prompt, ""
	if ticket.Session != "" {
		prior, err := s.Run(id)
		if err != nil {
			return err
		}
		if idle := now().Sub(prior.EndedAt); !prior.EndedAt.IsZero() && idle > cacheLifetime {
			ticket.Session = ""
			text = restartPrompt(prior.Error)
			note = fmt.Sprintf("last run ended %s ago, new session", idle.Round(time.Minute))
		} else {
			note = fmt.Sprintf("last run ended %s ago, loaded session", idle.Round(time.Minute))
		}
	}
	var runID int64
	if modelOverride != "" {
		model = modelOverride
		runID, err = s.RestartWithModel(id, agent.ID, modelOverride)
	} else {
		runID, err = s.Restart(id, agent.ID, modelID)
	}
	if err != nil {
		return err
	}
	return supervise(s, cfg, ticket, runID, agent, model, text, note)
}

// Conflict runs a ready ticket again to merge the default branch of its
// project and correct the conflicts that the branch brought, so the person
// does not have to open a chat and ask for the merge. It starts a new session
// in the existing worktree and not the session that reported the commit, which
// can hold a very large context; the commits of the branch hold the work.
//
// The transition is the one dg ticket rerun uses, written here rather than by
// the command that found the conflict so that the run records the PID of this
// supervisor. A ready ticket already holds its place in the capacity count, so
// the run needs no free slot.
func Conflict(s *store.Store, id int64, cfg config.Config) error {
	ticket, err := s.Ticket(id)
	if err != nil {
		return err
	}
	agent, model, _, err := sessionAgent(s, ticket, cfg)
	if err != nil {
		return err
	}
	runID, err := s.Rerun(id, agent.ID, os.Getpid())
	if err != nil {
		return err
	}
	ticket.Session = ""
	return supervise(s, cfg, ticket, runID, agent, model, conflictPrompt, "")
}

// StartNext claims and runs the first eligible queued ticket under cfg
// limits. Selection and claim are atomic, so concurrent supervisors cannot
// claim the same ticket.
//
// It returns false without error when no work can start, including when
// another supervisor took the available capacity. Callers should only trigger
// more work if this supervisor claimed a ticket.
func StartNext(s *store.Store, cfg config.Config) (bool, error) {
	claimable, err := s.ClaimableCount(cfg)
	if err != nil {
		return false, err
	}
	if claimable == 0 {
		return false, nil
	}
	agent, err := s.Agent(cfg.DefaultAgent)
	if err != nil {
		return false, err
	}
	ticket, runID, err := s.ClaimNext(cfg, func(t store.Ticket) string { return branch(t.ID, t.Title) }, agent.ID)
	if errors.Is(err, store.ErrNoRoom) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, supervise(s, cfg, ticket, runID, agent, cfg.DefaultModel, prompt, "")
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
func supervise(s *store.Store, cfg config.Config, ticket store.Ticket, runID int64, agent store.Agent, model string, text firstPrompt, note string) (err error) {
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
		return errors.Join(err, s.ChangeStatus(id, store.Failed), s.EndRun(runID, noExitCode, err))
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
	if note != "" {
		if _, err := fmt.Fprintf(log, "delegator: %s\n", note); err != nil {
			return err
		}
	}

	return superviseACP(ctx, s, agent, ticket, runID, worktree, cacheDir, model, text, log)
}

// logTime uses RFC 3339 with filename-safe separators and millisecond
// precision. Rapid restarts can occur within one second; distinct names avoid
// O_EXCL failures when creating their logs.
const logTime = "2006-01-02T15-04-05.000"

// openLog creates the log for one run below runs/<id>. Each run gets its own
// file, named for when it started, so a restart leaves the earlier log alone.
func openLog(dataDir string, id int64) (*os.File, error) {
	dir := LogDirPath(dataDir, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	name := time.Now().UTC().Format(logTime) + ".log"
	return os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
}

// firstPrompt builds the prompt that opens a run. The supervisor takes it from
// its caller, which chooses the prompt that suits the run it starts.
type firstPrompt func(id int64, dataDir, cacheDir, defaultBranch string) string

// prompt tells the agent to read its ticket with dg ticket show and report its
// commit with dg ticket finish, and to mark the ticket running again with dg
// ticket rerun when the person asks for changes later in the same session. It
// names the default branch so that the agent merges it before finish refuses a
// commit that conflicts with it. It also gives concrete limits on repository
// reads to keep unnecessary files out of the agent's context.
func prompt(id int64, dataDir, cacheDir, defaultBranch string) string {
	return fmt.Sprintf(`You are working on delegator ticket %[1]d, in this directory. It is a
git worktree on a branch of its own.

%[5]s
1. Run dg ticket show %[1]d --data-dir %[2]q to read the ticket.
2. Do what the ticket asks.
3. Merge %[4]s into this branch when %[4]s has new commits. Merge it;
   do not rebase. Correct each conflict and do the checks of the project
   again.
4. Commit your work with git. The commit message is your report: say
   what you did and why, and name anything that did not go as the
   ticket said. If you changed nothing, commit with --allow-empty and
   say why in the message.
   If you saw something outside the ticket that the person should
   decide, such as a bug in code you did not touch or a default branch
   that does not build, add a Finding: trailer for it to the footer of
   the message, beside the ticket line. Write one trailer for each
   finding, and indent a continuation line when a finding needs more
   than one line. A finding is not a note about the work of the ticket,
   which belongs in the body, and it is not a request for permission:
   finish your ticket either way.
5. Run dg ticket finish %[1]d <hash> --data-dir %[2]q with the hash of the commit you made.
   It refuses a commit that conflicts with %[4]s.

%[6]s`, id, dataDir, cacheDir, defaultBranch, cacheNote(cacheDir), laterChanges(id, dataDir, defaultBranch))
}

// cacheNote tells the agent where the project cache is and what may go in it.
func cacheNote(cacheDir string) string {
	return fmt.Sprintf(`A project-scoped cache directory shared by concurrent tickets is writable at
%q. If useful, use it only for caches that support concurrent access; keep
ticket-specific temporary artifacts in this worktree. Its contents are
disposable and are not removed with this worktree.
`, cacheDir)
}

// laterChanges gives the steps for a change request in the same session, and
// the limits on repository reads.
func laterChanges(id int64, dataDir, defaultBranch string) string {
	return fmt.Sprintf(`If the person continues this session later and asks for changes:

1. Run dg ticket rerun %[1]d --data-dir %[2]q before you change anything.
2. Merge %[3]s into this branch again when %[3]s has new commits, by the same
   rule: merge it, do not rebase, correct each conflict.
3. Make the changes and commit them. Add a new commit on top of the one you
   reported before; do not amend it, so that the reviewer sees what changed
   since the last review.
4. Run dg ticket finish %[1]d <hash> --data-dir %[2]q with the hash of the new
   commit.

Do not run rerun when the person only asks questions and does not ask for a
change.

How to read the repository:

- To find code, grep for the identifier and read the lines around what
  grep gives you. Do not cat a file to learn what is in it.
- Do not read a source file of over 200 lines whole, or a document of
  over 100 lines whole. Read the part you came for.
- Read a document only when the ticket needs a decision that the code
  does not hold. For what the code does, read the code.
`, id, dataDir, defaultBranch)
}

// conflictPrompt opens a new session for a run that merges the default branch
// into a branch whose reported commit no longer merges. The session has not
// seen the work, so the prompt sends the agent to the ticket and to the
// commits of the branch.
func conflictPrompt(id int64, dataDir, cacheDir, defaultBranch string) string {
	return fmt.Sprintf(`You are working on delegator ticket %[1]d, in this directory. It is a
git worktree on a branch of its own. The branch holds the work on the ticket.
The commit that was reported for it no longer merges into %[2]s: new commits
of %[2]s conflict with this branch.

%[4]s
1. Run dg ticket show %[1]d --data-dir %[3]q to read the ticket.
2. Run git log %[2]s..HEAD to read the commits of the branch before the merge.
3. Merge %[2]s into this branch. Merge it; do not rebase.
4. Correct each conflict. Keep the work of the ticket and the work that
   landed on %[2]s.
5. Do the checks of the project again.
6. Commit the correction. Add a new commit on top of the one that was
   reported; do not amend it, so that the reviewer sees what changed.
7. Run dg ticket finish %[1]d <hash> --data-dir %[3]q with the hash of the new
   commit.

%[5]s`, id, defaultBranch, dataDir, cacheNote(cacheDir), laterChanges(id, dataDir, defaultBranch))
}

// restartPrompt opens a new session for a run that restarts a failed ticket
// after the cache of its old session expired. The session has not seen the
// work, so the prompt gives the error of the failed run and sends the agent to
// the commits and the state of the branch.
func restartPrompt(failure string) firstPrompt {
	return func(id int64, dataDir, cacheDir, defaultBranch string) string {
		return prompt(id, dataDir, cacheDir, defaultBranch) + fmt.Sprintf(`
The branch holds the work of an earlier run of this ticket that failed. The
error of that run was:

%s

Before you change anything, run git log %s..HEAD and git status to see what
that run did.
`, failure, defaultBranch)
	}
}
