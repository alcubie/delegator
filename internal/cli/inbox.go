// Terminal inbox rendering. internal/inbox owns grouping and ordering; this
// file renders headings and notes, with row layout in row.go.

package cli

import (
	"fmt"
	"io"
	"time"

	"github.com/alcubie/delegator/internal/config"
	"github.com/alcubie/delegator/internal/inbox"
	"github.com/alcubie/delegator/internal/project"
	"github.com/alcubie/delegator/internal/store"
)

// emptyGroup is the line below the heading of a group that holds no ticket.
const emptyGroup = "  none"

// emptyInbox replaces empty group headings with guidance for creating a first
// ticket.
const emptyInbox = "There are no active tickets. Use `dg ticket create` to add."

// Always show queue state so users can distinguish paused work from an idle
// queue.
const (
	statusRunning = "Status: Running"
	statusPaused  = "Status: Paused"
)

// statusLine renders queue state with color controlled by mode and the output
// terminal.
func statusLine(out io.Writer, box inbox.Inbox, mode colourMode) string {
	if box.QueueRunning {
		return colour(mode.on(out), green, statusRunning)
	}
	return colour(mode.on(out), yellow, statusPaused)
}

// group is one heading of the inbox and the tickets below it.
type group struct {
	heading string
	tickets []store.OpenTicket
}

// doneHeading labels DONE with its configured acceptance window in hours.
func doneHeading(window time.Duration) string {
	return fmt.Sprintf("DONE (last %dh)", int(window.Hours()))
}

// groups returns inbox sections in a fixed order, with recently accepted work
// first. Empty groups keep their headings except FAILED, which appears only
// when action is needed. done supplies the DONE display window.
func groups(box inbox.Inbox, done time.Duration) []group {
	gs := []group{
		{doneHeading(done), box.Done},
		{"READY", box.Ready},
		{"RUNNING", box.Running},
	}
	if len(box.Failed) > 0 {
		gs = append(gs, group{"FAILED", box.Failed})
	}
	return append(gs, group{"QUEUED", box.Queued})
}

// widths calculates shared ID and project column widths so all groups align.
func widths(gs []group) (id, project int) {
	id = minIDWidth
	for _, g := range gs {
		gid, gproject := ticketWidths(g.tickets)
		id, project = max(id, gid), max(project, gproject)
	}
	return id, project
}

// empty reports whether every group is empty, including DONE.
func empty(gs []group) bool {
	for _, g := range gs {
		if len(g.tickets) > 0 {
			return false
		}
	}
	return true
}

// conflictNote marks a ready ticket whose change no longer merges into the
// default branch of its project. dg ticket show names the branch.
const conflictNote = "conflict"

// mergeAnswers holds the merge answer of each ready ticket, keyed by ticket.
// A ticket without an entry has no answer: it is not ready, it holds no
// commit, or Git failed to answer for it.
type mergeAnswers map[int64]*bool

// note returns the marker of one row, which is empty for every ticket but one
// whose change conflicts.
func (a mergeAnswers) note(id int64) string {
	if clean := a[id]; clean != nil && !*clean {
		return conflictNote
	}
	return ""
}

// mergeAnswersFor asks Git whether the commit of each ready ticket still
// merges into the default branch of its project. The default branch moves
// while a ticket waits for review, and this marker is the only sign of the
// conflict when no automatic run started for it. ReadyMerges already leaves
// out a ticket without a commit and a ticket of an archived project, so a
// ready ticket of one reports no answer rather than costing a Git process;
// the automatic check behind dg ticket conflicting already leaves those
// projects alone. A Git failure leaves that ticket without an answer rather
// than refusing the whole inbox.
func mergeAnswersFor(s *store.Store) (mergeAnswers, error) {
	ready, err := s.ReadyMerges()
	if err != nil {
		return nil, err
	}

	answers := make(mergeAnswers)
	for _, p := range ready {
		for _, t := range p.Tickets {
			clean, err := project.MergesCleanly(p.Project.Path, t.Commit, p.Project.DefaultBranch)
			if err != nil {
				continue
			}
			answers[t.ID] = &clean
		}
	}
	return answers, nil
}

// rowNote shows elapsed time for running tickets, blockers for queued
// tickets, and a conflict with the default branch for ready ones. Other rows
// have no note; ready tickets must not keep counting their completed runs.
func rowNote(t store.OpenTicket, now time.Time, merges mergeAnswers) string {
	switch t.Status {
	case store.Running:
		return elapsed(t.Started, now)
	case store.Queued:
		return dependsOnText(t.DependsOn)
	case store.Ready:
		return merges.note(t.ID)
	}
	return ""
}

// dependsOnText formats blocker IDs, or returns empty when there are none.
// The QUEUED heading supplies the explanation.
func dependsOnText(ids []int64) string {
	if len(ids) == 0 {
		return ""
	}
	return ticketNames(ids)
}

// writeInbox renders using the supplied time for repeatable elapsed values.
// done supplies the acceptance window shown in the heading, and merges the
// answer that marks a ready ticket in conflict.
func writeInbox(out io.Writer, box inbox.Inbox, mode colourMode, now time.Time, done time.Duration, merges mergeAnswers) {
	gs := groups(box, done)
	fmt.Fprintln(out, statusLine(out, box, mode))
	if empty(gs) {
		fmt.Fprintln(out, emptyInbox)
		return
	}

	idWidth, projectWidth := widths(gs)
	rowWidth := outputWidth(out)
	for _, g := range gs {
		fmt.Fprintln(out, g.heading)
		if len(g.tickets) == 0 {
			fmt.Fprintln(out, emptyGroup)
			continue
		}
		for _, t := range g.tickets {
			fmt.Fprintln(out, ticketRow(t, idWidth, projectWidth, rowWidth, rowNote(t, now, merges)))
		}
	}
}

// showInbox builds the inbox using the configured DONE window.
func showInbox(dataDir string, cfg *config.Config) (inboxJSON, error) {
	done := cfg.DoneWindow()
	var value inboxJSON
	err := withStore(dataDir, cfg, func(s *store.Store) error {

		now := time.Now()
		box, err := inbox.Get(s, now.Add(-done))
		if err != nil {
			return err
		}
		merges, err := mergeAnswersFor(s)
		if err != nil {
			return err
		}
		value = inboxValue(box, now, done, merges)
		return nil
	})
	return value, err
}
