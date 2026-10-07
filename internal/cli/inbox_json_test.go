package cli

import (
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/alcubie/delegator/internal/inbox"
	"github.com/alcubie/delegator/internal/store"
	"github.com/alcubie/delegator/internal/testfix"
)

// jsonGroups is every group that RPC inbox result writes, in the order that the text
// form of the inbox gives them.
var jsonGroups = []string{"done", "ready", "running", "failed", "queued"}

// jsonInboxFields is every key that one ticket of RPC inbox result holds.
var jsonInboxFields = []string{
	"id", "title", "status", "project", "created", "accepted", "started",
}

// eachGroup creates one ticket per inbox group and returns the store,
// repository, and IDs keyed by group name.
func eachGroup(t *testing.T, dataDir string) (*store.Store, string, map[string]int64) {
	t.Helper()
	s, queued, repo := queuedTicket(t, dataDir)
	ids := map[string]int64{"queued": queued}
	for _, name := range []string{"done", "ready", "running", "failed"} {
		ids[name] = queuedIn(t, s, repo, "the "+name+" ticket")
	}

	finishIn(t, s, ids["ready"])
	finishIn(t, s, ids["done"])
	if err := s.ChangeStatus(ids["done"], store.Done); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Claim(ids["running"], "delegator/running", testAgentID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Claim(ids["failed"], "delegator/failed", testAgentID); err != nil {
		t.Fatal(err)
	}
	if err := s.ChangeStatus(ids["failed"], store.Failed); err != nil {
		t.Fatal(err)
	}
	return s, repo, ids
}

// readInboxJSON reads the inbox document through dg rpc.
func readInboxJSON(t *testing.T, dataDir, workDir string, args ...string) map[string]any {
	t.Helper()
	return rpcDocument(t, dataDir, workDir, "inbox", args...)
}

// jsonGroup returns the tickets of one group of the document.
func jsonGroup(t *testing.T, got map[string]any, name string) []map[string]any {
	t.Helper()
	held, there := got[name]
	if !there {
		t.Fatalf("RPC inbox result holds no group %q: %v", name, got)
	}
	list, ok := held.([]any)
	if !ok {
		t.Fatalf("the group %q is %v, want a list", name, held)
	}
	tickets := make([]map[string]any, 0, len(list))
	for _, item := range list {
		ticket, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("the group %q holds %v, want a ticket", name, item)
		}
		tickets = append(tickets, ticket)
	}
	return tickets
}

// jsonGroupIDs returns the id of each ticket of one group, in the order that
// the document gives them.
func jsonGroupIDs(t *testing.T, got map[string]any, name string) []int64 {
	t.Helper()
	var ids []int64
	for _, ticket := range jsonGroup(t, got, name) {
		id, ok := ticket["id"].(float64)
		if !ok {
			t.Fatalf("a ticket of %q has the id %v, want a number", name, ticket["id"])
		}
		ids = append(ids, int64(id))
	}
	return ids
}

// ticketIDs returns the id of each ticket of one group of an inbox.Inbox, so a
// test compares the document with the structure it comes from.
func ticketIDs(tickets []store.OpenTicket) []int64 {
	var ids []int64
	for _, ticket := range tickets {
		ids = append(ids, ticket.ID)
	}
	return ids
}

// RPC supplies structured inbox data for external clients.
func TestRunJSONHoldsTheQueueAndEachGroup(t *testing.T) {
	dataDir := t.TempDir()
	_, repo, ids := eachGroup(t, dataDir)

	got := readInboxJSON(t, dataDir, repo)

	keys := slices.Sorted(maps.Keys(got))
	want := slices.Sorted(slices.Values(append([]string{"queue", "done_hours"}, jsonGroups...)))
	if !slices.Equal(keys, want) {
		t.Errorf("RPC inbox result holds the keys %v, want %v", keys, want)
	}
	for _, name := range jsonGroups {
		if held := jsonGroupIDs(t, got, name); !slices.Equal(held, []int64{ids[name]}) {
			t.Errorf("the group %q holds %v, want [%d]", name, held, ids[name])
		}
	}
}

func TestRunJSONGivesTheDefaultDoneWindowWhenDoneIsEmpty(t *testing.T) {
	dataDir := t.TempDir()
	_, _, repo := queuedTicket(t, dataDir)

	got := readInboxJSON(t, dataDir, repo)

	if got["done_hours"] != float64(24) {
		t.Errorf("done_hours = %v, want 24", got["done_hours"])
	}
	if tickets := jsonGroup(t, got, "done"); len(tickets) != 0 {
		t.Errorf("done = %v, want no ticket", tickets)
	}
}

func TestRunJSONDoneWindowAgreesWithDoneFiltering(t *testing.T) {
	for _, test := range []struct {
		name      string
		doneHours int
		age       time.Duration
		wantDone  bool
	}{
		{"custom window includes recent acceptance", 6, 5 * time.Hour, true},
		{"custom window excludes old acceptance", 6, 7 * time.Hour, false},
		{"zero excludes recent acceptance", 0, 0, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			dataDir := t.TempDir()
			s, _, repo := queuedTicket(t, dataDir)
			id := queuedIn(t, s, repo, "accepted ticket")
			finishIn(t, s, id)
			if err := s.ChangeStatus(id, store.Done); err != nil {
				t.Fatal(err)
			}
			if test.age > 0 {
				testfix.AgeAcceptance(t, dataDir, id, test.age)
			}
			if err := s.SetSetting("done_hours", fmt.Sprint(test.doneHours)); err != nil {
				t.Fatal(err)
			}

			got := readInboxJSON(t, dataDir, repo)

			if got["done_hours"] != float64(test.doneHours) {
				t.Errorf("done_hours = %v, want %d", got["done_hours"], test.doneHours)
			}
			ids := jsonGroupIDs(t, got, "done")
			if test.wantDone && !slices.Equal(ids, []int64{id}) {
				t.Errorf("done = %v, want [%d]", ids, id)
			}
			if !test.wantDone && len(ids) != 0 {
				t.Errorf("done = %v, want no ticket", ids)
			}
		})
	}
}

func TestRunJSONSaysWhetherTheQueueIsRunning(t *testing.T) {
	dataDir := t.TempDir()
	_, _, repo := queuedTicket(t, dataDir)

	if got := readInboxJSON(t, dataDir, repo); got["queue"] != "running" {
		t.Errorf("queue = %v, want running", got["queue"])
	}
	if _, err := runIn(t, dataDir, repo, "pause"); err != nil {
		t.Fatal(err)
	}
	if got := readInboxJSON(t, dataDir, repo); got["queue"] != "paused" {
		t.Errorf("queue = %v after dg pause, want paused", got["queue"])
	}
}

func TestRunJSONGivesAnEmptyListForAGroupWithNoTicket(t *testing.T) {
	dataDir := t.TempDir()
	_, _, repo := queuedTicket(t, dataDir)

	got := readInboxJSON(t, dataDir, repo)

	for _, name := range []string{"done", "ready", "running", "failed"} {
		if tickets := jsonGroup(t, got, name); len(tickets) != 0 {
			t.Errorf("the group %q holds %v, want no ticket", name, tickets)
		}
	}
}

// JSON ordering must match inbox.Get, including user-set positions that
// differ from ID order.
func TestRunJSONGivesEachGroupInTheOrderOfTheInbox(t *testing.T) {
	dataDir := t.TempDir()
	s, repo, ids := eachGroup(t, dataDir)
	for _, title := range []string{"the second queued", "the third queued"} {
		queuedIn(t, s, repo, title)
	}
	if _, err := runIn(t, dataDir, repo, "move", fmt.Sprint(ids["queued"]), "bottom"); err != nil {
		t.Fatal(err)
	}

	box, err := inbox.Get(s, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if want := ticketIDs(box.Queued); slices.IsSorted(want) {
		t.Fatalf("the queue is %v, want an order that the ids alone do not give", want)
	}

	got := readInboxJSON(t, dataDir, repo)

	for name, tickets := range map[string][]store.OpenTicket{
		"done": box.Done, "ready": box.Ready, "running": box.Running,
		"failed": box.Failed, "queued": box.Queued,
	} {
		want := ticketIDs(tickets)
		if held := jsonGroupIDs(t, got, name); !slices.Equal(held, want) {
			t.Errorf("the group %q holds %v, want %v", name, held, want)
		}
	}
}

// one returns the single ticket of a group, for a test whose inbox holds one
// ticket in each.
func one(t *testing.T, got map[string]any, name string) map[string]any {
	t.Helper()
	tickets := jsonGroup(t, got, name)
	if len(tickets) != 1 {
		t.Fatalf("the group %q holds %v, want one ticket", name, tickets)
	}
	return tickets[0]
}

// wantRFC3339 reads one key as a time of RFC 3339.
func wantRFC3339(t *testing.T, ticket map[string]any, key string) time.Time {
	t.Helper()
	at, err := time.Parse(time.RFC3339, fmt.Sprint(ticket[key]))
	if err != nil {
		t.Fatalf("%s = %v, want a time of RFC 3339", key, ticket[key])
	}
	return at
}

// Expose timestamps rather than elapsed strings so clients can keep their own
// clocks updating.
func TestRunJSONHoldsEachFieldOfATicket(t *testing.T) {
	dataDir := t.TempDir()
	s, repo, ids := eachGroup(t, dataDir)
	ticket, err := s.Ticket(ids["ready"])
	if err != nil {
		t.Fatal(err)
	}

	got := one(t, readInboxJSON(t, dataDir, repo), "ready")

	keys := slices.Sorted(maps.Keys(got))
	if want := slices.Sorted(slices.Values(jsonInboxFields)); !slices.Equal(keys, want) {
		t.Errorf("a ticket of RPC inbox result holds the keys %v, want %v", keys, want)
	}
	if got["id"] != float64(ids["ready"]) {
		t.Errorf("id = %v, want %d", got["id"], ids["ready"])
	}
	for key, want := range map[string]any{
		"title":   ticket.Title,
		"status":  string(ticket.Status),
		"project": repo,
	} {
		if got[key] != want {
			t.Errorf("%s = %v, want %v", key, got[key], want)
		}
	}
}

func TestRunJSONGivesTheTimesOfATicket(t *testing.T) {
	dataDir := t.TempDir()
	s, repo, ids := eachGroup(t, dataDir)
	ticket, err := s.Ticket(ids["done"])
	if err != nil {
		t.Fatal(err)
	}
	got := readInboxJSON(t, dataDir, repo)

	done := one(t, got, "done")
	if at := wantRFC3339(t, done, "created"); !at.Equal(ticket.Created) {
		t.Errorf("created = %v, want %v", at, ticket.Created)
	}
	if at := wantRFC3339(t, done, "accepted"); !at.Equal(ticket.Accepted) {
		t.Errorf("accepted = %v, want %v", at, ticket.Accepted)
	}

	lastRun, err := s.Run(ids["running"])
	if err != nil {
		t.Fatal(err)
	}
	if at := wantRFC3339(t, one(t, got, "running"), "started"); !at.Equal(lastRun.StartedAt) {
		t.Errorf("started = %v, want %v", at, lastRun.StartedAt)
	}
}

func TestRunJSONGivesNullForATimeWithNoValue(t *testing.T) {
	dataDir := t.TempDir()
	_, _, repo := queuedTicket(t, dataDir)

	got := one(t, readInboxJSON(t, dataDir, repo), "queued")

	for _, key := range []string{"accepted", "started"} {
		if value, held := got[key]; !held || value != nil {
			t.Errorf("%s = %v for a ticket in the queue, want null", key, value)
		}
	}
	if got["created"] == nil {
		t.Error("created = null for a ticket in the queue, want the time it arrived")
	}
}

// Keep absolute paths; shells do not expand a tilde obtained from a variable.
func TestRunJSONGivesTheFullPathOfTheProject(t *testing.T) {
	dataDir := t.TempDir()
	_, _, repo := queuedTicket(t, dataDir)
	// Place the repository under home to exercise abbreviation
	// boundaries.
	t.Setenv("HOME", filepath.Dir(repo))

	got := one(t, readInboxJSON(t, dataDir, repo), "queued")

	path, ok := got["project"].(string)
	if !ok {
		t.Fatalf("project = %v, want a path", got["project"])
	}
	if !filepath.IsAbs(path) || strings.Contains(path, "~") {
		t.Errorf("project = %q, want a full path with no tilde", path)
	}
}

// Shared fields must match the show result. Started is inbox-specific for
// clients calculating elapsed time.
func TestRunJSONNamesEachFieldAsShowDoes(t *testing.T) {
	dataDir := t.TempDir()
	_, repo, ids := eachGroup(t, dataDir)

	row := one(t, readInboxJSON(t, dataDir, repo), "done")
	shown := showJSON(t, dataDir, repo, fmt.Sprint(ids["done"]))

	for key, want := range row {
		if key == "started" {
			continue
		}
		got, there := shown[key]
		if !there {
			t.Errorf("RPC inbox result holds the key %q and RPC show result holds %v", key, slices.Sorted(maps.Keys(shown)))
			continue
		}
		if got != want {
			t.Errorf("%s is %v in the inbox and %v in dg show", key, want, got)
		}
	}
}

// Color flags must not inject terminal escapes into structured results.
func TestRunJSONTakesNoColour(t *testing.T) {
	dataDir := t.TempDir()
	_, repo, _ := eachGroup(t, dataDir)

	plain, err := rpcIn(t, dataDir, repo, `{"jsonrpc":"2.0","method":"inbox","id":1}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"always", "never", "auto"} {
		got, err := rpcIn(t, dataDir, repo, fmt.Sprintf(`{"jsonrpc":"2.0","method":"inbox","params":{"color":%q},"id":1}`, mode))
		if err != nil {
			t.Fatal(err)
		}
		if got != plain {
			t.Errorf("RPC inbox result --color=%s wrote\n%s\nwant\n%s", mode, got, plain)
		}
	}
	if strings.Contains(plain, "\x1b") {
		t.Errorf("RPC inbox result is coloured:\n%q", plain)
	}
}

// Keep <, >, and & readable; this JSON is not embedded in HTML.
func TestRunJSONKeepsTheCharactersOfTheTitle(t *testing.T) {
	dataDir := t.TempDir()
	s, _, repo := queuedTicket(t, dataDir)
	const title = "Take <staging> out of the DNS & the load balancer"
	queuedIn(t, s, repo, title)

	out, err := rpcIn(t, dataDir, repo, `{"jsonrpc":"2.0","method":"inbox","id":1}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, title) {
		t.Errorf("RPC inbox result does not hold the title as it is:\n%s", out)
	}
}
