//go:build integration

package cli

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alcubie/delegator/internal/project"
	"github.com/alcubie/delegator/internal/store"
	"github.com/alcubie/delegator/internal/testfix"
)

// waitForStatus returns the ticket once its run is over, and fails the test
// if the run has not ended after the wait. A real agent takes minutes, and the
// run that works the ticket is detached, so the test has nothing to wait on
// but the ticket itself. Queued counts as waiting too: dg ticket returns as
// soon as it has started the run, and the run claims the ticket after that.
func waitForStatus(t *testing.T, s *store.Store, id int64) store.Ticket {
	t.Helper()
	deadline := time.Now().Add(10 * time.Minute)
	for time.Now().Before(deadline) {
		ticket, err := s.Ticket(id)
		if err != nil {
			t.Fatal(err)
		}
		if ticket.Status != store.Queued && ticket.Status != store.Running {
			return ticket
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("the run of ticket %d has not ended", id)
	return store.Ticket{}
}

// waitForBranch waits until the run has cut the branch of the ticket, so that
// a commit the test makes after it lands on the default branch behind the run
// and not in front of it. The claim writes the branch on the ticket before git
// makes it, so the test asks the repository and not the database.
func waitForBranch(t *testing.T, s *store.Store, repo string, id int64) {
	t.Helper()
	deadline := time.Now().Add(time.Minute)
	for time.Now().Before(deadline) {
		ticket, err := s.Ticket(id)
		if err != nil {
			t.Fatal(err)
		}
		if ticket.Branch != "" && project.Command(repo, "rev-parse", "--verify", ticket.Branch).Run() == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("the run of ticket %d cut no branch", id)
}

// TestClaudeRunsOneTicket runs one real ticket through claude, end to end:
// dg ticket, the run that dg ticket starts, and the dg finish that claude
// itself calls. It costs money and takes minutes, so it is behind the build
// tag "integration" and runs with make integration or make release, not with
// make check.
//
// The agent's own dg calls must reach this test's database and this test's
// build of dg, so XDG_DATA_HOME and PATH are set for the run.
func TestClaudeRunsOneTicket(t *testing.T) {
	dataDir := testfix.XDGDataDir(t)
	useLaunch(t, func() *exec.Cmd {
		return exec.Command("dg", "run")
	})

	repo := testfix.Repo(t, repoBranch)
	testfix.CommitIn(t, repo, "first")

	// dg ticket queues the ticket and starts the run for it, so nothing here
	// calls dg run: a second one would meet a ticket already running.
	out, err := runIn(t, dataDir, repo, "ticket", "Add a greeting",
		"Create a file named HELLO.md at the root of the repository. Its only content is the word hello.")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("ticket %s", strings.TrimSpace(out))

	s := testfix.OpenStore(t, dataDir)

	// The default branch moves on under a run that is open, which is what the
	// rebase in the prompt is for. The commit below is that move, and the
	// agent must land its own commit on top of it.
	waitForBranch(t, s, repo, 1)
	testfix.CommitIn(t, repo, "second")
	moved := testfix.GitOut(t, repo, "rev-parse", repoBranch)

	ticket := waitForStatus(t, s, 1)
	if ticket.Status != store.Ready {
		t.Errorf("status = %q, want %q", ticket.Status, store.Ready)
	}
	if ticket.Session == "" {
		t.Error("no session was recorded")
	}
	if ticket.Commit == "" {
		t.Fatal("no commit was recorded")
	}

	file, err := project.Command(repo, "show", ticket.Commit+":HELLO.md").Output()
	if err != nil {
		t.Fatalf("HELLO.md is not in commit %s: %v", ticket.Commit, err)
	}
	if got := strings.TrimSpace(string(file)); got != "hello" {
		t.Errorf("HELLO.md holds %q, want %q", got, "hello")
	}

	if err := project.Command(repo, "merge-base", "--is-ancestor", moved, ticket.Commit).Run(); err != nil {
		t.Errorf("commit %s does not sit on top of %s at %s: %v", ticket.Commit, repoBranch, moved, err)
	}

	logs, _ := filepath.Glob(filepath.Join(dataDir, "runs", "1", "*.log"))
	if len(logs) != 1 {
		t.Errorf("logs = %v, want one", logs)
	}
	t.Logf("session %s, commit %s", ticket.Session, ticket.Commit)
}
