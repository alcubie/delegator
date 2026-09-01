package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runIn runs one command and returns what it wrote to the output. cobra writes
// each error to the error output, and cmd/dg makes the text for those, so the
// test reads the two apart.
func runIn(t *testing.T, dataDir, workDir string, args ...string) (string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	root := Root(dataDir, workDir)
	root.SetOut(&out)
	root.SetErr(&errOut)
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
	repo := gitRepo(t)
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

func TestDataDirWithNoXDGDataHome(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")

	got, err := DataDir()
	if err != nil {
		t.Fatal(err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".local", "share", "delegator"); got != want {
		t.Errorf("DataDir = %q, want %q", got, want)
	}
}
