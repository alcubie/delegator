package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
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

// agentsResult reads the array that agents returns through the protocol, with
// params as the JSON object of the request.
func agentsResult(t *testing.T, dataDir, workDir, params string) []any {
	t.Helper()
	out, err := rpcIn(t, dataDir, workDir, `{"jsonrpc":"2.0","method":"agents","params":`+params+`,"id":1}`)
	if err != nil {
		t.Fatal(err)
	}
	response := rpcObject(t, out)
	if response["error"] != nil {
		t.Fatalf("agents response = %#v, want a result", response)
	}
	result, ok := response["result"].([]any)
	if !ok {
		t.Fatalf("agents result = %#v, want an array", response["result"])
	}
	return result
}

// agentArgv reads the argv of one row of an agents result.
func agentArgv(t *testing.T, row map[string]any) []string {
	t.Helper()
	values, ok := row["argv"].([]any)
	if !ok {
		t.Fatalf("agent row = %#v, want an argv array", row)
	}
	argv := make([]string, 0, len(values))
	for _, value := range values {
		arg, ok := value.(string)
		if !ok {
			t.Fatalf("argv = %#v, want strings", values)
		}
		argv = append(argv, arg)
	}
	return argv
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

func TestAgentsResultHoldsEveryRegisteredAgentAndWhetherItIsFound(t *testing.T) {
	dataDir := t.TempDir()
	bin := t.TempDir()
	executable(t, bin, "codex-acp")
	t.Setenv("PATH", bin)

	result := agentsResult(t, dataDir, t.TempDir(), "{}")
	agents, err := testfix.OpenStore(t, dataDir).Agents()
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != len(agents) {
		t.Fatalf("agents result = %#v, want %d agents", result, len(agents))
	}
	for i, agent := range agents {
		row, ok := result[i].(map[string]any)
		if !ok {
			t.Fatalf("agent row = %#v, want an object", result[i])
		}
		if row["name"] != agent.Name || !slices.Equal(agentArgv(t, row), agent.Argv) {
			t.Errorf("row %d = %#v, want %s with argv %v", i, row, agent.Name, agent.Argv)
		}
		if want := agent.Name == "codex"; row["found"] != want {
			t.Errorf("%s found = %#v, want %t", agent.Name, row["found"], want)
		}
	}
}

func TestAgentsResultKeepsMissingAgentsWithAndWithoutAll(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("PATH", t.TempDir())

	result := agentsResult(t, dataDir, t.TempDir(), "{}")
	if len(result) == 0 {
		t.Fatal("agents result is empty while the registry has agents")
	}
	for _, value := range result {
		if row := value.(map[string]any); row["found"] != false {
			t.Errorf("row = %#v, want found false", row)
		}
	}
	if withAll := agentsResult(t, dataDir, t.TempDir(), `{"all":true}`); !reflect.DeepEqual(withAll, result) {
		t.Errorf("agents result with all = %#v, want %#v", withAll, result)
	}
}

func TestAgentsRPCArgumentErrorStaysAnErrorEnvelope(t *testing.T) {
	out, err := rpcIn(t, t.TempDir(), t.TempDir(), `{"jsonrpc":"2.0","method":"agents","params":{"args":["extra"]},"id":"agents"}`)
	if err != nil {
		t.Fatal(err)
	}
	response := rpcObject(t, out)
	if _, ok := response["error"].(map[string]any); !ok {
		t.Errorf("response = %#v, want an error envelope", response)
	}
	if _, ok := response["result"]; ok {
		t.Errorf("error response has a successful result: %#v", response)
	}
}

func TestRPCDiscoverAgentsResultContract(t *testing.T) {
	document := rpcDiscover(t)
	validator := openRPCResultValidator(t, document, "agents")

	dataDir := t.TempDir()
	bin := t.TempDir()
	executable(t, bin, "codex-acp")
	t.Setenv("PATH", bin)
	result := agentsResult(t, dataDir, t.TempDir(), "{}")
	if err := validator.Validate(result); err != nil {
		t.Fatalf("agent list does not satisfy the advertised schema: %v", err)
	}
	for _, invalid := range []any{nil, false, float64(0), "", map[string]any{}} {
		if err := validator.Validate(invalid); err == nil {
			t.Errorf("agents schema accepts %#v", invalid)
		}
	}

	for _, field := range []string{"name", "argv", "found"} {
		encoded, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		var copied []any
		if err := json.Unmarshal(encoded, &copied); err != nil {
			t.Fatal(err)
		}
		delete(copied[0].(map[string]any), field)
		if err := validator.Validate(copied); err == nil {
			t.Errorf("a result missing %q satisfies the schema", field)
		}
	}
}

func TestRPCDiscoverPublishesTheAgentsResultSchema(t *testing.T) {
	if got, want := openRPCMethodNamed(t, rpcOpenRPC(Root(t.TempDir())), "agents").Result.Schema, agentsResultSchema(); !reflect.DeepEqual(got, want) {
		t.Errorf("agents result schema = %#v, want %#v", got, want)
	}
	published := openRPCMethodNamed(t, rpcDiscover(t), "agents").Result
	if published.Schema["type"] != "array" {
		t.Errorf("published agents result schema = %#v, want an array", published.Schema)
	}
	if strings.Contains(published.Description, "not yet exposed") ||
		!strings.Contains(published.Description, "all") {
		t.Errorf("agents result description = %q, want the structured contract", published.Description)
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
	models, _, err := root.Find([]string{"agents", "models"})
	if err != nil || models.Name() != "models" || models.Parent() != agents {
		t.Fatalf("agents models command = %v, error = %v, want agents child", models, err)
	}
	if command, _, err := root.Find([]string{"agents", "default"}); err == nil && command.Name() == "default" {
		t.Fatal("agents unexpectedly has a default subcommand")
	}
}

func TestAgentsHelpSpellsOutAgentClientProtocol(t *testing.T) {
	out, err := runIn(t, t.TempDir(), t.TempDir(), "agents", "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"List registered Agent Client Protocol (ACP) commands",
		"Add or update an Agent Client Protocol (ACP) command",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("dg agents --help does not contain %q:\n%s", want, out)
		}
	}
}

func TestAgentsModelsListsWhatTheAgentOffers(t *testing.T) {
	dataDir := t.TempDir()
	useFakeAgent(t, dataDir, "models: sonnet opus", "stop end_turn")

	out, err := runIn(t, dataDir, t.TempDir(), "agents", "models", "fake")
	if err != nil {
		t.Fatal(err)
	}
	if out != "sonnet\nopus\n" {
		t.Errorf("dg agents models wrote %q, want the advertised IDs in order", out)
	}
}

// The models of an agent that is not the default are what the desktop
// Settings view needs to fill its select before default_agent changes.
func TestAgentsModelsProbesAnAgentThatIsNotTheDefault(t *testing.T) {
	dataDir := t.TempDir()
	useFakeAgent(t, dataDir, "models: sonnet opus", "stop end_turn")
	registerFakeAgent(t, dataDir, "second", "models: haiku", "stop end_turn")

	out, err := runIn(t, dataDir, t.TempDir(), "agents", "models", "second")
	if err != nil {
		t.Fatal(err)
	}
	if out != "haiku\n" {
		t.Errorf("dg agents models second wrote %q, want the models of second", out)
	}
}

// The desktop Settings view reads the select of default_model from this
// result, so the models must be in the RPC result and not only in the text.
func TestAgentsModelsReturnsTheProbedModelsOverRPC(t *testing.T) {
	dataDir := t.TempDir()
	useFakeAgent(t, dataDir, "models: sonnet opus", "stop end_turn")

	out, err := rpcIn(t, dataDir, t.TempDir(),
		`{"jsonrpc":"2.0","method":"agents.models","params":{"args":["fake"]},"id":1}`)
	if err != nil {
		t.Fatal(err)
	}
	response := rpcObject(t, out)
	result, ok := response["result"].([]any)
	if !ok {
		t.Fatalf("agents.models response = %#v, want an array result", response)
	}
	if !reflect.DeepEqual(result, []any{"sonnet", "opus"}) {
		t.Errorf("agents.models result = %#v, want sonnet and opus", result)
	}
}

// An unregistered name is refused before anything is started, with the
// command that registers one.
func TestAgentsModelsRefusesAnUnregisteredAgent(t *testing.T) {
	dataDir := t.TempDir()
	useFakeAgent(t, dataDir, "models: sonnet", "stop end_turn")

	_, err := runIn(t, dataDir, t.TempDir(), "agents", "models", "absent")
	want := "agent \"absent\" is not registered.\n" +
		"Add it with:\n" +
		"    dg agents add absent --command /path/to/executable"
	if err == nil || err.Error() != want {
		t.Fatalf("error = %v, want %q", err, want)
	}
}

// A probe that cannot run leaves the caller with an error rather than an
// empty list, which would read as an agent that offers no model. Which stage
// of the session fails depends on how far the agent gets, so the assertion is
// on the agent it names.
func TestAgentsModelsReportsAProbeThatFails(t *testing.T) {
	dataDir := t.TempDir()
	testfix.UseAgent(t, dataDir, "broken", testfix.FakeAgent(t), filepath.Join(t.TempDir(), "missing-script"))

	out, err := runIn(t, dataDir, t.TempDir(), "agents", "models", "broken")
	if err == nil || !strings.Contains(err.Error(), `agent "broken"`) {
		t.Fatalf("error = %v, want a failure naming the agent", err)
	}
	if out != "" {
		t.Errorf("dg agents models wrote %q for a failed probe, want nothing", out)
	}
}

// An agent whose selector holds no model keeps the result an array, so the
// desktop select reads an empty list rather than a null.
func TestAgentsModelsReturnsAnArrayForAnEmptySelector(t *testing.T) {
	dataDir := t.TempDir()
	useFakeAgent(t, dataDir, "models:", "stop end_turn")

	out, err := rpcIn(t, dataDir, t.TempDir(),
		`{"jsonrpc":"2.0","method":"agents.models","params":{"args":["fake"]},"id":1}`)
	if err != nil {
		t.Fatal(err)
	}
	response := rpcObject(t, out)
	result, ok := response["result"].([]any)
	if !ok || len(result) != 0 {
		t.Fatalf("agents.models response = %#v, want an empty array result", response)
	}
}

// An agent without a model selector offers nothing to choose from, which is
// not the same as a probe that could not run.
func TestAgentsModelsReportsAnAgentWithNoModelOption(t *testing.T) {
	dataDir := t.TempDir()
	useFakeAgent(t, dataDir, "stop end_turn")

	_, err := runIn(t, dataDir, t.TempDir(), "agents", "models", "fake")
	want := "agent does not allow setting a specific model"
	if err == nil || err.Error() != want {
		t.Fatalf("error = %v, want %q", err, want)
	}
}
