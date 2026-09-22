package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestGenerateCommandReference(t *testing.T) {
	tests := []struct {
		format       string
		root         string
		nested       string
		hidden       string
		rootContains []string
		nestedTitle  string
	}{
		{
			format: markdownFormat,
			root:   "dg.md", nested: "dg_config_get.md", hidden: "dg_run.md",
			rootContains: []string{"# Alcubi Delegator CLI Reference\n", "## dg\n"},
			nestedTitle:  "# Alcubi Delegator CLI Reference: dg config get\n",
		},
		{
			format: manFormat,
			root:   "dg.1", nested: "dg-config-get.1", hidden: "dg-run.1",
			rootContains: []string{`.TH "DG" "1" "Jan 1970" "Alcubi Delegator" "Alcubi Delegator Manual"`, ".SH NAME"},
			nestedTitle:  `.TH "DG-CONFIG-GET" "1" "Jan 1970" "Alcubi Delegator" "Alcubi Delegator Manual"`,
		},
	}

	for _, test := range tests {
		t.Run(test.format, func(t *testing.T) {
			first := t.TempDir()
			if err := generate(first, test.format); err != nil {
				t.Fatal(err)
			}

			root := readGenerated(t, first, test.root)
			for _, want := range test.rootContains {
				if !strings.Contains(root, want) {
					t.Errorf("%s does not contain %q:\n%s", test.root, want, root)
				}
			}
			nested := readGenerated(t, first, test.nested)
			if !strings.Contains(nested, test.nestedTitle) {
				t.Errorf("%s does not contain %q:\n%s", test.nested, test.nestedTitle, nested)
			}
			for name, contents := range map[string]string{test.root: root, test.nested: nested} {
				if strings.Contains(contents, "Auto generated") {
					t.Errorf("%s contains a volatile generation notice:\n%s", name, contents)
				}
			}
			if _, err := os.Stat(filepath.Join(first, test.hidden)); !os.IsNotExist(err) {
				t.Errorf("hidden command page %s exists: %v", test.hidden, err)
			}

			second := t.TempDir()
			if err := generate(second, test.format); err != nil {
				t.Fatal(err)
			}
			if got, want := generatedFiles(t, second), generatedFiles(t, first); !reflect.DeepEqual(got, want) {
				t.Errorf("two generations differ:\nfirst:  %v\nsecond: %v", want, got)
			}
		})
	}
}

func TestGenerateRejectsBadOptions(t *testing.T) {
	tests := []struct {
		name      string
		outputDir string
		format    string
		want      string
	}{
		{name: "no options", want: "--output-dir and --format are required"},
		{name: "no output directory", format: markdownFormat, want: "--output-dir is required"},
		{name: "no format", outputDir: t.TempDir(), want: "--format is required"},
		{name: "unknown format", outputDir: t.TempDir(), format: "html", want: `unsupported --format "html"`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := generate(test.outputDir, test.format)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Errorf("generate error = %v, want one containing %q", err, test.want)
			}
		})
	}
}

func TestCommandTakesFormatAndOutputDirectoryOptions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "reference")
	if err := run([]string{"--format", markdownFormat, "--output-dir", dir}); err != nil {
		t.Fatal(err)
	}
	readGenerated(t, dir, "dg.md")
}

func TestGenerateReportsAnUnusableOutputDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(path, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := generate(path, markdownFormat)
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
