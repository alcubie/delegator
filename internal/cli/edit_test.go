package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/alcubie/delegator/internal/store"
	"github.com/alcubie/delegator/internal/testfix"
)

// editedTo puts an editor in place of the one of the person. It keeps the text
// that the command gave the editor, which the test reads through the pointer it
// returns, and writes text in its place, which is what a person who changes a
// ticket does.
func editedTo(t *testing.T, text string) *string {
	t.Helper()
	var gave string
	setEditor(t, func(path string) error {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		gave = string(data)
		return os.WriteFile(path, []byte(text), 0o600)
	})
	return &gave
}

// editedUnchanged puts an editor in place of the one of the person that writes
// back the text it got, as it was, which is what a person who opens a ticket
// and closes it does.
func editedUnchanged(t *testing.T) {
	t.Helper()
	setEditor(t, func(path string) error {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(path, data, 0o600)
	})
}

// editedTicket makes one ticket that waits in the queue, with prose, and
// returns its id and the repository of its project.
func editedTicket(t *testing.T, dataDir, title, body string) (int64, string) {
	t.Helper()
	repo := testfix.Repo(t, repoBranch)
	id, err := ticketIn(t, dataDir, repo, title, body)
	if err != nil {
		t.Fatal(err)
	}
	return id, repo
}

// ageProse moves the time of the file of prose of a ticket to testNow, and
// returns it. A file that a command writes takes the time of that write, so a
// time that is still testNow is a file that nothing wrote.
func ageProse(t *testing.T, dataDir string, id int64) time.Time {
	t.Helper()
	if err := os.Chtimes(proseFile(dataDir, id), testNow, testNow); err != nil {
		t.Fatal(err)
	}
	return testNow
}

// proseWritten returns the time that the file of prose of a ticket was last
// written.
func proseWritten(t *testing.T, dataDir string, id int64) time.Time {
	t.Helper()
	info, err := os.Stat(proseFile(dataDir, id))
	if err != nil {
		t.Fatal(err)
	}
	return info.ModTime()
}

// statePath holds each state that a ticket goes through to reach one state,
// because a ticket moves one state at a time.
var statePath = map[store.TicketStatus][]store.TicketStatus{
	store.Ready:     {store.Running, store.Ready},
	store.Failed:    {store.Running, store.Failed},
	store.Done:      {store.Running, store.Ready, store.Done},
	store.Cancelled: {store.Cancelled},
}

// editIn runs dg edit on one ticket.
func editIn(t *testing.T, dataDir, workDir string, id int64) (string, error) {
	t.Helper()
	return runIn(t, dataDir, workDir, "edit", fmt.Sprint(id))
}

// The editor gets the ticket in the form that dg ticket with no arguments
// takes, and what the person leaves there goes back to the column and the file
// it came from.
func TestEditWritesBackWhatTheEditorGave(t *testing.T) {
	dataDir := t.TempDir()
	id, repo := editedTicket(t, dataDir, "Remove staging infrastructure", "Remove the staging app.\n")
	gave := editedTo(t, "Remove the staging volume\n\nRemove the volume of the staging app.\n")

	out, err := editIn(t, dataDir, repo, id)
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Errorf("dg edit wrote %q, want nothing", out)
	}

	if want := "Remove staging infrastructure\n\nRemove the staging app.\n"; *gave != want {
		t.Errorf("the editor got %q, want %q", *gave, want)
	}
	if got := testfix.ReadTicket(t, dataDir, id).Title; got != "Remove the staging volume" {
		t.Errorf("title = %q, want %q", got, "Remove the staging volume")
	}
	if want := "Remove the volume of the staging app.\n"; proseOf(t, dataDir) != want {
		t.Errorf("the prose is %q, want %q", proseOf(t, dataDir), want)
	}

	shown, err := runIn(t, dataDir, repo, "show", fmt.Sprint(id))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(shown, "Remove the staging volume") {
		t.Errorf("dg show gave %q, want the new title in it", shown)
	}
	if !strings.Contains(shown, "Remove the volume of the staging app.") {
		t.Errorf("dg show gave %q, want the new prose in it", shown)
	}
}

// An editor that closes with the text as it was changes nothing. The file is
// the file of the person, and a write of the same text is still a write of
// text that delegator holds in memory over a file that it does not own.
func TestEditWithNoChangeWritesNothing(t *testing.T) {
	dataDir := t.TempDir()
	id, repo := editedTicket(t, dataDir, "Remove staging infrastructure", "Remove the staging app.\n")
	editedUnchanged(t)
	when := ageProse(t, dataDir, id)

	if _, err := editIn(t, dataDir, repo, id); err != nil {
		t.Fatal(err)
	}

	if got := proseWritten(t, dataDir, id); !got.Equal(when) {
		t.Errorf("the file of prose was written at %s, want it not written since %s", got, when)
	}
	if got := testfix.ReadTicket(t, dataDir, id).Title; got != "Remove staging infrastructure" {
		t.Errorf("title = %q, want %q", got, "Remove staging infrastructure")
	}
	if want := "Remove the staging app.\n"; proseOf(t, dataDir) != want {
		t.Errorf("the prose is %q, want %q", proseOf(t, dataDir), want)
	}
}

// A person who takes the first line away has given the ticket no title, which
// is the error that dg ticket gives for the same text. The ticket stays as it
// was, so the work of the person is not lost with the title.
func TestEditWithNoTitleIsRefused(t *testing.T) {
	tests := []struct {
		name string
		text string
	}{
		{"an empty file", ""},
		{"a newline only", "\n"},
		{"spaces and newlines", "   \n\n"},
		{"a body with no first line", "\n\nRemove the staging app.\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dataDir := t.TempDir()
			id, repo := editedTicket(t, dataDir, "Remove staging infrastructure", "Remove the staging app.\n")
			editedTo(t, test.text)
			when := ageProse(t, dataDir, id)

			if _, err := editIn(t, dataDir, repo, id); !errors.Is(err, ErrNoTitle) {
				t.Fatalf("err = %v, want ErrNoTitle", err)
			}

			if got := proseWritten(t, dataDir, id); !got.Equal(when) {
				t.Errorf("the file of prose was written at %s, want it not written since %s", got, when)
			}
			if got := testfix.ReadTicket(t, dataDir, id).Title; got != "Remove staging infrastructure" {
				t.Errorf("title = %q, want %q", got, "Remove staging infrastructure")
			}
			if want := "Remove the staging app.\n"; proseOf(t, dataDir) != want {
				t.Errorf("the prose is %q, want %q", proseOf(t, dataDir), want)
			}
		})
	}
}

// The agent read the ticket when its run started, so a change it cannot see
// leaves a report that answers a ticket which is not there any more. dg revise
// gives the work again, and the error says so.
func TestEditARunningTicketIsRefused(t *testing.T) {
	dataDir := t.TempDir()
	id, repo := editedTicket(t, dataDir, "Remove staging infrastructure", "Remove the staging app.\n")
	if _, err := testfix.OpenStore(t, dataDir).Claim(id, "delegator/1-remove-staging-infrastructure"); err != nil {
		t.Fatal(err)
	}
	testfix.LiveRun(t, dataDir, id)
	editedTo(t, "A title from the editor\n\nProse from the editor.\n")
	when := ageProse(t, dataDir, id)

	_, err := editIn(t, dataDir, repo, id)
	if err == nil {
		t.Fatal("dg edit gave no error for a ticket that is running")
	}
	if !strings.Contains(err.Error(), "dg revise") {
		t.Errorf("the error is %q, want dg revise named in it", err)
	}
	if got := proseWritten(t, dataDir, id); !got.Equal(when) {
		t.Errorf("the file of prose was written at %s, want it not written since %s", got, when)
	}
	if got := testfix.ReadTicket(t, dataDir, id).Title; got != "Remove staging infrastructure" {
		t.Errorf("title = %q, want %q", got, "Remove staging infrastructure")
	}
}

// A ticket the queue does not hold is a ticket that a run has already read, so
// only a ticket that waits can be changed.
func TestEditATicketThatIsNotInTheQueueIsRefused(t *testing.T) {
	for _, state := range []store.TicketStatus{store.Ready, store.Failed, store.Done, store.Cancelled} {
		t.Run(string(state), func(t *testing.T) {
			dataDir := t.TempDir()
			id, repo := editedTicket(t, dataDir, "Remove staging infrastructure", "Remove the staging app.\n")
			s := testfix.OpenStore(t, dataDir)
			for _, step := range statePath[state] {
				if err := s.ChangeStatus(id, step); err != nil {
					t.Fatal(err)
				}
			}
			editedTo(t, "A title from the editor\n\nProse from the editor.\n")
			when := ageProse(t, dataDir, id)

			_, err := editIn(t, dataDir, repo, id)
			if err == nil {
				t.Fatalf("dg edit gave no error for a ticket that is %s", state)
			}
			if !strings.Contains(err.Error(), string(state)) {
				t.Errorf("the error is %q, want the state %s named in it", err, state)
			}
			if got := proseWritten(t, dataDir, id); !got.Equal(when) {
				t.Errorf("the file of prose was written at %s, want it not written since %s", got, when)
			}
			if got := testfix.ReadTicket(t, dataDir, id).Title; got != "Remove staging infrastructure" {
				t.Errorf("title = %q, want %q", got, "Remove staging infrastructure")
			}
		})
	}
}
