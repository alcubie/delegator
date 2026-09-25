package cli

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/alcubie/delegator/internal/testfix"
)

// setEditor replaces the editor until test cleanup.
func setEditor(t *testing.T, fn func(path string) error) {
	t.Helper()
	old := editor
	editor = fn
	t.Cleanup(func() { editor = old })
}

// withEditor installs an editor that writes the supplied text.
func withEditor(t *testing.T, text string) {
	t.Helper()
	setEditor(t, func(path string) error {
		return os.WriteFile(path, []byte(text), 0o600)
	})
}

// fakeEditor creates a program that writes text to its final path argument.
func fakeEditor(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-editor")
	script := "#!/bin/sh\nfor f; do :; done\nprintf '%s' " +
		"'" + text + "'" + " > \"$f\"\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

// echoEditor records argv to verify EDITOR values with arguments, such as
// "code --wait".
func echoEditor(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "echo-editor")
	script := "#!/bin/sh\nfor f; do :; done\nprintf '%s' \"$*\" > \"$f\"\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestTicketFromEditorTakesTheFirstLineAsTheTitle(t *testing.T) {
	dataDir := t.TempDir()
	withEditor(t, "Remove staging infrastructure\n\nRemove the staging app and the volume.\n")

	id, err := ticketFromEditorIn(t, dataDir, testfix.Repo(t, repoBranch))
	if err != nil {
		t.Fatal(err)
	}

	queue, err := testfix.OpenStore(t, dataDir).ListQueue()
	if err != nil {
		t.Fatal(err)
	}
	if len(queue) != 1 {
		t.Fatalf("the queue holds %d tickets, want 1", len(queue))
	}
	if queue[0].ID != id {
		t.Errorf("the queue holds ticket %d, want %d", queue[0].ID, id)
	}
	if want := "Remove staging infrastructure"; queue[0].Title != want {
		t.Errorf("title = %q, want %q", queue[0].Title, want)
	}

	if want := "Remove the staging app and the volume.\n"; proseOf(t, dataDir) != want {
		t.Errorf("the prose is %q, want %q", proseOf(t, dataDir), want)
	}
}

func TestTicketFromEditorWithOneLineWritesNoProse(t *testing.T) {
	dataDir := t.TempDir()
	withEditor(t, "Remove staging infrastructure\n")

	if _, err := ticketFromEditorIn(t, dataDir, testfix.Repo(t, repoBranch)); err != nil {
		t.Fatal(err)
	}

	queue, err := testfix.OpenStore(t, dataDir).ListQueue()
	if err != nil {
		t.Fatal(err)
	}
	if want := "Remove staging infrastructure"; queue[0].Title != want {
		t.Errorf("title = %q, want %q", queue[0].Title, want)
	}
	if got := proseOf(t, dataDir); got != "" {
		t.Errorf("the prose is %q, want it empty", got)
	}
}

func TestTicketFromEditorKeepsEachLineOfTheProse(t *testing.T) {
	dataDir := t.TempDir()
	withEditor(t, "Title\n\nOne.\n\nTwo.\n")

	if _, err := ticketFromEditorIn(t, dataDir, testfix.Repo(t, repoBranch)); err != nil {
		t.Fatal(err)
	}

	if want := "One.\n\nTwo.\n"; proseOf(t, dataDir) != want {
		t.Errorf("the prose is %q, want %q", proseOf(t, dataDir), want)
	}
}

// Accept trailing spaces and CRLF line endings.
func TestTicketFromEditorTrimsTheEndOfTheTitle(t *testing.T) {
	dataDir := t.TempDir()
	withEditor(t, "Remove staging infrastructure  \r\n\nRemove the staging app.\n")

	if _, err := ticketFromEditorIn(t, dataDir, testfix.Repo(t, repoBranch)); err != nil {
		t.Fatal(err)
	}

	queue, err := testfix.OpenStore(t, dataDir).ListQueue()
	if err != nil {
		t.Fatal(err)
	}
	if want := "Remove staging infrastructure"; queue[0].Title != want {
		t.Errorf("title = %q, want %q", queue[0].Title, want)
	}
}

func TestStartEditorRunsTheEditorOfThePerson(t *testing.T) {
	note := filepath.Join(t.TempDir(), "note.md")
	t.Setenv("EDITOR", fakeEditor(t, "from the editor"))

	if err := startEditor(note); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(note)
	if err != nil {
		t.Fatal(err)
	}
	if want := "from the editor"; string(data) != want {
		t.Errorf("the file holds %q, want %q", data, want)
	}
}

func TestStartEditorWithArgumentsInEDITOR(t *testing.T) {
	note := filepath.Join(t.TempDir(), "note.md")
	t.Setenv("EDITOR", echoEditor(t)+" --wait")

	if err := startEditor(note); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(note)
	if err != nil {
		t.Fatal(err)
	}
	if want := "--wait " + note; string(data) != want {
		t.Errorf("the editor got %q, want %q", data, want)
	}
}

func TestEditorNameTakesEDITOR(t *testing.T) {
	t.Setenv("EDITOR", "nano")
	if got := editorName(); got != "nano" {
		t.Errorf("editorName = %q, want nano", got)
	}
}

func TestEditorNameWithNoEDITOR(t *testing.T) {
	t.Setenv("EDITOR", "")
	if got := editorName(); got != "vi" {
		t.Errorf("editorName = %q, want vi", got)
	}
}

// Missing or whitespace-only titles must leave the database unchanged.
func TestTicketFromEditorWithNoTitle(t *testing.T) {
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
			withEditor(t, test.text)

			_, err := ticketFromEditorIn(t, dataDir, testfix.Repo(t, repoBranch))
			if !errors.Is(err, ErrNoTitle) {
				t.Fatalf("err = %v, want ErrNoTitle", err)
			}

			queue, err := testfix.OpenStore(t, dataDir).ListQueue()
			if err != nil {
				t.Fatal(err)
			}
			if len(queue) != 0 {
				t.Errorf("the queue holds %d tickets, want none", len(queue))
			}
			if files := proseFiles(t, dataDir); len(files) != 0 {
				t.Errorf("the files of prose are %v, want none", files)
			}
		})
	}
}

// A refused ticket must not consume an ID.
func TestTicketFromEditorWithNoTitleUsesNoID(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)

	withEditor(t, "")
	if _, err := ticketFromEditorIn(t, dataDir, repo); !errors.Is(err, ErrNoTitle) {
		t.Fatalf("err = %v, want ErrNoTitle", err)
	}

	withEditor(t, "Remove staging infrastructure\n")
	id, err := ticketFromEditorIn(t, dataDir, repo)
	if err != nil {
		t.Fatal(err)
	}
	if id != 1 {
		t.Errorf("id = %d, want 1", id)
	}
}
