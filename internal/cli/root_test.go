package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alcubie/delegator/internal/testfix"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// runIn executes a command with separate output and error buffers.
func runIn(t *testing.T, dataDir, workDir string, args ...string) (string, error) {
	t.Helper()
	out, _, err := runInOutputs(t, dataDir, workDir, "", args...)
	return out, err
}

// runInWithStdin runs one command with text on its standard input, for a
// command that reads it.
func runInWithStdin(t *testing.T, dataDir, workDir, stdin string, args ...string) (string, error) {
	t.Helper()
	out, _, err := runInOutputs(t, dataDir, workDir, stdin, args...)
	return out, err
}

// runInOutputs keeps ordinary output separate from warnings written to the
// error stream.
func runInOutputs(t *testing.T, dataDir, workDir, stdin string, args ...string) (string, string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	root := Root(workDir)
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetIn(strings.NewReader(stdin))
	// Use a non-nil slice so Cobra does not read the test process's
	// os.Args.
	if args == nil {
		args = []string{}
	}
	if dataDir != "" {
		args = append([]string{"--data-dir", dataDir}, args...)
	}
	root.SetArgs(args)

	err := root.Execute()
	return out.String(), errOut.String(), err
}

func TestRunWithNoCommandShowsTheInbox(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	if _, err := runIn(t, dataDir, repo, "ticket", "create", "Remove staging infrastructure", "--no-body"); err != nil {
		t.Fatal(err)
	}

	out, err := runIn(t, dataDir, repo)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"READY", "RUNNING", "QUEUED", "Remove staging infrastructure"} {
		if !strings.Contains(out, want) {
			t.Errorf("the inbox does not hold %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Usage:") {
		t.Errorf("dg with no command shows the help:\n%s", out)
	}
}

// Cobra makes the command reference from this metadata. Walk the whole visible
// tree so a new top-level or nested command cannot silently ship incomplete
// help. Hidden implementation commands are not part of that reference.
func TestEachVisibleCommandHasCompleteHelp(t *testing.T) {
	var walk func(*cobra.Command)
	walk = func(command *cobra.Command) {
		if command.Hidden {
			return
		}
		for field, value := range map[string]string{
			"Short":   command.Short,
			"Long":    command.Long,
			"Example": command.Example,
		} {
			if strings.TrimSpace(value) == "" {
				t.Errorf("the command %q has no %s help", command.CommandPath(), field)
			}
		}
		command.NonInheritedFlags().VisitAll(func(flag *pflag.Flag) {
			if !flag.Hidden && strings.TrimSpace(flag.Usage) == "" {
				t.Errorf("the visible flag --%s of %q has no description", flag.Name, command.CommandPath())
			}
		})
		for _, child := range command.Commands() {
			walk(child)
		}
	}
	walk(Root(t.TempDir()))
}

func TestRootHelpExplainsTheDefaultInboxAndGlobalFlags(t *testing.T) {
	out, err := runIn(t, filepath.Join(t.TempDir(), "absent"), t.TempDir(), "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Run dg without a command to see the inbox",
		`dg ticket create "Add request tracing" --no-body`,
		"auto (terminals only) (default auto)",
		"default: the platform data directory",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("dg help does not contain %q:\n%s", want, out)
		}
	}
}

func TestDependHelpSeparatesArgumentsFromFlags(t *testing.T) {
	out, err := runIn(t, filepath.Join(t.TempDir(), "absent"), t.TempDir(), "depend", "--help")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Usage:\n  dg depend <id> [flags]") {
		t.Errorf("dg depend help has the wrong usage:\n%s", out)
	}
	for _, want := range []string{
		"dg depend 42 --after 17",
		"dg depend 42 --after 17 --remove",
		"--after int64Slice",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("dg depend help does not contain %q:\n%s", want, out)
		}
	}
}

// Help and completion describe the command tree, which exists before an
// instance does. They therefore neither make an absent data directory nor try
// to open one that cannot contain Delegator's files.
func TestHelpAndCompletionDoNotOpenTheInstance(t *testing.T) {
	commands := []struct {
		name string
		args []string
	}{
		{"root help", []string{"--help"}},
		{"command help", []string{"ticket", "--help"}},
		{"help command", []string{"help", "ticket"}},
		{"completion", []string{"completion", "bash"}},
	}

	for _, command := range commands {
		t.Run(command.name, func(t *testing.T) {
			t.Run("absent", func(t *testing.T) {
				dataDir := filepath.Join(t.TempDir(), "delegator")
				out, err := runIn(t, dataDir, t.TempDir(), command.args...)
				if err != nil {
					t.Fatal(err)
				}
				if out == "" {
					t.Error("command wrote no help or completion")
				}
				if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
					t.Errorf("static command created the data directory: %v", err)
				}
			})

			t.Run("unusable", func(t *testing.T) {
				dataDir := filepath.Join(t.TempDir(), "not-a-directory")
				if err := os.WriteFile(dataDir, []byte("not a directory"), 0o600); err != nil {
					t.Fatal(err)
				}
				out, err := runIn(t, dataDir, t.TempDir(), command.args...)
				if err != nil {
					t.Fatal(err)
				}
				if out == "" {
					t.Error("command wrote no help or completion")
				}
			})
		})
	}
}

func TestCompletionListsCanonicalCommandGroups(t *testing.T) {
	for group, want := range map[string][]string{
		"ticket": {"accept", "cancel", "chat", "create", "depend", "edit", "finish", "list", "map", "move", "restart", "runs", "search", "show"},
		"queue":  {"pause", "start"},
	} {
		t.Run(group, func(t *testing.T) {
			out, err := runIn(t, filepath.Join(t.TempDir(), "absent"), t.TempDir(), cobra.ShellCompRequestCmd, group, "")
			if err != nil {
				t.Fatal(err)
			}
			for _, command := range want {
				if !strings.Contains(out, command+"\t") {
					t.Errorf("%s completion does not contain %q:\n%s", group, command, out)
				}
			}
		})
	}
}

func TestDataDirTakesXDGDataHome(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/somewhere/data")

	got, err := DataDir()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("/somewhere/data", "delegator"); got != want {
		t.Errorf("DataDir = %q, want %q", got, want)
	}
}

func TestSelectedDataDirMustBeAbsoluteAndIsCleaned(t *testing.T) {
	if _, err := selectedDataDir("relative"); err == nil || !strings.Contains(err.Error(), "--data-dir") {
		t.Errorf("selectedDataDir(relative) error = %v, want an error naming --data-dir", err)
	}

	dir := filepath.Join(t.TempDir(), "first", "..", "selected")
	got, err := selectedDataDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Clean(dir); got != want {
		t.Errorf("selectedDataDir(%q) = %q, want %q", dir, got, want)
	}
}

func TestDataDirFlagBeforeAndAfterCommandsSelectsIsolatedInstances(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	defaultDir := filepath.Join(xdg, "delegator")
	first := filepath.Join(t.TempDir(), "first", "..", "selected-first")
	second := filepath.Join(t.TempDir(), "selected-second")
	repo := testfix.Repo(t, repoBranch)

	if _, err := runIn(t, "", repo,
		"--data-dir", first, "ticket", "create", "In the first instance", "--no-body"); err != nil {
		t.Fatal(err)
	}
	if _, err := runIn(t, "", repo,
		"ticket", "create", "In the second instance", "--no-body", "--data-dir", second); err != nil {
		t.Fatal(err)
	}

	first = filepath.Clean(first)
	for _, instance := range []struct {
		dir   string
		title string
	}{
		{first, "In the first instance"},
		{second, "In the second instance"},
	} {
		for _, name := range []string{"delegator.db", "tickets", "runs", "worktrees"} {
			if _, err := os.Stat(filepath.Join(instance.dir, name)); err != nil {
				t.Errorf("selected instance %q has no %s: %v", instance.dir, name, err)
			}
		}
		queue, err := testfix.OpenStore(t, instance.dir).ListQueue()
		if err != nil {
			t.Fatal(err)
		}
		if len(queue) != 1 || queue[0].Title != instance.title {
			t.Errorf("queue in %q = %+v, want only %q", instance.dir, queue, instance.title)
		}
	}
	if _, err := os.Stat(filepath.Join(defaultDir, "delegator.db")); !os.IsNotExist(err) {
		t.Errorf("default data directory was opened despite --data-dir: %v", err)
	}
}

func TestDataDirFlagIsValidatedBeforeTheDefaultStoreOpens(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	repo := testfix.Repo(t, repoBranch)

	_, err := runIn(t, "", repo, "ticket", "create", "Never added", "--no-body", "--data-dir", "relative")
	if err == nil || !strings.Contains(err.Error(), "--data-dir") {
		t.Fatalf("relative --data-dir error = %v, want an error naming --data-dir", err)
	}
	if _, err := os.Stat(filepath.Join(xdg, "delegator")); !os.IsNotExist(err) {
		t.Errorf("the default store was opened before validation: %v", err)
	}
}

func TestAbsentDataDirFlagUsesTheDefaultAfterParsing(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	repo := testfix.Repo(t, repoBranch)

	if _, err := runIn(t, "", repo, "ticket", "create", "Uses the default", "--no-body"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(xdg, "delegator", "delegator.db")); err != nil {
		t.Errorf("the XDG default has no database: %v", err)
	}
}

// Inject the platform to verify Windows LOCALAPPDATA behavior on any host.
func TestDataDirOnWindowsTakesLocalAppData(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("LOCALAPPDATA", `C:\Users\person\AppData\Local`)

	got, err := dataDir("windows")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(`C:\Users\person\AppData\Local`, "delegator"); got != want {
		t.Errorf("dataDir(windows) = %q, want %q", got, want)
	}
}

// Missing LOCALAPPDATA must fail rather than create a relative data path.
func TestDataDirOnWindowsWithNoLocalAppData(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("LOCALAPPDATA", "")

	got, err := dataDir("windows")
	if err == nil {
		t.Fatalf("dataDir(windows) with no LOCALAPPDATA = %q, want an error", got)
	}
	if !strings.Contains(err.Error(), "LOCALAPPDATA") {
		t.Errorf("the error is %q, which does not name LOCALAPPDATA", err)
	}
}

// XDG_DATA_HOME must override LOCALAPPDATA even on Windows.
func TestXDGDataHomeWinsOnEachSystem(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/somewhere/data")
	t.Setenv("LOCALAPPDATA", `C:\Users\person\AppData\Local`)

	for _, goos := range []string{"linux", "darwin", "windows"} {
		got, err := dataDir(goos)
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join("/somewhere/data", "delegator"); got != want {
			t.Errorf("dataDir(%s) = %q, want %q", goos, got, want)
		}
	}
}

// Unix defaults must ignore LOCALAPPDATA even when it is set.
func TestDataDirOffWindowsIsTheXDGDirectory(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("LOCALAPPDATA", `C:\Users\person\AppData\Local`)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}

	for _, goos := range []string{"linux", "darwin"} {
		got, err := dataDir(goos)
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join(home, ".local", "share", "delegator"); got != want {
			t.Errorf("dataDir(%s) = %q, want %q", goos, got, want)
		}
	}
}
