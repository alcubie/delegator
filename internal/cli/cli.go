// Package cli implements dg's Cobra commands and terminal output. cmd/dg
// supplies the environment and executes the tree; commands can be tested here
// with buffered I/O.
package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/alcubie/delegator/internal/config"
	"github.com/alcubie/delegator/internal/inbox"
	"github.com/alcubie/delegator/internal/project"
	"github.com/alcubie/delegator/internal/run"
	"github.com/alcubie/delegator/internal/store"
)

// writeValue routes a command result to JSON or its text renderer, giving all
// commands a shared output path.
func writeValue(out io.Writer, value any, asJSON bool, writeText func(io.Writer)) error {
	// RPC captures the value before rendering, avoiding a round trip
	// through command output.
	if rpc, ok := out.(*rpcValueWriter); ok {
		rpc.value = value
		return nil
	}
	if !asJSON {
		writeText(out)
		return nil
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	// Values from tickets are prose and paths, not HTML for a page.
	enc.SetEscapeHTML(false)
	return enc.Encode(value)
}

// withStore opens the store, reconciles stale runs, and calls fn with the
// same store. cfg points to the settings snapshot loaded by the root hook
// after command construction.
func withStore(dataDir string, cfg *config.Config, fn func(*store.Store) error) error {
	return store.With(dataDir, func(s *store.Store) error {
		if err := run.Reconcile(s, launchFrom(dataDir, launch), *cfg); err != nil {
			return err
		}
		return fn(s)
	})
}

// ticketArg parses a ticket ID with consistent errors across commands.
func ticketArg(arg string) (int64, error) {
	id, err := strconv.ParseInt(arg, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%q is not the id of a ticket", arg)
	}
	return id, nil
}

// ticketNames formats ticket references consistently for the inbox and dg
// show.
func ticketNames(ids []int64) string {
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		names = append(names, "#"+strconv.FormatInt(id, 10))
	}
	return strings.Join(names, " ")
}

// resolveTicketID parses an explicit ID or selects the first ready ticket for
// the current project (overridden by --project). It uses the shared inbox
// ordering so implicit commands select the ticket shown first to the user.
func resolveTicketID(s *store.Store, cfg *config.Config, args []string, workDir, projectFlag string) (int64, error) {
	if len(args) > 0 {
		return ticketArg(args[0])
	}

	dir, err := ticketProject(workDir, projectFlag)
	if err != nil {
		return 0, err
	}
	root, err := project.Root(dir)
	if err != nil {
		return 0, err
	}
	box, err := inbox.Get(s, time.Now().Add(-cfg.DoneWindow()))
	if err != nil {
		return 0, err
	}
	ticket, found := box.FirstReady(root)
	if !found {
		return 0, fmt.Errorf("no ticket of %s is ready", root)
	}
	return ticket.ID, nil
}

// proseFile returns the path of the file that holds the prose of one ticket.
func proseFile(dataDir string, id int64) string {
	return filepath.Join(dataDir, "tickets", strconv.FormatInt(id, 10)+".md")
}

// elapsed formats run time as HH:MM:SS. Hours can exceed two digits, zero
// start time returns empty, and future starts clamp to zero after a clock
// rollback. Fixed-width minutes and seconds keep watched output aligned.
func elapsed(start, now time.Time) string {
	if start.IsZero() {
		return ""
	}
	seconds := int(now.Sub(start).Seconds())
	if seconds < 0 {
		seconds = 0
	}
	return fmt.Sprintf("%02d:%02d:%02d", seconds/3600, seconds/60%60, seconds%60)
}
