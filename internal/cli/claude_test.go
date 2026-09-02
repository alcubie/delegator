//go:build integration

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alcubie/delegator/internal/project"
	"github.com/alcubie/delegator/internal/store"
)

// TestClaudeRunsOneTicket runs one real ticket through claude, end to end:
// dg ticket, dg run, and the dg finish that claude itself calls. It costs
// money and takes minutes, so it is behind the build tag "integration" and
// runs with make integration or make release, not with make check.
//
// The agent's own dg calls must reach this test's database and this test's
// build of dg, so XDG_DATA_HOME and PATH are set for the run.
func TestClaudeRunsOneTicket(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	dataDir := filepath.Join(xdg, "delegator")

	bin := t.TempDir()
	if out, err := exec.Command("go", "build", "-o", filepath.Join(bin, "dg"),
		"github.com/alcubie/delegator/cmd/dg").CombinedOutput(); err != nil {
		t.Fatalf("go build dg: %v: %s", err, out)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	repo := gitRepo(t)
	commitIn(t, repo, "first")

	out, err := runIn(t, dataDir, repo, "ticket", "Add a greeting",
		"Create a file named HELLO.md at the root of the repository. Its only content is the word hello.")
	if err != nil {
		t.Fatal(err)
	}
	id := strings.TrimSpace(out)

	if _, err := runIn(t, dataDir, repo, "run", id); err != nil {
		t.Fatalf("dg run: %v", err)
	}

	s := openStore(t, dataDir)
	ticket, err := s.Ticket(1)
	if err != nil {
		t.Fatal(err)
	}
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
