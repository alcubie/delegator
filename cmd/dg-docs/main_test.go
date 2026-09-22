package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestGenerateCommandReference(t *testing.T) {
	first := t.TempDir()
	if err := generate(first); err != nil {
		t.Fatal(err)
	}

	root := readGenerated(t, first, "dg.md")
	for _, want := range []string{"# Alcubi Delegator CLI Reference\n", "## dg\n"} {
		if !strings.Contains(root, want) {
			t.Errorf("dg.md does not contain %q:\n%s", want, root)
		}
	}
	nested := readGenerated(t, first, "dg_config_get.md")
	nestedTitle := "# Alcubi Delegator CLI Reference: dg config get\n"
	if !strings.Contains(nested, nestedTitle) {
		t.Errorf("dg_config_get.md does not contain %q:\n%s", nestedTitle, nested)
	}
	for name, contents := range map[string]string{"dg.md": root, "dg_config_get.md": nested} {
		if strings.Contains(contents, "Auto generated") {
			t.Errorf("%s contains a volatile generation notice:\n%s", name, contents)
		}
	}
	if _, err := os.Stat(filepath.Join(first, "dg_run.md")); !os.IsNotExist(err) {
		t.Errorf("hidden command page dg_run.md exists: %v", err)
	}

	second := t.TempDir()
	if err := generate(second); err != nil {
		t.Fatal(err)
	}
	if got, want := generatedFiles(t, second), generatedFiles(t, first); !reflect.DeepEqual(got, want) {
		t.Errorf("two generations differ:\nfirst:  %v\nsecond: %v", want, got)
	}
}

func TestGenerateRequiresAnOutputDirectory(t *testing.T) {
	err := generate("")
	if err == nil || !strings.Contains(err.Error(), "--output-dir is required") {
		t.Errorf("generate error = %v, want the missing option", err)
	}
}

func TestCommandTakesTheOutputDirectoryOption(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "reference")
	if err := run([]string{"--output-dir", dir}); err != nil {
		t.Fatal(err)
	}
	readGenerated(t, dir, "dg.md")
}

func TestGenerateReportsAnUnusableOutputDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(path, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := generate(path)
	if err == nil || !strings.Contains(err.Error(), "make output directory") || !strings.Contains(err.Error(), path) {
		t.Errorf("generate error = %v, want the output directory and operation", err)
	}
}

func readGenerated(t *testing.T, dir, name string) string {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(contents)
}

func generatedFiles(t *testing.T, dir string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	files := make(map[string]string, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			t.Fatalf("generated directory contains subdirectory %s", entry.Name())
		}
		files[entry.Name()] = readGenerated(t, dir, entry.Name())
	}
	return files
}
