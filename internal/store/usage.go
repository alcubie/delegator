package store

import (
	"database/sql"
	"errors"
	"fmt"
)

// AggregateUsage is the token usage an agent reported for the prompts of one
// run. Every category is a pointer so the database's NULL remains distinct
// from a reported zero. ACP currently requires input, output and total when a
// response has usage, but those stay nullable for runs made by older or
// unsupported agents and for compatibility with future protocol shapes.
type AggregateUsage struct {
	InputTokens       *int
	CachedWriteTokens *int
	CachedReadTokens  *int
	OutputTokens      *int
	ThoughtTokens     *int
	TotalTokens       *int
}

// AddRunUsage adds the usage of one completed prompt to the aggregate of its
// run. An optional category remains complete only when every contributing
// prompt reported it: once either side is NULL, the aggregate stays NULL.
// Total is the sum of the totals the agent reported, not a value recomputed
// from the categories. Thought remains separate because output includes it.
func (s *Store) AddRunUsage(runID int64, u AggregateUsage) error {
	result, err := s.db.Exec(`
		INSERT INTO run_usage
		  (run_id, input_tokens, cached_write_tokens, cached_read_tokens,
		   output_tokens, thought_tokens, total_tokens)
		SELECT id, ?, ?, ?, ?, ?, ? FROM runs WHERE id = ?
		ON CONFLICT(run_id) DO UPDATE SET
		  input_tokens = CASE
		    WHEN run_usage.input_tokens IS NULL OR excluded.input_tokens IS NULL THEN NULL
		    ELSE run_usage.input_tokens + excluded.input_tokens END,
		  cached_write_tokens = CASE
		    WHEN run_usage.cached_write_tokens IS NULL OR excluded.cached_write_tokens IS NULL THEN NULL
		    ELSE run_usage.cached_write_tokens + excluded.cached_write_tokens END,
		  cached_read_tokens = CASE
		    WHEN run_usage.cached_read_tokens IS NULL OR excluded.cached_read_tokens IS NULL THEN NULL
		    ELSE run_usage.cached_read_tokens + excluded.cached_read_tokens END,
		  output_tokens = CASE
		    WHEN run_usage.output_tokens IS NULL OR excluded.output_tokens IS NULL THEN NULL
		    ELSE run_usage.output_tokens + excluded.output_tokens END,
		  thought_tokens = CASE
		    WHEN run_usage.thought_tokens IS NULL OR excluded.thought_tokens IS NULL THEN NULL
		    ELSE run_usage.thought_tokens + excluded.thought_tokens END,
		  total_tokens = CASE
		    WHEN run_usage.total_tokens IS NULL OR excluded.total_tokens IS NULL THEN NULL
		    ELSE run_usage.total_tokens + excluded.total_tokens END`,
		u.InputTokens, u.CachedWriteTokens, u.CachedReadTokens,
		u.OutputTokens, u.ThoughtTokens, u.TotalTokens, runID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("%w: run %d", ErrNoRun, runID)
	}
	return nil
}

// RunUsage gives the aggregate usage recorded for a run. The boolean is false
// when its agent returned no Usage object, which is not an error and is
// distinct from a row whose values are reported zeroes.
func (s *Store) RunUsage(runID int64) (AggregateUsage, bool, error) {
	var u AggregateUsage
	err := s.db.QueryRow(`
		SELECT input_tokens, cached_write_tokens, cached_read_tokens,
		       output_tokens, thought_tokens, total_tokens
		FROM run_usage WHERE run_id = ?`, runID).Scan(
		&u.InputTokens, &u.CachedWriteTokens, &u.CachedReadTokens,
		&u.OutputTokens, &u.ThoughtTokens, &u.TotalTokens)
	if errors.Is(err, sql.ErrNoRows) {
		return AggregateUsage{}, false, nil
	}
	if err != nil {
		return AggregateUsage{}, false, err
	}
	return u, true, nil
}
