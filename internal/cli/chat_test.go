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

// started is what dg chat asked to start: the argv and the directory. A test
// that expects nothing to start compares against the zero value.
type started struct {
	argv []string
	dir  string
}

// useChat puts a recorder in place of the start of dg chat, for one test, and
// returns the record it writes. The recorder starts program in place of the
// agent, so no test starts an agent and dg chat still waits for a real status.
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

// chattableTicket makes a ticket that dg chat can continue: it has a session,
// it is not running, and its worktree is on disk. It returns the id of the
// ticket, the repository and the session.
func chattableTicket(t *testing.T, dataDir string) (int64, string, string) {
	t.Helper()
	s, ticketID, repo, commit := runningTicket(t, dataDir)
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

// chattableIn makes a ticket of the project at repo that dg chat can continue,
// and gives it session as the session of its run. It returns the id.
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

// The argv is the adapter's, because the adapter knows which program continues
// a session and with which arguments, and the person types no line of config.
func TestChatStartsTheResumeOfTheAdapter(t *testing.T) {
	dataDir := t.TempDir()
	ticketID, repo, session := chattableTicket(t, dataDir)
	fake := fakeAgent(t)
	useAgent(t, fake)
	record := useChat(t, "true")

	if _, err := runIn(t, dataDir, repo, "chat", fmt.Sprint(ticketID)); err != nil {
		t.Fatal(err)
	}

	want := fake.Resume(session)
	if !slices.Equal(record.argv, want) {
		t.Errorf("argv = %v, want %v", record.argv, want)
	}
}

// claude keeps the record of a conversation below the directory it ran in, so
// the directory of the start is the worktree of the ticket and not the
// directory the person typed in.
func TestChatStartsInTheWorktreeOfTheTicket(t *testing.T) {
	dataDir := t.TempDir()
	ticketID, repo, _ := chattableTicket(t, dataDir)
	useAgent(t, fakeAgent(t))
	record := useChat(t, "true")

	if _, err := runIn(t, dataDir, repo, "chat", fmt.Sprint(ticketID)); err != nil {
		t.Fatal(err)
	}

	if want := run.WorktreePath(dataDir, ticketID); record.dir != want {
		t.Errorf("directory = %q, want %q", record.dir, want)
	}
}

// A ticket in running has an agent on that session already, and two writers on
// one conversation is the fault this command exists to stop. The message names
// the process id, because that is what a person needs to see what holds the
// run.
func TestChatRefusesATicketThatIsRunning(t *testing.T) {
	dataDir := t.TempDir()
	s, ticketID, repo, _ := runningTicket(t, dataDir)
	if err := s.SetSession(ticketID, "session-of-the-run"); err != nil {
		t.Fatal(err)
	}
	useAgent(t, fakeAgent(t))
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

// A ticket that never ran has no conversation to continue. It is given a
// worktree, so the only thing wrong with it is the session.
func TestChatRefusesATicketWithNoSession(t *testing.T) {
	dataDir := t.TempDir()
	_, ticketID, repo := queuedTicket(t, dataDir)
	if err := os.MkdirAll(run.WorktreePath(dataDir, ticketID), 0o700); err != nil {
		t.Fatal(err)
	}
	useAgent(t, fakeAgent(t))
	record := useChat(t, "true")

	_, err := runIn(t, dataDir, repo, "chat", fmt.Sprint(ticketID))
	if err == nil {
		t.Fatal("dg chat continued a ticket that has no session")
	}
	if record.argv != nil {
		t.Errorf("dg chat started %v, want nothing", record.argv)
	}
}

// claude looks for the conversation below the directory it ran in, so a start
// with no worktree gives a new conversation under the id of the old one, which
// is the fault this command exists to stop.
func TestChatRefusesATicketWithNoWorktree(t *testing.T) {
	dataDir := t.TempDir()
	ticketID, repo, _ := chattableTicket(t, dataDir)
	if err := os.RemoveAll(run.WorktreePath(dataDir, ticketID)); err != nil {
		t.Fatal(err)
	}
	useAgent(t, fakeAgent(t))
	record := useChat(t, "true")

	_, err := runIn(t, dataDir, repo, "chat", fmt.Sprint(ticketID))
	if err == nil {
		t.Fatal("dg chat continued a session whose worktree is gone")
	}
	if record.argv != nil {
		t.Errorf("dg chat started %v, want nothing", record.argv)
	}
}

// dg is the terminal of the conversation for as long as it runs, so the status
// of dg is the status of the program it started. cmd/dg exits with the code
// this error carries.
func TestChatGivesTheStatusOfTheProgramItStarted(t *testing.T) {
	dataDir := t.TempDir()
	ticketID, repo, _ := chattableTicket(t, dataDir)
	useAgent(t, fakeAgent(t))
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

// The command that continues a session runs in the worktree, with the three
// streams of the terminal the person typed in, because the conversation is
// between the person and the agent and dg is only the program that started it.
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

// The ticket a person has something to say to is nearly always the one they
// have just read, so dg chat with no id continues the head of READY. The head
// is the ready ticket that finished first, and it is not the smallest id: the
// ticket that finished first here is the second one made.
func TestChatWithNoIDContinuesTheHeadOfReady(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	s := testfix.OpenStore(t, dataDir)
	fake := fakeAgent(t)
	useAgent(t, fake)
	record := useChat(t, "true")

	later := queuedIn(t, s, repo, "the ticket that finished last")
	head := chattableIn(t, s, dataDir, repo, "the ticket that finished first", "session-of-the-head")
	nextSecond(t)
	finishIn(t, s, later)

	if _, err := runIn(t, dataDir, repo, "chat"); err != nil {
		t.Fatal(err)
	}

	want := fake.Resume("session-of-the-head")
	if !slices.Equal(record.argv, want) {
		t.Errorf("argv = %v, want the head %d resumed with %v", record.argv, head, want)
	}
}

// The head of READY is the head for one project. A person who says something
// to the work of this repository must not land in the conversation of another
// one, whatever the order of the inbox as a whole.
func TestChatWithNoIDSkipsAnotherProject(t *testing.T) {
	dataDir, mine, _, mineSession, _ := twoChattableProjects(t)
	fake := fakeAgent(t)
	useAgent(t, fake)
	record := useChat(t, "true")

	if _, err := runIn(t, dataDir, mine, "chat"); err != nil {
		t.Fatal(err)
	}

	want := fake.Resume(mineSession)
	if !slices.Equal(record.argv, want) {
		t.Errorf("argv = %v, want %v", record.argv, want)
	}
}

// --project names the project, as it does on dg show and dg accept, so a
// person continues the work of a repository from somewhere else. The path here
// is relative, which is the form that has a directory to be joined to.
func TestChatWithNoIDTakesTheProjectOfTheFlag(t *testing.T) {
	dataDir, mine, other, _, otherSession := twoChattableProjects(t)
	fake := fakeAgent(t)
	useAgent(t, fake)
	record := useChat(t, "true")

	relative, err := filepath.Rel(mine, other)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runIn(t, dataDir, mine, "chat", "--project", relative); err != nil {
		t.Fatal(err)
	}

	want := fake.Resume(otherSession)
	if !slices.Equal(record.argv, want) {
		t.Errorf("argv = %v, want %v", record.argv, want)
	}
}

// A project with nothing ready has no conversation to continue, and the error
// names it, because a person who gave --project may be looking at a project
// that is not the one they meant. Another project's session is not an answer
// to the question that was asked.
func TestChatWithNoIDAndNoReadyTicket(t *testing.T) {
	dataDir := t.TempDir()
	mine := testfix.Repo(t, repoBranch)
	other := testfix.Repo(t, repoBranch)
	s := testfix.OpenStore(t, dataDir)
	useAgent(t, fakeAgent(t))
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

// A directory outside any repository names no project, so there is no head of
// READY whose session to continue.
func TestChatWithNoIDOutsideAProject(t *testing.T) {
	useAgent(t, fakeAgent(t))
	record := useChat(t, "true")

	_, err := runIn(t, t.TempDir(), t.TempDir(), "chat")

	if !errors.Is(err, project.ErrNotARepository) {
		t.Fatalf("err = %v, want ErrNotARepository", err)
	}
	if record.argv != nil {
		t.Errorf("dg chat started %v, want nothing", record.argv)
	}
}
