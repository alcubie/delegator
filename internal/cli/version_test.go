package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// setVersion gives Version a value for one test and puts the old one back.
func setVersion(t *testing.T, value string) {
	t.Helper()
	old := Version
	t.Cleanup(func() { Version = old })
	Version = value
}

// noGitEnv strips hook-inherited Git variables so build subprocesses use
// their own working directories.
func noGitEnv() []string {
	kept := []string{}
	for _, v := range os.Environ() {
		if !strings.HasPrefix(v, "GIT_") {
			kept = append(kept, v)
		}
	}
	return kept
}

// output runs one program in the directory of the test and returns what it
// wrote to the two outputs.
func output(t *testing.T, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Env = noGitEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v: %s", name, strings.Join(args, " "), err, out)
	}
	return string(out)
}

// repoRoot locates the justfile from the package directory used by go test.
func repoRoot(t *testing.T) string {
	t.Helper()
	return strings.TrimSpace(output(t, "git", "rev-parse", "--show-toplevel"))
}

// gitDescribe is the version that the justfile gives a build of this tree.
func gitDescribe(t *testing.T, root string) string {
	t.Helper()
	return strings.TrimSpace(output(t, "git", "-C", root, "describe", "--tags", "--always", "--dirty"))
}

// justIn runs one recipe of the justfile and returns what it wrote.
func justIn(t *testing.T, root string, args ...string) string {
	t.Helper()
	return output(t, "just", append([]string{"--justfile", filepath.Join(root, "justfile"), "--working-directory", root}, args...)...)
}

func TestVersionOfABuildWithNoValue(t *testing.T) {
	out, err := runIn(t, t.TempDir(), t.TempDir(), "version")
	if err != nil {
		t.Fatal(err)
	}
	if out != "dg dev\n" {
		t.Errorf("dg version wrote %q, want %q", out, "dg dev\n")
	}
}

func TestVersionWritesTheValueOfTheVariable(t *testing.T) {
	setVersion(t, "v1.2.3")

	out, err := runIn(t, t.TempDir(), t.TempDir(), "version")
	if err != nil {
		t.Fatal(err)
	}
	if out != "dg v1.2.3\n" {
		t.Errorf("dg version wrote %q, want %q", out, "dg v1.2.3\n")
	}
}

func TestVersionJSONHoldsTheVersionAndTheSchema(t *testing.T) {
	setVersion(t, "v1.2.3")

	got := rpcDocument(t, t.TempDir(), t.TempDir(), "version")
	if got["version"] != "v1.2.3" {
		t.Errorf("version = %v, want v1.2.3", got["version"])
	}
	if got["schema"] != float64(1) {
		t.Errorf("schema = %v, want 1", got["schema"])
	}
}

func TestVersionJSONWritesTheSchemaAsANumber(t *testing.T) {
	got := rpcDocument(t, t.TempDir(), t.TempDir(), "version")
	if _, ok := got["schema"].(float64); !ok {
		t.Errorf("schema is %T and holds %v, want a number", got["schema"], got["schema"])
	}
	if len(got) != 3 {
		t.Errorf("the object holds %d keys (%v), want version, schema and data_dir alone", len(got), got)
	}
}

func TestVersionJSONHoldsTheSelectedDataDirectory(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "chosen")

	got := rpcDocument(t, dataDir, t.TempDir(), "version")
	if got["data_dir"] != dataDir {
		t.Errorf("data_dir = %v, want %s", got["data_dir"], dataDir)
	}
}

func TestVersionJSONHoldsTheDefaultDataDirectory(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	want, err := DataDir()
	if err != nil {
		t.Fatal(err)
	}

	got := rpcDocument(t, "", t.TempDir(), "version")
	if got["data_dir"] != want {
		t.Errorf("data_dir = %v, want %s", got["data_dir"], want)
	}
}

// Version queries must not create instance data or trigger reconciliation.
func TestVersionMakesNoDataDirectory(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "never-used")

	out, err := runIn(t, dataDir, t.TempDir(), "version")
	if err != nil {
		t.Fatal(err)
	}
	if out != "dg dev\n" {
		t.Errorf("dg version wrote %q, want %q", out, "dg dev\n")
	}
	if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
		t.Errorf("dg version made %s", dataDir)
	}
}

// Build into a temporary BIN path and verify its embedded version.
func TestJustBuildSetsTheVersion(t *testing.T) {
	root := repoRoot(t)
	bin := filepath.Join(t.TempDir(), "dg")
	justIn(t, root, "BIN="+bin, "build")

	out, err := exec.Command(bin, "version").Output()
	if err != nil {
		t.Fatalf("%s version: %v", bin, err)
	}
	want := "dg " + gitDescribe(t, root) + "\n"
	if string(out) != want {
		t.Errorf("the binary of just build wrote %q, want %q", out, want)
	}
}

// Install into a temporary GOBIN so the user's dg binary stays in place.
func TestJustInstallSetsTheVersion(t *testing.T) {
	root := repoRoot(t)
	gobin := t.TempDir()
	t.Setenv("GOBIN", gobin)
	justIn(t, root, "install")

	out, err := exec.Command(filepath.Join(gobin, "dg"), "version").Output()
	if err != nil {
		t.Fatalf("installed dg version: %v", err)
	}
	want := "dg " + gitDescribe(t, root) + "\n"
	if string(out) != want {
		t.Errorf("the binary of just install wrote %q, want %q", out, want)
	}
}
