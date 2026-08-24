package project

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
)

const testRoot = "/project/delegator"

func initRepo(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("git", "init", dir)
	if _, err := cmd.Output(); err != nil {
		t.Fatal(err)
	}
}

func TestRootFindsTheRoot(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	result, err := Root(dir)
	if err != nil {
		t.Fatal(err)
	}

	if result != dir {
		t.Errorf("result = %s, want = %s", result, dir)
	}

	subdir := filepath.Join(dir, "subdir")
	if err := os.Mkdir(subdir, 0o700); err != nil {
		t.Fatal(err)
	}
	result, err = Root(subdir)
	if err != nil {
		t.Fatal(err)
	}
	if result != dir {
		t.Errorf("result = %s, want = %s", result, dir)
	}
}

func TestRootErrorsOutsideARepository(t *testing.T) {
	dir := t.TempDir()
	_, err := Root(dir)
	if !errors.Is(err, ErrNotARepository) {
		t.Errorf("err = %v, want %v", err, ErrNotARepository)
	}
}

func TestRootErrorsWhenGitNotOnPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", "")
	_, err := Root(dir)
	if !errors.Is(err, ErrGitNotOnPath) {
		t.Errorf("err = %v, want %v", err, ErrGitNotOnPath)
	}
}

func TestRootWithDirectoryWithTrailingSpace(t *testing.T) {
	dir := t.TempDir()
	spaceDir := filepath.Join(dir, "dir-with-space ")
	if err := os.Mkdir(spaceDir, 0o700); err != nil {
		t.Fatal(err)
	}

	initRepo(t, spaceDir)
	result, err := Root(spaceDir)
	if err != nil {
		t.Fatal(err)
	}
	if result != spaceDir {
		t.Errorf("result = %s, want = %s", result, spaceDir)
	}
}

// The key below is a literal. A test that did the hash a second time would stay
// green with an error in the code that it examines, because the test would make
// the same error. The value therefore comes from the shell:
//
//	printf '%s' '/project/delegator' | sha256sum | cut -c1-6
func TestKeyForAKnownPath(t *testing.T) {
	got := Key(testRoot)
	want := "delegator-f5dfd0"
	if got != want {
		t.Errorf("Key = %q, want %q", got, want)
	}
}

func TestCreateMakesTheDirectories(t *testing.T) {
	data := t.TempDir()
	dir, err := Create(data, testRoot, "main")
	if err != nil {
		t.Fatal(err)
	}

	want := filepath.Join(data, "projects", "delegator-f5dfd0")
	if dir != want {
		t.Fatalf("dir = %q, want %q", dir, want)
	}

	// The empty name is the directory of the project. The test says the
	// permission 0700 a second time as a literal. A test that read dirPerm would
	// stay green after a change of dirPerm to 0755, because that one change moves
	// the value that the test wants at the same time.
	for _, name := range []string{"", "tickets", "worktrees", "runs"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Errorf("%q: %v", name, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("%q is not a directory", name)
		}
		if got := info.Mode().Perm(); got != 0o700 {
			t.Errorf("%q permission = %o, want 700", name, got)
		}
	}
}

func TestCreateWritesProjectToml(t *testing.T) {
	// Two branches, because one value cannot show that Create reads its
	// argument. A Create that always writes "main" passes a test that only
	// ever gives it "main".
	for _, branch := range []string{"main", "trunk"} {
		dataDir := t.TempDir()
		projectDir := createProject(t, dataDir, testRoot, branch)
		path := filepath.Join(projectDir, "project.toml")

		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}

		// The content is a literal. A test that read the file back with the
		// library that wrote it would stay green after a change of the name of
		// a field: repo_path would go out and come back, and the format of the
		// file would still be wrong.
		want := "path = \"/project/delegator\"\n" +
			"default_branch = \"" + branch + "\"\n"
		if string(got) != want {
			t.Errorf("project.toml = %q, want %q", got, want)
		}

		// The file project.toml can name a private path, so only its person can
		// read it. The test says the permission a second time as a literal, and
		// does not read the constant that the code gives to the write.
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("permission = %o, want 600", perm)
		}
	}
}

// createProject creates the project and returns the projectDir
func createProject(t *testing.T, dataDir, root, branch string) string {
	t.Helper()
	dir, err := Create(dataDir, root, branch)
	if err != nil {
		t.Fatal(err)
	}

	return dir
}

func TestCreateTomlAgainRemainsUnmodified(t *testing.T) {
	dataDir := t.TempDir()
	projectDir := createProject(t, dataDir, testRoot, "main")
	path := filepath.Join(projectDir, "project.toml")

	// Manually set the last modified time as a file modified immediately afterwards
	// may end up having the same mtime which would make the test unreliable
	old := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}

	createProject(t, dataDir, testRoot, "trunk")

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(old) {
		t.Errorf("project.toml was written again: mtime = %v, want %v", info.ModTime(), old)
	}
}

func TestConfig(t *testing.T) {
	dataDir := t.TempDir()
	projectDir := createProject(t, dataDir, testRoot, "main")
	config, err := Config(projectDir)
	if err != nil {
		t.Fatal(err)
	}

	if config.DefaultBranch != "main" {
		t.Errorf("default_branch = %s, want %s", config.DefaultBranch, "main")
	}

	if config.Path != testRoot {
		t.Errorf("path = %s, want %s", config.Path, testRoot)
	}
}

func TestConfigTomlDoesNotExist(t *testing.T) {
	dataDir := t.TempDir()
	projectDir := createProject(t, dataDir, testRoot, "main")
	err := os.Remove(filepath.Join(projectDir, "project.toml"))
	if err != nil {
		t.Fatal(err)
	}

	_, err = Config(projectDir)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("err = %v, want %v", err, fs.ErrNotExist)
	}
}

func TestConfigWithInvalidToml(t *testing.T) {
	dataDir := t.TempDir()
	projectDir := createProject(t, dataDir, testRoot, "main")
	if err := os.WriteFile(filepath.Join(projectDir, "project.toml"), []byte("invalid toml"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := Config(projectDir)
	if _, ok := errors.AsType[toml.ParseError](err); !ok {
		t.Errorf("err = %v, want a toml.ParseError", err)
	}
}

// gitIn runs one git command in dir. It stops the test if git gives an error,
// because a repository that the test cannot build is not a result of the test.
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	all := append([]string{"-C", dir}, args...)
	if out, err := exec.Command("git", all...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

// commitIn makes one empty commit, because a branch has no ref until a commit
// is on it. The identity is in the command, so the test does not read the
// config of the person.
func commitIn(t *testing.T, dir string) {
	t.Helper()
	gitIn(t, dir, "-c", "user.email=test@example.com", "-c", "user.name=Test",
		"commit", "--allow-empty", "-q", "-m", "first")
}

// trunkRepo makes a repository on a branch that is not main and not master, so
// each result shows which question of DefaultBranch gave the answer.
func trunkRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "trunk")
	return dir
}

func TestDefaultBranchTakesOriginHeadFirst(t *testing.T) {
	dir := trunkRepo(t)
	// A branch with the name main is present, so this shows that the remote
	// comes before it and not only before the branch of HEAD.
	commitIn(t, dir)
	gitIn(t, dir, "branch", "main")
	gitIn(t, dir, "remote", "add", "origin", "https://example.invalid/r.git")
	gitIn(t, dir, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/develop")

	got, err := DefaultBranch(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != "develop" {
		t.Errorf("DefaultBranch = %q, want %q", got, "develop")
	}
}

func TestDefaultBranchTakesMainBeforeTheBranchOfHead(t *testing.T) {
	dir := trunkRepo(t)
	// A branch has no ref until a commit is on it.
	commitIn(t, dir)
	gitIn(t, dir, "branch", "main")

	got, err := DefaultBranch(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != "main" {
		t.Errorf("DefaultBranch = %q, want %q", got, "main")
	}
}

func TestDefaultBranchTakesMasterWhenThereIsNoMain(t *testing.T) {
	dir := trunkRepo(t)
	commitIn(t, dir)
	gitIn(t, dir, "branch", "master")

	got, err := DefaultBranch(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != "master" {
		t.Errorf("DefaultBranch = %q, want %q", got, "master")
	}
}

func TestDefaultBranchTakesMainBeforeMaster(t *testing.T) {
	dir := trunkRepo(t)
	commitIn(t, dir)
	// master is made first, so an answer of master cannot come from the order
	// of the branches in the repository.
	gitIn(t, dir, "branch", "master")
	gitIn(t, dir, "branch", "main")

	got, err := DefaultBranch(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != "main" {
		t.Errorf("DefaultBranch = %q, want %q", got, "main")
	}
}

func TestDefaultBranchTakesTheBranchOfHeadLast(t *testing.T) {
	// The repository has no remote, no main, no master, and no commit.
	dir := trunkRepo(t)

	got, err := DefaultBranch(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != "trunk" {
		t.Errorf("DefaultBranch = %q, want %q", got, "trunk")
	}
}
