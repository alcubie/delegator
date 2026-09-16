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

// noGitEnv is the environment of the test with the variables that git exports
// to a hook taken out. The pre-commit hook runs make check, which runs these
// tests, and GIT_DIR there makes git read the working directory as the root of
// the tree. Each program below then sees what a person at a terminal sees.
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

// repoRoot is the directory that holds the Makefile. go test runs a test with
// the directory of its package as the working directory, so a test of the
// build has to find the root itself.
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

// The version of a build with no -ldflags. A program that reads it learns that
// the binary came from a tree and not from a release.
func TestVersionOfABuildWithNoValue(t *testing.T) {
	out, err := runIn(t, t.TempDir(), t.TempDir(), "version")
	if err != nil {
		t.Fatal(err)
	}
	if out != "dg dev\n" {
		t.Errorf("dg version wrote %q, want %q", out, "dg dev\n")
	}
}

// The line holds the value of the variable that -ldflags -X sets, and not a
// word that the code made up.
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

// The object a program reads at its start: the version it can show to a
// person, and the schema it can compare with the one it was written against.
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

// schema is a number and not a string, so a reader compares it with the schema
// it holds and does not parse it first.
func TestVersionJSONWritesTheSchemaAsANumber(t *testing.T) {
	got := rpcDocument(t, t.TempDir(), t.TempDir(), "version")
	if _, ok := got["schema"].(float64); !ok {
		t.Errorf("schema is %T and holds %v, want a number", got["schema"], got["schema"])
	}
	if len(got) != 2 {
		t.Errorf("the object holds %d keys (%v), want version and schema alone", len(got), got)
	}
}

// A program asks the version of a dg that the person has never run, so the
// command answers with no data directory on the disk. Opening the store would
// make one, and the reconcile behind it would start a supervisor.
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

// The binary that make build leaves says which tree it came from. BIN names
// the file, so the build of the test writes to a temporary directory and not
// into the repository.
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

// make install puts dg on the PATH of the person, so a test that ran it would
// replace the dg they use. -n writes the command and runs nothing.
func TestMakeInstallSetsTheVersion(t *testing.T) {
	root := repoRoot(t)
	out := makeIn(t, root, "-n", "install")

	want := "-X github.com/alcubie/delegator/internal/cli.Version=" + gitDescribe(t, root)
	if !strings.Contains(out, want) {
		t.Errorf("make install does not hold %q:\n%s", want, out)
	}
}
