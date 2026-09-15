// The helpers of the tests about the files that a build takes for one system,
// and the type check of the files that a build for Windows takes. bootTime has
// one file for each system, and alive, detachAttr and stop each have one for
// Unix and one for Windows, and the tests of all of them ask the same two
// questions: which files a build takes, and whether the declaration in one file
// is the declaration in another.

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

// The tests that need Windows cannot run in make check, which runs on Linux. A
// type check for Windows is what is left, and this is the whole of it: every
// code file that a build for Windows takes, and every test file that a build
// for Windows takes and a build for Linux does not. A Windows file that does
// not answer the call another file makes fails here, and so does a Windows test
// that names something no file declares.
//
// This names the files and not the package because the tests of the package do
// not build for Windows. internal/testfix starts a shell with Setsid and
// signals a process group, and next_test.go reads a process group with ps, so
// go vet of the package reaches them and stops.
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
