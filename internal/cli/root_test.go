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
	root := Root(workDir)
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetIn(strings.NewReader(stdin))
	// A nil slice makes cobra read os.Args, which holds the arguments of the
	// test, so the arguments are always a slice that is there.
	if args == nil {
		args = []string{}
	}
	if dataDir != "" {
		args = append([]string{"--data-dir", dataDir}, args...)
	}
	root.SetArgs(args)

	err := root.Execute()
	return out.String(), err
}

// dg with no command is the inbox. The help is still there, at dg help.
func TestRunWithNoCommandShowsTheInbox(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	if _, err := runIn(t, dataDir, repo, "ticket", "Remove staging infrastructure", "--no-body"); err != nil {
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
	for _, c := range Root(t.TempDir()).Commands() {
		if c.Short == "" {
			t.Errorf("the command %q has no Short, so the help says nothing about it", c.Name())
		}
	}
}

// Help and completion describe the command tree, which exists before an
// instance does. They therefore neither make an absent data directory nor try
// to open one that cannot contain Delegator's files.
func TestHelpAndCompletionDoNotOpenTheInstance(t *testing.T) {
	commands := []struct {
		name string
		args []string
	}{
		{"root help", []string{"--help"}},
		{"command help", []string{"ticket", "--help"}},
		{"help command", []string{"help", "ticket"}},
		{"completion", []string{"completion", "bash"}},
	}

	for _, command := range commands {
		t.Run(command.name, func(t *testing.T) {
			t.Run("absent", func(t *testing.T) {
				dataDir := filepath.Join(t.TempDir(), "delegator")
				out, err := runIn(t, dataDir, t.TempDir(), command.args...)
				if err != nil {
					t.Fatal(err)
				}
				if out == "" {
					t.Error("command wrote no help or completion")
				}
				if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
					t.Errorf("static command created the data directory: %v", err)
				}
			})

			t.Run("unusable", func(t *testing.T) {
				dataDir := filepath.Join(t.TempDir(), "not-a-directory")
				if err := os.WriteFile(dataDir, []byte("not a directory"), 0o600); err != nil {
					t.Fatal(err)
				}
				out, err := runIn(t, dataDir, t.TempDir(), command.args...)
				if err != nil {
					t.Fatal(err)
				}
				if out == "" {
					t.Error("command wrote no help or completion")
				}
			})
		})
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

func TestSelectedDataDirMustBeAbsoluteAndIsCleaned(t *testing.T) {
	if _, err := selectedDataDir("relative"); err == nil || !strings.Contains(err.Error(), "--data-dir") {
		t.Errorf("selectedDataDir(relative) error = %v, want an error naming --data-dir", err)
	}

	dir := filepath.Join(t.TempDir(), "first", "..", "selected")
	got, err := selectedDataDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Clean(dir); got != want {
		t.Errorf("selectedDataDir(%q) = %q, want %q", dir, got, want)
	}
}

func TestDataDirFlagBeforeAndAfterCommandsSelectsIsolatedInstances(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	defaultDir := filepath.Join(xdg, "delegator")
	first := filepath.Join(t.TempDir(), "first", "..", "selected-first")
	second := filepath.Join(t.TempDir(), "selected-second")
	repo := testfix.Repo(t, repoBranch)

	if _, err := runIn(t, "", repo,
		"--data-dir", first, "ticket", "In the first instance", "--no-body"); err != nil {
		t.Fatal(err)
	}
	if _, err := runIn(t, "", repo,
		"ticket", "In the second instance", "--no-body", "--data-dir", second); err != nil {
		t.Fatal(err)
	}

	first = filepath.Clean(first)
	for _, instance := range []struct {
		dir   string
		title string
	}{
		{first, "In the first instance"},
		{second, "In the second instance"},
	} {
		for _, name := range []string{"delegator.db", "tickets", "runs", "worktrees"} {
			if _, err := os.Stat(filepath.Join(instance.dir, name)); err != nil {
				t.Errorf("selected instance %q has no %s: %v", instance.dir, name, err)
			}
		}
		queue, err := testfix.OpenStore(t, instance.dir).ListQueue()
		if err != nil {
			t.Fatal(err)
		}
		if len(queue) != 1 || queue[0].Title != instance.title {
			t.Errorf("queue in %q = %+v, want only %q", instance.dir, queue, instance.title)
		}
	}
	if _, err := os.Stat(filepath.Join(defaultDir, "delegator.db")); !os.IsNotExist(err) {
		t.Errorf("default data directory was opened despite --data-dir: %v", err)
	}
}

func TestDataDirFlagIsValidatedBeforeTheDefaultStoreOpens(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	repo := testfix.Repo(t, repoBranch)

	_, err := runIn(t, "", repo, "ticket", "Never added", "--no-body", "--data-dir", "relative")
	if err == nil || !strings.Contains(err.Error(), "--data-dir") {
		t.Fatalf("relative --data-dir error = %v, want an error naming --data-dir", err)
	}
	if _, err := os.Stat(filepath.Join(xdg, "delegator")); !os.IsNotExist(err) {
		t.Errorf("the default store was opened before validation: %v", err)
	}
}

func TestAbsentDataDirFlagUsesTheDefaultAfterParsing(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	repo := testfix.Repo(t, repoBranch)

	if _, err := runIn(t, "", repo, "ticket", "Uses the default", "--no-body"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(xdg, "delegator", "delegator.db")); err != nil {
		t.Errorf("the XDG default has no database: %v", err)
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
