package run

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"

	"github.com/alcubie/delegator/internal/handler"
	"github.com/alcubie/delegator/internal/store"
)

// superviseACP runs the configured ACP agent in the ticket worktree. It saves
// the session ID before prompting and writes events and agent stderr to the
// run log.
//
// A completed turn records exit code zero; other outcomes record failure
// details. The supervisor context cancels the turn on timeout, alongside
// process-group termination.
func superviseACP(ctx context.Context, s *store.Store, agent store.Agent, ticket store.Ticket, runID int64, worktree, cacheDir, model string, log io.Writer) (err error) {
	// Always close the claimed run. Use noExitCode unless the turn
	// completes normally.
	code := noExitCode
	defer func() { err = errors.Join(err, s.EndRun(runID, code)) }()
	id := ticket.ID

	options := sessionOptions(cacheDir)
	options.Model = model
	var session *handler.Session
	if ticket.Session == "" {
		session, err = handler.Start(ctx, agent.Name, agent.Argv, handler.AllowAll(), worktree, options, log)
	} else {
		session, err = handler.Load(ctx, agent.Name, agent.Argv, handler.AllowAll(), worktree, ticket.Session, options, log)
	}
	if err != nil {
		return err
	}
	defer session.Close()

	// Save only newly created sessions. A failed load or prompt must leave a
	// restarted ticket's existing session available for another attempt.
	if ticket.Session == "" {
		if err := s.SetSession(id, session.ID()); err != nil {
			return err
		}
	}

	last, streamErr := stream(session.Prompt(ctx, prompt(id, s.DataDir(), cacheDir)), log)
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

// sessionOptions gives an agent access to only this project's cache and names
// it without choosing a cache convention for any particular ecosystem.
func sessionOptions(cacheDir string) handler.SessionOptions {
	return handler.SessionOptions{
		AdditionalDirectories: []string{cacheDir},
		Environment:           []string{ProjectCacheEnvironment + "=" + cacheDir},
	}
}

// recordUsage persists usage after a prompt response. Omitted Usage leaves no
// row; pointers distinguish reported zeroes from NULL.
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

// stream logs each event and returns the final event. It keeps consuming
// after log errors so a failed write does not cancel the turn or discard
// later events; errors are returned after the stream ends.
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
