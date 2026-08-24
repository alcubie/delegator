package cli

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/alcubie/delegator/internal/project"
	"github.com/alcubie/delegator/internal/queue"
	"github.com/alcubie/delegator/internal/ticket"
)

// gitRepo makes an empty repository, because Ticket finds the project from the
// directory that the person is in. The branch is not main and not master, so a
// value that does not come from the repository is visible in a result.
func gitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if out, err := exec.Command("git", "-C", dir, "init", "-q", "-b", repoBranch).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	return dir
}

// repoBranch is the branch of each repository that these tests make.
const repoBranch = "trunk"

// projectDir finds the one project below dataDir.
func projectDir(t *testing.T, dataDir string) string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dataDir, "projects", "*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("projects = %v, want one", matches)
	}
	return matches[0]
}

// ticketDir finds the one ticket below dataDir. The test does not build the
// path itself, because the name of the directory is condition 5 of the ticket
// and not this condition.
func ticketDir(t *testing.T, dataDir string) string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dataDir, "projects", "*", "tickets", "*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("tickets = %v, want one", matches)
	}
	return matches[0]
}

func TestTicketMakesTheTicketAndPutsTheIDInTheQueue(t *testing.T) {
	dataDir := t.TempDir()
	repo := gitRepo(t)
	const title = "Remove staging infrastructure"

	id, err := Ticket(dataDir, repo, title)
	if err != nil {
		t.Fatal(err)
	}

	dir := ticketDir(t, dataDir)
	for _, name := range []string{"ticket.yaml", "ticket.md"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Error(err)
		}
	}

	got, err := ticket.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != id {
		t.Errorf("ticket id = %d, want %d", got.ID, id)
	}
	if got.Title != title {
		t.Errorf("title = %q, want %q", got.Title, title)
	}

	ids, err := queue.List(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != id {
		t.Errorf("queue = %v, want [%d]", ids, id)
	}
}

// Ticket finds the project before it reserves an id. A directory that is not a
// repository must therefore cost no id, because an id in the queue that has no
// ticket is a gap that no command removes.
func TestTicketOutsideARepositoryUsesNoID(t *testing.T) {
	dataDir := t.TempDir()

	_, err := Ticket(dataDir, t.TempDir(), "Remove staging infrastructure")
	if !errors.Is(err, project.ErrNotARepository) {
		t.Fatalf("err = %v, want %v", err, project.ErrNotARepository)
	}

	// The ticket that comes after takes the first id, because the failure
	// took none.
	id, err := Ticket(dataDir, gitRepo(t), "Add rate limiting")
	if err != nil {
		t.Fatal(err)
	}
	if id != 1 {
		t.Errorf("id = %d, want 1", id)
	}

	ids, err := queue.List(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(ids, []int{1}) {
		t.Errorf("queue = %v, want [1]", ids)
	}
}

// The branch of the project is the start of the branch of each run, and
// project.Create writes it one time only, so the first ticket of a project
// settles it.
func TestTicketWritesTheBranchOfTheRepository(t *testing.T) {
	dataDir := t.TempDir()

	if _, err := Ticket(dataDir, gitRepo(t), "Remove staging infrastructure"); err != nil {
		t.Fatal(err)
	}

	got, err := project.Config(projectDir(t, dataDir))
	if err != nil {
		t.Fatal(err)
	}
	if got.DefaultBranch != repoBranch {
		t.Errorf("default branch = %q, want %q", got.DefaultBranch, repoBranch)
	}
}
