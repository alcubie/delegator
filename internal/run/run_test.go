package run

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/alcubie/delegator/internal/store"
	"github.com/alcubie/delegator/internal/testfix"
)

// repoOnMain makes a repository on main with one commit, which a worktree
// needs to start from.
func repoOnMain(t *testing.T) string {
	t.Helper()
	dir := testfix.Repo(t, "main")
	testfix.CommitIn(t, dir, "first")
	return dir
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
	testfix.GitIn(t, repo, "checkout", "-q", "-b", "other")
	testfix.CommitIn(t, repo, "second")
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
	if got := testfix.GitOut(t, path, "rev-parse", "--abbrev-ref", "HEAD"); got != "delegator/7-add-the-thing" {
		t.Errorf("branch = %s, want delegator/7-add-the-thing", got)
	}
	if got, want := testfix.GitOut(t, path, "rev-parse", "HEAD"), testfix.GitOut(t, repo, "rev-parse", "main"); got != want {
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
