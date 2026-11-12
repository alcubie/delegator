package cli

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/alcubie/delegator/internal/project"
	"github.com/alcubie/delegator/internal/run"
	"github.com/alcubie/delegator/internal/store"
	"github.com/alcubie/delegator/internal/testfix"
)

const testAgentID int64 = 1

// queuedTicket makes a data directory holding one project and one ticket in
// the queue, in a repository that has one commit. It returns the store, the id
// of the ticket and the repository.
func queuedTicket(t *testing.T, dataDir string) (*store.Store, int64, string) {
	t.Helper()
	repo := testfix.Repo(t, repoBranch)

	s := testfix.OpenStore(t, dataDir)
	projectID, err := s.AddProject(repo, repoBranch)
	if err != nil {
		t.Fatal(err)
	}
	ticketID, err := s.AddTicket(projectID, "ticket title")
	if err != nil {
		t.Fatal(err)
	}
	return s, ticketID, repo
}

// runningTicket makes a queued ticket and claims it for a run, on a branch
// that holds one commit. It returns the store, the id of the ticket, the
// repository and the hash of that commit.
func runningTicket(t *testing.T, dataDir string) (*store.Store, int64, string, string) {
	return runningTicketWithAgent(t, dataDir, "claude")
}

func runningTicketWithAgent(t *testing.T, dataDir, agent string) (*store.Store, int64, string, string) {
	t.Helper()
	s, ticketID, repo := queuedTicket(t, dataDir)

	branch := fmt.Sprintf("delegator/%d-ticket-title", ticketID)
	testfix.GitIn(t, repo, "branch", branch)
	agentID, err := s.AgentID(agent)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Claim(ticketID, branch, agentID); err != nil {
		t.Fatal(err)
	}
	return s, ticketID, repo, testfix.GitOut(t, repo, "rev-parse", branch)
}

// runningChange makes a running ticket whose branch holds one commit above the
// default branch, in a worktree of its own, as a run of an agent leaves it. It
// returns the store, the id of the ticket, the repository and that commit.
func runningChange(t *testing.T, dataDir string) (*store.Store, int64, string, string) {
	t.Helper()
	s, ticketID, repo, _ := runningTicket(t, dataDir)
	ticket, err := s.Ticket(ticketID)
	if err != nil {
		t.Fatal(err)
	}
	worktree := run.WorktreePath(dataDir, ticketID)
	testfix.GitIn(t, repo, "worktree", "add", worktree, ticket.Branch)
	testfix.CommitFileIn(t, worktree, ticketWorkFile, "the change of the ticket\n", "ticket work")
	return s, ticketID, repo, testfix.GitOut(t, worktree, "rev-parse", "HEAD")
}

// landConflictingChange changes the file that the work of the ticket changes on
// the default branch, which the primary repository holds checked out.
func landConflictingChange(t *testing.T, repo string) {
	t.Helper()
	testfix.CommitFileIn(t, repo, ticketWorkFile, "the change of another ticket\n", "another ticket landed")
}

// mergeDefaultBranch merges the default branch into the ticket branch in its
// worktree and keeps the change of the ticket where the two conflict, which is
// what the refusal of finish asks the agent to do. It returns the merge commit.
func mergeDefaultBranch(t *testing.T, worktree string) string {
	t.Helper()
	testfix.GitAs(t, worktree, "merge", "-q", "--no-edit", "-X", "ours", repoBranch)
	return testfix.GitOut(t, worktree, "rev-parse", "HEAD")
}

// recordSession stores a session on the ticket's latest run, as the setup of
// an ACP session does.
func recordSession(t *testing.T, s *store.Store, ticketID int64, session string) {
	t.Helper()
	run, err := s.Run(ticketID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RecordSession(run.ID, session, ""); err != nil {
		t.Fatal(err)
	}
}

// sessionOf returns the session the ticket holds.
func sessionOf(t *testing.T, s *store.Store, ticketID int64) string {
	t.Helper()
	ticket, err := s.Ticket(ticketID)
	if err != nil {
		t.Fatal(err)
	}
	return ticket.Session
}

// assertTicketState fails the test unless the ticket holds status and commit,
// which is what finish writes and what a refusal of finish leaves alone.
func assertTicketState(t *testing.T, s *store.Store, ticketID int64, status store.TicketStatus, commit string) {
	t.Helper()
	ticket, err := s.Ticket(ticketID)
	if err != nil {
		t.Fatal(err)
	}
	if ticket.Status != status {
		t.Errorf("status = %s, want %s", ticket.Status, status)
	}
	if ticket.Commit != commit {
		t.Errorf("commit_id = %q, want %q", ticket.Commit, commit)
	}
}

func TestFinishReadyTicket(t *testing.T) {
	dataDir := t.TempDir()
	s, ticketID, repo, commit := runningTicket(t, dataDir)

	out, err := runIn(t, dataDir, repo, "finish", fmt.Sprint(ticketID), commit)
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Errorf("dg finish wrote %q, want nothing", out)
	}

	assertTicketState(t, s, ticketID, store.Ready, commit)
}

func TestFinishFailedTicket(t *testing.T) {
	dataDir := t.TempDir()
	s, ticketID, repo, commit := runningTicket(t, dataDir)
	if err := s.ChangeStatus(ticketID, store.Failed); err != nil {
		t.Fatal(err)
	}

	if _, err := runIn(t, dataDir, repo, "finish", fmt.Sprint(ticketID), commit); err != nil {
		t.Fatal(err)
	}

	assertTicketState(t, s, ticketID, store.Ready, commit)
}

func TestFinishRefusesQueuedDoneAndCancelledTicketsBeforeConsultingGit(t *testing.T) {
	for _, status := range []store.TicketStatus{store.Queued, store.Done, store.Cancelled} {
		t.Run(string(status), func(t *testing.T) {
			dataDir := t.TempDir()
			var s *store.Store
			var ticketID int64
			var repo string
			if status == store.Queued {
				s, ticketID, repo = queuedTicket(t, dataDir)
			} else {
				var commit string
				s, ticketID, repo, commit = runningTicket(t, dataDir)
				if status == store.Done {
					if err := s.FinishTicket(ticketID, commit, ""); err != nil {
						t.Fatal(err)
					}
				}
				if err := s.ChangeStatus(ticketID, status); err != nil {
					t.Fatal(err)
				}
			}
			before, err := s.Ticket(ticketID)
			if err != nil {
				t.Fatal(err)
			}

			unknown := strings.Repeat("0", 40)
			_, err = runIn(t, dataDir, repo, "finish", fmt.Sprint(ticketID), unknown)
			if !errors.Is(err, store.ErrInvalidTicketStateChange) {
				t.Errorf("err = %v, want ErrInvalidTicketStateChange", err)
			}

			assertTicketState(t, s, ticketID, status, before.Commit)
		})
	}
}

func TestFinishRefusesACommitGitDoesNotKnow(t *testing.T) {
	dataDir := t.TempDir()
	s, ticketID, repo, _ := runningTicket(t, dataDir)
	unknown := strings.Repeat("0", 40)

	_, err := runIn(t, dataDir, repo, "finish", fmt.Sprint(ticketID), unknown)
	if !errors.Is(err, project.ErrUnknownCommit) {
		t.Errorf("err = %v, want ErrUnknownCommit", err)
	}

	assertTicketState(t, s, ticketID, store.Running, "")
}

func TestFinishRefusesACommitTheTicketBranchDoesNotHold(t *testing.T) {
	dataDir := t.TempDir()
	s, ticketID, repo, _ := runningTicket(t, dataDir)
	testfix.CommitIn(t, repo, "commit outside the ticket branch")
	other := testfix.GitOut(t, repo, "rev-parse", "HEAD")

	_, err := runIn(t, dataDir, repo, "finish", fmt.Sprint(ticketID), other)
	if !errors.Is(err, project.ErrCommitNotOnBranch) {
		t.Errorf("err = %v, want ErrCommitNotOnBranch", err)
	}

	assertTicketState(t, s, ticketID, store.Running, "")
}

// The agent is still in its session when it runs finish, so a refusal is the
// moment it can merge the default branch itself. A ticket becomes ready only
// when its change merges at that moment.
func TestFinishRefusesACommitThatConflictsWithTheDefaultBranch(t *testing.T) {
	dataDir := t.TempDir()
	s, ticketID, repo, commit := runningChange(t, dataDir)
	landConflictingChange(t, repo)

	_, err := runIn(t, dataDir, repo, "finish", fmt.Sprint(ticketID), commit)
	if !errors.Is(err, errCommitConflicts) {
		t.Errorf("err = %v, want errCommitConflicts", err)
	}

	assertTicketState(t, s, ticketID, store.Running, "")
}

// The message is for the agent, which needs the branch to merge.
func TestFinishNamesTheDefaultBranchTheCommitConflictsWith(t *testing.T) {
	dataDir := t.TempDir()
	_, ticketID, repo, commit := runningChange(t, dataDir)
	landConflictingChange(t, repo)

	_, err := runIn(t, dataDir, repo, "finish", fmt.Sprint(ticketID), commit)
	if err == nil {
		t.Fatal("finish accepted a commit that conflicts with the default branch")
	}
	if !strings.Contains(err.Error(), repoBranch) {
		t.Errorf("the refusal does not name the default branch %s: %v", repoBranch, err)
	}
}

// dg finish and dg ticket finish are two registrations of one command, and the
// refusal belongs to both.
func TestTicketFinishRefusesACommitThatConflictsWithTheDefaultBranch(t *testing.T) {
	dataDir := t.TempDir()
	s, ticketID, repo, commit := runningChange(t, dataDir)
	landConflictingChange(t, repo)

	_, err := runIn(t, dataDir, repo, "ticket", "finish", fmt.Sprint(ticketID), commit)
	if !errors.Is(err, errCommitConflicts) {
		t.Errorf("err = %v, want errCommitConflicts", err)
	}

	assertTicketState(t, s, ticketID, store.Running, "")
}

// Finish accepts a running, a failed and a ready ticket, and a conflict refuses
// each of the three.
func TestFinishRefusesAConflictInEveryStatusItAccepts(t *testing.T) {
	for _, status := range []store.TicketStatus{store.Running, store.Failed, store.Ready} {
		t.Run(string(status), func(t *testing.T) {
			dataDir := t.TempDir()
			s, ticketID, repo, commit := runningChange(t, dataDir)
			reported := ""
			switch status {
			case store.Failed:
				if err := s.ChangeStatus(ticketID, store.Failed); err != nil {
					t.Fatal(err)
				}
			case store.Ready:
				if err := s.FinishTicket(ticketID, commit, ""); err != nil {
					t.Fatal(err)
				}
				reported = commit
			}
			landConflictingChange(t, repo)

			_, err := runIn(t, dataDir, repo, "finish", fmt.Sprint(ticketID), commit)
			if !errors.Is(err, errCommitConflicts) {
				t.Errorf("err = %v, want errCommitConflicts", err)
			}

			assertTicketState(t, s, ticketID, status, reported)
		})
	}
}

// The way out of the refusal is the merge it asks for, so finish takes the
// commit that holds it.
func TestFinishAcceptsTheCommitOfTheMergeOfTheDefaultBranch(t *testing.T) {
	dataDir := t.TempDir()
	s, ticketID, repo, commit := runningChange(t, dataDir)
	landConflictingChange(t, repo)
	if _, err := runIn(t, dataDir, repo, "finish", fmt.Sprint(ticketID), commit); err == nil {
		t.Fatal("finish accepted a commit that conflicts with the default branch")
	}
	merged := mergeDefaultBranch(t, run.WorktreePath(dataDir, ticketID))

	if _, err := runIn(t, dataDir, repo, "finish", fmt.Sprint(ticketID), merged); err != nil {
		t.Fatal(err)
	}

	assertTicketState(t, s, ticketID, store.Ready, merged)
}

// The default branch moves while a ticket is in work, and a change of another
// file is no reason to refuse the report.
func TestFinishAcceptsACommitTheDefaultBranchDoesNotConflictWith(t *testing.T) {
	dataDir := t.TempDir()
	s, ticketID, repo, commit := runningChange(t, dataDir)
	testfix.CommitFileIn(t, repo, "another-file.txt", "the change of another ticket\n", "another ticket landed")

	if _, err := runIn(t, dataDir, repo, "finish", fmt.Sprint(ticketID), commit); err != nil {
		t.Fatal(err)
	}

	assertTicketState(t, s, ticketID, store.Ready, commit)
}

func TestFinishWritesTheFullHashWhenGivenAShortHash(t *testing.T) {
	dataDir := t.TempDir()
	s, ticketID, repo, commit := runningTicket(t, dataDir)

	if _, err := runIn(t, dataDir, repo, "finish", fmt.Sprint(ticketID), commit[:7]); err != nil {
		t.Fatal(err)
	}

	ticket, err := s.Ticket(ticketID)
	if err != nil {
		t.Fatal(err)
	}
	if ticket.Commit != commit {
		t.Errorf("commit_id = %q, want full hash %q", ticket.Commit, commit)
	}
}

// A Claude Code session that moves to the background continues as a fork
// under a new session ID and finishes from there, so finish records it: the
// stored session is the one dg ticket chat resumes.
func TestFinishRecordsTheSessionOfTheCallingClaudeSession(t *testing.T) {
	dataDir := t.TempDir()
	s, ticketID, repo, commit := runningTicket(t, dataDir)
	recordSession(t, s, ticketID, "before-the-fork")
	t.Setenv(claudeSessionEnv, "after-the-fork")

	if _, err := runIn(t, dataDir, repo, "finish", fmt.Sprint(ticketID), commit); err != nil {
		t.Fatal(err)
	}

	if got := sessionOf(t, s, ticketID); got != "after-the-fork" {
		t.Errorf("session = %q, want after-the-fork", got)
	}
}

func TestFinishWithNoSessionInTheEnvironmentKeepsTheStoredSession(t *testing.T) {
	dataDir := t.TempDir()
	s, ticketID, repo, commit := runningTicket(t, dataDir)
	recordSession(t, s, ticketID, "the-session")
	t.Setenv(claudeSessionEnv, "")

	if _, err := runIn(t, dataDir, repo, "finish", fmt.Sprint(ticketID), commit); err != nil {
		t.Fatal(err)
	}

	if got := sessionOf(t, s, ticketID); got != "the-session" {
		t.Errorf("session = %q, want the-session", got)
	}
}

// A person finishing a ticket by hand from a Claude session of their own must
// not replace the session of a ticket another agent ran.
func TestFinishKeepsTheSessionOfARunByAnotherAgent(t *testing.T) {
	dataDir := t.TempDir()
	s, ticketID, repo, commit := runningTicketWithAgent(t, dataDir, "codex")
	recordSession(t, s, ticketID, "the-codex-session")
	t.Setenv(claudeSessionEnv, "a-claude-session-of-my-own")

	if _, err := runIn(t, dataDir, repo, "finish", fmt.Sprint(ticketID), commit); err != nil {
		t.Fatal(err)
	}

	if got := sessionOf(t, s, ticketID); got != "the-codex-session" {
		t.Errorf("session = %q, want the-codex-session", got)
	}
}

// A ticket that has never run has no agent to attribute a session to.
func TestFinishKeepsTheSessionOfATicketWithNoRun(t *testing.T) {
	dataDir := t.TempDir()
	s, ticketID, _ := queuedTicket(t, dataDir)
	t.Setenv(claudeSessionEnv, "a-claude-session-of-my-own")
	ticket, err := s.Ticket(ticketID)
	if err != nil {
		t.Fatal(err)
	}

	session, err := callingSession(s, ticket)
	if err != nil {
		t.Fatal(err)
	}
	if session != "" {
		t.Errorf("session = %q, want none", session)
	}
}

func TestFinishWritesTheTimeTheRunStopped(t *testing.T) {
	dataDir := t.TempDir()
	s, ticketID, repo, commit := runningTicket(t, dataDir)

	if _, err := runIn(t, dataDir, repo, "finish", fmt.Sprint(ticketID), commit); err != nil {
		t.Fatal(err)
	}

	ticket, err := s.Ticket(ticketID)
	if err != nil {
		t.Fatal(err)
	}
	if ticket.Changed.IsZero() {
		t.Fatal("the time of the change is the zero time")
	}
	if since := time.Since(ticket.Changed); since < 0 || since > time.Minute {
		t.Errorf("the ticket changed at %v, and now is %v", ticket.Changed, time.Now())
	}
	// Stored timestamps must use UTC so textual ordering matches
	// chronological ordering.
	if ticket.Changed.Location() != time.UTC {
		t.Errorf("the ticket changed at %v, want the UTC form", ticket.Changed)
	}
}
