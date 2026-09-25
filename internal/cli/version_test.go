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

// repoRoot locates the Makefile from the package directory used by go test.
func repoRoot(t *testing.T) string {
	t.Helper()
	return strings.TrimSpace(output(t, "git", "rev-parse", "--show-toplevel"))
}

// gitDescribe is the version that the Makefile gives a build of this tree.
func gitDescribe(t *testing.T, root string) string {
	t.Helper()
	return strings.TrimSpace(output(t, "git", "-C", root, "describe", "--tags", "--always", "--dirty"))
}

// makeIn runs one target of the Makefile and returns what it wrote.
func makeIn(t *testing.T, root string, args ...string) string {
	t.Helper()
	return output(t, "make", append([]string{"-C", root}, args...)...)
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
	if len(got) != 2 {
		t.Errorf("the object holds %d keys (%v), want version and schema alone", len(got), got)
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
func TestMakeBuildSetsTheVersion(t *testing.T) {
	root := repoRoot(t)
	bin := filepath.Join(t.TempDir(), "dg")
	makeIn(t, root, "build", "BIN="+bin)

	out, err := exec.Command(bin, "version").Output()
	if err != nil {
		t.Fatalf("%s version: %v", bin, err)
	}
	want := "dg " + gitDescribe(t, root) + "\n"
	if string(out) != want {
		t.Errorf("the binary of make build wrote %q, want %q", out, want)
	}
}

// Use make -n to inspect installation without replacing the user's dg binary.
func TestMakeInstallSetsTheVersion(t *testing.T) {
	root := repoRoot(t)
	out := makeIn(t, root, "-n", "install")

	want := "-X github.com/alcubie/delegator/internal/cli.Version=" + gitDescribe(t, root)
	if !strings.Contains(out, want) {
		t.Errorf("make install does not hold %q:\n%s", want, out)
	}
}
