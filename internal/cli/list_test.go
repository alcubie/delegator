package cli

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/alcubie/delegator/internal/store"
	"github.com/alcubie/delegator/internal/testfix"
)

// listRows runs dg list and returns each line of what it wrote.
func listRows(t *testing.T, dataDir, workDir string, args ...string) []string {
	t.Helper()
	out, err := runIn(t, dataDir, workDir, append([]string{"list"}, args...)...)
	if err != nil {
		t.Fatal(err)
	}
	if out == "" {
		return nil
	}
	return strings.Split(strings.TrimRight(out, "\n"), "\n")
}

// rowOf returns the row of one ticket by the title in it, so a test names the
// ticket it examines rather than a place in the list that another ticket
// would move.
func rowOf(t *testing.T, rows []string, title string) string {
	t.Helper()
	for _, row := range rows {
		if strings.Contains(row, title) {
			return row
		}
	}
	t.Fatalf("no row holds the title %q:\n%s", title, strings.Join(rows, "\n"))
	return ""
}

// eachStatus makes a data directory that holds one ticket in every status,
// and returns the repository and the id of each ticket by its status. Each
// title names the status, so a test finds the row of one ticket by what it is.
//
// The person accepted the done ticket two days ago, so the window of DONE has
// passed it and the inbox no longer shows it.
func eachStatus(t *testing.T, dataDir string) (string, map[store.TicketStatus]int64) {
	t.Helper()
	repo := testfix.Repo(t, repoBranch)
	s := testfix.OpenStore(t, dataDir)
	ids := map[store.TicketStatus]int64{}
	for _, status := range []store.TicketStatus{
		store.Queued, store.Running, store.Ready, store.Failed, store.Done, store.Cancelled,
	} {
		id := queuedIn(t, s, repo, "the "+string(status)+" ticket")
		ids[status] = id
		switch status {
		case store.Running:
			claimIn(t, s, id)
		case store.Ready:
			finishIn(t, s, id)
		case store.Failed:
			claimIn(t, s, id)
			changeStatusIn(t, s, id, store.Failed)
		case store.Done:
			finishIn(t, s, id)
			changeStatusIn(t, s, id, store.Done)
			testfix.AgeAcceptance(t, dataDir, id, 48*time.Hour)
		case store.Cancelled:
			changeStatusIn(t, s, id, store.Cancelled)
		}
	}
	return repo, ids
}

// claimIn takes a queued ticket for a run, which is what a supervisor does.
func claimIn(t *testing.T, s *store.Store, id int64) {
	t.Helper()
	if _, err := s.Claim(id, fmt.Sprintf("delegator/%d-a-title", id)); err != nil {
		t.Fatal(err)
	}
}

// changeStatusIn moves a ticket to one status and stops the test if the store
// refuses the change.
func changeStatusIn(t *testing.T, s *store.Store, id int64, status store.TicketStatus) {
	t.Helper()
	if err := s.ChangeStatus(id, status); err != nil {
		t.Fatal(err)
	}
}

// One row for each ticket: the id, the name of the project, the title and the
// status, in the columns that the inbox puts them in. A title that reaches the
// status is cut where the title of an inbox row is cut, so a person who reads
// one list reads the other.
func TestWriteListWritesOneRowForEachTicket(t *testing.T) {
	var out bytes.Buffer
	writeList(&out, []store.OpenTicket{
		{ID: 4, Project: "/projects/one", Title: "Remove the staging app", Status: store.Queued},
		{ID: 12, Project: "/projects/other", Title: "Fix the query that broke the build", Status: store.Cancelled},
		{ID: 113, Project: "/projects/one", Status: store.Done,
			Title: "Read the log of a run that a person cannot see any more"},
	})

	wantLines(t, strings.Split(strings.TrimRight(out.String(), "\n"), "\n"), []string{
		"   4 one    Remove the staging app                           queued",
		"  12 other  Fix the query that broke the build            cancelled",
		" 113 one    Read the log of a run that a person cannot see a…  done",
	})
}

// The list holds every status, and the one the inbox cannot show is the point
// of the command: a ticket the person accepted two days ago is out of the
// window of DONE, and nothing else says it was ever there.
func TestListHoldsATicketOfEveryStatus(t *testing.T) {
	dataDir := t.TempDir()
	repo, ids := eachStatus(t, dataDir)

	got := listRows(t, dataDir, repo)
	if len(got) != len(ids) {
		t.Fatalf("dg list wrote %d rows, want %d:\n%s", len(got), len(ids), strings.Join(got, "\n"))
	}
	for status, id := range ids {
		row := rowOf(t, got, "the "+string(status)+" ticket")
		if got := strings.Fields(row)[0]; got != fmt.Sprint(id) {
			t.Errorf("the row of ticket %d begins with %q, want its id", id, got)
		}
		if !strings.HasSuffix(row, string(status)) {
			t.Errorf("the row of ticket %d is %q, want the status %q at the end", id, row, status)
		}
	}

	box, err := runIn(t, dataDir, repo)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(box, "the done ticket") {
		t.Errorf("the inbox still holds the done ticket, so dg list shows nothing it does not:\n%s", box)
	}
}

// The flag --project narrows the list to one project, and it names a
// directory as it does on dg ticket. A person who works in one repository
// wants the tickets of that repository.
func TestListTakesTheTicketsOfOneProject(t *testing.T) {
	dataDir := t.TempDir()
	here := testfix.Repo(t, repoBranch)
	elsewhere := testfix.Repo(t, "release")
	if _, err := runIn(t, dataDir, here, "ticket", "the ticket here", "--no-body"); err != nil {
		t.Fatal(err)
	}
	if _, err := runIn(t, dataDir, elsewhere, "ticket", "the ticket elsewhere", "--no-body"); err != nil {
		t.Fatal(err)
	}

	if both := listRows(t, dataDir, here); len(both) != 2 {
		t.Fatalf("dg list with no flag wrote %d rows, want both tickets:\n%s", len(both), strings.Join(both, "\n"))
	}
	got := listRows(t, dataDir, here, "--project", elsewhere)
	if len(got) != 1 || !strings.Contains(got[0], "the ticket elsewhere") {
		t.Errorf("dg list --project wrote:\n%s\nwant the one ticket of %s", strings.Join(got, "\n"), elsewhere)
	}
}

// A person who has made no ticket gets no list and no error. A line that said
// there were none would be the one line of output that a script reading the
// list would have to know about.
func TestListWithNoTicketsWritesNothing(t *testing.T) {
	out, err := runIn(t, t.TempDir(), testfix.Repo(t, repoBranch), "list")
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Errorf("dg list wrote %q with no tickets, want nothing", out)
	}
}

// The help is where a person looks for the command, and dg list exists
// because a person who wanted the whole list guessed at dg ticket list and
// made a ticket named list.
func TestHelpNamesList(t *testing.T) {
	out, err := runIn(t, t.TempDir(), testfix.Repo(t, repoBranch), "--help")
	if err != nil {
		t.Fatal(err)
	}
	var line string
	for _, l := range strings.Split(out, "\n") {
		if fields := strings.Fields(l); len(fields) > 0 && fields[0] == "list" {
			line = l
		}
	}
	if line == "" {
		t.Fatalf("the help names no command list:\n%s", out)
	}
	if !strings.Contains(line, "every ticket") {
		t.Errorf("the help line of dg list is %q, and does not say that it shows every ticket", line)
	}
}
