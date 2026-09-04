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
	running bool
	err     error
}

func (f fakeSource) OpenTickets() ([]store.OpenTicket, error) {
	return f.tickets, f.err
}

func (f fakeSource) IsQueueRunning() (bool, error) {
	return f.running, f.err
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
	source := fakeSource{tickets: []store.OpenTicket{
		{ID: 1, Status: store.Queued},
		{ID: 2, Status: store.Ready},
		{ID: 3, Status: store.Running},
		{ID: 4, Status: store.Queued},
		{ID: 5, Status: store.Ready},
	}}

	got, err := Get(source)
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
	got, err := Get(fakeSource{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Ready) != 0 || len(got.Running) != 0 || len(got.Queued) != 0 {
		t.Errorf("the inbox holds %v, %v and %v, want each group empty",
			ids(got.Ready), ids(got.Running), ids(got.Queued))
	}
}

// A status that no group shows is not a fault of the inbox, and it is not in a
// group either.
func TestGetLeavesOutAStatusThatNoGroupHolds(t *testing.T) {
	source := fakeSource{tickets: []store.OpenTicket{
		{ID: 1, Status: store.Done},
		{ID: 2, Status: store.Cancelled},
		{ID: 3, Status: store.Failed},
		{ID: 4, Status: store.Queued},
	}}

	got, err := Get(source)
	if err != nil {
		t.Fatal(err)
	}
	if want := []int64{4}; !slices.Equal(ids(got.Queued), want) {
		t.Errorf("QUEUED holds %v, want %v", ids(got.Queued), want)
	}
	if len(got.Ready) != 0 || len(got.Running) != 0 {
		t.Errorf("READY holds %v and RUNNING holds %v, want each empty",
			ids(got.Ready), ids(got.Running))
	}
}

func TestGetGivesTheErrorOfTheSource(t *testing.T) {
	want := errors.New("the database is not there")

	_, err := Get(fakeSource{err: want})
	if !errors.Is(err, want) {
		t.Errorf("err = %v, want %v", err, want)
	}
}

// READY is in the order of the time of completion, and a ticket that becomes
// ready goes at the end. The source gives them in another order, so the
// inbox and not the query does this work.
func TestGetPutsReadyInTheOrderOfCompletion(t *testing.T) {
	source := fakeSource{tickets: []store.OpenTicket{
		{ID: 1, Status: store.Ready, Completed: time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)},
		{ID: 2, Status: store.Ready, Completed: time.Date(2026, 8, 28, 9, 0, 0, 0, time.UTC)},
		{ID: 3, Status: store.Ready, Completed: time.Date(2026, 8, 28, 15, 0, 0, 0, time.UTC)},
	}}

	got, err := Get(source)
	if err != nil {
		t.Fatal(err)
	}
	if want := []int64{2, 1, 3}; !slices.Equal(ids(got.Ready), want) {
		t.Errorf("READY holds %v, want %v", ids(got.Ready), want)
	}
}

// Two tickets can hold the same time, because the time has one second and no
// part of a second. The id then keeps the order stable.
func TestGetKeepsReadyStableWhenTheTimeIsTheSame(t *testing.T) {
	same := time.Date(2026, 8, 28, 9, 0, 0, 0, time.UTC)
	source := fakeSource{tickets: []store.OpenTicket{
		{ID: 7, Status: store.Ready, Completed: same},
		{ID: 3, Status: store.Ready, Completed: same},
		{ID: 5, Status: store.Ready, Completed: same},
	}}

	got, err := Get(source)
	if err != nil {
		t.Fatal(err)
	}
	if want := []int64{3, 5, 7}; !slices.Equal(ids(got.Ready), want) {
		t.Errorf("READY holds %v, want %v", ids(got.Ready), want)
	}
}

func TestGetPutsQueuedInTheOrderOfPosition(t *testing.T) {
	source := fakeSource{tickets: []store.OpenTicket{
		{ID: 1, Status: store.Queued, Position: 2},
		{ID: 2, Status: store.Queued, Position: 3},
		{ID: 3, Status: store.Queued, Position: 1},
	}}

	got, err := Get(source)
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
	source := fakeSource{tickets: []store.OpenTicket{
		{ID: 9, Status: store.Running},
		{ID: 3, Status: store.Running},
		{ID: 7, Status: store.Running},
	}}

	got, err := Get(source)
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
	source := fakeSource{tickets: []store.OpenTicket{
		{ID: 3, Status: store.Running, Started: started},
	}}

	got, err := Get(source)
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
		got, err := Get(fakeSource{running: running})
		if err != nil {
			t.Fatal(err)
		}
		if got.QueueRunning != running {
			t.Errorf("queue running = %v, want %v", got.QueueRunning, running)
		}
	}
}
