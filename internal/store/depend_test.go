package store

import (
	"errors"
	"slices"
	"testing"

	"github.com/alcubie/delegator/internal/config"
)

// mustAddTicket adds a ticket that waits for the ids of waitsFor, and stops the
// test when the store refuses it. A test that examines a refusal calls
// AddTicket itself.
func mustAddTicket(t *testing.T, s *Store, projectID int64, title string, waitsFor ...int64) int64 {
	t.Helper()
	id, err := s.AddTicket(projectID, title, waitsFor...)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// linksOf returns the id of each ticket that the ticket id waits for, read from
// the table itself, because no command reads the links back yet.
func linksOf(t *testing.T, s *Store, id int64) []int64 {
	t.Helper()
	rows, err := s.db.Query(
		"SELECT depends_on FROM ticket_deps WHERE ticket_id = ? ORDER BY depends_on", id)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var on int64
		if err := rows.Scan(&on); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, on)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return ids
}

// waitingQueue returns a store whose queue holds one ticket that waits for an
// earlier ticket and then one that waits for nothing, with the id of the
// ticket waited for, the id of the waiting ticket and the id of the free one.
//
// The ticket that is waited for is running, so it is out of the queue and the
// waiting ticket is at the top of it. A queue that gave the free ticket the
// first place would claim it whether the links were read or not.
func waitingQueue(t *testing.T) (s *Store, waitedFor, waiting, free int64) {
	t.Helper()
	s, projectID := emptyStore(t)
	waitedFor = mustAddTicket(t, s, projectID, "the work it builds on")
	waiting = mustAddTicket(t, s, projectID, "waits", waitedFor)
	free = mustAddTicket(t, s, projectID, "waits for nothing")
	if err := s.ChangeStatus(waitedFor, Running); err != nil {
		t.Fatal(err)
	}
	return s, waitedFor, waiting, free
}

func TestAddTicketRecordsTheTicketsItWaitsFor(t *testing.T) {
	s, ids := threeTickets(t)
	projectID := mustProject(t, s)

	id := mustAddTicket(t, s, projectID, "waits for two", ids[0], ids[1])

	if got, want := linksOf(t, s, id), []int64{ids[0], ids[1]}; !slices.Equal(got, want) {
		t.Errorf("ticket %d waits for %v, want %v", id, got, want)
	}
	if got := linksOf(t, s, ids[0]); len(got) != 0 {
		t.Errorf("ticket %d waits for %v, want nothing", ids[0], got)
	}
}

// The same id twice is the state the caller asked for, so it is one row and not
// a fault.
func TestAddTicketWithOneIdTwiceMakesOneLink(t *testing.T) {
	s, id := oneTicket(t)
	projectID := mustProject(t, s)

	waiting := mustAddTicket(t, s, projectID, "waits", id, id)

	if got, want := linksOf(t, s, waiting), []int64{id}; !slices.Equal(got, want) {
		t.Errorf("ticket %d waits for %v, want %v", waiting, got, want)
	}
}

// A link to an id that names no ticket could never be satisfied, and the
// mistyped id is the likely cause, so the ticket is refused rather than made.
func TestAddTicketRefusesAnIdThatNamesNoTicket(t *testing.T) {
	s, id := oneTicket(t)
	projectID := mustProject(t, s)

	made, err := s.AddTicket(projectID, "waits for nobody", id+1000)

	if !errors.Is(err, ErrNoTicket) {
		t.Fatalf("err = %v, want ErrNoTicket", err)
	}
	if made != 0 {
		t.Errorf("AddTicket returned id %d, want 0", made)
	}
	if got, want := queueTitles(t, s), []string{"My Ticket"}; !slices.Equal(got, want) {
		t.Errorf("the queue is now %v, want %v: the refused ticket was written", got, want)
	}
}

// The rule the ticket asks for: no run starts on a ticket whose link is not
// satisfied, and a later ticket of the queue that waits for nothing goes first.
func TestClaimNextPassesOverATicketWhoseLinkIsNotDone(t *testing.T) {
	s, _, waiting, free := waitingQueue(t)

	claimed, _, err := s.ClaimNext(config.Config{Runs: 4}, claimBranch)

	if err != nil {
		t.Fatal(err)
	}
	if claimed.ID != free {
		t.Errorf("claimed ticket %d, want %d: ticket %d still waits", claimed.ID, free, waiting)
	}
}

// Done satisfies a link and no earlier status does. Only dg accept gives done,
// so a run starts on work that a person has looked at.
func TestClaimNextStillWaitsWhileTheOtherTicketIsOnlyReady(t *testing.T) {
	s, waitedFor, waiting, free := waitingQueue(t)
	if err := s.ChangeStatus(waitedFor, Ready); err != nil {
		t.Fatal(err)
	}

	claimed, _, err := s.ClaimNext(config.Config{Runs: 4}, claimBranch)

	if err != nil {
		t.Fatal(err)
	}
	if claimed.ID != free {
		t.Errorf("claimed ticket %d, want %d: ticket %d waits until the other is done",
			claimed.ID, free, waiting)
	}
}

func TestClaimNextTakesATicketOnceItsLinksAreDone(t *testing.T) {
	s, waitedFor, waiting, _ := waitingQueue(t)
	for _, status := range []TicketStatus{Ready, Done} {
		if err := s.ChangeStatus(waitedFor, status); err != nil {
			t.Fatal(err)
		}
	}

	claimed, _, err := s.ClaimNext(config.Config{Runs: 4}, claimBranch)

	if err != nil {
		t.Fatal(err)
	}
	if claimed.ID != waiting {
		t.Errorf("claimed ticket %d, want %d now that its link is done", claimed.ID, waiting)
	}
}

// A cancelled ticket is work that was thrown away, so a link to it is never
// satisfied and the ticket that waits for it keeps its place in the queue
// until the person takes the link away.
func TestClaimNextKeepsWaitingForACancelledTicket(t *testing.T) {
	s, waitedFor, _, free := waitingQueue(t)
	if err := s.ChangeStatus(waitedFor, Cancelled); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Claim(free, claimBranch(Ticket{ID: free})); err != nil {
		t.Fatal(err)
	}

	_, _, err := s.ClaimNext(config.Config{Runs: 4}, claimBranch)

	if !errors.Is(err, ErrNoRoom) {
		t.Errorf("err = %v, want ErrNoRoom: the link to a cancelled ticket holds the ticket back", err)
	}
}

// The database of a person who has an earlier version holds no links. The step
// that makes the table must reach that database on the next start, or every
// query that reads the links gives an error on it.
func TestOpenGivesAnOldDatabaseTheLinksTable(t *testing.T) {
	dataDir := t.TempDir()

	openBefore(t, dataDir, addDependenciesTable).Close()

	s, err := Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	for _, name := range []string{"ticket_deps", "ticket_deps_depends_on"} {
		var found string
		if err := s.db.QueryRow(
			"SELECT name FROM sqlite_master WHERE name = ?", name).Scan(&found); err != nil {
			t.Errorf("%s is not on the migrated database: %v", name, err)
		}
	}
}
