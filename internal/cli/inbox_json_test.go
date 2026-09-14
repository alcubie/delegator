package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/alcubie/delegator/internal/inbox"
	"github.com/alcubie/delegator/internal/store"
)

// jsonGroups is every group that dg --json writes, in the order that the text
// form of the inbox gives them.
var jsonGroups = []string{"done", "ready", "running", "failed", "queued"}

// jsonInboxFields is every key that one ticket of dg --json holds.
var jsonInboxFields = []string{
	"id", "title", "status", "project", "created", "accepted", "started",
}

// eachGroup makes a data directory whose inbox holds one ticket in every
// group, and returns the store, the repository, and the id of the ticket of
// each group by the name that the document gives that group.
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
	if _, err := s.Claim(ids["running"], "delegator/running"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Claim(ids["failed"], "delegator/failed"); err != nil {
		t.Fatal(err)
	}
	if err := s.ChangeStatus(ids["failed"], store.Failed); err != nil {
		t.Fatal(err)
	}
	return s, repo, ids
}

// readInboxJSON runs dg --json and gives the one object that it wrote. It
// reads the rest of the stream as well, because the flag promises one object
// and nothing else.
func readInboxJSON(t *testing.T, dataDir, workDir string, args ...string) map[string]any {
	t.Helper()
	out, err := runIn(t, dataDir, workDir, append([]string{"--json"}, args...)...)
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(strings.NewReader(out))
	var got map[string]any
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("dg --json wrote %q, which is not one JSON object: %v", out, err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		t.Errorf("dg --json wrote more than one object:\n%s", out)
	}
	return got
}

// jsonGroup returns the tickets of one group of the document.
func jsonGroup(t *testing.T, got map[string]any, name string) []map[string]any {
	t.Helper()
	held, there := got[name]
	if !there {
		t.Fatalf("dg --json holds no group %q: %v", name, got)
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

// §9.3 says that each command which shows data also accepts --json, so that a
// different interface reads the data and not the text. The desktop GUI reads
// the inbox through this flag at each refresh.
func TestRunJSONHoldsTheQueueAndEachGroup(t *testing.T) {
	dataDir := t.TempDir()
	_, repo, ids := eachGroup(t, dataDir)

	got := readInboxJSON(t, dataDir, repo)

	keys := slices.Sorted(maps.Keys(got))
	want := slices.Sorted(slices.Values(append([]string{"queue"}, jsonGroups...)))
	if !slices.Equal(keys, want) {
		t.Errorf("dg --json holds the keys %v, want %v", keys, want)
	}
	for _, name := range jsonGroups {
		if held := jsonGroupIDs(t, got, name); !slices.Equal(held, []int64{ids[name]}) {
			t.Errorf("the group %q holds %v, want [%d]", name, held, ids[name])
		}
	}
}

// The queue is running or paused, which is the state that the first line of
// the text form says.
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

// A group that holds no ticket is an empty list and not null, so a reader
// walks each group the one way.
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

// Each group comes in the order that inbox.Get gives, so the GUI shows the
// list that the terminal shows. The queue comes in the order the person last
// set with dg move, which is not the order of the ids.
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

// A row of the inbox holds the fields that the text form shows and the times
// that a different interface needs. It holds no elapsed string: the GUI counts
// the time of a run from started itself, and a string made at the moment the
// document was written would stop as soon as the GUI drew it.
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
		t.Errorf("a ticket of dg --json holds the keys %v, want %v", keys, want)
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

// Each time is RFC 3339, so a reader parses one form.
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

	// A ticket that runs holds the start of its run, which is the time the GUI
	// counts the elapsed time from.
	lastRun, err := s.Run(ids["running"])
	if err != nil {
		t.Fatal(err)
	}
	if at := wantRFC3339(t, one(t, got, "running"), "started"); !at.Equal(lastRun.StartedAt) {
		t.Errorf("started = %v, want %v", at, lastRun.StartedAt)
	}
}

// A time with no value is null, so a reader tests one thing and not two.
func TestRunJSONGivesNullForATimeWithNoValue(t *testing.T) {
	dataDir := t.TempDir()
	_, _, repo := queuedTicket(t, dataDir)

	got := one(t, readInboxJSON(t, dataDir, repo), "queued")

	for _, key := range []string{"accepted", "started"} {
		if value, held := got[key]; !held || value != nil {
			t.Errorf("%s = %v for a ticket in the queue, want null", key, value)
		}
	}
	// Every ticket arrived, so no ticket has a null there.
	if got["created"] == nil {
		t.Error("created = null for a ticket in the queue, want the time it arrived")
	}
}

// The project is a full path with no tilde. A reader gives the path to another
// command, and no command expands a tilde that came from a variable.
func TestRunJSONGivesTheFullPathOfTheProject(t *testing.T) {
	dataDir := t.TempDir()
	_, _, repo := queuedTicket(t, dataDir)
	// The repository is below the home of the person, which is what the text
	// form of dg show writes a tilde for.
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

// Where a key of the inbox and a key of dg show --json name the same field,
// the two keys are the same word and carry the same value, so a reader of one
// document knows the other without a second table. started is the one key of a
// row that dg show does not give: the inbox counts the run, and one ticket in
// full does not.
func TestRunJSONNamesEachFieldAsShowDoes(t *testing.T) {
	dataDir := t.TempDir()
	_, repo, ids := eachGroup(t, dataDir)

	row := one(t, readInboxJSON(t, dataDir, repo), "done")
	shown := showJSON(t, dataDir, repo, fmt.Sprint(ids["done"]), "--json")

	for key, want := range row {
		if key == "started" {
			continue
		}
		got, there := shown[key]
		if !there {
			t.Errorf("dg --json holds the key %q and dg show --json holds %v", key, slices.Sorted(maps.Keys(shown)))
			continue
		}
		if got != want {
			t.Errorf("%s is %v in the inbox and %v in dg show", key, want, got)
		}
	}
}

// --color has no effect on --json. A reader that asks for the colour of the
// terminal it writes to still gets JSON that parses, and the text form keeps
// the colour that TestColorFlagDecidesTheColour holds it to.
func TestRunJSONTakesNoColour(t *testing.T) {
	dataDir := t.TempDir()
	_, repo, _ := eachGroup(t, dataDir)

	plain, err := runIn(t, dataDir, repo, "--json")
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"always", "never", "auto"} {
		got, err := runIn(t, dataDir, repo, "--json", "--color="+mode)
		if err != nil {
			t.Fatal(err)
		}
		if got != plain {
			t.Errorf("dg --json --color=%s wrote\n%s\nwant\n%s", mode, got, plain)
		}
	}
	if strings.Contains(plain, "\x1b") {
		t.Errorf("dg --json is coloured:\n%q", plain)
	}
}

// The title is what the person wrote, and the encoder of Go escapes `<`, `>`
// and `&` for a browser that reads JSON inside a page. Nothing here is a page,
// and dg show --json makes the same choice for the prose.
func TestRunJSONKeepsTheCharactersOfTheTitle(t *testing.T) {
	dataDir := t.TempDir()
	s, _, repo := queuedTicket(t, dataDir)
	const title = "Take <staging> out of the DNS & the load balancer"
	queuedIn(t, s, repo, title)

	out, err := runIn(t, dataDir, repo, "--json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, title) {
		t.Errorf("dg --json does not hold the title as it is:\n%s", out)
	}
}
