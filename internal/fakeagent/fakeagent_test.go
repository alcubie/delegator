package fakeagent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// scriptFile writes one script to a temporary file and returns its path.
func scriptFile(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "script")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunExitsWithTheStatusOfTheScript(t *testing.T) {
	status, err := Run(scriptFile(t, "exit 3"))
	if err != nil {
		t.Fatal(err)
	}
	if status != 3 {
		t.Errorf("status = %d, want 3", status)
	}
}

func TestRunWritesAFile(t *testing.T) {
	target := filepath.Join(t.TempDir(), "report.md")
	script := scriptFile(t, "write "+target+" the work is complete", "exit 0")

	status, err := Run(script)
	if err != nil {
		t.Fatal(err)
	}
	if status != 0 {
		t.Errorf("status = %d, want 0", status)
	}

	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(content); got != "the work is complete" {
		t.Errorf("content = %q, want %q", got, "the work is complete")
	}
}

// The real use is dg finish <id> --result "...", so a quoted argument that
// holds a space must reach the program whole.
func TestRunRunsACommand(t *testing.T) {
	target := filepath.Join(t.TempDir(), "out")
	script := scriptFile(t, "run printf '%s' 'one two' > "+target, "exit 0")

	if _, err := Run(script); err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(content); got != "one two" {
		t.Errorf("content = %q, want %q", got, "one two")
	}
}

// The supervisor keeps the raw output of a run in runs/<id>, so what a command
// writes must reach the stdout of the fake agent and not go nowhere.
func TestRunGivesTheOutputOfACommandToStdout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stdout")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stdout
	os.Stdout = f
	defer func() { os.Stdout = saved }()

	if _, err := Run(scriptFile(t, "run printf 'hello'", "exit 0")); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(content); got != "hello" {
		t.Errorf("stdout = %q, want %q", got, "hello")
	}
}

// Each line runs, and the status comes from the last one. A loop that stops
// after the first line passes every test above, because each of those puts the
// one verb it examines first and exits with zero.
func TestRunDoesEveryLineOfTheScript(t *testing.T) {
	dir := t.TempDir()
	written, made := filepath.Join(dir, "written"), filepath.Join(dir, "made")
	script := scriptFile(t,
		"write "+written+" from write",
		"run printf 'from run' > "+made,
		"exit 4",
	)

	status, err := Run(script)
	if err != nil {
		t.Fatal(err)
	}
	if status != 4 {
		t.Errorf("status = %d, want 4", status)
	}
	for _, path := range []string{written, made} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s: %v", path, err)
		}
	}
}

func TestRunWithAVerbThatIsNotThere(t *testing.T) {
	_, err := Run(scriptFile(t, "fly to the moon"))
	if err == nil {
		t.Fatal("err = nil, want an error that names the verb")
	}
	if !strings.Contains(err.Error(), "fly") {
		t.Errorf("err = %v, and it does not name %q", err, "fly")
	}
}

// A person writes these scripts by hand, so an empty line is not a mistake.
func TestRunSkipsAnEmptyLine(t *testing.T) {
	status, err := Run(scriptFile(t, "", "exit 5", ""))
	if err != nil {
		t.Fatal(err)
	}
	if status != 5 {
		t.Errorf("status = %d, want 5", status)
	}
}

// A command that fails stops the script. A fake agent that carried on would
// let a test of the supervisor pass while the dg finish it believed it made
// did nothing at all.
func TestRunWithACommandThatFails(t *testing.T) {
	_, err := Run(scriptFile(t, "run exit 7", "exit 0"))
	if err == nil {
		t.Fatal("err = nil, want the error of the command")
	}
}
