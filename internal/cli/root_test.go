package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alcubie/delegator/internal/testfix"
)

// runIn runs one command and returns what it wrote to the output. cobra writes
// each error to the error output, and cmd/dg makes the text for those, so the
// test reads the two apart.
func runIn(t *testing.T, dataDir, workDir string, args ...string) (string, error) {
	t.Helper()
	return runInWithStdin(t, dataDir, workDir, "", args...)
}

// runInWithStdin runs one command with text on its standard input, for a
// command that reads it.
func runInWithStdin(t *testing.T, dataDir, workDir, stdin string, args ...string) (string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	root := Root(dataDir, workDir)
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetIn(strings.NewReader(stdin))
	// A nil slice makes cobra read os.Args, which holds the arguments of the
	// test, so the arguments are always a slice that is there.
	if args == nil {
		args = []string{}
	}
	root.SetArgs(args)

	err := root.Execute()
	return out.String(), err
}

// dg with no command is the inbox. The help is still there, at dg help.
func TestRunWithNoCommandShowsTheInbox(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	if _, err := runIn(t, dataDir, repo, "ticket", "Remove staging infrastructure"); err != nil {
		t.Fatal(err)
	}

	out, err := runIn(t, dataDir, repo)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"READY", "RUNNING", "QUEUED", "Remove staging infrastructure"} {
		if !strings.Contains(out, want) {
			t.Errorf("the inbox does not hold %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Usage:") {
		t.Errorf("dg with no command shows the help:\n%s", out)
	}
}

// cobra makes the help from the tree of commands, and a command with no Short
// arrives in that help with nothing beside it. The line is ours to write, so
// this reads the tree and not the help that cobra makes from it.
func TestEachCommandSaysWhatItDoes(t *testing.T) {
	for _, c := range Root(t.TempDir(), t.TempDir()).Commands() {
		if c.Short == "" {
			t.Errorf("the command %q has no Short, so the help says nothing about it", c.Name())
		}
	}
}

func TestDataDirTakesXDGDataHome(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/somewhere/data")

	got, err := DataDir()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("/somewhere/data", "delegator"); got != want {
		t.Errorf("DataDir = %q, want %q", got, want)
	}
}

// Windows has no XDG rule, and a person there keeps the data of a program
// below LOCALAPPDATA. make check runs on Linux, so the system is a parameter
// of the unexported dataDir and the test names it.
func TestDataDirOnWindowsTakesLocalAppData(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("LOCALAPPDATA", `C:\Users\person\AppData\Local`)

	got, err := dataDir("windows")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(`C:\Users\person\AppData\Local`, "delegator"); got != want {
		t.Errorf("dataDir(windows) = %q, want %q", got, want)
	}
}

// Windows sets LOCALAPPDATA for a person who signed in, so a run with it empty
// is a run with nothing to join, and joining nothing gives the relative path
// delegator, which would put the database wherever the command was run.
func TestDataDirOnWindowsWithNoLocalAppData(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("LOCALAPPDATA", "")

	got, err := dataDir("windows")
	if err == nil {
		t.Fatalf("dataDir(windows) with no LOCALAPPDATA = %q, want an error", got)
	}
	if !strings.Contains(err.Error(), "LOCALAPPDATA") {
		t.Errorf("the error is %q, which does not name LOCALAPPDATA", err)
	}
}

// A person who sets XDG_DATA_HOME means it on whichever system they are on,
// and the tests of this repository set it to hold their own directory, so it
// comes before the directory the system asks for. LOCALAPPDATA is set here to
// name the one that would otherwise win.
func TestXDGDataHomeWinsOnEachSystem(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/somewhere/data")
	t.Setenv("LOCALAPPDATA", `C:\Users\person\AppData\Local`)

	for _, goos := range []string{"linux", "darwin", "windows"} {
		got, err := dataDir(goos)
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join("/somewhere/data", "delegator"); got != want {
			t.Errorf("dataDir(%s) = %q, want %q", goos, got, want)
		}
	}
}

// Linux and macOS keep the directory they had. LOCALAPPDATA is set here
// because a person can set any variable on any system, and neither of these
// systems reads it.
func TestDataDirOffWindowsIsTheXDGDirectory(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("LOCALAPPDATA", `C:\Users\person\AppData\Local`)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}

	for _, goos := range []string{"linux", "darwin"} {
		got, err := dataDir(goos)
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join(home, ".local", "share", "delegator"); got != want {
			t.Errorf("dataDir(%s) = %q, want %q", goos, got, want)
		}
	}
}
