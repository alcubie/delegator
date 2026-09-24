package cli

import (
	"bytes"
	"fmt"
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

func TestInitShowsNumberedAgentNamesInTheSelector(t *testing.T) {
	dataDir := t.TempDir()
	bin := t.TempDir()
	executable(t, bin, "goose")
	executable(t, bin, "opencode")
	t.Setenv("PATH", bin)

	out, err := runInteractiveInit(t, dataDir, "\n", initOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Choose your default agent", "1. Goose", "2. OpenCode", "Selection [1]", "tokens from your plan"} {
		if !strings.Contains(out, want) {
			t.Errorf("dg init output does not contain %q:\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"COMMAND", "PATH", "RESUME", "Configure another agent"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("dg init output contains diagnostic column %q:\n%s", unwanted, out)
		}
	}
	if strings.Contains(out, "\x1b[") {
		t.Errorf("numbered selector contains terminal control sequences:\n%s", out)
	}
}

func TestInitSelectsAndPersistsAnAvailableAgent(t *testing.T) {
	dataDir := t.TempDir()
	bin := t.TempDir()
	executable(t, bin, "codex-acp")
	t.Setenv("PATH", bin)

	out, err := runInteractiveInit(t, dataDir, "\n", initOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Setup complete.\n\nDefault agent: Codex\n\nThe default agent",
		"default agent will be used to execute ticket tasks", "tokens from your plan",
		"dg          view the inbox", "dg ticket   create your first ticket",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("dg init output does not contain %q:\n%s", want, out)
		}
	}
	if strings.Count(out, "Default agent: Codex") != 1 {
		t.Errorf("dg init should name the selected default once:\n%s", out)
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

	out, err := runInteractiveInit(t, dataDir, "q\n", initOptions{}, nil)
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

func TestInitWithoutAKnownAgentExplainsHowToRegisterOne(t *testing.T) {
	dataDir := t.TempDir()
	bin := t.TempDir()
	t.Setenv("PATH", bin)

	out, err := runInteractiveInit(t, dataDir, "", initOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"No supported Agent Client Protocol (ACP) command was found", "dg agents add NAME --command /path/to/executable", "Setup is incomplete"} {
		if !strings.Contains(out, want) {
			t.Errorf("dg init output does not contain %q:\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"Choose your default agent", "Configure another agent", "Agent name", "Setup complete", "tokens from your plan"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("dg init output contains %q when no agents were found:\n%s", unwanted, out)
		}
	}
	if got, err := testfix.OpenStore(t, dataDir).DefaultAgent(); err != nil || got != "" {
		t.Errorf("default agent = %q, %v; want none", got, err)
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

	out, err := runInteractiveInit(t, dataDir, "1\ny\n", initOptions{}, install)
	if err != nil {
		t.Fatal(err)
	}
	if !installed {
		t.Fatal("dg init did not install the selected adapter")
	}
	for _, want := range []string{"1. Codex\nSelection", "Delegator uses Agent Client Protocol (ACP) to communicate with Codex while it runs ticket tasks", "The required ACP command codex-acp is not available", "npm install -g @agentclientprotocol/codex-acp", "Setup complete"} {
		if !strings.Contains(out, want) {
			t.Errorf("dg init output does not contain %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "1. Codex —") {
		t.Errorf("dg init describes the missing ACP command before Codex is selected:\n%s", out)
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

	out, err := runInteractiveInit(t, dataDir, "1\nn\n"+adapter+"\n", initOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "ACP command or absolute path") || !strings.Contains(out, "Setup complete") {
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

	out, err := runInteractiveInit(t, dataDir, "\n", initOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Codex (current)") || !strings.Contains(out, "Default agent: Codex") {
		t.Errorf("rerun did not start on and keep the current default:\n%s", out)
	}
	if got, err := testfix.OpenStore(t, dataDir).DefaultAgent(); err != nil || got != "codex" {
		t.Errorf("default agent = %q, %v; want codex", got, err)
	}
}

func TestInitOffATerminalRequiresAnAgentFlag(t *testing.T) {
	dataDir := t.TempDir()
	bin := t.TempDir()
	executable(t, bin, "goose")
	executable(t, bin, "opencode")
	t.Setenv("PATH", bin)

	out, err := runIn(t, dataDir, t.TempDir(), "init")
	if err == nil {
		t.Fatalf("dg init unexpectedly succeeded off a terminal:\n%s", out)
	}
	if want := "dg init requires an interactive terminal; use dg init --agent NAME for scripted setup"; !strings.Contains(err.Error(), want) {
		t.Errorf("dg init error = %q, want %q", err, want)
	}
	for _, unwanted := range []string{"Available agents", "Goose", "OpenCode", "Setup complete"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("non-terminal output contains %q:\n%s", unwanted, out)
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
	for _, want := range []string{"Default agent: Codex", "tokens from your plan", "dg ticket"} {
		if !strings.Contains(out, want) {
			t.Errorf("scripted setup output does not contain %q:\n%s", want, out)
		}
	}
}
