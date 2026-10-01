package testfix

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

var fakeAgentBinary, dgBinary helperBinary

const (
	fakeAgentEnv = "DELEGATOR_TEST_FAKE_AGENT"
	dgEnv        = "DELEGATOR_TEST_DG"
)

// FakeAgent returns the suite-supplied dg-fake-agent, or builds it once on
// first use in this package process. TestMain must call CleanupBinaries after
// m.Run.
func FakeAgent(t testing.TB) string {
	t.Helper()
	return fakeAgentBinary.get(t, "dg-fake-agent")
}

// DG returns the suite-supplied dg, or builds it once on first use in this
// package process. Tests whose scripts invoke dg by name must also add its
// directory to PATH. TestMain must call CleanupBinaries after m.Run.
func DG(t testing.TB) string {
	t.Helper()
	return dgBinary.get(t, "dg")
}

// CleanupBinaries removes any helper builds after all tests have finished.
// Call it before os.Exit, which skips deferred cleanup. Unused helpers create
// no directories; suite-supplied helpers are owned by the suite runner.
func CleanupBinaries() {
	fakeAgentBinary.cleanup()
	dgBinary.cleanup()
}

type helperBinary struct {
	once sync.Once
	dir  string
	path string
	err  error
}

func (b *helperBinary) get(t failing, name string) string {
	t.Helper()
	b.once.Do(func() {
		if supplied := os.Getenv(helperEnv(name)); supplied != "" {
			b.path, b.err = supplied, validateHelper(name, supplied)
			return
		}
		b.dir, b.err = os.MkdirTemp("", "delegator-testfix-")
		if b.err != nil {
			return
		}
		b.path = filepath.Join(b.dir, name)
		pkg := "github.com/alcubie/delegator/cmd/" + name
		out, err := exec.Command("go", "build", "-o", b.path, pkg).CombinedOutput()
		if err != nil {
			b.err = fmt.Errorf("go build %s: %w: %s", pkg, err, out)
		}
	})
	if b.err != nil {
		t.Fatalf("prepare %s: %v", name, b.err)
		return ""
	}
	return b.path
}

func helperEnv(name string) string {
	if name == "dg-fake-agent" {
		return fakeAgentEnv
	}
	return dgEnv
}

func validateHelper(name, path string) error {
	env := helperEnv(name)
	if !filepath.IsAbs(path) {
		return fmt.Errorf("%s must contain an absolute path, got %q", env, path)
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("%s=%q: %w", env, path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s=%q is not a regular file", env, path)
	}
	if _, err := exec.LookPath(path); err != nil {
		return fmt.Errorf("%s=%q is not executable: %w", env, path, err)
	}
	return nil
}

func (b *helperBinary) cleanup() {
	if b.dir != "" {
		os.RemoveAll(b.dir)
	}
}
