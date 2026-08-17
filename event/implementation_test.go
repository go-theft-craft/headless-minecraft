package event_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/go-theft-craft/headless-minecraft/event"
)

// TestEveryDeclaredNameHasAnImplementation is M6.3's exit criterion 5, finally
// checkable now that M7 has implemented every domain.
//
// It is the test that would have caught the seven world event names M7's plan
// originally left unowned: a taxonomy written ahead of its code drifts from it
// silently, and a name with no struct is a promise to a consumer that nothing
// keeps.
//
// It reads the package's own source rather than reflecting over a registry,
// because there is no registry — the taxonomy is a map of names and the
// structs are separate declarations, which is exactly the gap that lets one
// exist without the other.
func TestEveryDeclaredNameHasAnImplementation(t *testing.T) {
	t.Parallel()

	implemented := implementedNames(t)

	for _, name := range event.AllNames() {
		if !implemented[name] {
			t.Errorf("event %q is declared in the taxonomy and no struct reports it", name)
		}
	}
}

// TestNoStructReportsAnUndeclaredName is the other direction. A struct
// reporting a name the taxonomy does not declare would reach a subscriber
// through a domain selector while being invisible to AllNames, and every
// completeness check would pass without covering it.
func TestNoStructReportsAnUndeclaredName(t *testing.T) {
	t.Parallel()

	declared := make(map[event.Name]bool, len(event.AllNames()))
	for _, name := range event.AllNames() {
		declared[name] = true
	}

	for name := range implementedNames(t) {
		// The two raw names are deliberately outside the named taxonomy: raw
		// delivery is a selector, not a taxonomy entry.
		if name == event.NameSessionPacketReceived || name == event.NameSessionPacketSent {
			continue
		}
		if !declared[name] {
			t.Errorf("a struct reports %q, which the taxonomy does not declare", name)
		}
	}
}

// implementedNames returns every name a Name() method in this package returns.
//
// Every such method is one line — `return NameX` — which is what makes reading
// them out of the source honest rather than fragile: a method that computed a
// name would not match, and the test would say so by finding fewer names than
// there are structs.
func implementedNames(t *testing.T) map[event.Name]bool {
	t.Helper()

	constants := constantValues(t)
	names := make(map[event.Name]bool, len(constants))

	for _, file := range sourceFiles(t) {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name.Name != "Name" || fn.Recv == nil {
				continue
			}
			constant, ok := returnedConstant(fn)
			if !ok {
				t.Errorf("%s.Name() does not return a taxonomy constant directly", receiver(fn))

				continue
			}
			value, ok := constants[constant]
			if !ok {
				t.Errorf("%s.Name() returns %s, which is not a declared constant",
					receiver(fn), constant)

				continue
			}
			names[value] = true
		}
	}

	if len(names) == 0 {
		t.Fatal("found no event implementations at all, which means this test is not testing anything")
	}

	return names
}

// constantValues maps each taxonomy constant's identifier to its value, read
// from the package's own declarations.
func constantValues(t *testing.T) map[string]event.Name {
	t.Helper()

	values := make(map[string]event.Name)
	for _, file := range sourceFiles(t) {
		for _, decl := range file.Decls {
			declaration, ok := decl.(*ast.GenDecl)
			if !ok || declaration.Tok != token.CONST {
				continue
			}
			for _, spec := range declaration.Specs {
				value, ok := spec.(*ast.ValueSpec)
				if !ok || len(value.Names) != 1 || len(value.Values) != 1 {
					continue
				}
				literal, ok := value.Values[0].(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					continue
				}
				values[value.Names[0].Name] = event.Name(strings.Trim(literal.Value, `"`))
			}
		}
	}

	return values
}

// sourceFiles parses every non-test Go file in this package. Test files are
// skipped because they declare no events.
func sourceFiles(t *testing.T) []*ast.File {
	t.Helper()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read the event package: %v", err)
	}

	fset := token.NewFileSet()
	files := make([]*ast.File, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		files = append(files, file)
	}

	return files
}

// returnedConstant reads the identifier a one-line Name() method returns.
func returnedConstant(fn *ast.FuncDecl) (string, bool) {
	if fn.Body == nil || len(fn.Body.List) != 1 {
		return "", false
	}
	ret, ok := fn.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return "", false
	}
	ident, ok := ret.Results[0].(*ast.Ident)
	if !ok {
		return "", false
	}

	return ident.Name, true
}

// receiver names the type a method belongs to, for a readable failure.
func receiver(fn *ast.FuncDecl) string {
	if len(fn.Recv.List) == 0 {
		return "<unknown>"
	}
	switch value := fn.Recv.List[0].Type.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.StarExpr:
		if ident, ok := value.X.(*ast.Ident); ok {
			return "*" + ident.Name
		}
	}

	return reflect.TypeOf(fn.Recv.List[0].Type).String()
}
