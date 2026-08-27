package cli

import (
	"errors"
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

func TestTicketWritesTheRowTheProseAndTheQueue(t *testing.T) {
	dataDir := t.TempDir()
	const title = "Remove staging infrastructure"

	id, err := Ticket(dataDir, gitRepo(t), title)
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

	_, err := Ticket(dataDir, t.TempDir(), "Remove staging infrastructure")
	if !errors.Is(err, project.ErrNotARepository) {
		t.Fatalf("err = %v, want %v", err, project.ErrNotARepository)
	}

	id, err := Ticket(dataDir, gitRepo(t), "Add rate limiting")
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

	if _, err := Ticket(dataDir, gitRepo(t), "Remove staging infrastructure"); err != nil {
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

	if _, err := Ticket(dataDir, repo, "Remove staging infrastructure"); err != nil {
		t.Fatal(err)
	}
	if _, err := Ticket(dataDir, repo, "Add rate limiting"); err != nil {
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
