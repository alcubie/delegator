package cli

import (
	"errors"
	"fmt"
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

func TestMapMovesTicketUpAfterRemovingItsCurrentParent(t *testing.T) {
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
	want := fmt.Sprintf("\n  ○ #%d Later work (waits on #%d)\n", dependent, branch)
	if !strings.Contains(out, want) {
		t.Errorf("ticket did not move back under its open parent after removal:\n%s\nwant line:%s", out, want)
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
		if err := s.FinishTicket(id, ""); err != nil {
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
