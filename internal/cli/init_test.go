package cli

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/alcubie/delegator/internal/store"
	"github.com/alcubie/delegator/internal/testfix"
)

func runInteractiveInit(t *testing.T, dataDir, input string, options initOptions, install adapterInstaller) (string, error) {
	t.Helper()
	s, err := store.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	cfg, err := s.Settings()
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader(input))
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if install == nil {
		install = func(*cobra.Command, adapterSpec) error {
			return fmt.Errorf("the test did not expect an installation")
		}
	}
	err = runInit(cmd, s, &cfg, options, readAgentSelection, install)
	return out.String(), err
}

func queueIn(t *testing.T, dataDir string) []store.QueuedTicket {
	t.Helper()
	queue, err := testfix.OpenStore(t, dataDir).ListQueue()
	if err != nil {
		t.Fatal(err)
	}
	return queue
}

func TestInitShowsOnlyAgentNamesInTheSelector(t *testing.T) {
	dataDir := t.TempDir()
	bin := t.TempDir()
	executable(t, bin, "goose")
	executable(t, bin, "opencode")
	t.Setenv("PATH", bin)

	out, err := runInteractiveInit(t, dataDir, "\r", initOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Choose your default agent", "Goose", "OpenCode", "↑/↓ move", "tokens from your plan"} {
		if !strings.Contains(out, want) {
			t.Errorf("dg init output does not contain %q:\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"COMMAND", "PATH", "RESUME"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("dg init output contains diagnostic column %q:\n%s", unwanted, out)
		}
	}
}

func TestInitSelectsAndPersistsAnAvailableAgent(t *testing.T) {
	dataDir := t.TempDir()
	bin := t.TempDir()
	executable(t, bin, "codex-acp")
	t.Setenv("PATH", bin)

	out, err := runInteractiveInit(t, dataDir, "\r", initOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Default agent set to Codex", "Setup complete. Default agent: Codex",
		"default agent will be used to execute ticket tasks", "tokens from your plan",
		"dg          view the inbox", "dg ticket   create your first ticket",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("dg init output does not contain %q:\n%s", want, out)
		}
	}
	cfg, err := testfix.OpenStore(t, dataDir).Settings()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultAgent != "codex" {
		t.Errorf("default agent = %q, want codex", cfg.DefaultAgent)
	}
	if queue := queueIn(t, dataDir); len(queue) != 0 {
		t.Errorf("dg init created tickets: %v", queue)
	}
}

func TestInitCancellationDoesNotDescribeARun(t *testing.T) {
	dataDir := t.TempDir()
	bin := t.TempDir()
	executable(t, bin, "goose")
	t.Setenv("PATH", bin)

	out, err := runInteractiveInit(t, dataDir, "q", initOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, unwanted := range []string{"default agent will", "tokens from your plan", "Next steps"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("cancelled dg init output contains %q:\n%s", unwanted, out)
		}
	}
	if got, err := testfix.OpenStore(t, dataDir).DefaultAgent(); err != nil || got != "" {
		t.Errorf("default agent = %q, %v; want none after cancellation", got, err)
	}
}

func TestInitConfiguresACommandWhenNoKnownAgentIsFound(t *testing.T) {
	dataDir := t.TempDir()
	bin := t.TempDir()
	command := executable(t, bin, "mine-acp")
	t.Setenv("PATH", bin)

	out, err := runInteractiveInit(t, dataDir, "mine\n"+command+"\n", initOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"No supported ACP agent command was found", "Saved mine", "Setup complete"} {
		if !strings.Contains(out, want) {
			t.Errorf("dg init output does not contain %q:\n%s", want, out)
		}
	}
	s := testfix.OpenStore(t, dataDir)
	agent, err := s.Agent("mine")
	if err != nil {
		t.Fatal(err)
	}
	if len(agent.Argv) != 1 || agent.Argv[0] != command {
		t.Errorf("saved agent command = %v, want %q", agent.Argv, command)
	}
	cfg, err := s.Settings()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultAgent != "mine" {
		t.Errorf("default agent = %q, want mine", cfg.DefaultAgent)
	}
}

func TestInitPromptsToInstallAMissingCodexAdapter(t *testing.T) {
	dataDir := t.TempDir()
	bin := t.TempDir()
	executable(t, bin, "codex")
	t.Setenv("PATH", bin)
	installed := false
	install := func(_ *cobra.Command, spec adapterSpec) error {
		installed = true
		if spec.Executable != "codex-acp" || spec.Package != "@agentclientprotocol/codex-acp" {
			t.Errorf("adapter = %+v, want Codex", spec)
		}
		executable(t, bin, spec.Executable)
		return nil
	}

	out, err := runInteractiveInit(t, dataDir, "\ry\n", initOptions{}, install)
	if err != nil {
		t.Fatal(err)
	}
	if !installed {
		t.Fatal("dg init did not install the selected adapter")
	}
	for _, want := range []string{"Codex — ACP adapter not installed", "codex-acp is not", "npm install -g @agentclientprotocol/codex-acp", "Setup complete"} {
		if !strings.Contains(out, want) {
			t.Errorf("dg init output does not contain %q:\n%s", want, out)
		}
	}
	if got, err := testfix.OpenStore(t, dataDir).DefaultAgent(); err != nil || got != "codex" {
		t.Errorf("default agent = %q, %v; want codex", got, err)
	}
}

func TestInitAcceptsAManualPathForADeclinedAdapterInstall(t *testing.T) {
	dataDir := t.TempDir()
	bin := t.TempDir()
	executable(t, bin, "claude")
	adapter := executable(t, t.TempDir(), "my-claude-acp")
	t.Setenv("PATH", bin)

	out, err := runInteractiveInit(t, dataDir, "\rn\n"+adapter+"\n", initOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "ACP adapter command or absolute path") || !strings.Contains(out, "Setup complete") {
		t.Errorf("dg init did not accept the manual adapter path:\n%s", out)
	}
	agent, err := testfix.OpenStore(t, dataDir).Agent("claude")
	if err != nil {
		t.Fatal(err)
	}
	if agent.Argv[0] != adapter {
		t.Errorf("Claude command = %q, want %q", agent.Argv[0], adapter)
	}
}

func TestInitRerunStartsOnTheCurrentDefaultAndCanKeepIt(t *testing.T) {
	dataDir := t.TempDir()
	bin := t.TempDir()
	executable(t, bin, "codex-acp")
	executable(t, bin, "gemini")
	t.Setenv("PATH", bin)
	if _, err := runIn(t, dataDir, t.TempDir(), "init", "--agent", "codex"); err != nil {
		t.Fatal(err)
	}

	out, err := runInteractiveInit(t, dataDir, "\r", initOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Codex (current)") || !strings.Contains(out, "Default agent set to Codex") {
		t.Errorf("rerun did not start on and keep the current default:\n%s", out)
	}
	if got, err := testfix.OpenStore(t, dataDir).DefaultAgent(); err != nil || got != "codex" {
		t.Errorf("default agent = %q, %v; want codex", got, err)
	}
}

func TestInitOffATerminalUsesNamesWithoutDiagnosticColumns(t *testing.T) {
	dataDir := t.TempDir()
	bin := t.TempDir()
	executable(t, bin, "goose")
	executable(t, bin, "opencode")
	t.Setenv("PATH", bin)

	out, err := runIn(t, dataDir, t.TempDir(), "init")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Available agents", "Goose", "OpenCode", "--agent NAME"} {
		if !strings.Contains(out, want) {
			t.Errorf("non-terminal output does not contain %q:\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"COMMAND", "PATH", "RESUME"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("non-terminal output contains diagnostic column %q:\n%s", unwanted, out)
		}
	}
}

func TestInitOffATerminalSupportsScriptedAgentSelection(t *testing.T) {
	dataDir := t.TempDir()
	bin := t.TempDir()
	executable(t, bin, "codex-acp")
	t.Setenv("PATH", bin)

	out, err := runIn(t, dataDir, t.TempDir(), "init", "--agent", "codex")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Default agent set to Codex", "tokens from your plan", "dg ticket"} {
		if !strings.Contains(out, want) {
			t.Errorf("scripted setup output does not contain %q:\n%s", want, out)
		}
	}
}

func TestInitRetriesAnInvalidCustomCommand(t *testing.T) {
	dataDir := t.TempDir()
	bin := t.TempDir()
	command := executable(t, bin, "mine-acp")
	t.Setenv("PATH", bin)
	missing := filepath.Join(t.TempDir(), "missing")

	out, err := runInteractiveInit(t, dataDir, "mine\n"+missing+"\n"+command+"\n", initOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "not an executable command or path. Try again") {
		t.Errorf("dg init did not retry the invalid command:\n%s", out)
	}
}
