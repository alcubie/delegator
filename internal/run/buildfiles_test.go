// Shared checks for platform-specific file selection and function signatures.

package run

import (
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

// signature formats a function declaration for comparison across platform
// files.
func signature(t *testing.T, file, name string) string {
	t.Helper()
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, file, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, decl := range parsed.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != name {
			continue
		}
		var out strings.Builder
		if err := printer.Fprint(&out, fset, fn.Type); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}
	t.Fatalf("%s declares no %s", file, name)
	return ""
}

// files asks go list for the selected GoFiles or TestGoFiles on a target
// platform.
func files(t *testing.T, goos, field string) []string {
	t.Helper()
	cmd := exec.Command("go", "list", "-f", "{{."+field+"}}", ".")
	cmd.Env = append(os.Environ(), "GOOS="+goos)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list %s for %s: %v", field, goos, err)
	}
	return strings.Fields(strings.Trim(strings.TrimSpace(string(out)), "[]"))
}

// Type-check Windows production files and Windows-only tests from the host
// platform. Select files explicitly because common test fixtures use Unix-
// only shells, Setsid, and process-group signals, preventing a whole-package
// Windows test build.
func TestTheWindowsFilesTypeCheck(t *testing.T) {
	vetted := files(t, "windows", "GoFiles")
	onLinux := files(t, "linux", "TestGoFiles")
	for _, file := range files(t, "windows", "TestGoFiles") {
		if !slices.Contains(onLinux, file) {
			vetted = append(vetted, file)
		}
	}

	cmd := exec.Command("go", append([]string{"vet"}, vetted...)...)
	cmd.Env = append(os.Environ(), "GOOS=windows")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go vet for windows: %v: %s", err, out)
	}
}
