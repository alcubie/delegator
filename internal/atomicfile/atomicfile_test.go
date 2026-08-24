package atomicfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWritePutsTheContentInTheFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := Write(path, []byte("hello"), Perm); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Errorf("content = %q, want %q", got, "hello")
	}
}

// The test compares against the literal 0600, and not against Perm, so the test
// says the value a second time. A test that read Perm would stay green after a
// change of Perm to 0644, because that one change moves the value that the test
// wants at the same time.
func TestWriteGivesThePermission(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := Write(path, []byte("hello"), Perm); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("permission = %o, want 600", got)
	}
}

func TestWriteKeepsTheOldFileWhenItCannotWrite(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root does not obey the permissions of a directory")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}

	// A directory that does not permit a write stops a new file, but it does
	// not stop a write to a file that is already in it.
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })

	if err := Write(path, []byte("new"), Perm); err == nil {
		t.Fatal("want an error")
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old" {
		t.Errorf("content = %q, want %q", got, "old")
	}
}

func TestWriteRemovesItsTemporaryFileAfterAnError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	// A directory with the name of the target makes the rename fail, after the
	// temporary file is made. This is the path on which the cleanup operates.
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}

	if err := Write(path, []byte("new"), Perm); err == nil {
		t.Fatal("want an error")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("the temporary file %s is still there", e.Name())
		}
	}
}
