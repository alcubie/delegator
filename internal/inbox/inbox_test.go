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
	err     error
}

func (f fakeSource) OpenTickets() ([]store.OpenTicket, error) {
	return f.tickets, f.err
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
