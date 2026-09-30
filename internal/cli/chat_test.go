package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/alcubie/delegator/internal/project"
	"github.com/alcubie/delegator/internal/run"
	"github.com/alcubie/delegator/internal/store"
	"github.com/alcubie/delegator/internal/testfix"
)

// started records launch arguments and directory. The zero value means no
// launch.
type started struct {
	argv []string
	dir  string
}

// useChat records the requested launch and substitutes a harmless program so
// dg chat still waits for a real exit status.
func useChat(t *testing.T, program string) *started {
	t.Helper()
	var record started
	saved := chat
	chat = func(argv []string, dir string) *exec.Cmd {
		record = started{argv: argv, dir: dir}
		return exec.Command(program)
	}
	t.Cleanup(func() { chat = saved })
	return &record
}

// resumeArgv is the command that opens a session of claude in a terminal, the
// argv dg chat builds from the table of agents.
func resumeArgv(session string) []string {
	return []string{"claude", "--resume", session}
}

// chattableTicket creates a non-running ticket with a saved session and
// existing worktree, returning ID, repository, and session.
func chattableTicket(t *testing.T, dataDir string) (int64, string, string) {
	return chattableTicketWithAgent(t, dataDir, "claude")
}

func chattableTicketWithAgent(t *testing.T, dataDir, agent string) (int64, string, string) {
	t.Helper()
	s, ticketID, repo, commit := runningTicketWithAgent(t, dataDir, agent)
	const session = "session-of-the-run"
	if err := s.SetSession(ticketID, session); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishTicket(ticketID, commit); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(run.WorktreePath(dataDir, ticketID), 0o700); err != nil {
		t.Fatal(err)
	}
	return ticketID, repo, session
}

// chattableIn creates a resumable ticket in repo with the supplied session
// ID.
func chattableIn(t *testing.T, s *store.Store, dataDir, repo, title, session string) int64 {
	t.Helper()
	id := queuedIn(t, s, repo, title)
	finishIn(t, s, id)
	if err := s.SetSession(id, session); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(run.WorktreePath(dataDir, id), 0o700); err != nil {
		t.Fatal(err)
	}
	return id
}

// twoChattableProjects makes two repositories, each with one ready ticket that
// dg chat can continue. It returns the data directory, the two repositories,
// and the session of each ticket.
func twoChattableProjects(t *testing.T) (dataDir, mine, other, mineSession, otherSession string) {
	t.Helper()
	dataDir = t.TempDir()
	mine = testfix.Repo(t, repoBranch)
	other = testfix.Repo(t, repoBranch)
	s := testfix.OpenStore(t, dataDir)

	mineSession, otherSession = "session-of-this-project", "session-of-the-other-project"
	chattableIn(t, s, dataDir, other, "the ticket of the other project", otherSession)
	chattableIn(t, s, dataDir, mine, "the ticket of this project", mineSession)
	return dataDir, mine, other, mineSession, otherSession
}

func TestChatStartsTheResumeOfTheAgent(t *testing.T) {
	dataDir := t.TempDir()
	ticketID, repo, session := chattableTicket(t, dataDir)
	record := useChat(t, "true")

	if _, err := runIn(t, dataDir, repo, "chat", fmt.Sprint(ticketID)); err != nil {
		t.Fatal(err)
	}

	want := resumeArgv(session)
	if !slices.Equal(record.argv, want) {
		t.Errorf("argv = %v, want %v", record.argv, want)
	}
}

// A failed supervisor leaves both the session and its worktree available. The
// agent can finish the work in dg chat and report its commit without dg chat
// creating a supervisor or another run.
func TestChatCanFinishAFailedTicketWithoutStartingAnotherRun(t *testing.T) {
	dataDir := t.TempDir()
	s, ticketID, repo, commit := runningTicket(t, dataDir)
	const session = "session-of-the-failed-run"
	if err := s.SetSession(ticketID, session); err != nil {
		t.Fatal(err)
	}
	held, err := s.Run(ticketID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ChangeStatus(ticketID, store.Failed); err != nil {
		t.Fatal(err)
	}
	if err := s.EndRun(held.ID, 1); err != nil {
		t.Fatal(err)
	}
	worktree := run.WorktreePath(dataDir, ticketID)
	if err := os.MkdirAll(worktree, 0o700); err != nil {
		t.Fatal(err)
	}

	var record started
	saved := chat
	chat = func(argv []string, dir string) *exec.Cmd {
		record = started{argv: argv, dir: dir}
		cmd := exec.Command(testfix.DG(t), "finish", fmt.Sprint(ticketID), commit, "--data-dir", dataDir)
		cmd.Dir = dir
		return cmd
	}
	t.Cleanup(func() { chat = saved })

	if _, err := runIn(t, dataDir, repo, "chat", fmt.Sprint(ticketID)); err != nil {
		t.Fatal(err)
	}

	if want := resumeArgv(session); !slices.Equal(record.argv, want) {
		t.Errorf("argv = %v, want %v", record.argv, want)
	}
	if record.dir != worktree {
		t.Errorf("directory = %q, want %q", record.dir, worktree)
	}
	afterRun, err := s.Run(ticketID)
	if err != nil {
		t.Fatal(err)
	}
	if afterRun.ID != held.ID {
		t.Errorf("latest run = %d, want the existing run %d", afterRun.ID, held.ID)
	}
	ticket, err := s.Ticket(ticketID)
	if err != nil {
		t.Fatal(err)
	}
	if ticket.Status != store.Ready {
		t.Errorf("status = %s, want ready", ticket.Status)
	}
	if ticket.Commit != commit {
		t.Errorf("commit_id = %q, want %q", ticket.Commit, commit)
	}
}

// Chat uses the agent that opened the session even after the default changes.
func TestChatStartsTheResumeOfTheRecordedAgent(t *testing.T) {
	dataDir := t.TempDir()
	ticketID, repo, session := chattableTicketWithAgent(t, dataDir, "codex")
	record := useChat(t, "true")

	if _, err := runIn(t, dataDir, repo, "chat", fmt.Sprint(ticketID)); err != nil {
		t.Fatal(err)
	}

	want := []string{"codex", "resume", session}
	if !slices.Equal(record.argv, want) {
		t.Errorf("argv = %v, want %v", record.argv, want)
	}
}

// The registry supplies the resume command for an existing session.
func TestChatTakesTheResumeOfTheRegistry(t *testing.T) {
	dataDir := t.TempDir()
	ticketID, repo, session := chattableTicket(t, dataDir)
	s := testfix.OpenStore(t, dataDir)
	if err := s.SaveAgent(store.Agent{Name: "claude", Argv: []string{"claude-agent-acp"}, Resume: []string{"my-claude", "--continue", "{session}"}}); err != nil {
		t.Fatal(err)
	}
	record := useChat(t, "true")

	if _, err := runIn(t, dataDir, repo, "chat", fmt.Sprint(ticketID)); err != nil {
		t.Fatal(err)
	}

	want := []string{"my-claude", "--continue", session}
	if !slices.Equal(record.argv, want) {
		t.Errorf("argv = %v, want %v", record.argv, want)
	}
}

// Reject missing resume commands and commands that omit the session ID,
// naming the agent in the error.
func TestChatRefusesAnAgentWithNoResume(t *testing.T) {
	dataDir := t.TempDir()
	ticketID, repo, _ := chattableTicket(t, dataDir)
	s := testfix.OpenStore(t, dataDir)
	if err := s.SaveAgent(store.Agent{Name: "claude", Argv: []string{"claude-agent-acp"}}); err != nil {
		t.Fatal(err)
	}
	record := useChat(t, "true")

	_, err := runIn(t, dataDir, repo, "chat", fmt.Sprint(ticketID))
	if err == nil {
		t.Fatal("dg chat started an agent that has no command that opens a session")
	}
	if !strings.Contains(err.Error(), "claude") {
		t.Errorf("error = %q, want the agent claude in it", err)
	}
	if record.argv != nil {
		t.Errorf("dg chat started %v, want nothing", record.argv)
	}
}

// Launch in the original worktree so the agent can find its session history.
func TestChatStartsInTheWorktreeOfTheTicket(t *testing.T) {
	dataDir := t.TempDir()
	ticketID, repo, _ := chattableTicket(t, dataDir)
	record := useChat(t, "true")

	if _, err := runIn(t, dataDir, repo, "chat", fmt.Sprint(ticketID)); err != nil {
		t.Fatal(err)
	}

	if want := run.WorktreePath(dataDir, ticketID); record.dir != want {
		t.Errorf("directory = %q, want %q", record.dir, want)
	}
}

// Reject active sessions to avoid concurrent writers; include the supervisor
// PID for diagnosis.
func TestChatRefusesATicketThatIsRunning(t *testing.T) {
	dataDir := t.TempDir()
	s, ticketID, repo, _ := runningTicket(t, dataDir)
	if err := s.SetSession(ticketID, "session-of-the-run"); err != nil {
		t.Fatal(err)
	}
	record := useChat(t, "true")

	_, err := runIn(t, dataDir, repo, "chat", fmt.Sprint(ticketID))
	if err == nil {
		t.Fatal("dg chat started a session that a run holds")
	}
	if want := fmt.Sprint(os.Getpid()); !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want the process id %s in it", err, want)
	}
	if record.argv != nil {
		t.Errorf("dg chat started %v, want nothing", record.argv)
	}
}

// Provide a worktree so the missing session is the only invalid condition.
func TestChatRefusesATicketWithNoSession(t *testing.T) {
	dataDir := t.TempDir()
	_, ticketID, repo := queuedTicket(t, dataDir)
	if err := os.MkdirAll(run.WorktreePath(dataDir, ticketID), 0o700); err != nil {
		t.Fatal(err)
	}
	record := useChat(t, "true")

	_, err := runIn(t, dataDir, repo, "chat", fmt.Sprint(ticketID))
	if err == nil {
		t.Fatal("dg chat continued a ticket that has no session")
	}
	if record.argv != nil {
		t.Errorf("dg chat started %v, want nothing", record.argv)
	}
}

// Refuse a missing worktree rather than resume in an unrelated directory.
func TestChatRefusesATicketWithNoWorktree(t *testing.T) {
	dataDir := t.TempDir()
	ticketID, repo, _ := chattableTicket(t, dataDir)
	if err := os.RemoveAll(run.WorktreePath(dataDir, ticketID)); err != nil {
		t.Fatal(err)
	}
	record := useChat(t, "true")

	_, err := runIn(t, dataDir, repo, "chat", fmt.Sprint(ticketID))
	if err == nil {
		t.Fatal("dg chat continued a session whose worktree is gone")
	}
	if record.argv != nil {
		t.Errorf("dg chat started %v, want nothing", record.argv)
	}
}

func TestChatGivesTheStatusOfTheProgramItStarted(t *testing.T) {
	dataDir := t.TempDir()
	ticketID, repo, _ := chattableTicket(t, dataDir)
	useChat(t, "false")

	_, err := runIn(t, dataDir, repo, "chat", fmt.Sprint(ticketID))

	var exit ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("err = %v, want an ExitError", err)
	}
	if exit.Code != 1 {
		t.Errorf("code = %d, want 1", exit.Code)
	}
}

// Resume with the original worktree and inherited terminal streams.
func TestChatCmdTakesTheDirectoryAndTheTerminal(t *testing.T) {
	argv := []string{"agent", "--resume", "s-1"}

	cmd := chatCmd(argv, "/the/worktree")

	if !slices.Equal(cmd.Args, argv) {
		t.Errorf("args = %v, want %v", cmd.Args, argv)
	}
	if cmd.Dir != "/the/worktree" {
		t.Errorf("directory = %q, want %q", cmd.Dir, "/the/worktree")
	}
	if cmd.Stdin != os.Stdin || cmd.Stdout != os.Stdout || cmd.Stderr != os.Stderr {
		t.Error("the streams of the command are not the streams of the terminal")
	}
}

// Implicit chat follows ready position, not ID; finish the second-created
// ticket first.
func TestChatWithNoIDContinuesTheHeadOfReady(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	s := testfix.OpenStore(t, dataDir)
	record := useChat(t, "true")

	later := queuedIn(t, s, repo, "the ticket that finished last")
	head := chattableIn(t, s, dataDir, repo, "the ticket that finished first", "session-of-the-head")
	nextSecond(t)
	finishIn(t, s, later)

	if _, err := runIn(t, dataDir, repo, "chat"); err != nil {
		t.Fatal(err)
	}

	want := resumeArgv("session-of-the-head")
	if !slices.Equal(record.argv, want) {
		t.Errorf("argv = %v, want the head %d resumed with %v", record.argv, head, want)
	}
}

// Implicit chat must stay within the requested project.
func TestChatWithNoIDSkipsAnotherProject(t *testing.T) {
	dataDir, mine, _, mineSession, _ := twoChattableProjects(t)
	record := useChat(t, "true")

	if _, err := runIn(t, dataDir, mine, "chat"); err != nil {
		t.Fatal(err)
	}

	want := resumeArgv(mineSession)
	if !slices.Equal(record.argv, want) {
		t.Errorf("argv = %v, want %v", record.argv, want)
	}
}

func TestChatWithNoIDInALinkedWorktreeFindsThePrimaryProject(t *testing.T) {
	dataDir := t.TempDir()
	primary, linked := linkedWorktree(t)
	s := testfix.OpenStore(t, dataDir)
	chattableIn(t, s, dataDir, primary, "the ticket of the primary project", "primary-session")
	record := useChat(t, "true")

	if _, err := runIn(t, dataDir, linked, "chat"); err != nil {
		t.Fatal(err)
	}

	want := resumeArgv("primary-session")
	if !slices.Equal(record.argv, want) {
		t.Errorf("argv = %v, want %v", record.argv, want)
	}
}

// Resolve relative --project paths from the command working directory.
func TestChatWithNoIDTakesTheProjectOfTheFlag(t *testing.T) {
	dataDir, mine, other, _, otherSession := twoChattableProjects(t)
	record := useChat(t, "true")

	relative, err := filepath.Rel(mine, other)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runIn(t, dataDir, mine, "chat", "--project", relative); err != nil {
		t.Fatal(err)
	}

	want := resumeArgv(otherSession)
	if !slices.Equal(record.argv, want) {
		t.Errorf("argv = %v, want %v", record.argv, want)
	}
}

// Report no ready work in the requested project without opening another
// project's session.
func TestChatWithNoIDAndNoReadyTicket(t *testing.T) {
	dataDir := t.TempDir()
	mine := testfix.Repo(t, repoBranch)
	other := testfix.Repo(t, repoBranch)
	s := testfix.OpenStore(t, dataDir)
	record := useChat(t, "true")

	chattableIn(t, s, dataDir, other, "the ticket of the other project", "session-of-the-other-project")
	queuedIn(t, s, mine, "the ticket that waits for a run")

	_, err := runIn(t, dataDir, mine, "chat")
	if err == nil {
		t.Fatal("dg chat with no ready ticket continued a session")
	}
	if !strings.Contains(err.Error(), mine) {
		t.Errorf("err = %v, and it does not name the project %s", err, mine)
	}
	if record.argv != nil {
		t.Errorf("dg chat started %v, want nothing", record.argv)
	}
}

func TestChatWithNoIDOutsideAProject(t *testing.T) {
	record := useChat(t, "true")

	_, err := runIn(t, t.TempDir(), t.TempDir(), "chat")

	if !errors.Is(err, project.ErrNotARepository) {
		t.Fatalf("err = %v, want ErrNotARepository", err)
	}
	if record.argv != nil {
		t.Errorf("dg chat started %v, want nothing", record.argv)
	}
}

// A session without a run is corrupt state, not a reason to use config.
func TestChatRefusesSessionWithoutRun(t *testing.T) {
	dataDir := t.TempDir()
	s := testfix.OpenStore(t, dataDir)
	repo := testfix.Repo(t, repoBranch)
	id := queuedIn(t, s, repo, "session without run")
	if err := s.SetSession(id, "orphan-session"); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(run.WorktreePath(dataDir, id), 0o700); err != nil {
		t.Fatal(err)
	}
	record := useChat(t, "true")
	_, err := runIn(t, dataDir, repo, "chat", fmt.Sprint(id))
	if !errors.Is(err, store.ErrNoRun) {
		t.Fatalf("error = %v, want ErrNoRun", err)
	}
	if record.argv != nil {
		t.Errorf("started %v, want nothing", record.argv)
	}
}
