package testfix

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// Record helper builds without compiling commands or relying on Go's cache.
func recordBuilds(t *testing.T, fail bool) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "builds")
	script := "#!/bin/sh\necho \"$4\" >> \"$BUILD_LOG\"\n"
	if fail {
		script += "echo compiler-diagnostic >&2\nexit 1\n"
	} else {
		script += "touch \"$3\"\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "go"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BUILD_LOG", log)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}

func TestHelperBinariesBuildOnDemandAndSurviveIndividualTests(t *testing.T) {
	log := recordBuilds(t, false)
	var fake, dg helperBinary
	t.Cleanup(fake.cleanup)
	t.Cleanup(dg.cleanup)
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Fatalf("unused helpers ran a build: %v", err)
	}
	var path string
	t.Run("first users", func(t *testing.T) {
		var users sync.WaitGroup
		for range 8 {
			users.Go(func() { fake.get(t, "dg-fake-agent") })
		}
		users.Wait()
		path = fake.get(t, "dg-fake-agent")
	})
	t.Run("later user", func(t *testing.T) {
		if got := fake.get(t, "dg-fake-agent"); got != path {
			t.Fatalf("helper changed from %s to %s", path, got)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("helper removed before package cleanup: %v", err)
		}
	})
	if dg.dir != "" {
		t.Fatal("fake-agent users initialized dg")
	}
	dg.get(t, "dg")
	dg.get(t, "dg")
	want := "github.com/alcubie/delegator/cmd/dg-fake-agent\ngithub.com/alcubie/delegator/cmd/dg\n"
	if got, err := os.ReadFile(log); err != nil || string(got) != want {
		t.Fatalf("builds = %q, %v; want %q", got, err, want)
	}
	var isolated helperBinary
	t.Cleanup(isolated.cleanup)
	if got := isolated.get(t, "dg-fake-agent"); got == path {
		t.Fatal("independent helpers shared a build directory")
	}
	fake.cleanup()
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatalf("package cleanup left helper directory: %v", err)
	}
	if _, err := os.Stat(dg.path); err != nil {
		t.Fatalf("cleanup removed another helper: %v", err)
	}
}

func TestHelperBinaryCachesBuildFailureWithDiagnostics(t *testing.T) {
	log := recordBuilds(t, true)
	var binary helperBinary
	t.Cleanup(binary.cleanup)
	for range 2 {
		f := &fakeT{}
		if path := binary.get(f, "dg"); path != "" || !f.failed {
			t.Fatalf("failed build returned %q, failed = %v", path, f.failed)
		}
	}
	if binary.err == nil || !strings.Contains(binary.err.Error(), "go build github.com/alcubie/delegator/cmd/dg: exit status 1: compiler-diagnostic") {
		t.Fatalf("build error lost command or compiler output: %v", binary.err)
	}
	if got, err := os.ReadFile(log); err != nil || strings.Count(string(got), "\n") != 1 {
		t.Fatalf("failed build was not cached: %q, %v", got, err)
	}
	dir := binary.dir
	binary.cleanup()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("failed build directory survived cleanup: %v", err)
	}
}
