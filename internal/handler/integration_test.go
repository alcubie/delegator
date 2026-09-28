//go:build integration

package handler

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/alcubie/delegator/internal/testfix"
	"github.com/creack/pty"
)

// The file word is committed to the repository. The kept word appears only in
// the conversation, so recalling it proves session continuity rather than a
// fresh file read.
const (
	integrationFile = "hello.txt"
	integrationWord = "marmalade"
	integrationKept = "peppercorn"
)

// The first prompt requests a file edit and commit, exercising both tool
// kinds, and plants the word used by the later terminal recall check.
const (
	integrationWork = "Write the file " + integrationFile + " at the root of this repository. " +
		"Its only content is the word " + integrationWord + ". " +
		"Then stage it and commit it with the message \"Add " + integrationFile + "\". " +
		"Keep the word " + integrationKept + " in mind for a later question, and write it into no file."
	integrationAsk      = "What is the only word in " + integrationFile + "? Reply with that word and nothing else."
	integrationRecall   = "What word did I ask you to keep in mind? Reply exactly as TERMINAL-RECALL=<word>."
	integrationRecalled = "terminal-recall=" + integrationKept
)

// integrationTurn bounds each real-agent turn separately from the package
// timeout.
const integrationTurn = 5 * time.Minute

// agentOnThePath loads the production registry entry and skips when its
// executable is unavailable.
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

// integrationRepo creates a one-commit repository with an explicit identity
// independent of user Git configuration.
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

// permissions extracts tool names and policy decisions for focused failure
// output.
func permissions(events []Event) []string {
	var asked []string
	for _, e := range events {
		if e.Type == TypePermission {
			asked = append(asked, e.Tool+": "+e.Status)
		}
	}
	return asked
}

// said extracts new assistant text, excluding replayed history.
func said(events []Event) string {
	var text strings.Builder
	for _, e := range events {
		if e.Type == TypeText && !e.Replay {
			text.WriteString(e.Text)
		}
	}
	return text.String()
}

// writeAndCommit asks the agent to edit and commit, verifies both results,
// and returns the session ID and events for subsequent load and permission
// checks.
func writeAndCommit(t *testing.T, name string, argv []string, repo string) (string, []Event) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), integrationTurn)
	defer cancel()
	s, err := Start(ctx, name, argv, AllowAll(), repo, SessionOptions{}, os.Stderr)
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

// loadAndAsk resumes the session, verifies history replay, and returns its
// answer to a new question.
func loadAndAsk(t *testing.T, name string, argv []string, repo, id, question string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), integrationTurn)
	defer cancel()
	s, err := Load(ctx, name, argv, AllowAll(), repo, id, SessionOptions{}, os.Stderr)
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

// driveASession exercises editing, committing, and session reload, returning
// the repository, session ID, and initial turn events.
func driveASession(t *testing.T, name, repo string) (string, []Event) {
	t.Helper()
	name, argv, _ := agentOnThePath(t, name)
	id, events := writeAndCommit(t, name, argv, repo)
	t.Logf("%s opened session %s in %s", name, id, repo)
	answer := loadAndAsk(t, name, argv, repo, id, integrationAsk)
	if !strings.Contains(strings.ToLower(answer), integrationWord) {
		t.Errorf("the loaded session answered %q, and %s holds %q", answer, integrationFile, integrationWord)
	}
	return id, events
}

// verifyAgent keeps the ACP launch and session-load assertion apart from the
// terminal command assertion while giving both the same real session. The
// repository belongs to the parent test, so it remains on disk for the second
// subtest after the ACP process has stopped.
func verifyAgent(t *testing.T, name string, inspect func(*testing.T, []Event), resume func(*testing.T, string, string)) {
	t.Helper()
	repo := integrationRepo(t)
	var id string
	if !t.Run("ACP launch and session load", func(t *testing.T) {
		var events []Event
		id, events = driveASession(t, name, repo)
		inspect(t, events)
	}) {
		return
	}
	t.Run("terminal resume", func(t *testing.T) {
		resume(t, repo, id)
	})
}

// resumeArgv expands the registry command exactly as dg chat does. The
// caller may append only flags or a prompt that turn the terminal program
// into a finite integration test.
func resumeArgv(t *testing.T, name, id string) []string {
	t.Helper()
	_, _, resume := agentOnThePath(t, name)
	if len(resume) == 0 {
		t.Fatalf("agent %q has no terminal resume command", name)
	}
	argv := make([]string, 0, len(resume))
	for _, arg := range resume {
		argv = append(argv, strings.ReplaceAll(arg, "{session}", id))
	}
	return argv
}

// resumeAndRecall runs the registry's terminal resume command and asks for a
// word held only in the session. extra holds the agent-specific flags that
// make the terminal command answer once and exit.
func resumeAndRecall(t *testing.T, name, repo, id string, extra ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), integrationTurn)
	defer cancel()
	argv := resumeArgv(t, name, id)
	argv = append(argv, extra...)
	argv = append(argv, integrationRecall)
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = repo
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%v: %v", cmd.Args, err)
	}
	if !strings.Contains(strings.ToLower(string(out)), integrationRecalled) {
		t.Errorf("%v answered %q, and the session was given %q", cmd.Args, strings.TrimSpace(string(out)), integrationKept)
	}
}

// withoutTerminalControl removes terminal escape strings so a TUI's rendered
// answer can be searched. It accepts CSI and OSC strings, the two forms used
// by the tested clients, as well as the other strings terminated by ST.
func withoutTerminalControl(p []byte) string {
	var plain strings.Builder
	for i := 0; i < len(p); {
		if p[i] != 0x1b {
			if p[i] >= 0x20 || p[i] == '\n' {
				plain.WriteByte(p[i])
			}
			i++
			continue
		}
		i++
		if i == len(p) {
			break
		}
		switch p[i] {
		case '[':
			i++
			for i < len(p) {
				c := p[i]
				i++
				if c >= 0x40 && c <= 0x7e {
					break
				}
			}
		case ']', 'P', 'X', '^', '_':
			i++
			for i < len(p) {
				if p[i] == '\a' {
					i++
					break
				}
				if p[i] == 0x1b && i+1 < len(p) && p[i+1] == '\\' {
					i += 2
					break
				}
				i++
			}
		default:
			i++
		}
	}
	return plain.String()
}

// terminalText keeps only the characters that carry the test's words. TUIs
// often position each word separately instead of printing the spaces between
// them, so matching their rendered byte stream must not depend on whitespace.
func terminalText(p []byte) string {
	var text strings.Builder
	for _, c := range strings.ToLower(withoutTerminalControl(p)) {
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || strings.ContainsRune("-=<>", c) {
			text.WriteRune(c)
		}
	}
	return text.String()
}

// resumeAndRecallInPTY covers clients that expose no one-shot resume mode.
// confirm is a dialog that takes the default answer on Enter, or empty when
// there is none. A promptArgument client starts its turn from the optional
// positional prompt. The other kind is typed after a short startup delay;
// prompt and Enter are separate writes because TUIs otherwise treat both as
// pasted text rather than a submitted prompt.
func resumeAndRecallInPTY(t *testing.T, name, repo, id, confirm string, promptArgument bool, extra ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), integrationTurn)
	defer cancel()
	argv := append(resumeArgv(t, name, id), extra...)
	if promptArgument {
		argv = append(argv, integrationRecall)
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = repo
	terminal, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 40, Cols: 120})
	if errors.Is(err, pty.ErrUnsupported) {
		t.Skipf("%s requires a pseudo-terminal, which this platform does not provide", name)
	}
	if err != nil {
		t.Fatalf("start %v in a pseudo-terminal: %v", cmd.Args, err)
	}
	promptResult := make(chan error, 1)
	sendPrompt := func() {
		go func() {
			timer := time.NewTimer(3 * time.Second)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				promptResult <- ctx.Err()
				return
			case <-timer.C:
			}
			if _, err := io.WriteString(terminal, integrationRecall); err != nil {
				promptResult <- err
				return
			}
			timer.Reset(250 * time.Millisecond)
			select {
			case <-ctx.Done():
				promptResult <- ctx.Err()
			case <-timer.C:
				_, err := io.WriteString(terminal, "\r")
				promptResult <- err
			}
		}()
	}
	if confirm == "" && !promptArgument {
		sendPrompt()
	}

	var out bytes.Buffer
	buf := make([]byte, 4096)
	for {
		n, readErr := terminal.Read(buf)
		out.Write(buf[:n])
		plain := terminalText(out.Bytes())
		if strings.Contains(plain, terminalText([]byte(integrationRecalled))) {
			_ = terminal.Close()
			cancel()
			_ = cmd.Wait()
			return
		}
		if confirm != "" && strings.Contains(plain, terminalText([]byte(confirm))) {
			// The dialog text can reach the PTY just before the TUI starts
			// accepting keys. Let that state transition finish before Enter.
			time.Sleep(250 * time.Millisecond)
			if _, err := io.WriteString(terminal, "\r"); err != nil {
				t.Fatalf("answer the confirmation from %v: %v", cmd.Args, err)
			}
			t.Logf("answered the confirmation from %v", cmd.Args)
			confirm = ""
			out.Reset()
			if !promptArgument {
				sendPrompt()
			}
			continue
		}
		select {
		case err := <-promptResult:
			if err != nil {
				t.Fatalf("type and submit the recall prompt to %v: %v", cmd.Args, err)
			}
			t.Logf("typed and submitted the recall prompt to %v", cmd.Args)
		default:
		}
		if readErr != nil {
			_ = terminal.Close()
			waitErr := cmd.Wait()
			if ctx.Err() != nil {
				t.Fatalf("%v did not recall %q before its timeout; terminal output:\n%s", cmd.Args, integrationKept, withoutTerminalControl(out.Bytes()))
			}
			t.Fatalf("%v ended before recalling %q (read: %v; wait: %v); terminal output:\n%s", cmd.Args, integrationKept, readErr, waitErr, withoutTerminalControl(out.Bytes()))
		}
	}
}

// TestIntegrationClaudeTakesASessionFromStartToTheTerminal was verified with
// claude-agent-acp 0.77.0 and Claude Code 2.1.273. It uses the registry
// resume command with -p to recall a word stored only in the session. This
// paid test runs under make integration, not make check.
func TestIntegrationClaudeTakesASessionFromStartToTheTerminal(t *testing.T) {
	verifyAgent(t, "claude", func(t *testing.T, events []Event) {
		if !allowed(events) {
			t.Errorf("the turn allowed no permission, and the policy allows every tool: %v", permissions(events))
		}
	}, func(t *testing.T, repo, id string) {
		resumeAndRecall(t, "claude", repo, id, "-p")
	})
}

// TestIntegrationCodexTakesASessionFromStartToTheTerminal was verified with
// codex-acp 1.11.0 and Codex CLI 0.155.0. Resume runs in a PTY because that
// terminal command has no batch mode.
//
// Permission requests are logged but not required: on 2026-09-15, the
// adapter's "Approve for me" mode edited and committed without asking
// delegator for approval.
func TestIntegrationCodexTakesASessionFromStartToTheTerminal(t *testing.T) {
	verifyAgent(t, "codex", func(t *testing.T, events []Event) {
		asked := permissions(events)
		t.Logf("codex asked delegator to answer %d permissions: %v", len(asked), asked)
	}, func(t *testing.T, repo, id string) {
		// The repository is made by this test, so the PTY accepts the trust
		// dialog's default before Codex runs the positional recall prompt.
		resumeAndRecallInPTY(t, "codex", repo, id, "Trust and continue", true, "--no-alt-screen")
	})
}

// TestIntegrationGooseTakesASessionFromStartToTheTerminal verifies Goose
// 1.50.1 with [goose acp], then the registry's interactive-only
// [goose session --resume --session-id {session}] command in a pseudo-terminal.
func TestIntegrationGooseTakesASessionFromStartToTheTerminal(t *testing.T) {
	verifyAgent(t, "goose", func(t *testing.T, events []Event) {
		asked := permissions(events)
		t.Logf("goose asked delegator to answer %d permissions: %v", len(asked), asked)
	}, func(t *testing.T, repo, id string) {
		resumeAndRecallInPTY(t, "goose", repo, id, "", false)
	})
}

// TestIntegrationCursorTakesASessionFromStartToLoad verifies Cursor Agent's ACP server
// against a real authenticated session, prompt, and session load. Cursor
// Agent 2026.09.15-d2fe57e is launched as "agent acp". Its terminal client
// does not accept an ACP session id, so the registry has no resume command.
func TestIntegrationCursorTakesASessionFromStartToLoad(t *testing.T) {
	repo := integrationRepo(t)
	_, events := driveASession(t, "cursor", repo)
	asked := permissions(events)
	t.Logf("cursor asked delegator to answer %d permissions: %v", len(asked), asked)
}

// TestIntegrationOpenCodeTakesASessionFromStartToTheTerminal verifies OpenCode
// 1.18.31 with [opencode acp], then extends the registry's
// [opencode --session {session}] command with its non-interactive run command.
func TestIntegrationOpenCodeTakesASessionFromStartToTheTerminal(t *testing.T) {
	// A temporary data directory gives the ACP server and terminal client one
	// clean session database and keeps the test independent of databases left
	// by other OpenCode versions. Authentication remains in its config dir.
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	verifyAgent(t, "opencode", func(t *testing.T, events []Event) {
		asked := permissions(events)
		t.Logf("opencode asked delegator to answer %d permissions: %v", len(asked), asked)
	}, func(t *testing.T, repo, id string) {
		resumeAndRecall(t, "opencode", repo, id, "run")
	})
}

// TestIntegrationGitHubCopilotTakesASessionFromStartToTheTerminal verifies GitHub
// Copilot CLI 1.0.86 with [copilot --acp], then the registry's non-interactive
// [copilot --resume={session}] command.
func TestIntegrationGitHubCopilotTakesASessionFromStartToTheTerminal(t *testing.T) {
	verifyAgent(t, "github-copilot", func(t *testing.T, events []Event) {
		t.Logf("github-copilot asked delegator to answer %d permissions: %v", len(permissions(events)), permissions(events))
	}, func(t *testing.T, repo, id string) {
		resumeAndRecall(t, "github-copilot", repo, id, "--allow-all-tools", "-p")
	})
}

// TestIntegrationPiTakesASessionFromStartToTheTerminal verifies Pi 0.85.1
// through pi-acp 0.0.33 with [pi-acp], then extends the registry's
// [pi --session {session}] command with Pi's non-interactive print flag.
func TestIntegrationPiTakesASessionFromStartToTheTerminal(t *testing.T) {
	verifyAgent(t, "pi", func(t *testing.T, events []Event) {
		asked := permissions(events)
		t.Logf("pi asked delegator to answer %d permissions: %v", len(asked), asked)
	}, func(t *testing.T, repo, id string) {
		resumeAndRecall(t, "pi", repo, id, "-p")
	})
}
