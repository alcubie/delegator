package inbox

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/alcubie/delegator/internal/store"
)

// fakeSource supplies tickets without a database; store queries have separate
// tests.
type fakeSource struct {
	tickets []store.OpenTicket
	done    []store.OpenTicket
	since   time.Time
	running bool
	err     error
	// doneErr reaches the second query; err would fail OpenTickets first.
	doneErr error
}

func (f fakeSource) OpenTickets() ([]store.OpenTicket, error) {
	return f.tickets, f.err
}

// Record the cutoff to verify that Get forwards it unchanged. Store tests
// cover filtering.
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

// get uses a cutoff early enough to include every fixture ticket.
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

// Unknown statuses are ignored.
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

// Shuffle the source order to verify that FAILED sorts by failure time,
// oldest first.
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

// Shuffle the source order to verify that READY follows user-set positions.
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

// Acceptance time must not override ready positions.
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

// RUNNING must sort by ID independently of source query order.
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

// Pass through the stored run start unchanged; renderers calculate elapsed
// time.
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

// Queue state distinguishes paused work from an idle queue.
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

// Shuffle the source order to verify that DONE sorts by acceptance, oldest
// first.
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

// Get must forward the caller's cutoff rather than read its own clock.
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

func TestGetGivesTheErrorOfTheDoneTickets(t *testing.T) {
	want := errors.New("the database is not there")

	_, err := get(&fakeSource{doneErr: want})
	if !errors.Is(err, want) {
		t.Errorf("err = %v, want %v", err, want)
	}
}

// Implicit selection uses the lowest ready position within the requested
// project.
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

// No ready ticket in this project must not select another project's work.
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
