package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runIn runs one command and gives what it wrote.
func runIn(t *testing.T, dataDir, workDir string, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := Run(&out, dataDir, workDir, args)
	return out.String(), err
}

func TestRunTicketShowsTheIDOfTheNewTicket(t *testing.T) {
	dataDir := t.TempDir()
	repo := gitRepo(t)

	out, err := runIn(t, dataDir, repo, "ticket", "Remove staging infrastructure")
	if err != nil {
		t.Fatal(err)
	}
	if out != "1\n" {
		t.Errorf("the command wrote %q, want %q", out, "1\n")
	}

	// the second ticket shows the next id
	out, err = runIn(t, dataDir, repo, "ticket", "Add rate limiting")
	if err != nil {
		t.Fatal(err)
	}
	if out != "2\n" {
		t.Errorf("the command wrote %q, want %q", out, "2\n")
	}
}

func TestRunTicketWithABody(t *testing.T) {
	dataDir := t.TempDir()

	out, err := runIn(t, dataDir, gitRepo(t), "ticket", "Remove staging infrastructure", "Remove the app.")
	if err != nil {
		t.Fatal(err)
	}
	if out != "1\n" {
		t.Errorf("the command wrote %q, want %q", out, "1\n")
	}
	if got := proseOf(t, dataDir); got != "Remove the app." {
		t.Errorf("the prose is %q", got)
	}
}

func TestRunTicketWithNoArgumentsOpensTheEditor(t *testing.T) {
	dataDir := t.TempDir()
	withEditor(t, "Remove staging infrastructure\n\nRemove the app.\n")

	out, err := runIn(t, dataDir, gitRepo(t), "ticket")
	if err != nil {
		t.Fatal(err)
	}
	if out != "1\n" {
		t.Errorf("the command wrote %q, want %q", out, "1\n")
	}

	// The title comes from the editor, and not from an argument that is not
	// there, so the title says that the editor gave it.
	queue, err := openStore(t, dataDir).ListQueue()
	if err != nil {
		t.Fatal(err)
	}
	if want := "Remove staging infrastructure"; queue[0].Title != want {
		t.Errorf("title = %q, want %q", queue[0].Title, want)
	}
}

// A command that gives an error writes no id.
func TestRunTicketThatFailsShowsNothing(t *testing.T) {
	dataDir := t.TempDir()

	out, err := runIn(t, dataDir, gitRepo(t), "ticket", "   ")
	if !errors.Is(err, ErrNoTitle) {
		t.Fatalf("err = %v, want ErrNoTitle", err)
	}
	if out != "" {
		t.Errorf("the command wrote %q, want nothing", out)
	}
}

func TestRunWithNoCommand(t *testing.T) {
	_, err := runIn(t, t.TempDir(), gitRepo(t))
	if !errors.Is(err, ErrNoCommand) {
		t.Errorf("err = %v, want ErrNoCommand", err)
	}
}

func TestRunWithACommandThatIsNotThere(t *testing.T) {
	_, err := runIn(t, t.TempDir(), gitRepo(t), "banana")
	if !errors.Is(err, ErrUnknownCommand) {
		t.Fatalf("err = %v, want ErrUnknownCommand", err)
	}
	// the message names the command, so the person can see the mistake
	if !strings.Contains(err.Error(), "banana") {
		t.Errorf("err = %v, and it does not name the command", err)
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
