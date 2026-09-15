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

// change is one row of the history of a ticket. Arrival is a row that holds no
// status before, which is the arrival of the ticket, and Before is empty there.
// The two are apart because a column that holds NULL and a column that holds the
// empty string are not the same row, and only the first one is an arrival.
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

// steps returns the history of one ticket as one string for each change, which
// is the form a test compares when it is about what happened and not about when.
// The arrival of a ticket is "new to queued".
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

// The first row of a ticket is its arrival. It holds no status before, because
// the ticket was in no state until the person wrote it.
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

	// The time goes in as the text of one format, which is the format that a
	// read parses back.
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

// Each ticket has its own history, and the arrival of one is not a row of
// another.
func TestAddTicketWritesTheArrivalOfThatTicketOnly(t *testing.T) {
	s, ids := threeTickets(t)

	for _, id := range ids {
		if got := steps(t, s, id); !slices.Equal(got, []string{"new to queued"}) {
			t.Errorf("the history of %d is %v, want the one arrival", id, got)
		}
	}
}

// Every way to change a status writes one row, so the history of a ticket holds
// each state it was in and the order it was in them. This ticket takes the whole
// path: a run, a report, dg revise, a second run that failed, dg restart, and a
// cancel.
func TestEveryChangeOfStatusWritesOneRow(t *testing.T) {
	s, id := oneTicket(t)

	firstRun, err := s.Claim(id, "delegator/1-my-ticket")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.FinishTicket(id, "abc1234"); err != nil {
		t.Fatal(err)
	}
	if err := s.ChangeStatus(id, Queued); err != nil {
		t.Fatal(err)
	}
	secondRun, err := s.Claim(id, "delegator/1-my-ticket")
	if err != nil {
		t.Fatal(err)
	}
	if firstRun == secondRun {
		t.Fatalf("the two claims gave the run %d twice", firstRun)
	}
	if err := s.FailUnfinished(secondRun); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Restart(id); err != nil {
		t.Fatal(err)
	}
	if err := s.Cancel(id, 0); err != nil {
		t.Fatal(err)
	}

	want := []string{
		"new to queued",
		"queued to running",
		"running to ready",
		"ready to queued",
		"queued to running",
		"running to failed",
		"failed to running",
		"running to cancelled",
	}
	if got := steps(t, s, id); !slices.Equal(got, want) {
		t.Errorf("the history is\n%v\nwant\n%v", got, want)
	}
}

// dg accept closes a ticket, and the work it does outside the database is
// inside the transaction of the change. The row of the change is inside it as
// well, so work that gives an error leaves the history as it was.
func TestChangeStatusWithWritesTheRowWithTheChange(t *testing.T) {
	s, id := oneTicket(t)
	if _, err := s.Claim(id, "delegator/1-my-ticket"); err != nil {
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

// A change that nextStates does not hold writes nothing at all, so the history
// holds no change that the ticket did not make.
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

// The reconcile marks a ticket whose supervisor is gone, and that is a change of
// state like each other one.
func TestReconcileWritesTheChangeOfTheTicketItMarks(t *testing.T) {
	s, id := oneTicket(t)
	if _, err := s.Claim(id, "delegator/1-my-ticket"); err != nil {
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

// The claim of a ticket is the start of its run, and the two records must not
// disagree: the run keeps its own start time, and the claim writes one time into
// both.
func TestClaimWritesOneTimeForTheRunAndTheChange(t *testing.T) {
	s, id := oneTicket(t)
	if _, err := s.Claim(id, "delegator/1-my-ticket"); err != nil {
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

// A run that a signal or a crash ended has no end time of its own, and the
// command that closes it writes one. That time and the time of the change of
// state are the same moment, so both records hold it.
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
			runID, err := s.Claim(id, "delegator/1-my-ticket")
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

// The database of a person who upgrades holds two times: the arrival of each
// ticket and the time that a ticket became ready. The step that makes the table
// writes those, and invents no time for a change that the database never held.
func TestOpenGivesAnOldTicketTheHistoryTheDatabaseHolds(t *testing.T) {
	dataDir := t.TempDir()

	before := openBefore(t, dataDir, addTransitionsTable)
	projectID, err := before.AddProject("/projects/path", "main")
	if err != nil {
		t.Fatal(err)
	}
	// The tickets go in by hand, because this version of the store writes the
	// history that the old database does not have yet.
	for _, ticket := range []struct {
		title     string
		status    TicketStatus
		position  any
		completed any
	}{
		{"ready", Ready, nil, "2026-08-28T15:00:00Z"},
		{"done", Done, nil, "2026-08-28T12:00:00Z"},
		{"revised", Queued, 1, "2026-08-28T09:00:00Z"},
		{"never ready", Failed, nil, nil},
	} {
		if _, err := before.db.Exec(`
			INSERT INTO tickets (project_id, title, status, position, created, completed)
			VALUES (?, ?, ?, ?, '2026-08-28T08:00:00Z', ?)`,
			projectID, ticket.title, ticket.status, ticket.position,
			ticket.completed); err != nil {
			t.Fatal(err)
		}
	}
	before.Close()

	s, err := Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	ids := map[string]int64{}
	rows, err := s.db.Query("SELECT title, id FROM tickets")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var title string
		var id int64
		if err := rows.Scan(&title, &id); err != nil {
			t.Fatal(err)
		}
		ids[title] = id
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	// A ticket in ready or in done is still where its time of completion put
	// it, so the row is the last change of that ticket. A ticket that left
	// ready has a later change that the database has no time for, and a row for
	// the earlier one would read as its last change.
	for _, test := range []struct {
		title string
		want  []string
	}{
		{"ready", []string{"new to queued", "running to ready"}},
		{"done", []string{"new to queued", "running to ready"}},
		{"revised", []string{"new to queued"}},
		{"never ready", []string{"new to queued"}},
	} {
		if got := steps(t, s, ids[test.title]); !slices.Equal(got, test.want) {
			t.Errorf("the history of the %s ticket is\n%v\nwant\n%v", test.title, got, test.want)
		}
	}

	arrived := time.Date(2026, 8, 28, 8, 0, 0, 0, time.UTC)
	got := history(t, s, ids["ready"])
	if !got[0].At.Equal(arrived) {
		t.Errorf("the arrival is at %s, want %s, the time the ticket was created", got[0].At, arrived)
	}
	if want := time.Date(2026, 8, 28, 15, 0, 0, 0, time.UTC); !got[1].At.Equal(want) {
		t.Errorf("the ticket became ready at %s, want %s", got[1].At, want)
	}
}

// backdate moves each change a ticket has so far to one earlier time, so a
// change that a test makes after it is a different second. Every time the store
// writes holds a whole second, and two changes in one test are otherwise the
// same second.
func backdate(t *testing.T, s *Store, id int64, at string) {
	t.Helper()
	if _, err := s.db.Exec(
		"UPDATE transitions SET at = ? WHERE ticket_id = ?", at, id); err != nil {
		t.Fatal(err)
	}
}

// A ticket holds the time that it entered the status it has. A ticket that
// dg revise put back in the queue entered the queue at that moment, and the run
// that is complete stopped before it.
func TestTicketGivesTheTimeOfTheLastChange(t *testing.T) {
	s, id := oneTicket(t)
	if _, err := s.Claim(id, "delegator/1-my-ticket"); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishTicket(id, "abc1234"); err != nil {
		t.Fatal(err)
	}
	stopped := time.Date(2026, 8, 28, 9, 0, 0, 0, time.UTC)
	backdate(t, s, id, rfc3339(stopped))

	revised := time.Now().UTC().Truncate(time.Second)
	if err := s.ChangeStatus(id, Queued); err != nil {
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

// The time of a done ticket is the time the person accepted it, and not the
// time the run that finished stopped. A ticket can sit in ready for days before
// the person reads it, and the inbox holds it for the window from the moment
// they did.
func TestTheTimeOfADoneTicketIsTheAcceptance(t *testing.T) {
	s, id := oneTicket(t)
	if _, err := s.Claim(id, "delegator/1-my-ticket"); err != nil {
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

// No column of a ticket holds a time that the history holds. The column
// completed went out of date the moment the ticket changed again, and the
// column created was the arrival written twice: once there and once in the
// first row of the history.
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

// A ticket holds the time that it arrived, which is the first row of its
// history and does not move when the ticket changes state.
func TestTicketGivesTheTimeItArrived(t *testing.T) {
	s, id := oneTicket(t)
	if _, err := s.Claim(id, "delegator/1-my-ticket"); err != nil {
		t.Fatal(err)
	}
	// The arrival alone goes back, so the later rows of the history hold a
	// different time and no other row can answer for it.
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

// The acceptance is the change into done, and a ticket that nobody has
// accepted holds no time for it.
func TestTicketGivesTheTimeOfTheAcceptance(t *testing.T) {
	s, id := oneTicket(t)
	if _, err := s.Claim(id, "delegator/1-my-ticket"); err != nil {
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
