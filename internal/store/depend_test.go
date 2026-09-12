package store

import (
	"errors"
	"slices"
	"testing"

	"github.com/alcubie/delegator/internal/config"
)

// mustAddTicket adds a ticket that depends on the ids of dependsOn, and stops
// the test when the store refuses it. A test that examines a refusal calls
// AddTicket itself.
func mustAddTicket(t *testing.T, s *Store, projectID int64, title string, dependsOn ...int64) int64 {
	t.Helper()
	id, err := s.AddTicket(projectID, title, dependsOn...)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// linksOf returns the id of each ticket that the ticket id depends on, and
// stops the test when the read fails.
func linksOf(t *testing.T, s *Store, id int64) []int64 {
	t.Helper()
	ids, err := s.Dependencies(id)
	if err != nil {
		t.Fatal(err)
	}
	return ids
}

// dependentQueue returns a store whose queue holds one ticket that depends on an
// earlier ticket and then one that depends on nothing, with the id of the ticket
// depended on, the id of the dependent ticket and the id of the free one.
//
// The ticket that is depended on is running, so it is out of the queue and the
// dependent ticket is at the top of it. A queue that gave the free ticket the
// first place would claim it whether the links were read or not.
func dependentQueue(t *testing.T) (s *Store, dependedOn, dependent, free int64) {
	t.Helper()
	s, projectID := emptyStore(t)
	dependedOn = mustAddTicket(t, s, projectID, "the work it builds on")
	dependent = mustAddTicket(t, s, projectID, "depends", dependedOn)
	free = mustAddTicket(t, s, projectID, "depends on nothing")
	if err := s.ChangeStatus(dependedOn, Running); err != nil {
		t.Fatal(err)
	}
	return s, dependedOn, dependent, free
}

func TestAddTicketRecordsTheTicketsItDependsOn(t *testing.T) {
	s, ids := threeTickets(t)
	projectID := mustProject(t, s)

	id := mustAddTicket(t, s, projectID, "depends on two", ids[0], ids[1])

	if got, want := linksOf(t, s, id), []int64{ids[0], ids[1]}; !slices.Equal(got, want) {
		t.Errorf("ticket %d depends on %v, want %v", id, got, want)
	}
	if got := linksOf(t, s, ids[0]); len(got) != 0 {
		t.Errorf("ticket %d depends on %v, want nothing", ids[0], got)
	}
}

// The same id twice is the state the caller asked for, so it is one row and not
// a fault.
func TestAddTicketWithOneIdTwiceMakesOneLink(t *testing.T) {
	s, id := oneTicket(t)
	projectID := mustProject(t, s)

	dependent := mustAddTicket(t, s, projectID, "depends", id, id)

	if got, want := linksOf(t, s, dependent), []int64{id}; !slices.Equal(got, want) {
		t.Errorf("ticket %d depends on %v, want %v", dependent, got, want)
	}
}

// A link to an id that names no ticket could never be satisfied, and the
// mistyped id is the likely cause, so the ticket is refused rather than made.
func TestAddTicketRefusesAnIdThatNamesNoTicket(t *testing.T) {
	s, id := oneTicket(t)
	projectID := mustProject(t, s)

	made, err := s.AddTicket(projectID, "depends on nobody", id+1000)

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
// satisfied, and a later ticket of the queue that depends on nothing goes first.
func TestClaimNextPassesOverATicketWhoseLinkIsNotDone(t *testing.T) {
	s, _, dependent, free := dependentQueue(t)

	claimed, _, err := s.ClaimNext(config.Config{Runs: 4}, claimBranch)

	if err != nil {
		t.Fatal(err)
	}
	if claimed.ID != free {
		t.Errorf("claimed ticket %d, want %d: ticket %d still waits", claimed.ID, free, dependent)
	}
}

// Done satisfies a link and no earlier status does. Only dg accept gives done,
// so a run starts on work that a person has looked at.
func TestClaimNextStillWaitsWhileTheOtherTicketIsOnlyReady(t *testing.T) {
	s, dependedOn, dependent, free := dependentQueue(t)
	if err := s.ChangeStatus(dependedOn, Ready); err != nil {
		t.Fatal(err)
	}

	claimed, _, err := s.ClaimNext(config.Config{Runs: 4}, claimBranch)

	if err != nil {
		t.Fatal(err)
	}
	if claimed.ID != free {
		t.Errorf("claimed ticket %d, want %d: ticket %d waits until the other is done",
			claimed.ID, free, dependent)
	}
}

func TestClaimNextTakesATicketOnceItsLinksAreDone(t *testing.T) {
	s, dependedOn, dependent, _ := dependentQueue(t)
	for _, status := range []TicketStatus{Ready, Done} {
		if err := s.ChangeStatus(dependedOn, status); err != nil {
			t.Fatal(err)
		}
	}

	claimed, _, err := s.ClaimNext(config.Config{Runs: 4}, claimBranch)

	if err != nil {
		t.Fatal(err)
	}
	if claimed.ID != dependent {
		t.Errorf("claimed ticket %d, want %d now that its link is done", claimed.ID, dependent)
	}
}

// A cancelled ticket is work that was thrown away, so a link to it is never
// satisfied and the ticket that depends on it keeps its place in the queue
// until the person takes the link away.
func TestClaimNextKeepsWaitingForACancelledTicket(t *testing.T) {
	s, dependedOn, _, free := dependentQueue(t)
	if err := s.ChangeStatus(dependedOn, Cancelled); err != nil {
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

// inboxDependsOn returns the DependsOn of one ticket of the inbox, and stops the
// test when the inbox does not hold it.
func inboxDependsOn(t *testing.T, s *Store, id int64) []int64 {
	t.Helper()
	open, err := s.OpenTickets()
	if err != nil {
		t.Fatal(err)
	}
	for _, ticket := range open {
		if ticket.ID == id {
			return ticket.DependsOn
		}
	}
	t.Fatalf("the inbox holds no ticket %d", id)
	return nil
}

// A ticket of the inbox names each ticket it depends on, so the row can say
// what holds it back.
func TestOpenTicketsNameTheLinksOfATicket(t *testing.T) {
	s, dependedOn, dependent, free := dependentQueue(t)

	if got, want := inboxDependsOn(t, s, dependent), []int64{dependedOn}; !slices.Equal(got, want) {
		t.Errorf("ticket %d depends on %v, want %v", dependent, got, want)
	}
	if got := inboxDependsOn(t, s, free); len(got) != 0 {
		t.Errorf("ticket %d depends on %v, want nothing", free, got)
	}
}

// A link to a ticket that is done holds nothing back, so the inbox leaves it
// out and the row of a ticket whose links are all done ends at its title. The
// link is still there, and dg show gives it.
func TestOpenTicketsLeaveOutALinkThatIsDone(t *testing.T) {
	s, dependedOn, dependent, _ := dependentQueue(t)
	for _, status := range []TicketStatus{Ready, Done} {
		if err := s.ChangeStatus(dependedOn, status); err != nil {
			t.Fatal(err)
		}
	}

	if got := inboxDependsOn(t, s, dependent); len(got) != 0 {
		t.Errorf("ticket %d depends on %v, want nothing once ticket %d is done",
			dependent, got, dependedOn)
	}
	if got, want := linksOf(t, s, dependent), []int64{dependedOn}; !slices.Equal(got, want) {
		t.Errorf("ticket %d is linked to %v, want %v: done satisfies a link and does not remove it",
			dependent, got, want)
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

// The link the ticket asks for: a ticket that is already in the queue is made
// to depend on another one.
func TestAddDependenciesLinksTwoTicketsThatAreThere(t *testing.T) {
	s, ids := threeTickets(t)

	if err := s.AddDependencies(ids[2], ids[0]); err != nil {
		t.Fatal(err)
	}

	if got, want := linksOf(t, s, ids[2]), []int64{ids[0]}; !slices.Equal(got, want) {
		t.Errorf("ticket %d depends on %v, want %v", ids[2], got, want)
	}
}

// The flag of the command repeats, so the call takes more than one id, and each
// one gets a link.
func TestAddDependenciesLinksEachIdItIsGiven(t *testing.T) {
	s, ids := threeTickets(t)

	if err := s.AddDependencies(ids[2], ids[0], ids[1]); err != nil {
		t.Fatal(err)
	}

	if got, want := linksOf(t, s, ids[2]), []int64{ids[0], ids[1]}; !slices.Equal(got, want) {
		t.Errorf("ticket %d depends on %v, want %v", ids[2], got, want)
	}
}

// A link that is there already is the state the person asked for, so the second
// command says the same thing as the first and leaves one row.
func TestAddDependenciesTwiceMakesOneLink(t *testing.T) {
	s, ids := threeTickets(t)
	if err := s.AddDependencies(ids[2], ids[0]); err != nil {
		t.Fatal(err)
	}

	if err := s.AddDependencies(ids[2], ids[0]); err != nil {
		t.Fatalf("the second call gave %v, want no error: a link that is there is not a fault", err)
	}

	if got, want := linksOf(t, s, ids[2]), []int64{ids[0]}; !slices.Equal(got, want) {
		t.Errorf("ticket %d depends on %v, want %v", ids[2], got, want)
	}
}

// A ticket that depends on itself could never start, and the person meant
// another id.
func TestAddDependenciesRefusesATicketThatDependsOnItself(t *testing.T) {
	s, id := oneTicket(t)

	err := s.AddDependencies(id, id)

	if !errors.Is(err, ErrSelfDependency) {
		t.Fatalf("err = %v, want ErrSelfDependency", err)
	}
	if got := linksOf(t, s, id); len(got) != 0 {
		t.Errorf("ticket %d depends on %v, want nothing", id, got)
	}
}

// No ticket of a ring can ever start, because each one waits for the next, so
// the link that would close the ring is refused rather than written.
func TestAddDependenciesRefusesARingOfTwo(t *testing.T) {
	s, ids := threeTickets(t)
	if err := s.AddDependencies(ids[1], ids[0]); err != nil {
		t.Fatal(err)
	}

	err := s.AddDependencies(ids[0], ids[1])

	if !errors.Is(err, ErrDependencyRing) {
		t.Fatalf("err = %v, want ErrDependencyRing", err)
	}
	if got := linksOf(t, s, ids[0]); len(got) != 0 {
		t.Errorf("ticket %d depends on %v, want nothing", ids[0], got)
	}
}

// The walk goes over every link and not only the first, so a ring that three
// tickets make is refused as a ring of two is.
func TestAddDependenciesRefusesARingOfThree(t *testing.T) {
	s, ids := threeTickets(t)
	if err := s.AddDependencies(ids[1], ids[0]); err != nil {
		t.Fatal(err)
	}
	if err := s.AddDependencies(ids[2], ids[1]); err != nil {
		t.Fatal(err)
	}

	err := s.AddDependencies(ids[0], ids[2])

	if !errors.Is(err, ErrDependencyRing) {
		t.Fatalf("err = %v, want ErrDependencyRing", err)
	}
	if got := linksOf(t, s, ids[0]); len(got) != 0 {
		t.Errorf("ticket %d depends on %v, want nothing", ids[0], got)
	}
}

// One call writes its links under one transaction, so a call that has to refuse
// its second id leaves the first one unwritten too.
func TestAddDependenciesWritesNoLinkWhenItRefusesOne(t *testing.T) {
	s, ids := threeTickets(t)

	err := s.AddDependencies(ids[2], ids[0], ids[2])

	if !errors.Is(err, ErrSelfDependency) {
		t.Fatalf("err = %v, want ErrSelfDependency", err)
	}
	if got := linksOf(t, s, ids[2]); len(got) != 0 {
		t.Errorf("ticket %d depends on %v, want nothing: the refused call wrote a link", ids[2], got)
	}
}

// A link only holds a ticket back in the queue, so a link on a ticket that has
// left it changes nothing and the command says so rather than writing a row
// that means nothing.
func TestAddDependenciesRefusesATicketThatIsNotQueued(t *testing.T) {
	s, ids := threeTickets(t)
	if err := s.ChangeStatus(ids[2], Running); err != nil {
		t.Fatal(err)
	}

	err := s.AddDependencies(ids[2], ids[0])

	if !errors.Is(err, ErrNotQueued) {
		t.Fatalf("err = %v, want ErrNotQueued", err)
	}
	if got := linksOf(t, s, ids[2]); len(got) != 0 {
		t.Errorf("ticket %d depends on %v, want nothing", ids[2], got)
	}
}

// The way out for a ticket that depends on one that was cancelled.
func TestRemoveDependenciesTakesTheLinkAway(t *testing.T) {
	s, ids := threeTickets(t)
	if err := s.AddDependencies(ids[2], ids[0], ids[1]); err != nil {
		t.Fatal(err)
	}

	if err := s.RemoveDependencies(ids[2], ids[0]); err != nil {
		t.Fatal(err)
	}

	if got, want := linksOf(t, s, ids[2]), []int64{ids[1]}; !slices.Equal(got, want) {
		t.Errorf("ticket %d depends on %v, want %v", ids[2], got, want)
	}
}

// A remove that takes nothing away is a person who named the wrong ticket, and
// a command that said nothing would leave them believing the link is gone.
func TestRemoveDependenciesRefusesALinkThatIsNotThere(t *testing.T) {
	s, ids := threeTickets(t)

	err := s.RemoveDependencies(ids[2], ids[0])

	if !errors.Is(err, ErrNoDependency) {
		t.Fatalf("err = %v, want ErrNoDependency", err)
	}
}

// A ticket that has left the queue is refused whichever way the link goes, for
// the reason the add is refused: the link changes nothing now.
func TestRemoveDependenciesRefusesATicketThatIsNotQueued(t *testing.T) {
	s, ids := threeTickets(t)
	if err := s.AddDependencies(ids[2], ids[0]); err != nil {
		t.Fatal(err)
	}
	if err := s.ChangeStatus(ids[2], Running); err != nil {
		t.Fatal(err)
	}

	err := s.RemoveDependencies(ids[2], ids[0])

	if !errors.Is(err, ErrNotQueued) {
		t.Fatalf("err = %v, want ErrNotQueued", err)
	}
	if got, want := linksOf(t, s, ids[2]), []int64{ids[0]}; !slices.Equal(got, want) {
		t.Errorf("ticket %d depends on %v, want %v", ids[2], got, want)
	}
}

// An id that names no ticket is a mistyped id, and the error names it.
func TestAddDependenciesRefusesAnIdThatNamesNoTicket(t *testing.T) {
	s, id := oneTicket(t)

	if err := s.AddDependencies(id, id+1000); !errors.Is(err, ErrNoTicket) {
		t.Errorf("err = %v, want ErrNoTicket", err)
	}
	if err := s.AddDependencies(id+1000, id); !errors.Is(err, ErrNoTicket) {
		t.Errorf("err = %v, want ErrNoTicket", err)
	}
}
