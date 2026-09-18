package run

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"

	"github.com/alcubie/delegator/internal/config"
	"github.com/alcubie/delegator/internal/handler"
	"github.com/alcubie/delegator/internal/store"
)

// superviseACP works the claimed ticket through internal/handler: it starts
// the agent's ACP server in the worktree, puts the id of the session on the
// ticket before the turn starts, and writes each event of the turn to the log
// as one line. The agent's own stderr goes to the same log, which is the only
// place an ACP agent has to say what the protocol does not carry.
//
// The agent is the one the key agent of the config names, and a name that no
// agent has ends the run before it starts, with the names there are.
//
// A turn the agent ran to its end is a run that succeeded, with an exit code
// of 0. Any other reason it gives is a failed run, and the reason is in the
// error and in the last line of the log.
//
// The supervisor owns the context. Its timeout cancels the turn as it stops
// the process group, while a run with no limit keeps the context until its
// ordinary end.
func superviseACP(ctx context.Context, s *store.Store, cfg config.Config, id, runID int64, worktree string, log io.Writer) (err error) {
	// The caller claimed the ticket and this run holds it, so the row of the
	// run is ended whatever way the function below is left. The code is the
	// one of a run with no process of its own to give one until a turn ends
	// of itself.
	code := noExitCode
	defer func() { err = errors.Join(err, s.EndRun(runID, code)) }()

	entry, err := s.Agent(cfg.DefaultAgent)
	if err != nil {
		return err
	}
	agentID, err := s.AgentID(entry.Name)
	if err != nil {
		return err
	}
	if err := s.SetRunAgent(runID, agentID); err != nil {
		return err
	}
	session, err := handler.Start(ctx, entry.Name, entry.Argv, handler.AllowAll(), worktree, log)
	if err != nil {
		return err
	}
	defer session.Close()

	// The id goes on the ticket before the prompt. The agent gave it when the
	// session opened, so a run that stops part way, from the timeout or a
	// restart, has already left the id a person opens it with.
	if err := s.SetSession(id, session.ID()); err != nil {
		return err
	}

	last, streamErr := stream(session.Prompt(ctx, prompt(id, s.DataDir())), log)
	usageErr := recordUsage(s, runID, session.Usage())
	if err := errors.Join(streamErr, usageErr); err != nil {
		return err
	}
	if last.Status != handler.StopEndTurn {
		return fmt.Errorf("the agent stopped the turn of ticket %d: %s", id, last.Status)
	}
	code = 0
	return nil
}

// recordUsage writes only a response that carried ACP usage. The session sets
// it when the response ends the prompt stream, so its counts reach the
// database only after the prompt has answered. An agent that omits Usage
// leaves no row. Required fields are pointers here so a reported zero remains
// different from NULL.
func recordUsage(s *store.Store, runID int64, u *handler.Usage) error {
	if u == nil {
		return nil
	}
	return s.AddRunUsage(runID, store.AggregateUsage{
		InputTokens:       &u.InputTokens,
		CachedWriteTokens: u.CachedWriteTokens,
		CachedReadTokens:  u.CachedReadTokens,
		OutputTokens:      &u.OutputTokens,
		ThoughtTokens:     u.ThoughtTokens,
		TotalTokens:       &u.TotalTokens,
	})
}

// stream writes each event of a turn to the log as one line and gives back the
// event that ended the turn.
//
// A log that will not take a line does not end the reading: walking out of the
// sequence stops the turn, and the rest of what the agent did would be lost.
// stream reads to the end and gives the faults it kept.
func stream(events iter.Seq2[handler.Event, error], log io.Writer) (handler.Event, error) {
	var last handler.Event
	var logErr, runErr error
	for e, err := range events {
		if bad := handler.WriteEvent(log, e); bad != nil && logErr == nil {
			logErr = bad
		}
		last, runErr = e, err
	}
	return last, errors.Join(runErr, logErr)
}
