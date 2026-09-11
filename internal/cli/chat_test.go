package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/alcubie/delegator/internal/run"
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
