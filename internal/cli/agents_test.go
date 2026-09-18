package cli

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/alcubie/delegator/internal/testfix"
)

func executable(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAgentsListsAvailableAgentsAndTheirCapabilities(t *testing.T) {
	dataDir := t.TempDir()
	bin := t.TempDir()
	executable(t, bin, "codex-acp")
	t.Setenv("PATH", bin)
	if _, err := runIn(t, dataDir, t.TempDir(), "config", "set", "default_agent", "codex"); err != nil {
		t.Fatal(err)
	}

	out, err := runIn(t, dataDir, t.TempDir(), "agents")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"NAME", "COMMAND", "PATH", "RESUME", "DEFAULT", "codex", "codex-acp", bin, "yes", "*", "does not prove the agent works"} {
		if !strings.Contains(out, want) {
			t.Errorf("agents output does not contain %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "claude-agent-acp") {
		t.Errorf("agents output includes an unavailable agent:\n%s", out)
	}
}

func TestAgentsDefaultMarkerFollowsConfigChanges(t *testing.T) {
	dataDir := t.TempDir()
	bin := t.TempDir()
	executable(t, bin, "codex-acp")
	executable(t, bin, "gemini")
	t.Setenv("PATH", bin)

	for _, name := range []string{"codex", "gemini"} {
		if _, err := runIn(t, dataDir, t.TempDir(), "config", "set", "default_agent", name); err != nil {
			t.Fatal(err)
		}
		out, err := runIn(t, dataDir, t.TempDir(), "agents")
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, line := range strings.Split(out, "\n") {
			if strings.HasPrefix(line, name+" ") {
				found = true
				if !strings.HasSuffix(strings.TrimSpace(line), "*") {
					t.Errorf("default %q has no marker:\n%s", name, out)
				}
			}
		}
		if !found {
			t.Errorf("default %q is not listed:\n%s", name, out)
		}
	}
}

func TestAgentsAllIncludesMissingAgentAndItsHint(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	out, err := runIn(t, dataDir, t.TempDir(), "agents", "--all")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"opencode acp", "missing", "Install OpenCode"} {
		if !strings.Contains(out, want) {
			t.Errorf("agents --all output does not contain %q:\n%s", want, out)
		}
	}
}

func TestAgentsDoesNotShowHintForAvailableAgent(t *testing.T) {
	dataDir := t.TempDir()
	bin := t.TempDir()
	executable(t, bin, "opencode")
	t.Setenv("PATH", bin)
	out, err := runIn(t, dataDir, t.TempDir(), "agents", "--all")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "Install OpenCode") {
		t.Errorf("agents output includes an installation hint for an available agent:\n%s", out)
	}
}

func TestAgentsAddSetsKnownAgentPathAndKeepsArguments(t *testing.T) {
	dataDir := t.TempDir()
	bin := t.TempDir()
	path := executable(t, bin, "gemini-acp")
	if _, err := runIn(t, dataDir, t.TempDir(), "agents", "add", "gemini", "--path", path); err != nil {
		t.Fatal(err)
	}
	agent, err := testfix.OpenStore(t, dataDir).Agent("gemini")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{path, "--experimental-acp"}; !slices.Equal(agent.Argv, want) {
		t.Errorf("argv = %v, want %v", agent.Argv, want)
	}
	if agent.InstallHint != "" {
		t.Errorf("install hint = %q, want empty", agent.InstallHint)
	}
}

func TestAgentsAddStoresCustomCommandAndArguments(t *testing.T) {
	dataDir := t.TempDir()
	bin := t.TempDir()
	command := executable(t, bin, "mine-acp")
	if _, err := runIn(t, dataDir, t.TempDir(), "agents", "add", "mine", "--command", command, "--arg", "serve", "--arg=--stdio"); err != nil {
		t.Fatal(err)
	}
	agent, err := testfix.OpenStore(t, dataDir).Agent("mine")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{command, "serve", "--stdio"}; !slices.Equal(agent.Argv, want) {
		t.Errorf("argv = %v, want %v", agent.Argv, want)
	}
	if agent.InstallHint != "" {
		t.Errorf("install hint = %q, want empty", agent.InstallHint)
	}
}

func TestAgentsAddRefusesMissingExecutable(t *testing.T) {
	dataDir := t.TempDir()
	_, err := runIn(t, dataDir, t.TempDir(), "agents", "add", "mine", "--command", filepath.Join(t.TempDir(), "missing"))
	if err == nil || !strings.Contains(err.Error(), "not executable") {
		t.Fatalf("error = %v, want a missing executable error", err)
	}
}

func TestAgentsCommandsAreRegisteredAsParentAndChild(t *testing.T) {
	root := Root(t.TempDir())
	agents, _, err := root.Find([]string{"agents"})
	if err != nil || agents.Name() != "agents" || agents.Parent() != root {
		t.Fatalf("agents command = %v, error = %v, want root child", agents, err)
	}
	add, _, err := root.Find([]string{"agents", "add"})
	if err != nil || add.Name() != "add" || add.Parent() != agents {
		t.Fatalf("agents add command = %v, error = %v, want agents child", add, err)
	}
	if command, _, err := root.Find([]string{"agents", "default"}); err == nil && command.Name() == "default" {
		t.Fatal("agents unexpectedly has a default subcommand")
	}
}
