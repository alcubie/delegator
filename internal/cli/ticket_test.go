package cli

import (
	"errors"
	"testing"
)

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

	out, err := runIn(t, dataDir, gitRepo(t), "ticket", "Remove staging infrastructure", "Remove the staging app.")
	if err != nil {
		t.Fatal(err)
	}
	if out != "1\n" {
		t.Errorf("the command wrote %q, want %q", out, "1\n")
	}
	if got := proseOf(t, dataDir); got != "Remove the staging app." {
		t.Errorf("the prose is %q", got)
	}
}

func TestRunTicketWithNoArgumentsOpensTheEditor(t *testing.T) {
	dataDir := t.TempDir()
	withEditor(t, "Remove staging infrastructure\n\nRemove the staging app.\n")

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
