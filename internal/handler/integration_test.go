//go:build integration

package handler

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/alcubie/delegator/internal/testfix"
)

// The words the session is given. The file word is written into the
// repository and committed, so a question about it is a question about the
// file. The kept word goes into no file, so an answer that has it came from
// the session and from nowhere else.
const (
	integrationFile = "hello.txt"
	integrationWord = "marmalade"
	integrationKept = "peppercorn"
)

// The three prompts of the test. The first asks for the file and the commit
// in one turn, so the turn holds a permission for an edit and one for a
// command, and it plants the word that the terminal asks for later.
const (
	integrationWork = "Write the file " + integrationFile + " at the root of this repository. " +
		"Its only content is the word " + integrationWord + ". " +
		"Then stage it and commit it with the message \"Add " + integrationFile + "\". " +
		"Keep the word " + integrationKept + " in mind for a later question, and write it into no file."
	integrationAsk    = "What is the only word in " + integrationFile + "? Reply with that word and nothing else."
	integrationRecall = "What word did I ask you to keep in mind? Reply with that word and nothing else."
)

// integrationTurn is how long one turn of a real agent has. A turn that takes
// longer than this has gone wrong, and the test says so rather than holding
// the run to the timeout of the whole package.
const integrationTurn = 5 * time.Minute

// agentOnThePath is the registry entry that delegator itself would start. A
// machine without that command skips the test and says which command it
// wanted, because an agent nobody has installed is not a failure of the code.
func agentOnThePath(t *testing.T, name string) (string, []string, []string) {
	t.Helper()
	agent, err := testfix.OpenStore(t, t.TempDir()).Agent(name)
	if err != nil {
		t.Fatal(err)
	}
	if len(agent.Argv) == 0 {
		t.Skipf("the agent %q has no command, so no session is driven", name)
	}
	if _, err := exec.LookPath(agent.Argv[0]); err != nil {
		t.Skipf("%s is not on the PATH, so no %s session is driven: %v", agent.Argv[0], name, err)
	}
	return agent.Name, agent.Argv, agent.Resume
}

// integrationRepo is a git repository of one commit, with an identity of its
// own so that the commit the agent makes does not depend on the git config of
// the person who runs the test.
func integrationRepo(t *testing.T) string {
	t.Helper()
	repo := testfix.Repo(t, "main")
	testfix.GitIn(t, repo, "config", "user.email", "test@example.com")
	testfix.GitIn(t, repo, "config", "user.name", "Test")
	testfix.CommitIn(t, repo, "first")
	return repo
}

// allowed says whether the policy answered a permission with an allow.
func allowed(events []Event) bool {
	return slices.ContainsFunc(events, func(e Event) bool {
		return e.Type == TypePermission && e.Status == StatusAllowed
	})
}

// permissions is the line each permission of the turn leaves: the tool the
// agent asked for and the answer it was given. A turn holds hundreds of
// events and a failure is about these, so it shows these and not the turn.
func permissions(events []Event) []string {
	var asked []string
	for _, e := range events {
		if e.Type == TypePermission {
			asked = append(asked, e.Tool+": "+e.Status)
		}
	}
	return asked
}

// said is what the agent said in the turn, and not what a loaded session
// replayed of the turns before it.
func said(events []Event) string {
	var text strings.Builder
	for _, e := range events {
		if e.Type == TypeText && !e.Replay {
			text.WriteString(e.Text)
		}
	}
	return text.String()
}

// writeAndCommit opens a session of the agent in repo and has it write the
// file and commit it. It checks that the file is on disk and that the commit
// landed, and gives back the id of the session, which the caller loads once
// the process is gone, and the events of the turn, which hold whatever the
// agent asked delegator to answer on the way.
func writeAndCommit(t *testing.T, name string, argv []string, repo string) (string, []Event) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), integrationTurn)
	defer cancel()
	s, err := Start(ctx, name, argv, AllowAll(), repo, os.Stderr)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Close() }()
	if s.ID() == "" {
		t.Fatal("the agent opened a session with no id")
	}
	events, err := turn(ctx, s, integrationWork)
	if err != nil {
		t.Fatalf("the turn failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, integrationFile)); err != nil {
		t.Fatalf("the agent wrote no %s: %v", integrationFile, err)
	}
	if got := testfix.GitOut(t, repo, "show", "HEAD:"+integrationFile); !strings.Contains(got, integrationWord) {
		t.Errorf("the commit holds %q, and the file was to hold %q", got, integrationWord)
	}
	return s.ID(), events
}

// loadAndAsk loads the session the id names and asks it one question. It
// checks that the agent replayed a history, which is what tells a loaded
// session from a new one in the same directory, and gives back what the agent
// said in the turn.
func loadAndAsk(t *testing.T, name string, argv []string, repo, id, question string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), integrationTurn)
	defer cancel()
	s, err := Load(ctx, name, argv, AllowAll(), repo, id, os.Stderr)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	defer func() { _ = s.Close() }()
	events, err := turn(ctx, s, question)
	if err != nil {
		t.Fatalf("the turn failed: %v", err)
	}
	if !slices.ContainsFunc(events, func(e Event) bool { return e.Replay }) {
		t.Errorf("the loaded session replayed nothing, and session %s has a turn behind it", id)
	}
	return said(events)
}

// driveASession is what both agents are asked for: a session that writes a
// file and commits it, and that same session loaded by its id and asked about
// the file. It gives back the repository, the id of the session and the
// events of the turn that did the work, which the two agents answer for
// differently.
func driveASession(t *testing.T, name string) (string, string, []Event) {
	t.Helper()
	name, argv, _ := agentOnThePath(t, name)
	repo := integrationRepo(t)
	id, events := writeAndCommit(t, name, argv, repo)
	t.Logf("%s opened session %s in %s", name, id, repo)
	answer := loadAndAsk(t, name, argv, repo, id, integrationAsk)
	if !strings.Contains(strings.ToLower(answer), integrationWord) {
		t.Errorf("the loaded session answered %q, and %s holds %q", answer, integrationFile, integrationWord)
	}
	return repo, id, events
}

// TestClaudeTakesASessionFromStartToTheTerminal drives the real claude
// through everything delegator asks of an agent: a new session that writes a
// file and commits it under AllowAll, the same session loaded by its id, and
// the command of the agent table that opens that session in a terminal. It
// costs money, so it runs with make integration and not with make check.
//
// The terminal step asks for the word that is in the session and in no file,
// so the answer cannot come from reading the repository. -p makes the command
// answer once and exit, where a person would get a terminal.
func TestClaudeTakesASessionFromStartToTheTerminal(t *testing.T) {
	repo, id, events := driveASession(t, "claude")
	if !allowed(events) {
		t.Errorf("the turn allowed no permission, and the policy allows every tool: %v", permissions(events))
	}

	_, _, resume := agentOnThePath(t, "claude")
	ctx, cancel := context.WithTimeout(t.Context(), integrationTurn)
	defer cancel()
	argv := make([]string, 0, len(resume)+2)
	for _, arg := range resume {
		argv = append(argv, strings.ReplaceAll(arg, "{session}", id))
	}
	argv = append(argv, "-p", integrationRecall)
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = repo
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%v: %v", cmd.Args, err)
	}
	if !strings.Contains(strings.ToLower(string(out)), integrationKept) {
		t.Errorf("%v answered %q, and the session was given %q", cmd.Args, strings.TrimSpace(string(out)), integrationKept)
	}
}

// TestCodexTakesASessionFromStartToLoad asks codex for the same session that
// claude takes, up to the terminal: codex resumes into a terminal of its own
// and has no batch form here, so the last step of claude's test is claude's
// alone. It skips when codex-acp is not installed.
//
// It does not ask that the policy answered a permission. codex-acp 1.11.0
// opens a session in the mode it calls "Approve for me", which reviews its
// own approvals, and on 2026-09-15 it wrote the file and made the commit
// without asking delegator for anything. What it did ask for goes to the log,
// so a run says what the policy was given to answer.
func TestCodexTakesASessionFromStartToLoad(t *testing.T) {
	_, _, events := driveASession(t, "codex")
	asked := permissions(events)
	t.Logf("codex asked delegator to answer %d permissions: %v", len(asked), asked)
}

// TestOpenCodeTakesASessionFromStartToLoad drives OpenCode's built-in ACP
// server through a new session, a prompt, and a load of that same session.
// It skips when OpenCode is not installed; a signed-in OpenCode is required
// when make integration runs it.
func TestOpenCodeTakesASessionFromStartToLoad(t *testing.T) {
	_, _, events := driveASession(t, "opencode")
	asked := permissions(events)
	t.Logf("opencode asked delegator to answer %d permissions: %v", len(asked), asked)
}
