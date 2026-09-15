package handler

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// useConfig points config at a temporary directory holding text as
// config.toml, so a test says what the file of the person holds and no test
// reads the file of the machine. Empty text leaves no file, which is a person
// who has written none.
func useConfig(t *testing.T, text string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if text == "" {
		return
	}
	path := filepath.Join(dir, "delegator", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

// namesOf is the name of each kind, in the order they came in.
func namesOf(kinds []Kind) []string {
	names := make([]string, 0, len(kinds))
	for _, k := range kinds {
		names = append(names, k.Name)
	}
	return names
}

// kindNamed finds the kind of a name and fails the test when there is none.
func kindNamed(t *testing.T, kinds []Kind, name string) Kind {
	t.Helper()
	i := slices.IndexFunc(kinds, func(k Kind) bool { return k.Name == name })
	if i < 0 {
		t.Fatalf("no agent %q in %v", name, namesOf(kinds))
	}
	return kinds[i]
}

func TestKindsGivesTheAgentsOfTheTable(t *testing.T) {
	useConfig(t, "")

	want := []string{"claude", "codex", "gemini"}
	if got := namesOf(Kinds()); !slices.Equal(got, want) {
		t.Errorf("names = %v, want %v", got, want)
	}
}

func TestKindsTakesTheArgvAndResumeOfAnAgentFromTheConfig(t *testing.T) {
	useConfig(t, "[agents.claude]\nargv = [\"my-acp\", \"--stdio\"]\nresume = [\"my-agent\", \"open\", \"{session}\"]\n")

	kinds := Kinds()
	if got := namesOf(kinds); !slices.Equal(got, []string{"claude", "codex", "gemini"}) {
		t.Errorf("names = %v, want the three of the table", got)
	}
	claude := kindNamed(t, kinds, "claude")
	if want := []string{"my-acp", "--stdio"}; !slices.Equal(claude.Argv, want) {
		t.Errorf("claude Argv = %v, want %v", claude.Argv, want)
	}
	if want := []string{"my-agent", "open", "{session}"}; !slices.Equal(claude.Resume, want) {
		t.Errorf("claude Resume = %v, want %v", claude.Resume, want)
	}
}

// A person who names another command for an agent keeps the way to open its
// sessions, and a person who names another way to open them keeps the command.
func TestKindsKeepsTheTableValueOfAKeyTheConfigLeavesOut(t *testing.T) {
	useConfig(t, "[agents.claude]\nargv = [\"my-acp\"]\n\n[agents.codex]\nresume = [\"codex\", \"resume\", \"--last\"]\n")

	kinds := Kinds()
	claude := kindNamed(t, kinds, "claude")
	if want := []string{"claude", "--resume", "{session}"}; !slices.Equal(claude.Resume, want) {
		t.Errorf("claude Resume = %v, want %v", claude.Resume, want)
	}
	codex := kindNamed(t, kinds, "codex")
	if want := []string{"codex-acp"}; !slices.Equal(codex.Argv, want) {
		t.Errorf("codex Argv = %v, want %v", codex.Argv, want)
	}
}

// Two agents pin the order they come in. A map has no order of its own, so
// without the sort the names a person sees would change from run to run.
func TestKindsAddsTheAgentsTheTableDoesNotHoldByName(t *testing.T) {
	useConfig(t, "[agents.opencode]\nargv = [\"opencode\"]\n\n[agents.amp]\nargv = [\"amp-acp\"]\nresume = [\"amp\", \"threads\", \"continue\", \"{session}\"]\n")

	kinds := Kinds()
	want := []string{"claude", "codex", "gemini", "amp", "opencode"}
	if got := namesOf(kinds); !slices.Equal(got, want) {
		t.Errorf("names = %v, want %v", got, want)
	}
	if amp := kindNamed(t, kinds, "amp"); !slices.Equal(amp.Argv, []string{"amp-acp"}) {
		t.Errorf("amp Argv = %v, want [amp-acp]", amp.Argv)
	}
}

func TestKindsGivesTheTableWhenTheConfigDoesNotRead(t *testing.T) {
	useConfig(t, "runs = \"three\"\n")

	want := []string{"claude", "codex", "gemini"}
	if got := namesOf(Kinds()); !slices.Equal(got, want) {
		t.Errorf("names = %v, want %v", got, want)
	}
}

func TestResumeArgvPutsTheSessionInTheCommand(t *testing.T) {
	useConfig(t, "")

	argv, err := ResumeArgv("claude", "f402c87f-a765-4b98-9db8-c421b33ec610")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"claude", "--resume", "f402c87f-a765-4b98-9db8-c421b33ec610"}
	if !slices.Equal(argv, want) {
		t.Errorf("argv = %v, want %v", argv, want)
	}
}

func TestResumeArgvTakesTheCommandFromTheConfig(t *testing.T) {
	useConfig(t, "[agents.codex]\nresume = [\"codex\", \"resume\", \"--last\", \"{session}\"]\n")

	argv, err := ResumeArgv("codex", "abc")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"codex", "resume", "--last", "abc"}
	if !slices.Equal(argv, want) {
		t.Errorf("argv = %v, want %v", argv, want)
	}
}

func TestResumeArgvRefusesAnAgentThatIsNotInTheTable(t *testing.T) {
	useConfig(t, "")

	_, err := ResumeArgv("cursor", "abc")
	if err == nil {
		t.Fatal("ResumeArgv of an unknown agent gave no error")
	}
	for _, want := range []string{"cursor", "claude", "codex", "gemini"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
}

func TestResumeArgvRefusesAnAgentWithNoResumeCommand(t *testing.T) {
	useConfig(t, "")

	if _, err := ResumeArgv("gemini", "abc"); err == nil {
		t.Fatal("ResumeArgv of an agent with no resume command gave no error")
	}
}
