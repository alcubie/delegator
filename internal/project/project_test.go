package project

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

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
//	printf '%s' '/home/person/projects/alcubi/delegator' | sha256sum | cut -c1-6
func TestKeyForAKnownPath(t *testing.T) {
	got := Key("/home/person/projects/alcubi/delegator")
	want := "delegator-b2fb4a"
	if got != want {
		t.Errorf("Key = %q, want %q", got, want)
	}
}

func TestCreateMakesTheDirectories(t *testing.T) {
	data := t.TempDir()
	dir, err := Create(data, "/home/person/projects/alcubi/delegator", "main")
	if err != nil {
		t.Fatal(err)
	}

	want := filepath.Join(data, "projects", "delegator-b2fb4a")
	if dir != want {
		t.Fatalf("dir = %q, want %q", dir, want)
	}

	// The empty name is the directory of the project. The permission 0700 comes
	// from section 11, and the test says it a second time as a literal. A test
	// that read dirPerm would stay green after a change of dirPerm to 0755,
	// because that one change moves the value that the test wants at the same
	// time.
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
		path := createProject(t, dataDir, branch)

		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}

		// The content is a literal. A test that read the file back with the
		// library that wrote it would stay green after a change of the name of
		// a field: repo_path would go out and come back, and the format of the
		// file would still be wrong.
		want := "path = \"/home/person/projects/alcubi/delegator\"\n" +
			"default_branch = \"" + branch + "\"\n"
		if string(got) != want {
			t.Errorf("project.toml = %q, want %q", got, want)
		}

		// Section 11 names project.toml, so only its person can read it. The
		// test says the permission a second time as a literal, and does not read
		// the constant that the code gives to the write.
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("permission = %o, want 600", perm)
		}
	}
}

func createProject(t *testing.T, dataDir string, branch string) string {
	t.Helper()
	dir, err := Create(dataDir, "/home/person/projects/alcubi/delegator", branch)
	if err != nil {
		t.Fatal(err)
	}

	return filepath.Join(dir, "project.toml")
}

func TestCreateTomlAgainRemainsUnmodified(t *testing.T) {
	dataDir := t.TempDir()
	path := createProject(t, dataDir, "main")

	// Manually set the last modified time as a file modified immediately afterwards
	// may end up having the same mtime which would make the test unreliable
	old := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}

	createProject(t, dataDir, "trunk")

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(old) {
		t.Errorf("project.toml was written again: mtime = %v, want %v", info.ModTime(), old)
	}
}
