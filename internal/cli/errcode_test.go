// Check that every sentinel error has a unique application code outside JSON-
// RPC's reserved range.

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

// errorDirs lists packages whose sentinel errors require codes.
var errorDirs = []string{".", "../project", "../store"}

// namedErrors is checked against parsed declarations so new sentinel errors
// cannot escape these tests.
var namedErrors = map[string]error{
	"ErrNoTitle":                  ErrNoTitle,
	"errTwoBodies":                errTwoBodies,
	"errBodyAndNoBody":            errBodyAndNoBody,
	"errNoBody":                   errNoBody,
	"errEditTwoBodies":            errEditTwoBodies,
	"errEditorAndText":            errEditorAndText,
	"errEditNoForm":               errEditNoForm,
	"ErrGitNotOnPath":             project.ErrGitNotOnPath,
	"ErrNoCommit":                 project.ErrNoCommit,
	"ErrUnknownCommit":            project.ErrUnknownCommit,
	"ErrCommitNotOnBranch":        project.ErrCommitNotOnBranch,
	"ErrBranchNotMerged":          project.ErrBranchNotMerged,
	"ErrNotARepository":           project.ErrNotARepository,
	"ErrNewerDatabase":            store.ErrNewerDatabase,
	"ErrInvalidAgent":             store.ErrInvalidAgent,
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

// declaredErrors parses package-level errors.New and fmt.Errorf declarations;
// variable names are unavailable through runtime reflection.
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

// buildsAnError recognizes errors.New and fmt.Errorf initializers.
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

// Positive application codes avoid JSON-RPC's reserved protocol range.
func TestTheCodesArePositive(t *testing.T) {
	for _, name := range slices.Sorted(maps.Keys(namedErrors)) {
		if code := errorCodes[namedErrors[name]]; code <= 0 {
			t.Errorf("%s has the code %d, want a positive one", name, code)
		}
	}
}

// Wrapping must preserve the underlying condition's code.
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

func TestErrorCodeOfAnErrorThatIsNotNamed(t *testing.T) {
	plain := errors.New("the file is not there")
	if got := ErrorCode(plain); got != 1 {
		t.Errorf("ErrorCode of an error with no name gave %d, want 1", got)
	}
	if got := ErrorCode(fmt.Errorf("ticket 4: %w", plain)); got != 1 {
		t.Errorf("ErrorCode of a wrapped error with no name gave %d, want 1", got)
	}
}
