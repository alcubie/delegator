package store

import (
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"testing"
	"time"
)

// change represents a history row. Arrival distinguishes SQL NULL in
// from_status from an empty string.
type change struct {
	Arrival bool
	Before  TicketStatus
	After   TicketStatus
	At      time.Time
}

// history returns each change of one ticket, in the order the rows went in.
func history(t *testing.T, s *Store, id int64) []change {
	t.Helper()
	rows, err := s.db.Query(`
		SELECT from_status, to_status, at
		FROM transitions WHERE ticket_id = ? ORDER BY id`, id)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	var changes []change
	for rows.Next() {
		var c change
		var before sql.NullString
		if err := rows.Scan(&before, &c.After, timeColumn{&c.At}); err != nil {
			t.Fatal(err)
		}
		c.Arrival = !before.Valid
		c.Before = TicketStatus(before.String)
		changes = append(changes, c)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return changes
}

// steps formats transitions without timestamps; creation is "new to queued".
func steps(t *testing.T, s *Store, id int64) []string {
	t.Helper()
	var steps []string
	for _, c := range history(t, s, id) {
		before := string(c.Before)
		if c.Arrival {
			before = "new"
		}
		steps = append(steps, fmt.Sprintf("%s to %s", before, c.After))
	}
	return steps
}

func TestAddTicketWritesTheArrivalOfTheTicket(t *testing.T) {
	before := time.Now().UTC().Truncate(time.Second)
	s, id := oneTicket(t)

	got := history(t, s, id)
	if len(got) != 1 {
		t.Fatalf("the history is %v, want the one arrival", got)
	}
	if !got[0].Arrival {
		t.Errorf("the arrival comes from %q, want no status at all", got[0].Before)
	}
	if got[0].After != Queued {
		t.Errorf("the arrival goes to %q, want %q", got[0].After, Queued)
	}
	if got[0].At.Before(before) || got[0].At.After(time.Now()) {
		t.Errorf("the arrival is at %s, want between %s and now", got[0].At, before)
	}

	var at string
	if err := s.db.QueryRow(
		"SELECT at FROM transitions WHERE ticket_id = ?", id).Scan(&at); err != nil {
		t.Fatal(err)
	}
	matched, err := regexp.MatchString(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$`, at)
	if err != nil {
		t.Fatal(err)
	}
	if !matched {
		t.Errorf("the arrival is at %q, want the format of RFC 3339", at)
	}
}

func TestAddTicketWritesTheArrivalOfThatTicketOnly(t *testing.T) {
	s, ids := threeTickets(t)

	for _, id := range ids {
		if got := steps(t, s, id); !slices.Equal(got, []string{"new to queued"}) {
			t.Errorf("the history of %d is %v, want the one arrival", id, got)
		}
	}
}

// Exercise a full history: fail, restart, finish, and accept. Every state
// change must append one entry.
func TestEveryChangeOfStatusWritesOneRow(t *testing.T) {
	s, id := oneTicket(t)

	firstRun, err := s.Claim(id, "delegator/1-my-ticket", testAgentID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.FailUnfinished(firstRun); err != nil {
		t.Fatal(err)
	}
	secondRun, err := s.Restart(id, testAgentID)
	if err != nil {
		t.Fatal(err)
	}
	if firstRun == secondRun {
		t.Fatalf("the claim and restart gave the run %d twice", firstRun)
	}
	if err := s.FinishTicket(id, "abc1234"); err != nil {
		t.Fatal(err)
	}
	if err := s.ChangeStatus(id, Done); err != nil {
		t.Fatal(err)
	}

	want := []string{
		"new to queued",
		"queued to running",
		"running to failed",
		"failed to running",
		"running to ready",
		"ready to done",
	}
	if got := steps(t, s, id); !slices.Equal(got, want) {
		t.Errorf("the history is\n%v\nwant\n%v", got, want)
	}
}

// A failed external callback must roll back transition history along with
// status.
func TestChangeStatusWithWritesTheRowWithTheChange(t *testing.T) {
	s, id := oneTicket(t)
	if _, err := s.Claim(id, "delegator/1-my-ticket", testAgentID); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishTicket(id, "abc1234"); err != nil {
		t.Fatal(err)
	}

	failed := errors.New("the worktree is still there")
	if err := s.ChangeStatusWith(id, Done, func() error { return failed }); !errors.Is(err, failed) {
		t.Fatalf("err = %v, want %v", err, failed)
	}
	want := []string{"new to queued", "queued to running", "running to ready"}
	if got := steps(t, s, id); !slices.Equal(got, want) {
		t.Errorf("the work gave an error and the history is\n%v\nwant\n%v", got, want)
	}

	if err := s.ChangeStatusWith(id, Done, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	want = append(want, "ready to done")
	if got := steps(t, s, id); !slices.Equal(got, want) {
		t.Errorf("the history is\n%v\nwant\n%v", got, want)
	}
}

func TestAChangeThatIsRefusedWritesNoRow(t *testing.T) {
	s, id := oneTicket(t)

	// queued goes to running or cancelled, and not to done
	if err := s.ChangeStatus(id, Done); !errors.Is(err, ErrInvalidTicketStateChange) {
		t.Fatalf("err = %v, want ErrInvalidTicketStateChange", err)
	}

	if got := steps(t, s, id); !slices.Equal(got, []string{"new to queued"}) {
		t.Errorf("the history is %v, want the arrival alone", got)
	}
}

func TestReconcileWritesTheChangeOfTheTicketItMarks(t *testing.T) {
	s, id := oneTicket(t)
	if _, err := s.Claim(id, "delegator/1-my-ticket", testAgentID); err != nil {
		t.Fatal(err)
	}

	if _, err := s.Reconcile(noneRunning); err != nil {
		t.Fatal(err)
	}

	want := []string{"new to queued", "queued to running", "running to failed"}
	if got := steps(t, s, id); !slices.Equal(got, want) {
		t.Errorf("the history is\n%v\nwant\n%v", got, want)
	}
}

// A claim's transition and run start must use the same timestamp.
func TestClaimWritesOneTimeForTheRunAndTheChange(t *testing.T) {
	s, id := oneTicket(t)
	if _, err := s.Claim(id, "delegator/1-my-ticket", testAgentID); err != nil {
		t.Fatal(err)
	}

	got := history(t, s, id)
	if len(got) != 2 {
		t.Fatalf("the history is %v, want the arrival and the claim", got)
	}
	run, err := s.Run(id)
	if err != nil {
		t.Fatal(err)
	}
	if !got[1].At.Equal(run.StartedAt) {
		t.Errorf("the ticket entered running at %s, and the run began at %s",
			got[1].At, run.StartedAt)
	}
}

// Recovery must give the run end and status transition the same timestamp.
func TestTheEndOfARunAndTheChangeOfStateHoldOneTime(t *testing.T) {
	for _, test := range []struct {
		name string
		end  func(s *Store, id, runID int64) error
	}{
		{"cancel", func(s *Store, id, runID int64) error { return s.Cancel(id, runID) }},
		{"fail", func(s *Store, _, runID int64) error { return s.FailUnfinished(runID) }},
		{"reconcile", func(s *Store, _, _ int64) error {
			_, err := s.Reconcile(noneRunning)
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, id := oneTicket(t)
			runID, err := s.Claim(id, "delegator/1-my-ticket", testAgentID)
			if err != nil {
				t.Fatal(err)
			}

			if err := test.end(s, id, runID); err != nil {
				t.Fatal(err)
			}

			got := history(t, s, id)
			run, err := s.Run(id)
			if err != nil {
				t.Fatal(err)
			}
			last := got[len(got)-1]
			if !last.At.Equal(run.EndedAt) {
				t.Errorf("the ticket went to %s at %s, and the run ended at %s",
					last.After, last.At, run.EndedAt)
			}
		})
	}
}

// backdate separates existing history from new changes despite second-
// precision timestamps.
func backdate(t *testing.T, s *Store, id int64, at string) {
	t.Helper()
	if _, err := s.db.Exec(
		"UPDATE transitions SET at = ? WHERE ticket_id = ?", at, id); err != nil {
		t.Fatal(err)
	}
}

// Cancelled tickets must show the cancellation time, not the previous
// completion.
func TestTicketGivesTheTimeOfTheLastChange(t *testing.T) {
	s, id := oneTicket(t)
	if _, err := s.Claim(id, "delegator/1-my-ticket", testAgentID); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishTicket(id, "abc1234"); err != nil {
		t.Fatal(err)
	}
	stopped := time.Date(2026, 8, 28, 9, 0, 0, 0, time.UTC)
	backdate(t, s, id, rfc3339(stopped))

	revised := time.Now().UTC().Truncate(time.Second)
	if err := s.Cancel(id, 0); err != nil {
		t.Fatal(err)
	}

	got, err := s.Ticket(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Changed.Before(revised) || got.Changed.After(time.Now()) {
		t.Errorf("the ticket changed at %s, want between %s and now", got.Changed, revised)
	}
	if got.Changed.Equal(stopped) {
		t.Error("the ticket holds the time that its earlier run stopped")
	}
}

// DONE uses acceptance time even when completion was days earlier.
func TestTheTimeOfADoneTicketIsTheAcceptance(t *testing.T) {
	s, id := oneTicket(t)
	if _, err := s.Claim(id, "delegator/1-my-ticket", testAgentID); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishTicket(id, "abc1234"); err != nil {
		t.Fatal(err)
	}
	// the run stopped long before the person read it
	backdate(t, s, id, "2026-08-28T09:00:00Z")

	accepted := time.Now().UTC().Truncate(time.Second)
	if err := s.ChangeStatus(id, Done); err != nil {
		t.Fatal(err)
	}

	done, err := s.DoneTickets(time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(done) != 1 {
		t.Fatalf("DoneTickets gives %d tickets, want 1", len(done))
	}
	if got := done[0].Accepted; got.Before(accepted) || got.After(time.Now()) {
		t.Errorf("the ticket was accepted at %s, want between %s and now", got, accepted)
	}
}

// Drop duplicated event columns: transitions now own creation and completion
// timestamps.
func TestTheTableOfTicketsHoldsNoTimeOfOneChange(t *testing.T) {
	s, _ := oneTicket(t)

	for _, column := range []string{"completed", "created"} {
		var count int
		if err := s.db.QueryRow(
			"SELECT COUNT(*) FROM pragma_table_info('tickets') WHERE name = ?", column,
		).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Errorf("the table tickets still holds the column %s", column)
		}
	}
}

// Creation time must remain stable across later transitions.
func TestTicketGivesTheTimeItArrived(t *testing.T) {
	s, id := oneTicket(t)
	if _, err := s.Claim(id, "delegator/1-my-ticket", testAgentID); err != nil {
		t.Fatal(err)
	}
	// Backdate only creation to distinguish it from every later entry.
	arrived := time.Date(2026, 8, 28, 9, 0, 0, 0, time.UTC)
	if _, err := s.db.Exec(`UPDATE transitions SET at = ?
		WHERE id = (SELECT MIN(id) FROM transitions WHERE ticket_id = ?)`,
		rfc3339(arrived), id); err != nil {
		t.Fatal(err)
	}

	got, err := s.Ticket(id)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Created.Equal(arrived) {
		t.Errorf("the ticket arrived at %s, want %s", got.Created, arrived)
	}
}

func TestTicketGivesTheTimeOfTheAcceptance(t *testing.T) {
	s, id := oneTicket(t)
	if _, err := s.Claim(id, "delegator/1-my-ticket", testAgentID); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishTicket(id, "abc1234"); err != nil {
		t.Fatal(err)
	}

	ready, err := s.Ticket(id)
	if err != nil {
		t.Fatal(err)
	}
	if !ready.Accepted.IsZero() {
		t.Errorf("a ready ticket was accepted at %s, want no time at all", ready.Accepted)
	}

	accepted := time.Now().UTC().Truncate(time.Second)
	if err := s.ChangeStatus(id, Done); err != nil {
		t.Fatal(err)
	}

	got, err := s.Ticket(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Accepted.Before(accepted) || got.Accepted.After(time.Now()) {
		t.Errorf("the ticket was accepted at %s, want between %s and now", got.Accepted, accepted)
	}
}
