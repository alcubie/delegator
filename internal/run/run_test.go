package run

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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

func TestProjectCachePathUsesTheDataDirectoryAndProjectID(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "selected data")
	want := filepath.Join(dataDir, "cache", "projects", "17")
	if got := ProjectCachePath(dataDir, 17); got != want {
		t.Errorf("ProjectCachePath = %q, want %q", got, want)
	}
}

func TestProjectCacheCreatesAPrivateDirectory(t *testing.T) {
	path, err := ProjectCache(t.TempDir(), 17)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o700 {
		t.Errorf("cache permission = %o, want 700", info.Mode().Perm())
	}
}

func TestProjectCacheIsSharedByTicketsAndIsolatedByProject(t *testing.T) {
	dataDir := t.TempDir()
	s := testfix.OpenStore(t, dataDir)
	firstProject, err := s.ProjectID(filepath.Join(t.TempDir(), "first"), "main")
	if err != nil {
		t.Fatal(err)
	}
	secondProject, err := s.ProjectID(filepath.Join(t.TempDir(), "second"), "main")
	if err != nil {
		t.Fatal(err)
	}
	firstTicket, err := s.AddTicket(firstProject, "the first")
	if err != nil {
		t.Fatal(err)
	}
	secondTicket, err := s.AddTicket(firstProject, "the second")
	if err != nil {
		t.Fatal(err)
	}
	otherTicket, err := s.AddTicket(secondProject, "the other")
	if err != nil {
		t.Fatal(err)
	}

	first := ProjectCachePath(dataDir, testfix.ReadTicket(t, dataDir, firstTicket).Project.ID)
	second := ProjectCachePath(dataDir, testfix.ReadTicket(t, dataDir, secondTicket).Project.ID)
	other := ProjectCachePath(dataDir, testfix.ReadTicket(t, dataDir, otherTicket).Project.ID)
	if first != second {
		t.Errorf("tickets of one project got %q and %q", first, second)
	}
	if first == other {
		t.Errorf("different projects both got %q", first)
	}
}

func TestProjectCacheSurvivesWorktreeRemoval(t *testing.T) {
	dataDir := t.TempDir()
	ticket := store.Ticket{
		ID:      7,
		Project: store.Project{ID: 17, Path: repoOnMain(t), DefaultBranch: "main"},
		Title:   "Add the thing",
	}
	worktree, err := Worktree(dataDir, ticket)
	if err != nil {
		t.Fatal(err)
	}
	cacheDir, err := ProjectCache(dataDir, ticket.Project.ID)
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(cacheDir, "reusable artifact")
	if err := os.WriteFile(marker, []byte("kept"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RemoveWorktree(dataDir, ticket, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(worktree); !os.IsNotExist(err) {
		t.Errorf("removed worktree stat = %v, want not exist", err)
	}
	if got, err := os.ReadFile(marker); err != nil || string(got) != "kept" {
		t.Errorf("cache marker after worktree removal = %q, %v", got, err)
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
