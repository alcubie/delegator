package run

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/alcubie/delegator/internal/adapters"
	"github.com/alcubie/delegator/internal/store"
)

// Start runs the ticket with the given id: it creates the worktree and its
// branch, claims the ticket for this run, then runs the agent in the worktree
// and waits for it to exit. What the agent writes goes to a log below
// runs/<id>, and the session id it reports goes on the ticket.
//
// The worktree is created first. If git refuses, the ticket stays queued where
// you can see it, rather than sitting in running with nowhere to work.
func Start(dataDir string, id int64, agent adapters.Adapter) error {
	s, err := store.Open(dataDir)
	if err != nil {
		return err
	}
	defer s.Close()

	ticket, err := s.Ticket(id)
	if err != nil {
		return err
	}

	worktree, err := Worktree(dataDir, ticket)
	if err != nil {
		return err
	}
	if err := s.Claim(id, branch(id, ticket.Title)); err != nil {
		return err
	}

	log, err := openLog(dataDir, id)
	if err != nil {
		return err
	}
	defer log.Close()

	var out bytes.Buffer
	cmd := agent.Launch(adapters.RunSpec{Worktree: worktree})
	cmd.Stdout = io.MultiWriter(&out, log)
	cmd.Stderr = log
	// Run returns only after the process exits and the copies into out and
	// the log have drained, so out is complete below.
	runErr := cmd.Run()

	// The session is recorded before the run's failure is reported: a run
	// that failed is the one a person most wants to open.
	session, err := agent.SessionID(out.Bytes())
	if err != nil {
		return err
	}
	if session != "" {
		if err := s.SetSession(id, session); err != nil {
			return err
		}
	}
	return runErr
}

// logTime is the layout of a log's name. It is RFC 3339 with the colons
// replaced, because a colon is not a safe character in a file name on every
// system, and it still sorts by time.
const logTime = "2006-01-02T15-04-05"

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
