// The tests of the codes that go beside the named errors. The GUI reads a code
// and not a sentence, so what these ask is that the table holds every named
// error of the three packages that have one, that no two errors share a code,
// and that a code the GUI reads is one JSON-RPC leaves to the application.

package cli

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/alcubie/delegator/internal/project"
	"github.com/alcubie/delegator/internal/store"
)

// errorDirs are the directories of the packages whose named errors have a code,
// as the tests of internal/cli reach them.
var errorDirs = []string{".", "../project", "../store"}

// namedErrors is every named error that a code is for, by the name the package
// declares it under. declaredErrors reads the same names out of the source, so
// an error that this list does not hold fails the test that compares them.
var namedErrors = map[string]error{
	"ErrNoTitle":                  ErrNoTitle,
	"errTwoBodies":                errTwoBodies,
	"errBodyAndNoBody":            errBodyAndNoBody,
	"errNoBody":                   errNoBody,
	"ErrGitNotOnPath":             project.ErrGitNotOnPath,
	"ErrNoCommit":                 project.ErrNoCommit,
	"ErrNotARepository":           project.ErrNotARepository,
	"ErrNewerDatabase":            store.ErrNewerDatabase,
	"ErrSelfDependency":           store.ErrSelfDependency,
	"ErrDependencyRing":           store.ErrDependencyRing,
	"ErrNoDependency":             store.ErrNoDependency,
	"ErrNotQueued":                store.ErrNotQueued,
	"ErrInvalidTicketStateChange": store.ErrInvalidTicketStateChange,
	"ErrNotMovable":               store.ErrNotMovable,
	"ErrNotInTheSameList":         store.ErrNotInTheSameList,
	"ErrNoTicket":                 store.ErrNoTicket,
	"ErrNoRoom":                   store.ErrNoRoom,
	"ErrNoRun":                    store.ErrNoRun,
}

// declaredErrors gives the names of the package-level errors that the code of
// one directory declares, which is every variable that errors.New or fmt.Errorf
// builds. It reads the source because nothing at run time can: Go keeps the
// name a variable has for the compiler and not for the program.
func declaredErrors(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, name), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, errorNames(file)...)
	}
	return names
}

// errorNames gives the names of the package-level errors of one parsed file.
func errorNames(file *ast.File) []string {
	var names []string
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range value.Names {
				if i < len(value.Values) && buildsAnError(value.Values[i]) {
					names = append(names, name.Name)
				}
			}
		}
	}
	return names
}

// buildsAnError says whether an expression is a call to errors.New or to
// fmt.Errorf, the two calls that make a named error in this repository.
func buildsAnError(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	fn, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := fn.X.(*ast.Ident)
	if !ok {
		return false
	}
	return (pkg.Name == "errors" && fn.Sel.Name == "New") ||
		(pkg.Name == "fmt" && fn.Sel.Name == "Errorf")
}

// The names the source declares are the names the test lists, so an error added
// to one of the three packages is an error the rest of these tests reach.
func TestTheNamedErrorsAreTheOnesDeclared(t *testing.T) {
	var declared []string
	for _, dir := range errorDirs {
		declared = append(declared, declaredErrors(t, dir)...)
	}
	slices.Sort(declared)

	listed := slices.Sorted(maps.Keys(namedErrors))
	if !slices.Equal(declared, listed) {
		t.Errorf("the source declares %v, and the test lists %v", declared, listed)
	}
}

// Each named error has a code, and no two errors share one, so a GUI that reads
// a code knows which condition it has.
func TestEveryNamedErrorHasACodeOfItsOwn(t *testing.T) {
	byCode := make(map[int]string, len(namedErrors))
	for _, name := range slices.Sorted(maps.Keys(namedErrors)) {
		code, ok := errorCodes[namedErrors[name]]
		if !ok {
			t.Errorf("%s has no code", name)
			continue
		}
		if code == codeUnknown {
			t.Errorf("%s has the code of an error with no name", name)
		}
		if other, taken := byCode[code]; taken {
			t.Errorf("%s and %s both have the code %d", other, name, code)
		}
		byCode[code] = name
	}
	if len(errorCodes) != len(namedErrors) {
		t.Errorf("the table holds %d errors, and the test lists %d", len(errorCodes), len(namedErrors))
	}
}

// Every code is a positive integer, which puts it outside the -32768 to -32000
// that JSON-RPC 2.0 keeps for the protocol, and leaves the GUI free to read a
// code of the protocol as a fault of its own.
func TestTheCodesArePositive(t *testing.T) {
	for _, name := range slices.Sorted(maps.Keys(namedErrors)) {
		if code := errorCodes[namedErrors[name]]; code <= 0 {
			t.Errorf("%s has the code %d, want a positive one", name, code)
		}
	}
}

// A named error gives its own code, and so does an error that wraps it: the
// command layer adds what it was doing to what went wrong, and the GUI reads
// the code of the condition and not of the sentence around it.
func TestErrorCodeOfAWrappedNamedError(t *testing.T) {
	for _, name := range slices.Sorted(maps.Keys(namedErrors)) {
		err := namedErrors[name]
		want := errorCodes[err]
		if got := ErrorCode(err); got != want {
			t.Errorf("%s has the code %d, and ErrorCode gave %d", name, want, got)
		}
		wrapped := fmt.Errorf("ticket 4: %w", err)
		if got := ErrorCode(wrapped); got != want {
			t.Errorf("%s wrapped has the code %d, and ErrorCode gave %d", name, want, got)
		}
	}
}

// An error that wraps no named error has the code 1, so the GUI has a code to
// read whatever came back.
func TestErrorCodeOfAnErrorThatIsNotNamed(t *testing.T) {
	plain := errors.New("the file is not there")
	if got := ErrorCode(plain); got != 1 {
		t.Errorf("ErrorCode of an error with no name gave %d, want 1", got)
	}
	if got := ErrorCode(fmt.Errorf("ticket 4: %w", plain)); got != 1 {
		t.Errorf("ErrorCode of a wrapped error with no name gave %d, want 1", got)
	}
}
