package inbox

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/alcubie/delegator/internal/store"
)

// fakeSource returns the tickets that a test asks for, with no database. The
// query of the store has its own test.
type fakeSource struct {
	tickets []store.OpenTicket
	done    []store.OpenTicket
	since   time.Time
	running bool
	err     error
	// doneErr is the error of DoneTickets alone. err stops OpenTickets first,
	// which is the call before it, so the error of DoneTickets needs a field
	// of its own to reach Get at all.
	doneErr error
}

func (f fakeSource) OpenTickets() ([]store.OpenTicket, error) {
	return f.tickets, f.err
}

// DoneTickets keeps the time it was asked for, so a test can say that Get
// passed the one it was given. The window itself is the work of the store.
func (f *fakeSource) DoneTickets(since time.Time) ([]store.OpenTicket, error) {
	f.since = since
	if f.doneErr != nil {
		return nil, f.doneErr
	}
	return f.done, f.err
}

func (f fakeSource) IsQueueRunning() (bool, error) {
	return f.running, f.err
}

// get calls Get with a since that reaches back far enough to hold every ticket
// a test makes, for the tests that are not about the window.
func get(source *fakeSource) (Inbox, error) {
	return Get(source, time.Time{})
}

// ids returns the id of each ticket of one group.
func ids(tickets []store.OpenTicket) []int64 {
	out := make([]int64, 0, len(tickets))
	for _, t := range tickets {
		out = append(out, t.ID)
	}
	return out
}

func TestGetPutsEachTicketInItsGroup(t *testing.T) {
	source := &fakeSource{tickets: []store.OpenTicket{
		{ID: 1, Status: store.Queued},
		{ID: 2, Status: store.Ready},
		{ID: 3, Status: store.Running},
		{ID: 4, Status: store.Queued},
		{ID: 5, Status: store.Ready},
	}}

	got, err := get(source)
	if err != nil {
		t.Fatal(err)
	}

	if want := []int64{2, 5}; !slices.Equal(ids(got.Ready), want) {
		t.Errorf("READY holds %v, want %v", ids(got.Ready), want)
	}
	if want := []int64{3}; !slices.Equal(ids(got.Running), want) {
		t.Errorf("RUNNING holds %v, want %v", ids(got.Running), want)
	}
	if want := []int64{1, 4}; !slices.Equal(ids(got.Queued), want) {
		t.Errorf("QUEUED holds %v, want %v", ids(got.Queued), want)
	}
}

func TestGetWithNoTicket(t *testing.T) {
	got, err := get(&fakeSource{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Ready) != 0 || len(got.Running) != 0 || len(got.Failed) != 0 || len(got.Queued) != 0 {
		t.Errorf("the inbox holds %v, %v, %v and %v, want each group empty",
			ids(got.Ready), ids(got.Running), ids(got.Failed), ids(got.Queued))
	}
}

// A status that no group shows is not a fault of the inbox, and it is not in a
// group either.
func TestGetLeavesOutAStatusThatNoGroupHolds(t *testing.T) {
	source := &fakeSource{tickets: []store.OpenTicket{
		{ID: 1, Status: store.Done},
		{ID: 2, Status: store.Cancelled},
		{ID: 4, Status: store.Queued},
	}}

	got, err := get(source)
	if err != nil {
		t.Fatal(err)
	}
	if want := []int64{4}; !slices.Equal(ids(got.Queued), want) {
		t.Errorf("QUEUED holds %v, want %v", ids(got.Queued), want)
	}
	if len(got.Ready) != 0 || len(got.Running) != 0 || len(got.Failed) != 0 {
		t.Errorf("READY holds %v, RUNNING holds %v and FAILED holds %v, want each empty",
			ids(got.Ready), ids(got.Running), ids(got.Failed))
	}
}

// FAILED holds the tickets whose run stopped without a report, in the order of
// the time of the failure, and the ticket that failed first is at the top, as
// the ticket accepted first is at the top of DONE. The source gives them in
// another order, so the inbox and not the query does this work.
func TestGetPutsFailedInTheOrderOfTheTimeOfFailure(t *testing.T) {
	first := time.Date(2026, 8, 28, 9, 0, 0, 0, time.UTC)
	last := time.Date(2026, 8, 28, 15, 0, 0, 0, time.UTC)
	source := &fakeSource{tickets: []store.OpenTicket{
		{ID: 3, Status: store.Failed, Changed: time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)},
		{ID: 1, Status: store.Failed, Changed: first},
		{ID: 2, Status: store.Failed, Changed: last},
		{ID: 4, Status: store.Queued},
	}}

	got, err := get(source)
	if err != nil {
		t.Fatal(err)
	}
	if want := []int64{1, 3, 2}; !slices.Equal(ids(got.Failed), want) {
		t.Errorf("FAILED holds %v, want %v", ids(got.Failed), want)
	}
	if got := got.Failed[0].Changed; !got.Equal(first) {
		t.Errorf("the first failed ticket holds the time %v, want %v", got, first)
	}
	if want := []int64{4}; !slices.Equal(ids(got.Queued), want) {
		t.Errorf("QUEUED holds %v, want %v", ids(got.Queued), want)
	}
}

func TestGetGivesTheErrorOfTheSource(t *testing.T) {
	want := errors.New("the database is not there")

	_, err := get(&fakeSource{err: want})
	if !errors.Is(err, want) {
		t.Errorf("err = %v, want %v", err, want)
	}
}

// READY is in the order of position, which is the order the person set with
// dg move. The source gives the tickets in another order, so the inbox and not
// the query does this work.
func TestGetPutsReadyInTheOrderOfPosition(t *testing.T) {
	source := &fakeSource{tickets: []store.OpenTicket{
		{ID: 1, Status: store.Ready, Position: 2},
		{ID: 2, Status: store.Ready, Position: 3},
		{ID: 3, Status: store.Ready, Position: 1},
	}}

	got, err := get(source)
	if err != nil {
		t.Fatal(err)
	}
	if want := []int64{3, 1, 2}; !slices.Equal(ids(got.Ready), want) {
		t.Errorf("READY holds %v, want %v", ids(got.Ready), want)
	}
}

// The time of acceptance no longer orders READY, so a ticket that holds a later
// one stays where the position puts it.
func TestGetLeavesReadyInPositionOrderWhenAcceptanceDisagrees(t *testing.T) {
	source := &fakeSource{tickets: []store.OpenTicket{
		{ID: 1, Status: store.Ready, Position: 1,
			Accepted: time.Date(2026, 8, 28, 15, 0, 0, 0, time.UTC)},
		{ID: 2, Status: store.Ready, Position: 2,
			Accepted: time.Date(2026, 8, 28, 9, 0, 0, 0, time.UTC)},
	}}

	got, err := get(source)
	if err != nil {
		t.Fatal(err)
	}
	if want := []int64{1, 2}; !slices.Equal(ids(got.Ready), want) {
		t.Errorf("READY holds %v, want %v", ids(got.Ready), want)
	}
}

func TestGetPutsQueuedInTheOrderOfPosition(t *testing.T) {
	source := &fakeSource{tickets: []store.OpenTicket{
		{ID: 1, Status: store.Queued, Position: 2},
		{ID: 2, Status: store.Queued, Position: 3},
		{ID: 3, Status: store.Queued, Position: 1},
	}}

	got, err := get(source)
	if err != nil {
		t.Fatal(err)
	}
	if want := []int64{3, 1, 2}; !slices.Equal(ids(got.Queued), want) {
		t.Errorf("QUEUED holds %v, want %v", ids(got.Queued), want)
	}
}

// RUNNING takes the order of the id. Version 1 has one run at a time, so the
// group holds one ticket and the order shows after a fault that leaves two. The
// order is a decision here and not the ORDER BY of the query, which a change
// for another reason would move.
func TestGetPutsRunningInTheOrderOfTheID(t *testing.T) {
	source := &fakeSource{tickets: []store.OpenTicket{
		{ID: 9, Status: store.Running},
		{ID: 3, Status: store.Running},
		{ID: 7, Status: store.Running},
	}}

	got, err := get(source)
	if err != nil {
		t.Fatal(err)
	}
	if want := []int64{3, 7, 9}; !slices.Equal(ids(got.Running), want) {
		t.Errorf("RUNNING holds %v, want %v", ids(got.Running), want)
	}
}

// The row of a running ticket shows how long the run has been going, so the
// inbox carries the time that the run started. The value comes from the
// source, which reads runs.started_at, and the inbox gives it out as it is:
// the duration belongs to the moment it is written, and this package writes
// no text.
func TestGetCarriesTheStartOfTheRun(t *testing.T) {
	started := time.Date(2026, 8, 28, 9, 30, 0, 0, time.UTC)
	source := &fakeSource{tickets: []store.OpenTicket{
		{ID: 3, Status: store.Running, Started: started},
	}}

	got, err := get(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Running) != 1 {
		t.Fatalf("RUNNING holds %v, want the one ticket", ids(got.Running))
	}
	if got := got.Running[0].Started; !got.Equal(started) {
		t.Errorf("started = %v, want %v", got, started)
	}
}

// A person who sees tickets in QUEUED and no run must know whether delegator
// is waiting or stopped, so the inbox carries the state of the queue.
func TestGetSaysWhetherTheQueueIsRunning(t *testing.T) {
	for _, running := range []bool{true, false} {
		got, err := get(&fakeSource{running: running})
		if err != nil {
			t.Fatal(err)
		}
		if got.QueueRunning != running {
			t.Errorf("queue running = %v, want %v", got.QueueRunning, running)
		}
	}
}

// DONE holds the tickets that the person accepted lately, so the work of a day
// is still in the inbox after each one of them closed.
func TestGetPutsTheAcceptedTicketsInDone(t *testing.T) {
	source := &fakeSource{
		tickets: []store.OpenTicket{{ID: 1, Status: store.Queued}},
		done: []store.OpenTicket{
			{ID: 4, Status: store.Done},
			{ID: 6, Status: store.Done},
		},
	}

	got, err := get(source)
	if err != nil {
		t.Fatal(err)
	}
	if want := []int64{4, 6}; !slices.Equal(ids(got.Done), want) {
		t.Errorf("DONE holds %v, want %v", ids(got.Done), want)
	}
	if want := []int64{1}; !slices.Equal(ids(got.Queued), want) {
		t.Errorf("QUEUED holds %v, want %v", ids(got.Queued), want)
	}
}

// DONE is in the order of the time of acceptance, and the ticket the person
// accepted last is at the end. The source gives them in another order, so the
// inbox and not the query does this work.
func TestGetPutsDoneInTheOrderOfAcceptance(t *testing.T) {
	source := &fakeSource{done: []store.OpenTicket{
		{ID: 1, Status: store.Done, Accepted: time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)},
		{ID: 2, Status: store.Done, Accepted: time.Date(2026, 8, 28, 9, 0, 0, 0, time.UTC)},
		{ID: 3, Status: store.Done, Accepted: time.Date(2026, 8, 28, 15, 0, 0, 0, time.UTC)},
	}}

	got, err := get(source)
	if err != nil {
		t.Fatal(err)
	}
	if want := []int64{2, 1, 3}; !slices.Equal(ids(got.Done), want) {
		t.Errorf("DONE holds %v, want %v", ids(got.Done), want)
	}
}

// The caller says how far back DONE reaches, and Get asks the source for that
// time and no other. A window that Get made up from a clock of its own would
// put a decision of the person in a package that reads no clock.
func TestGetAsksTheSourceForTheTimeItWasGiven(t *testing.T) {
	since := time.Date(2026, 8, 27, 9, 0, 0, 0, time.UTC)
	source := &fakeSource{}

	if _, err := Get(source, since); err != nil {
		t.Fatal(err)
	}
	if !source.since.Equal(since) {
		t.Errorf("the source was asked for %v, want %v", source.since, since)
	}
}

// An error from the tickets of DONE stops Get, as an error from the open
// tickets does.
func TestGetGivesTheErrorOfTheDoneTickets(t *testing.T) {
	want := errors.New("the database is not there")

	_, err := get(&fakeSource{doneErr: want})
	if !errors.Is(err, want) {
		t.Errorf("err = %v, want %v", err, want)
	}
}

// The ticket a person reviews is the head of READY of one project, which is
// the ready ticket with the smallest position. A ticket of another project is
// not it, and neither is a ticket that is queued or running.
func TestFirstReadyTakesTheHeadOfReadyOfTheProject(t *testing.T) {
	source := &fakeSource{tickets: []store.OpenTicket{
		{ID: 1, Project: "/projects/web-api", Status: store.Ready, Position: 3},
		{ID: 2, Project: "/projects/billing", Status: store.Ready, Position: 1},
		{ID: 3, Project: "/projects/web-api", Status: store.Queued, Position: 1},
		{ID: 4, Project: "/projects/web-api", Status: store.Ready, Position: 2},
		{ID: 5, Project: "/projects/web-api", Status: store.Running},
	}}

	box, err := get(source)
	if err != nil {
		t.Fatal(err)
	}

	got, found := box.FirstReady("/projects/web-api")
	if !found {
		t.Fatal("FirstReady found no ticket, want ticket 4")
	}
	if got.ID != 4 {
		t.Errorf("FirstReady = %d, want 4", got.ID)
	}
}

// A project whose tickets are all queued or running has no head of READY, and
// the caller says so in its own words rather than taking a ticket of another
// project.
func TestFirstReadyWithNoReadyTicketOfTheProject(t *testing.T) {
	source := &fakeSource{tickets: []store.OpenTicket{
		{ID: 1, Project: "/projects/billing", Status: store.Ready},
		{ID: 2, Project: "/projects/web-api", Status: store.Queued},
	}}

	box, err := get(source)
	if err != nil {
		t.Fatal(err)
	}

	if got, found := box.FirstReady("/projects/web-api"); found {
		t.Errorf("FirstReady found ticket %d, want none", got.ID)
	}
}
