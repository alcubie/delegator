package cli

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/alcubie/delegator/internal/project"
	"github.com/alcubie/delegator/internal/store"
)

// repoBranch is the branch of each repository that these tests make. It is not
// main and not master, so a value that does not come from the repository is
// visible in a result.
const repoBranch = "trunk"

// gitRepo makes an empty repository, because Ticket finds the project from the
// directory that the person is in.
func gitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if out, err := exec.Command("git", "-C", dir, "init", "-q", "-b", repoBranch).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	return dir
}

// openStore opens the database that a command wrote.
func openStore(t *testing.T, dataDir string) *store.Store {
	t.Helper()
	s, err := store.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// proseFiles gives each file of prose that a command made. The test does not
// build the name itself, because the name is condition 5 of the ticket.
func proseFiles(t *testing.T, dataDir string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dataDir, "tickets", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	return matches
}

// proseOf reads the one file of prose that a command made.
func proseOf(t *testing.T, dataDir string) string {
	t.Helper()
	files := proseFiles(t, dataDir)
	if len(files) != 1 {
		t.Fatalf("the files of prose are %v, want one", files)
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// withEditor puts an editor in place of the one of the person. It writes text
// into the file that the command gives it, which is what a person does.
func withEditor(t *testing.T, text string) {
	t.Helper()
	old := editor
	editor = func(path string) error {
		return os.WriteFile(path, []byte(text), 0o600)
	}
	t.Cleanup(func() { editor = old })
}

func TestTicketWritesTheRowTheProseAndTheQueue(t *testing.T) {
	dataDir := t.TempDir()
	const title = "Remove staging infrastructure"

	id, err := Ticket(dataDir, gitRepo(t), title, "")
	if err != nil {
		t.Fatal(err)
	}

	queue, err := openStore(t, dataDir).ListQueue()
	if err != nil {
		t.Fatal(err)
	}
	if len(queue) != 1 {
		t.Fatalf("the queue holds %d tickets, want 1", len(queue))
	}
	if queue[0].ID != id {
		t.Errorf("the queue holds ticket %d, want %d", queue[0].ID, id)
	}
	if queue[0].Title != title {
		t.Errorf("title = %q, want %q", queue[0].Title, title)
	}

	if got := proseFiles(t, dataDir); len(got) != 1 {
		t.Errorf("the files of prose are %v, want one", got)
	}
}

// Ticket finds the project before it writes a row, so a directory that is not a
// repository costs no id.
func TestTicketOutsideARepositoryUsesNoID(t *testing.T) {
	dataDir := t.TempDir()

	_, err := Ticket(dataDir, t.TempDir(), "Remove staging infrastructure", "")
	if !errors.Is(err, project.ErrNotARepository) {
		t.Fatalf("err = %v, want %v", err, project.ErrNotARepository)
	}

	id, err := Ticket(dataDir, gitRepo(t), "Add rate limiting", "")
	if err != nil {
		t.Fatal(err)
	}
	if id != 1 {
		t.Errorf("id = %d, want 1", id)
	}
}

// The branch of the project is the start of the branch of each run, so the
// first ticket of a project settles it.
func TestTicketWritesTheBranchOfTheRepository(t *testing.T) {
	dataDir := t.TempDir()

	if _, err := Ticket(dataDir, gitRepo(t), "Remove staging infrastructure", ""); err != nil {
		t.Fatal(err)
	}

	projects, err := openStore(t, dataDir).Projects()
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 {
		t.Fatalf("the database holds %d projects, want 1", len(projects))
	}
	if projects[0].DefaultBranch != repoBranch {
		t.Errorf("default branch = %q, want %q", projects[0].DefaultBranch, repoBranch)
	}
}

// A second ticket in the same repository uses the project that the first one
// made, because the path of a project is unique.
func TestTicketUsesTheProjectOfAnEarlierTicket(t *testing.T) {
	dataDir := t.TempDir()
	repo := gitRepo(t)

	if _, err := Ticket(dataDir, repo, "Remove staging infrastructure", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := Ticket(dataDir, repo, "Add rate limiting", ""); err != nil {
		t.Fatal(err)
	}

	projects, err := openStore(t, dataDir).Projects()
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 {
		t.Errorf("the database holds %d projects, want 1", len(projects))
	}
}

func TestTicketWritesTheBodyIntoTheProse(t *testing.T) {
	dataDir := t.TempDir()
	const body = "Remove the staging app, the volume and the DNS records."

	if _, err := Ticket(dataDir, gitRepo(t), "Remove staging infrastructure", body); err != nil {
		t.Fatal(err)
	}

	if got := proseOf(t, dataDir); got != body {
		t.Errorf("the prose is %q, want %q", got, body)
	}
}

func TestTicketWithNoBodyLeavesTheProseEmpty(t *testing.T) {
	dataDir := t.TempDir()

	if _, err := Ticket(dataDir, gitRepo(t), "Remove staging infrastructure", ""); err != nil {
		t.Fatal(err)
	}

	if got := proseOf(t, dataDir); got != "" {
		t.Errorf("the prose is %q, want it empty", got)
	}
}

// A ticket can hold private data, so only the person who made it can read the
// prose.
func TestTicketWritesTheProseForItsPersonOnly(t *testing.T) {
	dataDir := t.TempDir()

	if _, err := Ticket(dataDir, gitRepo(t), "Remove staging infrastructure", "body"); err != nil {
		t.Fatal(err)
	}

	files := proseFiles(t, dataDir)
	if len(files) != 1 {
		t.Fatalf("the files of prose are %v, want one", files)
	}
	info, err := os.Stat(files[0])
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("the permission is %v, want %v", got, os.FileMode(0o600))
	}
}

func TestTicketFromEditorTakesTheFirstLineAsTheTitle(t *testing.T) {
	dataDir := t.TempDir()
	withEditor(t, "Remove staging infrastructure\n\nRemove the app and the volume.\n")

	id, err := TicketFromEditor(dataDir, gitRepo(t))
	if err != nil {
		t.Fatal(err)
	}

	queue, err := openStore(t, dataDir).ListQueue()
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

	if want := "Remove the app and the volume.\n"; proseOf(t, dataDir) != want {
		t.Errorf("the prose is %q, want %q", proseOf(t, dataDir), want)
	}
}

func TestTicketFromEditorWithOneLineWritesNoProse(t *testing.T) {
	dataDir := t.TempDir()
	withEditor(t, "Remove staging infrastructure\n")

	if _, err := TicketFromEditor(dataDir, gitRepo(t)); err != nil {
		t.Fatal(err)
	}

	queue, err := openStore(t, dataDir).ListQueue()
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

	if _, err := TicketFromEditor(dataDir, gitRepo(t)); err != nil {
		t.Fatal(err)
	}

	if want := "One.\n\nTwo.\n"; proseOf(t, dataDir) != want {
		t.Errorf("the prose is %q, want %q", proseOf(t, dataDir), want)
	}
}

// An editor can leave a space at the end of the line, and an editor on another
// system ends a line with a return and a newline.
func TestTicketFromEditorTrimsTheEndOfTheTitle(t *testing.T) {
	dataDir := t.TempDir()
	withEditor(t, "Remove staging infrastructure  \r\n\nRemove the app.\n")

	if _, err := TicketFromEditor(dataDir, gitRepo(t)); err != nil {
		t.Fatal(err)
	}

	queue, err := openStore(t, dataDir).ListQueue()
	if err != nil {
		t.Fatal(err)
	}
	if want := "Remove staging infrastructure"; queue[0].Title != want {
		t.Errorf("title = %q, want %q", queue[0].Title, want)
	}
}

// fakeEditor writes a program that puts text into the last path that it gets,
// which is what an editor does.
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

// EDITOR can hold arguments, as "code --wait" does.
// echoEditor writes each argument that it gets into the last one, so a test can
// see the arguments that startEditor gave it.
func echoEditor(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "echo-editor")
	script := "#!/bin/sh\nfor f; do :; done\nprintf '%s' \"$*\" > \"$f\"\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
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

// A person who has set no EDITOR still gets an editor.
func TestEditorNameWithNoEDITOR(t *testing.T) {
	t.Setenv("EDITOR", "")
	if got := editorName(); got != "vi" {
		t.Errorf("editorName = %q, want vi", got)
	}
}
