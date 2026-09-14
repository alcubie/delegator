// The helpers of the tests about the files that a build takes for one system.
// bootTime has one file for each system, and alive has one for Unix and one
// for Windows, and the tests of both ask the same two questions: which files a
// build takes, and whether the declaration in one file is the declaration in
// another.

package run

import (
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// signature gives the declaration of a function in one file as text, so that
// the declaration of one file can be compared with the declaration of another.
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

// files gives the files that a build for one system takes from this package.
// The field is the name that go list gives the list, GoFiles for the code of
// the package and TestGoFiles for its tests.
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
