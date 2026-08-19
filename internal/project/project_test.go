package project

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
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

// The key below is a literal. A test that does the hash again agrees with an
// error in the code that it examines. The value comes from the shell:
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
	dir, err := Create(data, "/home/person/projects/alcubi/delegator")
	if err != nil {
		t.Fatal(err)
	}

	want := filepath.Join(data, "projects", "delegator-b2fb4a")
	if dir != want {
		t.Fatalf("dir = %q, want %q", dir, want)
	}

	for _, name := range []string{"tickets", "worktrees", "runs"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("%s is not a directory", name)
		}
	}
}

// The permission below is a literal, and not dirPerm. A test that reads the
// same constant as the code agrees with a change of that constant to 0755.
func TestCreateMakesPrivateDirectories(t *testing.T) {
	data := t.TempDir()
	dir, err := Create(data, "/home/person/projects/alcubi/delegator")
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"", "tickets", "worktrees", "runs"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o700 {
			t.Errorf("%q permission = %o, want 700", name, got)
		}
	}
}
