package inbox

import (
	"errors"
	"slices"
	"testing"

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
		{ID: 1, Status: store.Ready, Completed: "2026-08-28T12:00:00Z"},
		{ID: 2, Status: store.Ready, Completed: "2026-08-28T09:00:00Z"},
		{ID: 3, Status: store.Ready, Completed: "2026-08-28T15:00:00Z"},
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
	same := "2026-08-28T09:00:00Z"
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
