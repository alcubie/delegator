//go:build integration

package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alcubie/delegator/internal/handler"
	"github.com/alcubie/delegator/internal/run"
	"github.com/alcubie/delegator/internal/testfix"
	"github.com/creack/pty"
)

// Exercise the real chat command and Codex terminal with an isolated config.
// A read-only default catches resume commands that add a writable cache but
// forget to select a compatible sandbox. The remembered word is never stored
// in a workspace file: writing it after resume proves conversation continuity.
func TestIntegrationCodexChatResumesWithProjectCache(t *testing.T) {
	for _, binary := range []string{"codex", "codex-acp"} {
		if _, err := exec.LookPath(binary); err != nil {
			t.Skipf("%s is unavailable: %v", binary, err)
		}
	}
	home := os.Getenv("CODEX_HOME")
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			t.Fatal(err)
		}
		home = filepath.Join(userHome, ".codex")
	}
	isolated := t.TempDir()
	// Reuse authentication without changing the user's config or sessions.
	auth, err := os.ReadFile(filepath.Join(home, "auth.json"))
	if err != nil {
		t.Fatalf("read Codex authentication: %v", err)
	}
	if err := os.WriteFile(filepath.Join(isolated, "auth.json"), auth, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(isolated, "config.toml"), []byte("sandbox_mode = \"read-only\"\napproval_policy = \"on-request\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", isolated)

	dataDir := t.TempDir()
	id, repo, _ := chattableTicketWithAgent(t, dataDir, "codex")
	worktree := run.WorktreePath(dataDir, id)
	// The ordinary chat fixture creates the directory; make it a real worktree.
	testfix.GitIn(t, repo, "worktree", "add", "--detach", worktree)
	s := testfix.OpenStore(t, dataDir)
	agent, err := s.Agent("codex")
	if err != nil {
		t.Fatal(err)
	}
	ticket := testfix.ReadTicket(t, dataDir, id)
	cache, err := run.ProjectCache(dataDir, ticket.Project.ID)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	session, err := handler.Start(ctx, "codex", agent.Argv, handler.AllowAll(), worktree,
		handler.SessionOptions{AdditionalDirectories: []string{cache}}, os.Stderr)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer session.Close()
	const word = "saffron-orbit-719"
	for _, err := range session.Prompt(ctx, "Remember the secret word "+word+" for later. Do not write it to any file. Reply OK.") {
		if err != nil {
			t.Fatalf("seed conversation: %v", err)
		}
	}
	testfix.SetSession(t, dataDir, id, session.ID())
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}

	// Append terminal isolation flags and a prompt, preserving the production
	// resume arguments and letting dg expand both session and cache paths.
	agent.Resume = append(agent.Resume, "--no-alt-screen", "--no-daemon",
		"Write the secret word I asked you to remember into $DELEGATOR_PROJECT_CACHE_DIR/recall.txt. Use a shell command, with no trailing newline. Do not ask any questions.")
	if err := s.SaveAgent(agent); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, testfix.DG(t), "chat", fmt.Sprint(id), "--data-dir", dataDir)
	cmd.Dir = repo
	terminal, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 40, Cols: 160})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = terminal.Close(); cancel(); _ = cmd.Wait() }()
	output := make(chan string, 1)
	go func() {
		var out bytes.Buffer
		buf := make([]byte, 4096)
		confirmed := false
		for {
			n, err := terminal.Read(buf)
			out.Write(buf[:n])
			if !confirmed && strings.Contains(out.String(), "Trust and continue") {
				time.Sleep(250 * time.Millisecond)
				_, _ = terminal.Write([]byte("\r"))
				confirmed = true
			}
			if err != nil {
				output <- out.String()
				return
			}
		}
	}()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			content, _ := os.ReadFile(filepath.Join(cache, "recall.txt"))
			if string(content) == word {
				return
			}
		case out := <-output:
			t.Fatalf("dg chat ended before recalling the word into the cache:\n%s", out)
		case <-ctx.Done():
			_ = terminal.Close()
			t.Fatalf("dg chat did not recall the word into the cache: %v\n%s", ctx.Err(), <-output)
		}
	}
}
