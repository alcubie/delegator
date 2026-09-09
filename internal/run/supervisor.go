package run

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
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
// runs/<id>, the session id it reports goes on the ticket, and the time it
// exits and its exit code go on the row of the run.
//
// The worktree is created first. If git refuses, the ticket stays queued where
// you can see it, rather than sitting in running with nowhere to work.
//
// A ticket the run claimed and did not finish is failed before Start returns.
// dg finish is the only thing that makes a ticket ready, so a run that reached
// its end with the ticket still in running gave no report, whatever ended it.
func Start(s *store.Store, id int64, agent adapters.Adapter) (err error) {
	dataDir := s.DataDir()
	ticket, err := s.Ticket(id)
	if err != nil {
		return err
	}

	worktree, err := Worktree(dataDir, ticket)
	if err != nil {
		return err
	}
	if _, err := s.Claim(id, branch(id, ticket.Title)); err != nil {
		return err
	}
	// From the claim on, this run holds the ticket, and every way out of the
	// function below is a way out with no report.
	defer func() { err = errors.Join(err, s.FailUnfinished(id)) }()

	log, err := openLog(dataDir, id)
	if err != nil {
		return err
	}
	defer log.Close()

	cmd := agent.Launch(adapters.RunSpec{Worktree: worktree, Prompt: prompt(id)})
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = log
	if err := cmd.Start(); err != nil {
		return err
	}

	followErr := follow(s, id, agent, stdout, log)
	// Wait returns only after the process exits, and every line has been
	// read above. The session is recorded before the run's failure is
	// reported: a run that failed is the one a person most wants to open.
	runErr := cmd.Wait()
	// The end time and the exit code go on the row of the run whatever the
	// exit was. ProcessState is there after Wait whether or not Wait gave an
	// error, and its ExitCode is -1 for a process that a signal ended.
	endErr := s.EndRun(id, cmd.ProcessState.ExitCode())
	if followErr != nil {
		return followErr
	}
	if endErr != nil {
		return endErr
	}
	return runErr
}

// follow reads the output of a run as it arrives. Each line goes to the log,
// and the output so far goes to the adapter after each line until it gives
// the session. The session goes on the ticket at once: a run that stops part
// way, from a cancel, the timeout or a restart, has then already left the id
// a person opens it with. Once the adapter has answered, follow asks no more
// and keeps no more, because the log holds the output and nothing else reads
// it.
//
// A fault of the log or of the session does not stop the reading: a pipe
// that nobody reads fills, and the agent would then wait on it for ever.
// follow reads to the end and returns the faults it kept.
func follow(s *store.Store, id int64, agent adapters.Adapter, stdout io.Reader, log io.Writer) error {
	var out bytes.Buffer
	var logErr, sessionErr error
	found := false
	readErr := eachLine(stdout, func(line []byte) {
		if _, err := log.Write(line); err != nil && logErr == nil {
			logErr = err
		}
		if found || sessionErr != nil {
			return
		}
		out.Write(line)
		session, err := agent.SessionID(out.Bytes())
		if err == nil && session != "" {
			err = s.SetSession(id, session)
			found = err == nil
		}
		if found {
			out = bytes.Buffer{}
		}
		sessionErr = err
	})
	return errors.Join(readErr, logErr, sessionErr)
}

// eachLine calls each with every line of r as it arrives, newline included,
// and with the last line whether or not a newline ends it. A line has no
// limit on its length: a tool result in the stream of a real agent runs to
// hundreds of kilobytes, which is past what a scanner takes.
func eachLine(r io.Reader, each func(line []byte)) error {
	lines := bufio.NewReader(r)
	for {
		line, err := lines.ReadBytes('\n')
		if len(line) > 0 {
			each(line)
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
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
