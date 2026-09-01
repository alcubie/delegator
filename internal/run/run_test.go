package run

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alcubie/delegator/internal/project"
	"github.com/alcubie/delegator/internal/store"
)

// gitIn runs one git command in dir. It stops the test if git gives an error,
// because a repository the test cannot build is not a result of the test.
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	if out, err := project.Command(dir, args...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

// gitOut runs one git command in dir and returns its output with no final
// newline.
func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := project.Command(dir, args...).Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			t.Fatalf("git %v: %v: %s", args, err, exitErr.Stderr)
		}
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

// repoOnMain makes a repository on main with one commit. A worktree needs a
// commit to start from, and the name is fixed so the test does not depend on
// the config of the person who runs it.
func repoOnMain(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if out, err := project.Command(dir, "init", "-q", "-b", "main").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	commitIn(t, dir, "first")
	return dir
}

// commitIn makes one empty commit. The identity is in the command, so the test
// does not read the config of the person who runs it.
func commitIn(t *testing.T, dir, message string) {
	t.Helper()
	gitIn(t, dir, "-c", "user.email=test@example.com", "-c", "user.name=Test",
		"commit", "--allow-empty", "-q", "-m", message)
}

func TestBranchSuffix(t *testing.T) {
	tests := []struct {
		title string
		want  string
	}{
		{"UPPERCASE", "uppercase"},
		{"MixedCase", "mixedcase"},
		{"12numbered", "12numbered"},
		{"Very Very Long Title With Many Words and Characters in this Sentence", "very-very-long-title-with-many-words-and"},
		{"Supercalifragilisticexpialidocious antidisestablishmentarianism xyz", "supercalifragilisticexpialidocious"},
		{"antidisestablishmentarianismfloccinaucinihilipilification", "antidisestablishmentarianismfloccinaucin"},
		{"Handle café orders for Straße", "handle-cafe-orders-for-strasse"},
		{"Здравствуйте мир", "zdravstvuite-mir"},
	}

	for _, test := range tests {
		got := branch(1, test.title)
		want := fmt.Sprintf("delegator/1-%s", test.want)
		if got != want {
			t.Errorf("got = %s, want = %s", got, want)
		}
	}
}

func TestBranchNoAsciiTitle(t *testing.T) {
	got := branch(1, "🎉🎉🎉")
	want := "delegator/1"
	if got != want {
		t.Errorf("got = %s, want = %s", got, want)
	}
}

// HEAD moves away from main before the call, so the last assertion shows that
// the worktree starts at the default branch and not at the branch the person
// happens to be on.
func TestWorktreeMakesTheWorktreeOnItsBranch(t *testing.T) {
	repo := repoOnMain(t)
	gitIn(t, repo, "checkout", "-q", "-b", "other")
	commitIn(t, repo, "second")
	dataDir := t.TempDir()

	path, err := Worktree(dataDir, store.Ticket{
		ID:      7,
		Project: store.Project{Path: repo, DefaultBranch: "main"},
		Title:   "Add the thing",
	})
	if err != nil {
		t.Fatal(err)
	}

	if want := filepath.Join(dataDir, "worktrees", "7"); path != want {
		t.Errorf("path = %s, want %s", path, want)
	}
	if got := gitOut(t, path, "rev-parse", "--abbrev-ref", "HEAD"); got != "delegator/7-add-the-thing" {
		t.Errorf("branch = %s, want delegator/7-add-the-thing", got)
	}
	if got, want := gitOut(t, path, "rev-parse", "HEAD"), gitOut(t, repo, "rev-parse", "main"); got != want {
		t.Errorf("the worktree starts at %s, want main at %s", got, want)
	}
}

// A restart uses the worktree that the earlier run made, so a second call
// returns the same path and no error.
func TestWorktreeThatIsThereAlready(t *testing.T) {
	repo := repoOnMain(t)
	dataDir := t.TempDir()

	first, err := Worktree(dataDir, store.Ticket{
		ID:      7,
		Project: store.Project{Path: repo, DefaultBranch: "main"},
		Title:   "Add the thing",
	})
	if err != nil {
		t.Fatal(err)
	}

	second, err := Worktree(dataDir, store.Ticket{
		ID:      7,
		Project: store.Project{Path: repo, DefaultBranch: "main"},
		Title:   "Add the thing",
	})
	if err != nil {
		t.Fatal(err)
	}
	if second != first {
		t.Errorf("second = %s, want %s", second, first)
	}
}
