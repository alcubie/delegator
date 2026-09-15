package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
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

// editorRefused puts an editor in place of the one of the person that fails the
// test when a command starts it. A caller that hands dg the text it wants has
// no editor to open, so a form that takes a flag must open none.
func editorRefused(t *testing.T) {
	t.Helper()
	setEditor(t, func(path string) error {
		t.Errorf("the command opened the editor on %s, want no editor", path)
		return nil
	})
}

// editForm is one form of dg edit: a name for the subtest and the flags that
// tell the command where its new text comes from.
type editForm struct {
	name  string
	flags []string
}

// formTitle and formProse are the text that a form of dg edit carries. They are
// not the text of the ticket that a test makes, so a form that wrote when it
// was to write nothing is visible in the ticket.
const (
	formTitle = "A title from the flag"
	formProse = "Prose from the flag.\n"
)

// editTextForms returns each form of dg edit that carries the new text of the
// ticket in a flag. The file that --body-file names is a file of the test.
func editTextForms(t *testing.T) []editForm {
	t.Helper()
	path := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(path, []byte(formProse), filePerm); err != nil {
		t.Fatal(err)
	}
	return []editForm{
		{"a title", []string{"--title", formTitle}},
		{"a body", []string{"--body", formProse}},
		{"a body file", []string{"--body-file", path}},
	}
}

// editForms returns each form of dg edit: the three that carry the text of the
// caller, and the editor of the person.
func editForms(t *testing.T) []editForm {
	t.Helper()
	return append(editTextForms(t), editForm{"the editor", []string{"--editor"}})
}

// editIn runs dg edit on one ticket, with the flags of the form under test.
func editIn(t *testing.T, dataDir, workDir string, id int64, flags ...string) (string, error) {
	t.Helper()
	return runIn(t, dataDir, workDir, append([]string{"edit", fmt.Sprint(id)}, flags...)...)
}

// The editor gets the ticket in the form that dg ticket with no arguments
// takes, and what the person leaves there goes back to the column and the file
// it came from.
func TestEditWritesBackWhatTheEditorGave(t *testing.T) {
	dataDir := t.TempDir()
	id, repo := editedTicket(t, dataDir, "Remove staging infrastructure", "Remove the staging app.\n")
	gave := editedTo(t, "Remove the staging volume\n\nRemove the volume of the staging app.\n")

	out, err := editIn(t, dataDir, repo, id, "--editor")
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

// A caller that is not a person, such as the desktop GUI, has the new title
// already and has no editor, so --title takes it. The prose is a file of its
// own and stays as it was.
func TestEditWithATitleSetsTheTitleAndLeavesTheProse(t *testing.T) {
	dataDir := t.TempDir()
	id, repo := editedTicket(t, dataDir, "Remove staging infrastructure", "Remove the staging app.\n")
	editorRefused(t)
	when := ageProse(t, dataDir, id)

	out, err := editIn(t, dataDir, repo, id, "--title", "Remove the staging volume")
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Errorf("dg edit wrote %q, want nothing", out)
	}

	if got := testfix.ReadTicket(t, dataDir, id).Title; got != "Remove the staging volume" {
		t.Errorf("title = %q, want %q", got, "Remove the staging volume")
	}
	if got := proseWritten(t, dataDir, id); !got.Equal(when) {
		t.Errorf("the file of prose was written at %s, want it not written since %s", got, when)
	}
	if want := "Remove the staging app.\n"; proseOf(t, dataDir) != want {
		t.Errorf("the prose is %q, want %q", proseOf(t, dataDir), want)
	}
}

// A title that holds no word is a ticket that a person cannot find again,
// whether it came from the editor or from the flag, so the two give the one
// error. The ticket stays as it was, prose and all, because half an edit is a
// ticket that nobody wrote.
func TestEditWithAnEmptyTitleIsRefused(t *testing.T) {
	tests := []struct {
		name  string
		flags []string
	}{
		{"an empty title", []string{"--title", ""}},
		{"a title of spaces", []string{"--title", "   "}},
		{"an empty title beside a body", []string{"--title", "", "--body", formProse}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dataDir := t.TempDir()
			id, repo := editedTicket(t, dataDir, "Remove staging infrastructure", "Remove the staging app.\n")
			editorRefused(t)
			when := ageProse(t, dataDir, id)

			if _, err := editIn(t, dataDir, repo, id, test.flags...); !errors.Is(err, ErrNoTitle) {
				t.Fatalf("err = %v, want ErrNoTitle", err)
			}

			if got := testfix.ReadTicket(t, dataDir, id).Title; got != "Remove staging infrastructure" {
				t.Errorf("title = %q, want %q", got, "Remove staging infrastructure")
			}
			if got := proseWritten(t, dataDir, id); !got.Equal(when) {
				t.Errorf("the file of prose was written at %s, want it not written since %s", got, when)
			}
		})
	}
}

// The standard input of dg rpc is taken by the protocol it speaks, so a caller
// there has no - to read the prose from and hands it over as text. The title is
// a column of its own and stays as it was.
func TestEditWithABodySetsTheProseAndLeavesTheTitle(t *testing.T) {
	dataDir := t.TempDir()
	id, repo := editedTicket(t, dataDir, "Remove staging infrastructure", "Remove the staging app.\n")
	editorRefused(t)

	out, err := editIn(t, dataDir, repo, id, "--body", "Remove the volume of the staging app.\n")
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Errorf("dg edit wrote %q, want nothing", out)
	}

	if want := "Remove the volume of the staging app.\n"; proseOf(t, dataDir) != want {
		t.Errorf("the prose is %q, want %q", proseOf(t, dataDir), want)
	}
	if got := testfix.ReadTicket(t, dataDir, id).Title; got != "Remove staging infrastructure" {
		t.Errorf("title = %q, want %q", got, "Remove staging infrastructure")
	}
}

// A prose that is long meets the limit of the command line, so --body-file
// names a file that holds it, and it is the same word and reads the same way as
// --body-file of dg ticket.
func TestEditWithABodyFileSetsTheProseFromTheFile(t *testing.T) {
	dataDir := t.TempDir()
	id, repo := editedTicket(t, dataDir, "Remove staging infrastructure", "Remove the staging app.\n")
	editorRefused(t)
	const body = "Remove the volume of the staging app, and the records of the DNS.\n"
	path := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(path, []byte(body), filePerm); err != nil {
		t.Fatal(err)
	}

	out, err := editIn(t, dataDir, repo, id, "--body-file", path)
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Errorf("dg edit wrote %q, want nothing", out)
	}

	if proseOf(t, dataDir) != body {
		t.Errorf("the prose is %q, want %q", proseOf(t, dataDir), body)
	}
	if got := testfix.ReadTicket(t, dataDir, id).Title; got != "Remove staging infrastructure" {
		t.Errorf("title = %q, want %q", got, "Remove staging infrastructure")
	}
}

// A path of - is the standard input, as it is for dg ticket and for
// git commit -F, so a program that holds the prose can pipe it in.
func TestEditWithABodyFileOfADashReadsTheStandardInput(t *testing.T) {
	dataDir := t.TempDir()
	id, repo := editedTicket(t, dataDir, "Remove staging infrastructure", "Remove the staging app.\n")
	editorRefused(t)
	const body = "Remove the volume of the staging app.\n"

	if _, err := runInWithStdin(t, dataDir, repo, body,
		"edit", fmt.Sprint(id), "--body-file", "-"); err != nil {
		t.Fatal(err)
	}

	if proseOf(t, dataDir) != body {
		t.Errorf("the prose is %q, want %q", proseOf(t, dataDir), body)
	}
}

// The prose has one source, here as in dg ticket. With both flags dg cannot
// tell which of the two the person meant, so it refuses and writes nothing.
func TestEditWithABodyAndABodyFile(t *testing.T) {
	dataDir := t.TempDir()
	id, repo := editedTicket(t, dataDir, "Remove staging infrastructure", "Remove the staging app.\n")
	editorRefused(t)
	path := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(path, []byte("From the file.\n"), filePerm); err != nil {
		t.Fatal(err)
	}
	when := ageProse(t, dataDir, id)

	_, err := editIn(t, dataDir, repo, id, "--body", "From the flag.", "--body-file", path)
	if err == nil {
		t.Fatal("dg edit gave no error for a body and a body file")
	}
	if want := "dg edit takes the prose from --body or from --body-file, and got both"; err.Error() != want {
		t.Errorf("err = %q, want %q", err, want)
	}
	if got := proseWritten(t, dataDir, id); !got.Equal(when) {
		t.Errorf("the file of prose was written at %s, want it not written since %s", got, when)
	}
}

// A --body-file that names nothing is a prose the person wrote and dg cannot
// find, so the error holds the path and the ticket stays as it was.
func TestEditWithABodyFileThatIsNotThere(t *testing.T) {
	dataDir := t.TempDir()
	id, repo := editedTicket(t, dataDir, "Remove staging infrastructure", "Remove the staging app.\n")
	editorRefused(t)
	missing := filepath.Join(t.TempDir(), "body.md")
	when := ageProse(t, dataDir, id)

	_, err := editIn(t, dataDir, repo, id, "--body-file", missing)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want a path that is not there", err)
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("the error is %q, and does not name %q", err, missing)
	}
	if got := proseWritten(t, dataDir, id); !got.Equal(when) {
		t.Errorf("the file of prose was written at %s, want it not written since %s", got, when)
	}
}

// A caller that has both has one command to write both, so a ticket is never
// half of one edit and half of the one before it.
func TestEditWithATitleAndAProseSetsBoth(t *testing.T) {
	path := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(path, []byte("Remove the volume of the staging app.\n"), filePerm); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		flags []string
	}{
		{"a body", []string{"--body", "Remove the volume of the staging app.\n"}},
		{"a body file", []string{"--body-file", path}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dataDir := t.TempDir()
			id, repo := editedTicket(t, dataDir, "Remove staging infrastructure", "Remove the staging app.\n")
			editorRefused(t)

			flags := append([]string{"--title", "Remove the staging volume"}, test.flags...)
			if _, err := editIn(t, dataDir, repo, id, flags...); err != nil {
				t.Fatal(err)
			}

			if got := testfix.ReadTicket(t, dataDir, id).Title; got != "Remove the staging volume" {
				t.Errorf("title = %q, want %q", got, "Remove the staging volume")
			}
			if want := "Remove the volume of the staging app.\n"; proseOf(t, dataDir) != want {
				t.Errorf("the prose is %q, want %q", proseOf(t, dataDir), want)
			}
		})
	}
}

// The editor is one form of the command among four, and a person who wants it
// says so. A command that opened it by default would open it for a caller that
// meant a flag and typed it wrong, and wait for an editor nobody is at.
func TestEditWithNoFlagIsRefused(t *testing.T) {
	dataDir := t.TempDir()
	id, repo := editedTicket(t, dataDir, "Remove staging infrastructure", "Remove the staging app.\n")
	editorRefused(t)

	_, err := editIn(t, dataDir, repo, id)
	if err == nil {
		t.Fatal("dg edit gave no error for a command with no flag")
	}
	for _, flag := range []string{"--title", "--body", "--body-file", "--editor"} {
		if !strings.Contains(err.Error(), flag) {
			t.Errorf("the error is %q, and does not name %s", err, flag)
		}
	}
}

// The editor holds the title and the prose as one text, so a flag that holds
// one of them beside it is a second answer to the question the editor asks.
func TestEditWithTheEditorAndAFlagIsRefused(t *testing.T) {
	for _, form := range editTextForms(t) {
		t.Run(form.name, func(t *testing.T) {
			dataDir := t.TempDir()
			id, repo := editedTicket(t, dataDir, "Remove staging infrastructure", "Remove the staging app.\n")
			editorRefused(t)
			when := ageProse(t, dataDir, id)

			_, err := editIn(t, dataDir, repo, id, append([]string{"--editor"}, form.flags...)...)
			if err == nil {
				t.Fatalf("dg edit gave no error for --editor beside %s", form.name)
			}
			for _, flag := range []string{"--title", "--body", "--body-file"} {
				if !strings.Contains(err.Error(), flag) {
					t.Errorf("the error is %q, and does not name %s", err, flag)
				}
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

// An editor that closes with the text as it was changes nothing. The file is
// the file of the person, and a write of the same text is still a write of
// text that delegator holds in memory over a file that it does not own.
func TestEditWithNoChangeWritesNothing(t *testing.T) {
	dataDir := t.TempDir()
	id, repo := editedTicket(t, dataDir, "Remove staging infrastructure", "Remove the staging app.\n")
	editedUnchanged(t)
	when := ageProse(t, dataDir, id)

	if _, err := editIn(t, dataDir, repo, id, "--editor"); err != nil {
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

			if _, err := editIn(t, dataDir, repo, id, "--editor"); !errors.Is(err, ErrNoTitle) {
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
	for _, form := range editForms(t) {
		t.Run(form.name, func(t *testing.T) {
			dataDir := t.TempDir()
			id, repo := editedTicket(t, dataDir, "Remove staging infrastructure", "Remove the staging app.\n")
			if _, err := testfix.OpenStore(t, dataDir).Claim(id, "delegator/1-remove-staging-infrastructure"); err != nil {
				t.Fatal(err)
			}
			testfix.LiveRun(t, dataDir, id)
			editorRefused(t)
			when := ageProse(t, dataDir, id)

			_, err := editIn(t, dataDir, repo, id, form.flags...)
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
		})
	}
}

// A ticket the queue does not hold is a ticket that a run has already read, so
// only a ticket that waits can be changed.
func TestEditATicketThatIsNotInTheQueueIsRefused(t *testing.T) {
	forms := editForms(t)
	for _, state := range []store.TicketStatus{store.Ready, store.Failed, store.Done, store.Cancelled} {
		for _, form := range forms {
			t.Run(string(state)+" with "+form.name, func(t *testing.T) {
				dataDir := t.TempDir()
				id, repo := editedTicket(t, dataDir, "Remove staging infrastructure", "Remove the staging app.\n")
				s := testfix.OpenStore(t, dataDir)
				for _, step := range statePath[state] {
					if err := s.ChangeStatus(id, step); err != nil {
						t.Fatal(err)
					}
				}
				editorRefused(t)
				when := ageProse(t, dataDir, id)

				_, err := editIn(t, dataDir, repo, id, form.flags...)
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
}
