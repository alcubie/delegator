package cli

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/alcubie/delegator/internal/store"
	"github.com/alcubie/delegator/internal/testfix"
)

// The command the ticket asks for: a ticket that is already in the queue is
// made to depend on another, and the command writes nothing.
func TestRunDependAfterOneTicket(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	first, second := twoTickets(t, dataDir, repo)

	out, err := runIn(t, dataDir, repo, "depend", fmt.Sprint(second), "--after", fmt.Sprint(first))
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Errorf("the command wrote %q, want nothing", out)
	}
	if got, want := testfix.Dependencies(t, dataDir, second), []int64{first}; !slices.Equal(got, want) {
		t.Errorf("ticket %d depends on %v, want %v", second, got, want)
	}
}

// The flag repeats, as it does on dg ticket.
func TestRunDependAfterTwoTickets(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	first, second := twoTickets(t, dataDir, repo)
	third, err := ticketIn(t, dataDir, repo, "Add the rest of it", "")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := runIn(t, dataDir, repo, "depend", fmt.Sprint(third),
		"--after", fmt.Sprint(first), "--after", fmt.Sprint(second)); err != nil {
		t.Fatal(err)
	}

	if got, want := testfix.Dependencies(t, dataDir, third), []int64{first, second}; !slices.Equal(got, want) {
		t.Errorf("ticket %d depends on %v, want %v", third, got, want)
	}
}

// The same command twice leaves the state the person asked for.
func TestRunDependTwiceIsNotAnError(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	first, second := twoTickets(t, dataDir, repo)
	args := []string{"depend", fmt.Sprint(second), "--after", fmt.Sprint(first)}
	if _, err := runIn(t, dataDir, repo, args...); err != nil {
		t.Fatal(err)
	}

	if _, err := runIn(t, dataDir, repo, args...); err != nil {
		t.Fatalf("the second command gave %v, want no error", err)
	}

	if got, want := testfix.Dependencies(t, dataDir, second), []int64{first}; !slices.Equal(got, want) {
		t.Errorf("ticket %d depends on %v, want %v", second, got, want)
	}
}

// The flag --remove is the way out for a ticket that depends on one that was
// cancelled.
func TestRunDependRemove(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	first, second := twoTickets(t, dataDir, repo)
	if _, err := runIn(t, dataDir, repo, "depend", fmt.Sprint(second), "--after", fmt.Sprint(first)); err != nil {
		t.Fatal(err)
	}

	out, err := runIn(t, dataDir, repo, "depend", fmt.Sprint(second), "--after", fmt.Sprint(first), "--remove")
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Errorf("the command wrote %q, want nothing", out)
	}
	if got := testfix.Dependencies(t, dataDir, second); len(got) != 0 {
		t.Errorf("ticket %d depends on %v, want nothing", second, got)
	}
}

// A remove that takes nothing away is a person who named the wrong ticket, so
// the command says so rather than leaving them believing the link is gone.
func TestRunDependRemoveALinkThatIsNotThere(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	first, second := twoTickets(t, dataDir, repo)

	_, err := runIn(t, dataDir, repo, "depend", fmt.Sprint(second), "--after", fmt.Sprint(first), "--remove")

	if !errors.Is(err, store.ErrNoDependency) {
		t.Fatalf("err = %v, want ErrNoDependency", err)
	}
}

// A link holds a ticket back in the queue and nowhere else, so the command
// refuses a ticket that has left it and says which state it is in.
func TestRunDependOnATicketThatIsNotQueued(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	first, second := twoTickets(t, dataDir, repo)
	if err := testfix.OpenStore(t, dataDir).ChangeStatus(second, store.Running); err != nil {
		t.Fatal(err)
	}

	_, err := runIn(t, dataDir, repo, "depend", fmt.Sprint(second), "--after", fmt.Sprint(first))

	if !errors.Is(err, store.ErrNotQueued) {
		t.Fatalf("err = %v, want ErrNotQueued", err)
	}
	if !strings.Contains(err.Error(), string(store.Running)) {
		t.Errorf("the error is %q, and does not say that the ticket is running", err)
	}
}

// A ticket that depends on itself could never start.
func TestRunDependOnItself(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	first, _ := twoTickets(t, dataDir, repo)

	_, err := runIn(t, dataDir, repo, "depend", fmt.Sprint(first), "--after", fmt.Sprint(first))

	if !errors.Is(err, store.ErrSelfDependency) {
		t.Fatalf("err = %v, want ErrSelfDependency", err)
	}
}

// The id of the ticket is the one argument, so a command with none of it is
// cobra's error and not a link on a ticket nobody named.
func TestRunDependWithNoAfter(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	first, _ := twoTickets(t, dataDir, repo)

	_, err := runIn(t, dataDir, repo, "depend", fmt.Sprint(first))

	if err == nil {
		t.Fatal("the command gave no error")
	}
	if !strings.Contains(err.Error(), "after") {
		t.Errorf("the error is %q, and does not name the flag --after", err)
	}
}

// An id that is not a number is the error that every command of an id gives.
func TestRunDependOnSomethingThatIsNotANumber(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	first, _ := twoTickets(t, dataDir, repo)

	_, err := runIn(t, dataDir, repo, "depend", "twelve", "--after", fmt.Sprint(first))

	if err == nil {
		t.Fatal("the command gave no error")
	}
	if !strings.Contains(err.Error(), "twelve") {
		t.Errorf("the error is %q, and does not name %q", err, "twelve")
	}
}
