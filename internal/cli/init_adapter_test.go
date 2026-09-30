package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/alcubie/delegator/internal/store"
)

// Reuse the test binary as a fake npm so the installation test works without
// a shell, Node.js, network access, or the shared Unix process fixtures.
func init() {
	if prefix := os.Getenv("DG_TEST_ADAPTER_NPM_PREFIX"); prefix != "" {
		fmt.Println(prefix)
		os.Exit(0)
	}
}

func adapterTestExecutable(t *testing.T, dir, name string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		name += ".cmd"
		t.Setenv("PATHEXT", ".COM;.EXE;.BAT;.CMD")
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAdapterPathAtPrefixFindsCommandOutsidePATH(t *testing.T) {
	prefix := t.TempDir()
	bin := prefix
	if runtime.GOOS != "windows" {
		bin = filepath.Join(prefix, "bin")
		if err := os.Mkdir(bin, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	want := adapterTestExecutable(t, bin, "codex-acp")
	t.Setenv("PATH", t.TempDir())
	got, err := adapterPathAtPrefix(prefix, "codex-acp", runtime.GOOS)
	if err != nil || got != want {
		t.Fatalf("adapter path = %q, %v; want %q", got, err, want)
	}
}

func TestAdapterPathAtPrefixRejectsMissingCommandAndRelativePrefix(t *testing.T) {
	for _, prefix := range []string{"", "relative", t.TempDir()} {
		if path, err := adapterPathAtPrefix(prefix, "codex-acp", runtime.GOOS); err == nil {
			t.Errorf("prefix %q returned %q without an error", prefix, path)
		}
	}
}

func TestInitPersistsInstalledAdapterOutsidePATH(t *testing.T) {
	dataDir := t.TempDir()
	bin := t.TempDir()
	prefix := t.TempDir()
	adapterBin := prefix
	if runtime.GOOS != "windows" {
		adapterBin = filepath.Join(prefix, "bin")
		if err := os.Mkdir(adapterBin, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	adapterTestExecutable(t, bin, "codex")
	testBinary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	fakeNPM, err := os.ReadFile(testBinary)
	if err != nil {
		t.Fatal(err)
	}
	npmName := "npm"
	if runtime.GOOS == "windows" {
		npmName += ".exe"
	}
	if err := os.WriteFile(filepath.Join(bin, npmName), fakeNPM, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("DG_TEST_ADAPTER_NPM_PREFIX", prefix)
	var adapter string
	install := func(_ *cobra.Command, spec adapterSpec) error {
		adapter = adapterTestExecutable(t, adapterBin, spec.Executable)
		return nil
	}
	s, err := store.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	cfg, err := s.Settings()
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetIn(strings.NewReader("1\ny\n"))
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := runInit(cmd, s, &cfg, initOptions{}, readAgentSelection, install); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Setup complete") || strings.Contains(out.String(), "ACP command or absolute path") {
		t.Fatalf("setup did not finish automatically:\n%s", out.String())
	}
	// Reopen the store to verify the command survives across dg invocations.
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = store.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	agent, err := s.Agent("codex")
	if err != nil {
		t.Fatal(err)
	}
	if agent.Argv[0] != adapter || agent.InstallHint != "" {
		t.Fatalf("saved agent = %+v; want command %q without install hint", agent, adapter)
	}
	if cfg, err := s.Settings(); err != nil || cfg.DefaultAgent != "codex" {
		t.Fatalf("saved settings = %+v, %v; want default codex", cfg, err)
	}
	if _, err := agentExecutable(agent); err != nil {
		t.Fatalf("saved adapter unavailable on subsequent runs: %v", err)
	}
}
