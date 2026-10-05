package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alcubie/delegator/internal/project"
	"github.com/alcubie/delegator/internal/run"
	"github.com/alcubie/delegator/internal/store"
	"github.com/alcubie/delegator/internal/testfix"
)

// readyTicket creates a finished ticket with its worktree, returning the
// store, ID, and repository.
func readyTicket(t *testing.T, dataDir string) (*store.Store, int64, string) {
	t.Helper()
	s, ticketID, repo, commit := runningTicket(t, dataDir)

	ticket, err := s.Ticket(ticketID)
	if err != nil {
		t.Fatal(err)
	}
	testfix.GitIn(t, repo, "worktree", "add", run.WorktreePath(dataDir, ticketID), ticket.Branch)

	if err := s.FinishTicket(ticketID, commit); err != nil {
		t.Fatal(err)
	}
	return s, ticketID, repo
}

// commitReadyWork advances the branch of a ready ticket and records that
// commit as its report. The primary checkout stays on trunk, as it does while
// a person reviews the worktree of a real run.
func commitReadyWork(t *testing.T, s *store.Store, dataDir string, ticketID int64) store.Ticket {
	t.Helper()
	worktree := run.WorktreePath(dataDir, ticketID)
	name := "ticket-work.txt"
	if err := os.WriteFile(filepath.Join(worktree, name), []byte("the ticket change\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	testfix.GitIn(t, worktree, "add", name)
	testfix.CommitIn(t, worktree, "ticket work")
	commit := testfix.GitOut(t, worktree, "rev-parse", "HEAD")
	if err := s.FinishTicket(ticketID, commit); err != nil {
		t.Fatal(err)
	}
	ticket, err := s.Ticket(ticketID)
	if err != nil {
		t.Fatal(err)
	}
	return ticket
}

func TestAcceptClosesAReadyTicket(t *testing.T) {
	dataDir := t.TempDir()
	s, ticketID, repo := readyTicket(t, dataDir)

	out, err := runIn(t, dataDir, repo, "accept", fmt.Sprint(ticketID))
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Errorf("dg accept wrote %q, want nothing", out)
	}

	ticket, err := s.Ticket(ticketID)
	if err != nil {
		t.Fatal(err)
	}
	if ticket.Status != store.Done {
		t.Errorf("status = %q, want %q", ticket.Status, store.Done)
	}
}

func TestAcceptClosesATicketAfterItsBranchIsMerged(t *testing.T) {
	dataDir := t.TempDir()
	s, ticketID, repo := readyTicket(t, dataDir)
	ticket := commitReadyWork(t, s, dataDir, ticketID)
	testfix.GitIn(t, repo, "-c", "user.email=test@example.com", "-c", "user.name=Test",
		"merge", "-q", "--no-ff", "-m", "merge ticket", ticket.Branch)

	if _, err := runIn(t, dataDir, repo, "accept", fmt.Sprint(ticketID)); err != nil {
		t.Fatal(err)
	}
	if got := testfix.ReadTicket(t, dataDir, ticketID).Status; got != store.Done {
		t.Errorf("status = %q, want %q", got, store.Done)
	}
}

// A ready report is not enough for closure: the person must first put the
// branch into the project's HEAD. A refusal leaves both durable state and the
// worktree available for the next review.
func TestAcceptRefusesAnUnmergedBranch(t *testing.T) {
	dataDir := t.TempDir()
	s, ticketID, repo := readyTicket(t, dataDir)
	before := commitReadyWork(t, s, dataDir, ticketID)

	_, err := runIn(t, dataDir, repo, "accept", fmt.Sprint(ticketID))
	if !errors.Is(err, project.ErrBranchNotMerged) {
		t.Fatalf("err = %v, want ErrBranchNotMerged", err)
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("err = %q, want the force override", err)
	}
	after := testfix.ReadTicket(t, dataDir, ticketID)
	if after.Status != store.Ready {
		t.Errorf("status = %q, want %q", after.Status, store.Ready)
	}
	if after.Position != before.Position {
		t.Errorf("position = %d, want unchanged position %d", after.Position, before.Position)
	}
	if _, err := os.Stat(run.WorktreePath(dataDir, ticketID)); err != nil {
		t.Errorf("the worktree of the refused ticket is gone: %v", err)
	}
}

func TestAcceptClosesATicketAfterAnEquivalentSquash(t *testing.T) {
	dataDir := t.TempDir()
	s, ticketID, repo := readyTicket(t, dataDir)
	ticket := commitReadyWork(t, s, dataDir, ticketID)
	testfix.GitIn(t, repo, "merge", "-q", "--squash", "--ff", ticket.Branch)
	testfix.CommitIn(t, repo, "squash ticket")

	if _, err := runIn(t, dataDir, repo, "accept", fmt.Sprint(ticketID)); err != nil {
		t.Fatal(err)
	}
	if got := testfix.ReadTicket(t, dataDir, ticketID).Status; got != store.Done {
		t.Errorf("status = %q, want %q", got, store.Done)
	}
}

// Refuse other statuses without removing their worktrees, which may still
// have active agents.
func TestAcceptWithATicketThatIsNotReady(t *testing.T) {
	dataDir := t.TempDir()
	s, ticketID, repo, _ := runningTicket(t, dataDir)

	ticket, err := s.Ticket(ticketID)
	if err != nil {
		t.Fatal(err)
	}
	worktree := run.WorktreePath(dataDir, ticketID)
	testfix.GitIn(t, repo, "worktree", "add", worktree, ticket.Branch)

	if _, err := runIn(t, dataDir, repo, "accept", fmt.Sprint(ticketID)); err == nil {
		t.Fatal("err = nil, want ErrInvalidTicketStateChange")
	}

	if ticket, err = s.Ticket(ticketID); err != nil {
		t.Fatal(err)
	}
	if ticket.Status != store.Running {
		t.Errorf("status = %q, want %q", ticket.Status, store.Running)
	}
	if _, err := os.Stat(worktree); err != nil {
		t.Errorf("the worktree of a running agent is gone: %v", err)
	}
}

func TestAcceptRemovesTheWorktree(t *testing.T) {
	dataDir := t.TempDir()
	_, ticketID, repo := readyTicket(t, dataDir)

	worktree := run.WorktreePath(dataDir, ticketID)
	if _, err := os.Stat(worktree); err != nil {
		t.Fatalf("the fixture made no worktree: %v", err)
	}

	if _, err := runIn(t, dataDir, repo, "accept", fmt.Sprint(ticketID)); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(worktree); !os.IsNotExist(err) {
		t.Errorf("the worktree is still at %s", worktree)
	}
	// Check Git registration as well as directory removal; stale records
	// can block reuse of the path.
	if out := testfix.GitOut(t, repo, "worktree", "list"); strings.Contains(out, worktree) {
		t.Errorf("git still lists the worktree:\n%s", out)
	}
}

// Every kind of user-visible work must leave both ticket and worktree
// available for retry.
func TestAcceptRefusesDirtyWorktrees(t *testing.T) {
	tests := map[string]func(*testing.T, string){
		"staged": func(t *testing.T, worktree string) {
			if err := os.WriteFile(filepath.Join(worktree, "ticket-work.txt"), []byte("staged\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			testfix.GitIn(t, worktree, "add", "ticket-work.txt")
		},
		"unstaged": func(t *testing.T, worktree string) {
			if err := os.WriteFile(filepath.Join(worktree, "ticket-work.txt"), []byte("unstaged\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"untracked": func(t *testing.T, worktree string) {
			if err := os.WriteFile(filepath.Join(worktree, "not-committed.txt"), []byte("untracked\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, dirty := range tests {
		t.Run(name, func(t *testing.T) {
			dataDir := t.TempDir()
			s, ticketID, repo := readyTicket(t, dataDir)
			ticket := commitReadyWork(t, s, dataDir, ticketID)
			testfix.GitIn(t, repo, "merge", "-q", "--ff-only", ticket.Branch)
			worktree := run.WorktreePath(dataDir, ticketID)
			dirty(t, worktree)

			_, err := runIn(t, dataDir, repo, "accept", fmt.Sprint(ticketID))
			if !errors.Is(err, project.ErrWorktreeDirty) {
				t.Fatalf("err = %v, want ErrWorktreeDirty", err)
			}
			if got := testfix.ReadTicket(t, dataDir, ticketID).Status; got != store.Ready {
				t.Errorf("status = %q, want %q", got, store.Ready)
			}
			if _, err := os.Stat(worktree); err != nil {
				t.Errorf("the dirty worktree is gone: %v", err)
			}
		})
	}
}

func TestAcceptIgnoresIgnoredArtifacts(t *testing.T) {
	dataDir := t.TempDir()
	s, ticketID, repo := readyTicket(t, dataDir)
	worktree := run.WorktreePath(dataDir, ticketID)
	if err := os.WriteFile(filepath.Join(worktree, ".gitignore"), []byte("artifact.log\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	testfix.GitIn(t, worktree, "add", ".gitignore")
	testfix.CommitIn(t, worktree, "ignore build artifact")
	commit := testfix.GitOut(t, worktree, "rev-parse", "HEAD")
	if err := s.FinishTicket(ticketID, commit); err != nil {
		t.Fatal(err)
	}
	ticket := testfix.ReadTicket(t, dataDir, ticketID)
	testfix.GitIn(t, repo, "merge", "-q", "--ff-only", ticket.Branch)
	if err := os.WriteFile(filepath.Join(worktree, "artifact.log"), []byte("ignored\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := runIn(t, dataDir, repo, "accept", fmt.Sprint(ticketID)); err != nil {
		t.Fatal(err)
	}
	if got := testfix.ReadTicket(t, dataDir, ticketID).Status; got != store.Done {
		t.Errorf("status = %q, want %q", got, store.Done)
	}
}

func TestAcceptWithForceRemovesAWorktreeGitRefuses(t *testing.T) {
	dataDir := t.TempDir()
	s, ticketID, repo := readyTicket(t, dataDir)
	commitReadyWork(t, s, dataDir, ticketID)

	worktree := run.WorktreePath(dataDir, ticketID)
	stray := filepath.Join(worktree, "not-committed.txt")
	if err := os.WriteFile(stray, []byte("work the agent left"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := runIn(t, dataDir, repo, "accept", "--force", fmt.Sprint(ticketID)); err != nil {
		t.Fatal(err)
	}

	ticket, err := s.Ticket(ticketID)
	if err != nil {
		t.Fatal(err)
	}
	if ticket.Status != store.Done {
		t.Errorf("status = %q, want %q", ticket.Status, store.Done)
	}
	if _, err := os.Stat(worktree); !os.IsNotExist(err) {
		t.Errorf("the worktree is still at %s", worktree)
	}
	if out := testfix.GitOut(t, repo, "worktree", "list"); strings.Contains(out, worktree) {
		t.Errorf("git still lists the worktree:\n%s", out)
	}
}

func TestAcceptPreservesAnUnregisteredWorktree(t *testing.T) {
	for _, force := range []bool{false, true} {
		t.Run(fmt.Sprintf("force=%t", force), func(t *testing.T) {
			dataDir := t.TempDir()
			_, ticketID, repo := readyTicket(t, dataDir)
			testfix.SecondTicket(t, dataDir)
			l, record := testfix.RecordingLaunch(t)
			useLaunch(t, l)

			worktree := run.WorktreePath(dataDir, ticketID)
			if err := os.Remove(filepath.Join(worktree, ".git")); err != nil {
				t.Fatal(err)
			}
			leftover := filepath.Join(worktree, "leftover.txt")
			if err := os.WriteFile(leftover, []byte("preserve me\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			args := []string{"accept"}
			if force {
				args = append(args, "--force")
			}
			out, errOut, err := runInOutputs(t, dataDir, repo, "", args...)
			if err != nil {
				t.Fatal(err)
			}
			if want := fmt.Sprintf("%d\n", ticketID); out != want {
				t.Errorf("ordinary output = %q, want %q", out, want)
			}
			for _, want := range []string{"warning:", fmt.Sprint(ticketID), worktree, "no .git", "inspect it manually"} {
				if !strings.Contains(errOut, want) {
					t.Errorf("warning = %q, want it to contain %q", errOut, want)
				}
			}
			if got, err := os.ReadFile(leftover); err != nil || string(got) != "preserve me\n" {
				t.Errorf("leftover = %q, %v; want preserved contents", got, err)
			}
			if got := testfix.ReadTicket(t, dataDir, ticketID).Status; got != store.Done {
				t.Errorf("status = %q, want %q", got, store.Done)
			}
			testfix.WaitForStarts(t, record, 1)
		})
	}
}

func TestAcceptAllowsAnAbsentWorktree(t *testing.T) {
	dataDir := t.TempDir()
	_, ticketID, repo := readyTicket(t, dataDir)
	ticket := testfix.ReadTicket(t, dataDir, ticketID)
	if err := run.RemoveWorktree(dataDir, ticket, false); err != nil {
		t.Fatal(err)
	}

	_, errOut, err := runInOutputs(t, dataDir, repo, "", "accept", fmt.Sprint(ticketID))
	if err != nil {
		t.Fatal(err)
	}
	if errOut != "" {
		t.Errorf("error output = %q, want no warning", errOut)
	}
	if got := testfix.ReadTicket(t, dataDir, ticketID).Status; got != store.Done {
		t.Errorf("status = %q, want %q", got, store.Done)
	}
}

func TestAcceptStillChecksMergeWithoutARegisteredWorktree(t *testing.T) {
	for _, state := range []string{"absent", "unregistered"} {
		t.Run(state, func(t *testing.T) {
			dataDir := t.TempDir()
			s, ticketID, repo := readyTicket(t, dataDir)
			commitReadyWork(t, s, dataDir, ticketID)
			worktree := run.WorktreePath(dataDir, ticketID)
			if state == "absent" {
				if err := run.RemoveWorktree(dataDir, testfix.ReadTicket(t, dataDir, ticketID), false); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Remove(filepath.Join(worktree, ".git")); err != nil {
				t.Fatal(err)
			}

			if _, err := runIn(t, dataDir, repo, "accept", fmt.Sprint(ticketID)); !errors.Is(err, project.ErrBranchNotMerged) {
				t.Fatalf("err = %v, want ErrBranchNotMerged", err)
			}
			if got := testfix.ReadTicket(t, dataDir, ticketID).Status; got != store.Ready {
				t.Errorf("status = %q, want %q", got, store.Ready)
			}
		})
	}
}

func TestAcceptRefusesAWorktreeItCannotInspect(t *testing.T) {
	dataDir := t.TempDir()
	_, ticketID, repo := readyTicket(t, dataDir)
	worktree := run.WorktreePath(dataDir, ticketID)
	if err := os.Chmod(worktree, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(worktree, 0o700) })

	if _, err := runIn(t, dataDir, repo, "accept", fmt.Sprint(ticketID)); err == nil {
		t.Fatal("err = nil, want the worktree inspection error")
	}
	if got := testfix.ReadTicket(t, dataDir, ticketID).Status; got != store.Ready {
		t.Errorf("status = %q, want %q", got, store.Ready)
	}
}

func TestAcceptContinuesAfterCleanupFails(t *testing.T) {
	dataDir := t.TempDir()
	_, ticketID, repo := readyTicket(t, dataDir)
	testfix.SecondTicket(t, dataDir)
	l, record := testfix.RecordingLaunch(t)
	useLaunch(t, l)

	worktree := run.WorktreePath(dataDir, ticketID)
	worktreesDir := filepath.Dir(worktree)
	if err := os.Chmod(worktreesDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(worktreesDir, 0o700) })

	out, errOut, err := runInOutputs(t, dataDir, repo, "", "accept", fmt.Sprint(ticketID))
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Errorf("ordinary output = %q, want nothing", out)
	}
	for _, want := range []string{"warning:", fmt.Sprint(ticketID), worktree} {
		if !strings.Contains(errOut, want) {
			t.Errorf("warning = %q, want it to contain %q", errOut, want)
		}
	}
	if got := testfix.ReadTicket(t, dataDir, ticketID).Status; got != store.Done {
		t.Errorf("status = %q, want %q", got, store.Done)
	}
	testfix.WaitForStarts(t, record, 1)
}

// RPC is the boundary used by programs and the GUI. It receives the stable
// code of the named condition, not the catch-all code for an unknown error.
func TestRPCAcceptReportsTheCodeOfAnUnmergedBranch(t *testing.T) {
	dataDir := t.TempDir()
	s, ticketID, repo := readyTicket(t, dataDir)
	commitReadyWork(t, s, dataDir, ticketID)

	request := fmt.Sprintf(`{"jsonrpc":"2.0","method":"accept","params":{"args":[%d]},"id":1}`, ticketID)
	out, err := rpcIn(t, dataDir, repo, request)
	if err != nil {
		t.Fatal(err)
	}
	response := rpcObject(t, out)
	errorObject, ok := response["error"].(map[string]any)
	if !ok {
		t.Fatalf("response = %#v, want an error", response)
	}
	if got := errorObject["code"]; got != float64(codeBranchNotMerged) {
		t.Errorf("error code = %#v, want %d", got, codeBranchNotMerged)
	}
	if got := fmt.Sprint(errorObject["message"]); !strings.Contains(got, "--force") {
		t.Errorf("error message = %q, want the force override", got)
	}
}

// Preserve the branch after worktree removal so accepted work remains
// accessible.
func TestAcceptKeepsTheBranch(t *testing.T) {
	dataDir := t.TempDir()
	s, ticketID, repo := readyTicket(t, dataDir)

	ticket, err := s.Ticket(ticketID)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := runIn(t, dataDir, repo, "accept", fmt.Sprint(ticketID)); err != nil {
		t.Fatal(err)
	}

	if got := testfix.GitOut(t, repo, "branch", "--list", ticket.Branch); !strings.Contains(got, ticket.Branch) {
		t.Fatalf("git branch --list gives %q, and the branch of the run is gone", got)
	}
	if got := testfix.GitOut(t, repo, "rev-parse", ticket.Branch); got != ticket.Commit {
		t.Errorf("the branch is at %s, and the ticket holds %s", got, ticket.Commit)
	}
}

// Acceptance frees ready capacity and must trigger queued work.
func TestAcceptStartsTheNextTicket(t *testing.T) {
	dataDir := t.TempDir()
	_, ticketID, repo := readyTicket(t, dataDir)
	testfix.SecondTicket(t, dataDir)
	l, record := testfix.RecordingLaunch(t)
	useLaunch(t, l)

	if _, err := runIn(t, dataDir, repo, "accept", fmt.Sprint(ticketID)); err != nil {
		t.Fatal(err)
	}

	testfix.WaitForStarts(t, record, 1)
}

// Implicit acceptance follows ready position, not ID. Finish the second-
// created ticket first and verify the selected ID is printed.
func TestAcceptWithNoIDClosesTheHeadOfReady(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	s := testfix.OpenStore(t, dataDir)

	later := queuedIn(t, s, repo, "the ticket that finished last")
	head := queuedIn(t, s, repo, "the ticket that finished first")
	finishIn(t, s, head)
	finishIn(t, s, later)
	assertReadyOrder(t, s, head, later)

	out, err := runIn(t, dataDir, repo, "accept")
	if err != nil {
		t.Fatal(err)
	}
	if want := fmt.Sprintf("%d\n", head); out != want {
		t.Errorf("dg accept with no id wrote %q, want %q", out, want)
	}

	if got := testfix.ReadTicket(t, dataDir, head).Status; got != store.Done {
		t.Errorf("the head of READY is %q, want %q", got, store.Done)
	}
	if got := testfix.ReadTicket(t, dataDir, later).Status; got != store.Ready {
		t.Errorf("the ticket that finished last is %q, want %q", got, store.Ready)
	}
}

// Implicit selection must stay within the requested project.
func TestAcceptWithNoIDSkipsAnotherProject(t *testing.T) {
	dataDir, mine, _, mineID, otherID := twoProjects(t)

	out, err := runIn(t, dataDir, mine, "accept")
	if err != nil {
		t.Fatal(err)
	}
	if want := fmt.Sprintf("%d\n", mineID); out != want {
		t.Errorf("dg accept with no id wrote %q, want %q", out, want)
	}

	if got := testfix.ReadTicket(t, dataDir, mineID).Status; got != store.Done {
		t.Errorf("the ticket of this project is %q, want %q", got, store.Done)
	}
	if got := testfix.ReadTicket(t, dataDir, otherID).Status; got != store.Ready {
		t.Errorf("the ticket of the other project is %q, want %q", got, store.Ready)
	}
}

// Resolve relative --project paths from the command working directory.
func TestAcceptWithNoIDTakesTheProjectOfTheFlag(t *testing.T) {
	dataDir, mine, other, mineID, otherID := twoProjects(t)

	relative, err := filepath.Rel(mine, other)
	if err != nil {
		t.Fatal(err)
	}
	out, err := runIn(t, dataDir, mine, "accept", "--project", relative)
	if err != nil {
		t.Fatal(err)
	}
	if want := fmt.Sprintf("%d\n", otherID); out != want {
		t.Errorf("dg accept --project %s wrote %q, want %q", relative, out, want)
	}

	if got := testfix.ReadTicket(t, dataDir, otherID).Status; got != store.Done {
		t.Errorf("the ticket of the other project is %q, want %q", got, store.Done)
	}
	if got := testfix.ReadTicket(t, dataDir, mineID).Status; got != store.Ready {
		t.Errorf("the ticket of this project is %q, want %q", got, store.Ready)
	}
}

// A project without ready work must not select another project's ticket.
func TestAcceptWithNoIDAndNoReadyTicket(t *testing.T) {
	dataDir := t.TempDir()
	mine := testfix.Repo(t, repoBranch)
	other := testfix.Repo(t, repoBranch)
	s := testfix.OpenStore(t, dataDir)

	otherID := queuedIn(t, s, other, "the ticket of the other project")
	finishIn(t, s, otherID)
	queuedIn(t, s, mine, "the ticket that waits for a run")

	out, err := runIn(t, dataDir, mine, "accept")
	if err == nil {
		t.Fatalf("dg accept with no ready ticket wrote:\n%s\nwant an error", out)
	}
	if !strings.Contains(err.Error(), "ready") {
		t.Errorf("err = %v, and it does not say that no ticket is ready", err)
	}
	if !strings.Contains(err.Error(), mine) {
		t.Errorf("err = %v, and it does not name the project %s", err, mine)
	}
	if out != "" {
		t.Errorf("dg accept wrote %q, want nothing", out)
	}
	if got := testfix.ReadTicket(t, dataDir, otherID).Status; got != store.Ready {
		t.Errorf("the ticket of the other project is %q, want %q", got, store.Ready)
	}
}

func TestAcceptWithNoIDOutsideAProject(t *testing.T) {
	out, err := runIn(t, t.TempDir(), t.TempDir(), "accept")
	if !errors.Is(err, project.ErrNotARepository) {
		t.Fatalf("err = %v, want ErrNotARepository", err)
	}
	if out != "" {
		t.Errorf("dg accept wrote %q, want nothing", out)
	}
}

// Explicit IDs may select another project; resolve its merge check against
// that project's HEAD.
func TestAcceptWithAnIDClosesATicketOfAnotherProject(t *testing.T) {
	dataDir, mine, _, mineID, otherID := twoProjects(t)

	out, err := runIn(t, dataDir, mine, "accept", fmt.Sprint(otherID))
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Errorf("dg accept %d wrote %q, want nothing", otherID, out)
	}

	if got := testfix.ReadTicket(t, dataDir, otherID).Status; got != store.Done {
		t.Errorf("the ticket of the other project is %q, want %q", got, store.Done)
	}
	if got := testfix.ReadTicket(t, dataDir, mineID).Status; got != store.Ready {
		t.Errorf("the ticket of this project is %q, want %q", got, store.Ready)
	}
}

// Print the implicit ID only after acceptance succeeds.
func TestAcceptWithNoIDWritesNothingWhenTheCloseFails(t *testing.T) {
	dataDir := t.TempDir()
	_, ticketID, repo := readyTicket(t, dataDir)

	stray := filepath.Join(run.WorktreePath(dataDir, ticketID), "not-committed.txt")
	if err := os.WriteFile(stray, []byte("work the agent left"), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := runIn(t, dataDir, repo, "accept")
	if err == nil {
		t.Fatal("err = nil, want the refusal from git")
	}
	if out != "" {
		t.Errorf("dg accept wrote %q, want nothing", out)
	}
	if got := testfix.ReadTicket(t, dataDir, ticketID).Status; got != store.Ready {
		t.Errorf("status = %q, want %q", got, store.Ready)
	}
}
