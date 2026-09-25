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

// waitForStatus polls until the detached run leaves Queued or Running,
// failing at the deadline. There is no child process handle to wait on.
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

// TestIntegrationClaudeRunsOneTicket exercises dg ticket, the detached agent
// run, and the agent's dg finish. It is a paid integration test excluded from
// make check. XDG_DATA_HOME and PATH direct agent commands to this test's
// database and binary.
func TestIntegrationClaudeRunsOneTicket(t *testing.T) {
	dataDir := testfix.XDGDataDir(t)
	useLaunch(t, func() *exec.Cmd {
		return exec.Command("dg", "run")
	})

	repo := testfix.Repo(t, repoBranch)
	testfix.CommitIn(t, repo, "first")

	// Let dg ticket launch the supervisor; a second manual start would
	// compete for the same ticket.
	out, err := runIn(t, dataDir, repo, "ticket", "Add a greeting",
		"Create a file named HELLO.md at the root of the repository. Its only content is the word hello.")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("ticket %s", strings.TrimSpace(out))

	s := testfix.OpenStore(t, dataDir)
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

	logs, _ := filepath.Glob(filepath.Join(dataDir, "runs", "1", "*.log"))
	if len(logs) != 1 {
		t.Errorf("logs = %v, want one", logs)
	}
	t.Logf("session %s, commit %s", ticket.Session, ticket.Commit)
}
