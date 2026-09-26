package store

import (
	"errors"
	"slices"
	"testing"

	"github.com/alcubie/delegator/internal/config"
)

// mustAddTicket creates a ticket with dependencies and fails on error.
// Refusal tests call AddTicket directly.
func mustAddTicket(t *testing.T, s *Store, projectID int64, title string, dependsOn ...int64) int64 {
	t.Helper()
	id, err := s.AddTicket(projectID, title, dependsOn...)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// linksOf returns prerequisite IDs, failing on query errors.
func linksOf(t *testing.T, s *Store, id int64) []int64 {
	t.Helper()
	ids, err := s.Dependencies(id)
	if err != nil {
		t.Fatal(err)
	}
	return ids
}

// blockersOf returns dependent IDs, failing on query errors.
func blockersOf(t *testing.T, s *Store, id int64) []int64 {
	t.Helper()
	ids, err := s.Dependents(id)
	if err != nil {
		t.Fatal(err)
	}
	return ids
}

// dependentQueue creates a running prerequisite, its queued dependent, and an
// independent queued ticket, returning their IDs in that order. The blocked
// ticket leads the queue so selecting the independent ticket proves
// dependencies were checked.
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

// Reverse lookup returns sorted dependents, or none when there are no links.
func TestDependentsNamesTheTicketsThatWaitOnOne(t *testing.T) {
	s, ids := threeTickets(t)

	if err := s.AddDependencies(ids[1], ids[0]); err != nil {
		t.Fatal(err)
	}
	if err := s.AddDependencies(ids[2], ids[0]); err != nil {
		t.Fatal(err)
	}

	if got, want := blockersOf(t, s, ids[0]), []int64{ids[1], ids[2]}; !slices.Equal(got, want) {
		t.Errorf("tickets waiting on %d = %v, want %v", ids[0], got, want)
	}
	if got := blockersOf(t, s, ids[1]); len(got) != 0 {
		t.Errorf("tickets waiting on %d = %v, want nothing", ids[1], got)
	}
	if got := blockersOf(t, s, ids[2]+1000); len(got) != 0 {
		t.Errorf("tickets waiting on untouched id = %v, want nothing", got)
	}
}

func TestAddTicketWithOneIdTwiceMakesOneLink(t *testing.T) {
	s, id := oneTicket(t)
	projectID := mustProject(t, s)

	dependent := mustAddTicket(t, s, projectID, "depends", id, id)

	if got, want := linksOf(t, s, dependent), []int64{id}; !slices.Equal(got, want) {
		t.Errorf("ticket %d depends on %v, want %v", dependent, got, want)
	}
}

// Missing prerequisites must reject creation rather than leave permanently
// blocked work.
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

// Skip a blocked queue head to run independent work behind it.
func TestClaimNextPassesOverATicketWhoseLinkIsNotDone(t *testing.T) {
	s, _, dependent, free := dependentQueue(t)

	claimed, _, err := s.ClaimNext(config.Config{Runs: 4}, claimBranch, testAgentID)

	if err != nil {
		t.Fatal(err)
	}
	if claimed.ID != free {
		t.Errorf("claimed ticket %d, want %d: ticket %d still waits", claimed.ID, free, dependent)
	}
}

// Only accepted work satisfies a dependency.
func TestClaimNextStillWaitsWhileTheOtherTicketIsOnlyReady(t *testing.T) {
	s, dependedOn, dependent, free := dependentQueue(t)
	if err := s.ChangeStatus(dependedOn, Ready); err != nil {
		t.Fatal(err)
	}

	claimed, _, err := s.ClaimNext(config.Config{Runs: 4}, claimBranch, testAgentID)

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

	claimed, _, err := s.ClaimNext(config.Config{Runs: 4}, claimBranch, testAgentID)

	if err != nil {
		t.Fatal(err)
	}
	if claimed.ID != dependent {
		t.Errorf("claimed ticket %d, want %d now that its link is done", claimed.ID, dependent)
	}
}

// A dependency crosses project and repository boundaries. Put the dependent
// ticket first in the queue so ClaimNext can only pass it over by reading the
// link, then leave a global slot free while the other ticket runs and waits in
// READY. Done, and no earlier state, releases the dependent ticket.
func TestClaimNextHonorsADependencyFromAnotherProject(t *testing.T) {
	s, projectX := emptyStore(t)
	projectY, err := s.AddProject("/projects/y", "main")
	if err != nil {
		t.Fatal(err)
	}
	b := mustAddTicket(t, s, projectY, "ticket B")
	a := mustAddTicket(t, s, projectX, "ticket A")
	if err := s.AddDependencies(b, a); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Runs: 2}

	claimed, _, err := s.ClaimNext(cfg, claimBranch, testAgentID)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.ID != a {
		t.Errorf("claimed ticket %d, want ticket A %d after passing over earlier ticket B %d",
			claimed.ID, a, b)
	}

	for _, status := range []TicketStatus{Running, Ready} {
		if status == Ready {
			if err := s.ChangeStatus(a, Ready); err != nil {
				t.Fatal(err)
			}
		}
		claimed, _, err := s.ClaimNext(cfg, claimBranch, testAgentID)
		if !errors.Is(err, ErrNoRoom) {
			t.Errorf("with ticket A in %s: err = %v, want ErrNoRoom", status, err)
		}
		if claimed.ID != 0 {
			t.Errorf("with ticket A in %s: claimed ticket %d, want ticket B %d to wait",
				status, claimed.ID, b)
		}
	}

	if err := s.ChangeStatus(a, Done); err != nil {
		t.Fatal(err)
	}
	claimed, _, err = s.ClaimNext(cfg, claimBranch, testAgentID)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.ID != b {
		t.Errorf("claimed ticket %d, want ticket B %d once ticket A is done", claimed.ID, b)
	}
}

// Cancellation does not satisfy a dependency; removing its edge unblocks the
// dependent.
func TestClaimNextKeepsWaitingForACancelledTicket(t *testing.T) {
	s, dependedOn, _, free := dependentQueue(t)
	if err := s.ChangeStatus(dependedOn, Cancelled); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Claim(free, claimBranch(Ticket{ID: free}), testAgentID); err != nil {
		t.Fatal(err)
	}

	_, _, err := s.ClaimNext(config.Config{Runs: 4}, claimBranch, testAgentID)

	if !errors.Is(err, ErrNoRoom) {
		t.Errorf("err = %v, want ErrNoRoom: the link to a cancelled ticket holds the ticket back", err)
	}
}

// Free capacity with all work blocked must not count supervisors that would
// find ErrNoRoom.
func TestClaimableCountWithEveryTicketWaitingOnALinkCountsNothing(t *testing.T) {
	s, _, _, free := dependentQueue(t)
	if _, err := s.Claim(free, claimBranch(Ticket{ID: free}), testAgentID); err != nil {
		t.Fatal(err)
	}

	got := claimableCount(t, s, config.Config{Runs: 4})

	if got != 0 {
		t.Errorf("count = %d with slots free and every ticket waiting on a link, want 0", got)
	}
}

func TestClaimableCountCountsATicketWhoseLinksAreDone(t *testing.T) {
	s, dependedOn, _, _ := dependentQueue(t)
	for _, status := range []TicketStatus{Ready, Done} {
		if err := s.ChangeStatus(dependedOn, status); err != nil {
			t.Fatal(err)
		}
	}

	got := claimableCount(t, s, config.Config{Runs: 4})

	if got != 2 {
		t.Errorf("count = %d, want 2: the link is done and both tickets of the queue can run", got)
	}
}

// inboxDependsOn returns a ticket's inbox blockers, failing if the ticket is
// absent.
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

func TestOpenTicketsNameTheLinksOfATicket(t *testing.T) {
	s, dependedOn, dependent, free := dependentQueue(t)

	if got, want := inboxDependsOn(t, s, dependent), []int64{dependedOn}; !slices.Equal(got, want) {
		t.Errorf("ticket %d depends on %v, want %v", dependent, got, want)
	}
	if got := inboxDependsOn(t, s, free); len(got) != 0 {
		t.Errorf("ticket %d depends on %v, want nothing", free, got)
	}
}

// Completed edges disappear from inbox blockers but remain available to dg
// show.
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

func TestOpenCreatesTheLinksTableAndIndex(t *testing.T) {
	s, _ := emptyStore(t)

	for _, name := range []string{"ticket_deps", "ticket_deps_depends_on"} {
		var found string
		if err := s.db.QueryRow(
			"SELECT name FROM sqlite_master WHERE name = ?", name).Scan(&found); err != nil {
			t.Errorf("%s is not on the database: %v", name, err)
		}
	}
}

func TestAddDependenciesLinksEachIdItIsGiven(t *testing.T) {
	s, ids := threeTickets(t)

	if err := s.AddDependencies(ids[2], ids[0], ids[1]); err != nil {
		t.Fatal(err)
	}

	if got, want := linksOf(t, s, ids[2]), []int64{ids[0], ids[1]}; !slices.Equal(got, want) {
		t.Errorf("ticket %d depends on %v, want %v", ids[2], got, want)
	}
}

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

// Reject cycles before they can leave every member blocked.
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

// Cycle detection must traverse every edge, including indirect paths.
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

// An invalid second dependency must roll back the first insertion.
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

// Dependency changes are restricted to queued tickets.
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

// Removing a cancelled prerequisite unblocks the dependent.
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

// Removing a missing edge must report the mistake.
func TestRemoveDependenciesRefusesALinkThatIsNotThere(t *testing.T) {
	s, ids := threeTickets(t)

	err := s.RemoveDependencies(ids[2], ids[0])

	if !errors.Is(err, ErrNoDependency) {
		t.Fatalf("err = %v, want ErrNoDependency", err)
	}
}

// Removal has the same queued-only restriction as addition.
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

func TestAddDependenciesRefusesAnIdThatNamesNoTicket(t *testing.T) {
	s, id := oneTicket(t)

	if err := s.AddDependencies(id, id+1000); !errors.Is(err, ErrNoTicket) {
		t.Errorf("err = %v, want ErrNoTicket", err)
	}
	if err := s.AddDependencies(id+1000, id); !errors.Is(err, ErrNoTicket) {
		t.Errorf("err = %v, want ErrNoTicket", err)
	}
}
