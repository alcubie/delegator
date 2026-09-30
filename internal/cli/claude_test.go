//go:build integration

package cli

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alcubie/delegator/internal/project"
	"github.com/alcubie/delegator/internal/run"
	"github.com/alcubie/delegator/internal/store"
	"github.com/alcubie/delegator/internal/testfix"
)

// waitForStatus polls until the detached run ends and leaves Queued or
// Running, failing early if no supervisor claims it. There is no child
// process handle to wait on.
func waitForStatus(t *testing.T, s *store.Store, id int64) store.Ticket {
	t.Helper()
	started := time.Now()
	deadline := started.Add(10 * time.Minute)
	var status store.TicketStatus
	for time.Now().Before(deadline) {
		ticket, err := s.Ticket(id)
		if err != nil {
			t.Fatal(err)
		}
		if ticket.Status != status {
			t.Logf("ticket %d: %s", id, ticket.Status)
			status = ticket.Status
		}
		held, err := s.Run(id)
		if err != nil && !errors.Is(err, store.ErrNoRun) {
			t.Fatal(err)
		}
		if ticket.Status != store.Queued && ticket.Status != store.Running && err == nil && !held.EndedAt.IsZero() {
			return ticket
		}
		if ticket.Status == store.Queued && time.Since(started) > 30*time.Second {
			t.Fatalf("ticket %d is still queued after 30s; the detached supervisor did not claim it", id)
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("the run of ticket %d has not ended after 10m (status %s)", id, status)
	return store.Ticket{}
}

// TestIntegrationClaudeRunsOneTicket exercises dg ticket, the detached agent
// run, and the agent's dg finish. It is a paid integration test excluded from
// make check. XDG_DATA_HOME and PATH direct agent commands to this test's
// database and binary.
func TestIntegrationClaudeRunsOneTicket(t *testing.T) {
	t.Setenv("PATH", filepath.Dir(testfix.DG(t))+string(os.PathListSeparator)+os.Getenv("PATH"))
	dataDir := testfix.XDGDataDir(t)
	s := testfix.OpenStore(t, dataDir)
	// Fresh databases intentionally have no default until onboarding selects
	// one. This fixture drives Claude directly instead of running onboarding.
	if err := s.SetDefaultAgent("claude"); err != nil {
		t.Fatal(err)
	}
	agent, err := s.Agent("claude")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := exec.LookPath(agent.Argv[0]); err != nil {
		t.Fatalf("Claude integration requires %s: %v", agent.Argv[0], err)
	}

	useLaunch(t, func() *exec.Cmd {
		return exec.Command(testfix.DG(t), "run")
	})

	repo := testfix.Repo(t, repoBranch)
	testfix.GitIn(t, repo, "config", "user.email", "test@example.com")
	testfix.GitIn(t, repo, "config", "user.name", "Test")
	testfix.CommitIn(t, repo, "first")
	t.Cleanup(func() {
		if held, err := s.Run(1); err == nil && held.EndedAt.IsZero() {
			if err := run.Stop(held.PID, run.StopGrace); err != nil {
				t.Errorf("stop test supervisor: %v", err)
			}
		}
		if t.Failed() {
			logs, _ := filepath.Glob(filepath.Join(dataDir, "runs", "1", "*.log"))
			for _, path := range logs {
				data, err := os.ReadFile(path)
				t.Logf("run log %s (read error: %v):\n%s", path, err, data)
			}
		}
	})

	// Let dg ticket launch the supervisor; a second manual start would
	// compete for the same ticket.
	out, err := runIn(t, dataDir, repo, "ticket", "Add a greeting",
		"Create a file named HELLO.md at the root of the repository. Its only content is the word hello.")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("ticket %s", strings.TrimSpace(out))

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
