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

// FakeAgent returns dg-fake-agent, building it once on first use in this
// package process. TestMain must call CleanupBinaries after m.Run.
func FakeAgent(t testing.TB) string {
	t.Helper()
	return fakeAgentBinary.get(t, "dg-fake-agent")
}

// DG returns dg, building it once on first use in this package process.
// Tests whose scripts invoke dg by name must also add its directory to PATH.
// TestMain must call CleanupBinaries after m.Run.
func DG(t testing.TB) string {
	t.Helper()
	return dgBinary.get(t, "dg")
}

// CleanupBinaries removes any helper builds after all tests have finished.
// Call it before os.Exit, which skips deferred cleanup. Unused helpers create
// no directories; each package process owns its own temporary builds.
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
		t.Fatalf("build %s: %v", name, b.err)
		return ""
	}
	return b.path
}

func (b *helperBinary) cleanup() {
	if b.dir != "" {
		os.RemoveAll(b.dir)
	}
}
