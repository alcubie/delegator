package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/alcubie/delegator/internal/store"
	"github.com/alcubie/delegator/internal/testfix"
)

func mapIDs(tickets []mappedTicket) []int64 {
	ids := make([]int64, 0, len(tickets))
	for _, ticket := range tickets {
		ids = append(ids, ticket.ID)
	}
	return ids
}

func mapIn(t *testing.T, dataDir, repo string, id int64) string {
	t.Helper()
	out, err := runIn(t, dataDir, repo, "map", fmt.Sprint(id))
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func mermaidMapIn(t *testing.T, dataDir, repo string, id int64) string {
	t.Helper()
	out, err := runIn(t, dataDir, repo, "map", fmt.Sprint(id), "--mermaid")
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// rpcMapIn calls map over RPC and decodes its result, so a test can compare
// whole rows rather than reach through the generic JSON of the response.
func rpcMapIn(t *testing.T, dataDir, repo string, id int64) mapJSON {
	t.Helper()
	encoded, err := json.Marshal(rpcDocument(t, dataDir, repo, "map", fmt.Sprint(id)))
	if err != nil {
		t.Fatal(err)
	}
	var value mapJSON
	if err := json.Unmarshal(encoded, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

// dependActions is the actions object that the map result gives a ticket in
// one status, for a test whose subject is the rest of the row.
func dependActions(id int64, status store.TicketStatus) map[string]actionJSON {
	if status == store.Queued {
		return map[string]actionJSON{"depend": {Applicable: true, Available: true}}
	}
	reason := fmt.Sprintf("only a queued ticket can change what it depends on: ticket %d is %s", id, status)
	return map[string]actionJSON{"depend": {
		Applicable: status != store.Done && status != store.Cancelled,
		Reason:     &reason,
	}}
}

// dependActionIn reads the depend entry of one ticket of its own map.
func dependActionIn(t *testing.T, dataDir, repo string, id int64) actionJSON {
	t.Helper()
	for _, ticket := range rpcMapIn(t, dataDir, repo, id).Tickets {
		if ticket.ID == id {
			return ticket.Actions["depend"]
		}
	}
	t.Fatalf("the map of ticket %d holds no row for it", id)
	return actionJSON{}
}

// assertDependAction fails unless the map result reports want for the
// dependencies of one ticket.
func assertDependAction(t *testing.T, dataDir, repo string, id int64, want actionJSON) {
	t.Helper()
	if got := dependActionIn(t, dataDir, repo, id); !reflect.DeepEqual(got, want) {
		t.Errorf("actions.depend of ticket %d = %#v, want %#v", id, got, want)
	}
}

// dependErrorIn runs dg depend on a prerequisite of its own and returns the
// message it refuses with.
func dependErrorIn(t *testing.T, dataDir, repo string, id int64) string {
	t.Helper()
	after, err := ticketIn(t, dataDir, repo, "The prerequisite", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runIn(t, dataDir, repo, "depend", fmt.Sprint(id), "--after", fmt.Sprint(after)); err != nil {
		return err.Error()
	}
	t.Fatalf("dg depend changed what ticket %d depends on", id)
	return ""
}

func TestMapAChain(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	first, second := twoTickets(t, dataDir, repo)
	third, err := ticketIn(t, dataDir, repo, "Deploy it", "")
	if err != nil {
		t.Fatal(err)
	}
	s := testfix.OpenStore(t, dataDir)
	if err := s.AddDependencies(second, first); err != nil {
		t.Fatal(err)
	}
	if err := s.AddDependencies(third, second); err != nil {
		t.Fatal(err)
	}

	if got, want := mapIn(t, dataDir, repo, second), "0 of 3 done\n○ #1 Remove staging infrastructure\n  ○ #2 Add rate limiting\n    ○ #3 Deploy it\n"; got != want {
		t.Errorf("dg map wrote:\n%s\nwant:\n%s", got, want)
	}
}

func TestMapPrintsSharedBlockerOnce(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	first, second := twoTickets(t, dataDir, repo)
	third, err := ticketIn(t, dataDir, repo, "Add checks", "")
	if err != nil {
		t.Fatal(err)
	}
	fourth, err := ticketIn(t, dataDir, repo, "Release", "")
	if err != nil {
		t.Fatal(err)
	}
	s := testfix.OpenStore(t, dataDir)
	if err := s.AddDependencies(second, first); err != nil {
		t.Fatal(err)
	}
	if err := s.AddDependencies(third, first); err != nil {
		t.Fatal(err)
	}
	if err := s.AddDependencies(fourth, second, third); err != nil {
		t.Fatal(err)
	}

	out := mapIn(t, dataDir, repo, fourth)
	if got := strings.Count(out, "#1 "); got != 1 {
		t.Errorf("shared blocker appears %d times:\n%s", got, out)
	}
	if !strings.Contains(out, "#4 Release (waits on #2)") {
		t.Errorf("the other blocker is not named:\n%s", out)
	}
}

func TestMapReparentsTicketAfterRemovingItsCurrentParent(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	root, branch := twoTickets(t, dataDir, repo)
	branchChild, err := ticketIn(t, dataDir, repo, "Finish branch", "")
	if err != nil {
		t.Fatal(err)
	}
	sibling, err := ticketIn(t, dataDir, repo, "Sibling", "")
	if err != nil {
		t.Fatal(err)
	}
	dependent, err := ticketIn(t, dataDir, repo, "Later work", "")
	if err != nil {
		t.Fatal(err)
	}
	s := testfix.OpenStore(t, dataDir)
	if err := s.AddDependencies(branch, root); err != nil {
		t.Fatal(err)
	}
	if err := s.AddDependencies(branchChild, branch); err != nil {
		t.Fatal(err)
	}
	if err := s.AddDependencies(sibling, root); err != nil {
		t.Fatal(err)
	}
	if err := s.AddDependencies(dependent, root, branch, sibling); err != nil {
		t.Fatal(err)
	}

	if _, err := runIn(t, dataDir, repo, "depend", fmt.Sprint(dependent),
		"--after", fmt.Sprint(sibling), "--remove"); err != nil {
		t.Fatal(err)
	}

	out := mapIn(t, dataDir, repo, dependent)
	want := fmt.Sprintf("\n    ○ #%d Later work (waits on #%d)\n", dependent, root)
	if !strings.Contains(out, want) {
		t.Errorf("ticket did not move back under its deepest remaining parent after removal:\n%s\nwant line:%s", out, want)
	}
}

func TestMapKeepsDependentsUnderEarlierBranches(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	// Reproduce tickets 298-307 from the report as IDs 1-10. Ticket 9 must
	// remain a child of ticket 6 even when another branch intervenes.
	dependencies := [][]int64{
		nil, {1}, {2}, {1}, {3, 4}, {5}, {4}, {6, 7}, {6}, {7, 8, 9},
	}
	s := testfix.OpenStore(t, dataDir)
	for i, blockers := range dependencies {
		id, err := ticketIn(t, dataDir, repo, fmt.Sprintf("Task %d", i+1), "")
		if err != nil {
			t.Fatal(err)
		}
		if err := s.AddDependencies(id, blockers...); err != nil {
			t.Fatal(err)
		}
	}

	want := "0 of 10 done\n" +
		"○ #1 Task 1\n" +
		"  ○ #2 Task 2\n" +
		"    ○ #3 Task 3\n" +
		"      ○ #5 Task 5 (waits on #4)\n" +
		"        ○ #6 Task 6\n" +
		"          ○ #8 Task 8 (waits on #7)\n" +
		"          ○ #9 Task 9\n" +
		"            ○ #10 Task 10 (waits on #7 #8)\n" +
		"  ○ #4 Task 4\n" +
		"    ○ #7 Task 7\n"
	for _, id := range []int64{1, 6, 9, 10} {
		if got := mapIn(t, dataDir, repo, id); got != want {
			t.Errorf("dg map %d wrote:\n%s\nwant:\n%s", id, got, want)
		}
	}
}

func TestMapFromEachPartOfAChainIsTheSame(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	first, second := twoTickets(t, dataDir, repo)
	third, err := ticketIn(t, dataDir, repo, "Deploy it", "")
	if err != nil {
		t.Fatal(err)
	}
	s := testfix.OpenStore(t, dataDir)
	if err := s.AddDependencies(second, first); err != nil {
		t.Fatal(err)
	}
	if err := s.AddDependencies(third, second); err != nil {
		t.Fatal(err)
	}

	want := mapIn(t, dataDir, repo, first)
	for _, id := range []int64{second, third} {
		if got := mapIn(t, dataDir, repo, id); got != want {
			t.Errorf("dg map %d differs:\n%s\nwant:\n%s", id, got, want)
		}
	}
}

func TestMapKeepsComponentsApartAndCountsDone(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	first, second := twoTickets(t, dataDir, repo)
	third, err := ticketIn(t, dataDir, repo, "Separate work", "")
	if err != nil {
		t.Fatal(err)
	}
	s := testfix.OpenStore(t, dataDir)
	finishIn(t, s, first)
	if err := s.ChangeStatus(first, store.Done); err != nil {
		t.Fatal(err)
	}
	if err := s.AddDependencies(second, first); err != nil {
		t.Fatal(err)
	}

	out := mapIn(t, dataDir, repo, second)
	if !strings.HasPrefix(out, "1 of 2 done\n") {
		t.Errorf("map count is wrong:\n%s", out)
	}
	if strings.Contains(out, fmt.Sprintf("#%d", third)) {
		t.Errorf("separate component is in map:\n%s", out)
	}
}

func TestMapRefusesACycle(t *testing.T) {
	tickets := map[int64]mappedTicket{
		1: {Ticket: store.Ticket{ID: 1}, DependsOn: []int64{2}},
		2: {Ticket: store.Ticket{ID: 2}, DependsOn: []int64{1}},
	}
	_, err := topologicalMap(tickets)
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Errorf("topologicalMap error = %v, want cycle error", err)
	}
}

func TestMapOfAnUnlinkedTicket(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	id, err := ticketIn(t, dataDir, repo, "Alone", "")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := mapIn(t, dataDir, repo, id), "0 of 1 done\n○ #1 Alone\n"; got != want {
		t.Errorf("dg map wrote %q, want %q", got, want)
	}
}

func TestMapWithNoIDUsesTheHeadOfReady(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	first, second := twoTickets(t, dataDir, repo)
	s := testfix.OpenStore(t, dataDir)
	for _, id := range []int64{first, second} {
		if _, err := s.Claim(id, fmt.Sprintf("delegator/%d", id), testAgentID); err != nil {
			t.Fatal(err)
		}
		if err := s.FinishTicket(id, "", ""); err != nil {
			t.Fatal(err)
		}
	}

	out, err := runIn(t, dataDir, repo, "map")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, fmt.Sprintf("#%d", first)) || strings.Contains(out, fmt.Sprintf("#%d", second)) {
		t.Errorf("dg map did not use first READY ticket:\n%s", out)
	}
}

func TestTopologicalMapOrdersBlockersFirst(t *testing.T) {
	tickets := map[int64]mappedTicket{
		3: {Ticket: store.Ticket{ID: 3}, DependsOn: []int64{1, 2}},
		2: {Ticket: store.Ticket{ID: 2}},
		1: {Ticket: store.Ticket{ID: 1}},
	}
	got, err := topologicalMap(tickets)
	if err != nil {
		t.Fatal(err)
	}
	if want := []int64{1, 2, 3}; !slices.Equal(mapIDs(got), want) {
		t.Errorf("map order = %v, want %v", mapIDs(got), want)
	}
	if _, err := mapTickets(testfix.OpenStore(t, t.TempDir()), 1); !errors.Is(err, store.ErrNoTicket) {
		t.Errorf("missing ticket error = %v, want ErrNoTicket", err)
	}
}

func TestMermaidMapRendersAChain(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	first, second := twoTickets(t, dataDir, repo)
	third, err := ticketIn(t, dataDir, repo, "Deploy it", "")
	if err != nil {
		t.Fatal(err)
	}
	s := testfix.OpenStore(t, dataDir)
	if err := s.AddDependencies(second, first); err != nil {
		t.Fatal(err)
	}
	if err := s.AddDependencies(third, second); err != nil {
		t.Fatal(err)
	}

	want := "flowchart TD\n" +
		"    ticket1[\"#1 Remove staging infrastructure\"]:::queued\n" +
		"    ticket2[\"#2 Add rate limiting\"]:::queued\n" +
		"    ticket3[\"#3 Deploy it\"]:::queued\n" +
		"    ticket1 --> ticket2\n" +
		"    ticket2 --> ticket3\n"
	if got := mermaidMapIn(t, dataDir, repo, second); got != want {
		t.Errorf("dg map --mermaid wrote:\n%s\nwant:\n%s", got, want)
	}
}

func TestMermaidMapRendersEveryEdgeOfAFork(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	first, second := twoTickets(t, dataDir, repo)
	third, err := ticketIn(t, dataDir, repo, "Add checks", "")
	if err != nil {
		t.Fatal(err)
	}
	s := testfix.OpenStore(t, dataDir)
	if err := s.AddDependencies(second, first); err != nil {
		t.Fatal(err)
	}
	if err := s.AddDependencies(third, first); err != nil {
		t.Fatal(err)
	}

	out := mermaidMapIn(t, dataDir, repo, first)
	for _, edge := range []string{"ticket1 --> ticket2", "ticket1 --> ticket3"} {
		if strings.Count(out, edge) != 1 {
			t.Errorf("Mermaid map does not have one %q edge:\n%s", edge, out)
		}
	}
}

func TestMermaidMapQuotesAndEscapesLabels(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	id, err := ticketIn(t, dataDir, repo, `Render [the "quoted"] title`, "")
	if err != nil {
		t.Fatal(err)
	}

	want := "flowchart TD\n" +
		`    ticket1["#1 Render [the &quot;quoted&quot;] title"]:::queued` + "\n"
	if got := mermaidMapIn(t, dataDir, repo, id); got != want {
		t.Errorf("dg map --mermaid wrote %q, want %q", got, want)
	}
}

func TestMermaidMapUsesTheTextMapComponent(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	first, second := twoTickets(t, dataDir, repo)
	separate, err := ticketIn(t, dataDir, repo, "Separate work", "")
	if err != nil {
		t.Fatal(err)
	}
	s := testfix.OpenStore(t, dataDir)
	if err := s.AddDependencies(second, first); err != nil {
		t.Fatal(err)
	}

	textMap := mapIn(t, dataDir, repo, second)
	mermaidMap := mermaidMapIn(t, dataDir, repo, second)
	for _, id := range []int64{first, second, separate} {
		name := fmt.Sprintf("#%d ", id)
		if strings.Contains(textMap, name) != strings.Contains(mermaidMap, name) {
			t.Errorf("ticket %d differs between maps:\ntext:\n%s\nMermaid:\n%s", id, textMap, mermaidMap)
		}
	}
}

func TestMermaidMapCarriesStatusAsAClass(t *testing.T) {
	tickets := []mappedTicket{
		{Ticket: store.Ticket{ID: 1, Title: "Done", Status: store.Done}},
		{Ticket: store.Ticket{ID: 2, Title: "Running", Status: store.Running}},
		{Ticket: store.Ticket{ID: 3, Title: "Ready", Status: store.Ready}},
		{Ticket: store.Ticket{ID: 4, Title: "Queued", Status: store.Queued}},
	}
	var out strings.Builder
	writeMermaid(&out, tickets)
	for _, status := range []store.TicketStatus{store.Done, store.Running, store.Ready, store.Queued} {
		if got := strings.Count(out.String(), ":::"+string(status)); got != 1 {
			t.Errorf("class %q occurs %d times:\n%s", status, got, out.String())
		}
	}
}

func TestMapRPCResultHoldsTheWholeComponentInOrder(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	first, second := twoTickets(t, dataDir, repo)
	third, err := ticketIn(t, dataDir, repo, "Deploy it", "")
	if err != nil {
		t.Fatal(err)
	}
	separate, err := ticketIn(t, dataDir, repo, "Separate work", "")
	if err != nil {
		t.Fatal(err)
	}
	s := testfix.OpenStore(t, dataDir)
	if err := s.AddDependencies(second, first); err != nil {
		t.Fatal(err)
	}
	if err := s.AddDependencies(third, first, second); err != nil {
		t.Fatal(err)
	}
	claimIn(t, s, second)

	want := mapJSON{Tickets: []mapTicketJSON{
		{ID: first, Title: "Remove staging infrastructure", Status: "queued", Project: repo,
			DependsOn: []int64{}, Actions: dependActions(first, store.Queued)},
		{ID: second, Title: "Add rate limiting", Status: "running", Project: repo,
			DependsOn: []int64{first}, Actions: dependActions(second, store.Running)},
		{ID: third, Title: "Deploy it", Status: "queued", Project: repo,
			DependsOn: []int64{first, second}, Actions: dependActions(third, store.Queued)},
	}}
	if got := rpcMapIn(t, dataDir, repo, third); !reflect.DeepEqual(got, want) {
		t.Errorf("map result = %#v, want %#v", got, want)
	}
	for _, ticket := range rpcMapIn(t, dataDir, repo, third).Tickets {
		if ticket.ID == separate {
			t.Errorf("map result holds the separate component: %#v", ticket)
		}
	}
}

func TestMapRPCResultHoldsADoneDependency(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	first, second := twoTickets(t, dataDir, repo)
	s := testfix.OpenStore(t, dataDir)
	finishIn(t, s, first)
	if err := s.ChangeStatus(first, store.Done); err != nil {
		t.Fatal(err)
	}
	if err := s.AddDependencies(second, first); err != nil {
		t.Fatal(err)
	}

	want := mapJSON{Tickets: []mapTicketJSON{
		{ID: first, Title: "Remove staging infrastructure", Status: "done", Project: repo,
			DependsOn: []int64{}, Actions: dependActions(first, store.Done)},
		{ID: second, Title: "Add rate limiting", Status: "queued", Project: repo,
			DependsOn: []int64{first}, Actions: dependActions(second, store.Queued)},
	}}
	if got := rpcMapIn(t, dataDir, repo, second); !reflect.DeepEqual(got, want) {
		t.Errorf("map result = %#v, want %#v", got, want)
	}
}

// The desktop app opens a dependency menu on every node of the map, so each
// node of the result answers for itself and the app keeps no status rule.
func TestMapReportsDependAvailableForAQueuedTicket(t *testing.T) {
	dataDir := t.TempDir()
	id, repo := ticketWithStatus(t, dataDir, store.Queued)

	assertDependAction(t, dataDir, repo, id, actionJSON{Applicable: true, Available: true})
}

// A ticket that has left the queue is still waiting on its prerequisites, so
// the menu stays and the person reads why it refuses the change.
func TestMapRefusesDependWithTheWordsOfTheCommand(t *testing.T) {
	for _, status := range []store.TicketStatus{store.Running, store.Ready, store.Failed} {
		t.Run(string(status), func(t *testing.T) {
			dataDir := t.TempDir()
			id, repo := ticketWithStatus(t, dataDir, status)
			refusal := dependErrorIn(t, dataDir, repo, id)

			assertDependAction(t, dataDir, repo, id,
				actionJSON{Applicable: true, Available: false, Reason: &refusal})
		})
	}
}

// A done or a cancelled ticket waits for nothing any more, so the app leaves
// the menu out rather than offering a control that can never run.
func TestMapReportsDependInapplicableForAClosedTicket(t *testing.T) {
	for _, status := range []store.TicketStatus{store.Done, store.Cancelled} {
		t.Run(string(status), func(t *testing.T) {
			dataDir := t.TempDir()
			id, repo := ticketWithStatus(t, dataDir, status)
			refusal := dependErrorIn(t, dataDir, repo, id)

			assertDependAction(t, dataDir, repo, id,
				actionJSON{Applicable: false, Available: false, Reason: &refusal})
		})
	}
}

func TestRPCDiscoverMapResultContract(t *testing.T) {
	document := rpcDiscover(t)
	descriptor := openRPCMethodNamed(t, document, "map").Result
	if !strings.Contains(descriptor.Description, "dependency graph") ||
		strings.Contains(descriptor.Description, "not yet exposed") {
		t.Errorf("map result description = %q, want the structured result", descriptor.Description)
	}
	validator := openRPCResultValidator(t, document, "map")

	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	first, second := twoTickets(t, dataDir, repo)
	s := testfix.OpenStore(t, dataDir)
	if err := s.AddDependencies(second, first); err != nil {
		t.Fatal(err)
	}
	actual := rpcDocument(t, dataDir, repo, "map", fmt.Sprint(second))
	if err := validator.Validate(actual); err != nil {
		t.Fatalf("map result does not satisfy its advertised schema: %v", err)
	}

	encoded, err := json.Marshal(actual)
	if err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []any{nil, false, "", []any{}} {
		if err := validator.Validate(invalid); err == nil {
			t.Errorf("map schema accepts %#v", invalid)
		}
	}
	for _, test := range []struct {
		name   string
		change func(map[string]any)
	}{
		{"no tickets member", func(result map[string]any) { delete(result, "tickets") }},
		{"nonpositive identifier", func(result map[string]any) {
			result["tickets"].([]any)[0].(map[string]any)["id"] = float64(0)
		}},
		{"unknown status", func(result map[string]any) {
			result["tickets"].([]any)[0].(map[string]any)["status"] = "waiting"
		}},
		{"null dependencies", func(result map[string]any) {
			result["tickets"].([]any)[0].(map[string]any)["depends_on"] = nil
		}},
		{"missing title", func(result map[string]any) {
			delete(result["tickets"].([]any)[0].(map[string]any), "title")
		}},
		{"missing actions", func(result map[string]any) {
			delete(result["tickets"].([]any)[0].(map[string]any), "actions")
		}},
		{"missing the depend action", func(result map[string]any) {
			actions := result["tickets"].([]any)[0].(map[string]any)["actions"].(map[string]any)
			delete(actions, "depend")
		}},
		{"depend without a reason", func(result map[string]any) {
			actions := result["tickets"].([]any)[0].(map[string]any)["actions"].(map[string]any)
			actions["depend"] = map[string]any{"applicable": true, "available": true}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var result map[string]any
			if err := json.Unmarshal(encoded, &result); err != nil {
				t.Fatal(err)
			}
			test.change(result)
			if err := validator.Validate(result); err == nil {
				t.Error("invalid result satisfies the schema")
			}
		})
	}
}
