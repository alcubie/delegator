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

// editedTo records the editor's initial text and replaces it with text,
// returning a pointer to the captured input.
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

// editedUnchanged installs an editor that writes its input back unchanged.
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

// editedTicket creates a queued ticket with a description and returns its ID
// and repository.
func editedTicket(t *testing.T, dataDir, title, body string) (int64, string) {
	t.Helper()
	repo := testfix.Repo(t, repoBranch)
	id, err := ticketIn(t, dataDir, repo, title, body)
	if err != nil {
		t.Fatal(err)
	}
	return id, repo
}

// ageProse sets the description mtime to testNow so a subsequent rewrite is
// detectable.
func ageProse(t *testing.T, dataDir string, id int64) time.Time {
	t.Helper()
	if err := os.Chtimes(proseFile(dataDir, id), testNow, testNow); err != nil {
		t.Fatal(err)
	}
	return testNow
}

// proseWritten returns the description file's modification time.
func proseWritten(t *testing.T, dataDir string, id int64) time.Time {
	t.Helper()
	info, err := os.Stat(proseFile(dataDir, id))
	if err != nil {
		t.Fatal(err)
	}
	return info.ModTime()
}

// statePath lists valid transitions for arranging each test status.
var statePath = map[store.TicketStatus][]store.TicketStatus{
	store.Ready:     {store.Running, store.Ready},
	store.Failed:    {store.Running, store.Failed},
	store.Done:      {store.Running, store.Ready, store.Done},
	store.Cancelled: {store.Cancelled},
}

// editorRefused fails if a text-flag command unexpectedly opens an editor.
func editorRefused(t *testing.T) {
	t.Helper()
	setEditor(t, func(path string) error {
		t.Errorf("the command opened the editor on %s, want no editor", path)
		return nil
	})
}

// editForm names an input method and its command flags.
type editForm struct {
	name  string
	flags []string
}

// Use text distinct from the fixture to detect unintended writes.
const (
	formTitle = "A title from the flag"
	formProse = "Prose from the flag.\n"
)

// editTextForms supplies flag-based input methods with a test-owned body
// file.
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

// editForms includes all flag and editor input methods.
func editForms(t *testing.T) []editForm {
	t.Helper()
	return append(editTextForms(t), editForm{"the editor", []string{"--editor"}})
}

// editIn runs dg edit on one ticket, with the flags of the form under test.
func editIn(t *testing.T, dataDir, workDir string, id int64, flags ...string) (string, error) {
	t.Helper()
	return runIn(t, dataDir, workDir, append([]string{"edit", fmt.Sprint(id)}, flags...)...)
}

// Editor input and output must use the same title/description split as ticket
// creation.
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

// Title-only edits must preserve the separate description file.
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

// Both editor and flag input must reject blank titles without partially
// changing the ticket.
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

// RPC owns stdin, so its callers supply description text directly.
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

// File input supports descriptions too large for command-line arguments.
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

// Combined title and description edits must update both parts together.
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

// Require explicit editor selection so malformed scripted calls cannot hang
// waiting for interactive input.
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

// Unchanged editor contents must not rewrite the description file.
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

// Reject removal of the title while preserving the existing ticket.
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

// Reject edits after a run may have read the ticket; otherwise its report
// could describe obsolete instructions.
func TestEditARunningTicketIsRefused(t *testing.T) {
	for _, form := range editForms(t) {
		t.Run(form.name, func(t *testing.T) {
			dataDir := t.TempDir()
			id, repo := editedTicket(t, dataDir, "Remove staging infrastructure", "Remove the staging app.\n")
			if _, err := testfix.OpenStore(t, dataDir).Claim(id, "delegator/1-remove-staging-infrastructure", testAgentID); err != nil {
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
