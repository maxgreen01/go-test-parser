package testcase

import (
	"fmt"
	"go/ast"
	"go/token"
	"log/slog"
	"strings"
)

// Represents an individual test case
type TestCase struct {
	Name     string         // the name of the test case
	Package  string         // the fully qualified package name where the test case is defined
	FileName string         // the name of the file where the test case is defined
	FuncDecl *ast.FuncDecl  // the actual declaration of the test case
	fset     *token.FileSet // the file set used when parsing this project - needed for position information
}

// Create a new TestCase struct for storage
func CreateTestCase(funcDecl *ast.FuncDecl, fset *token.FileSet, packageName string) TestCase {
	return TestCase{
		Name:     funcDecl.Name.Name,
		Package:  packageName,
		FileName: fset.Position(funcDecl.Pos()).Filename,
		FuncDecl: funcDecl,
		fset:     fset,
	}
}

// Determine if the given function declaration is a valid test case.
// Returns two booleans: `valid` indicating whether this is a valid test case, and
// `badFormat` indicating whether the test case has an incorrect (but acceptable) format.
// `badFormat` is false if the function is not valid.
//
// The test case is validated using the following criteria:
// - The function name starts with "Test" followed by a capital letter
// - The function has `*testing.T` as its only formal parameter
// - The function does not have any receiver (i.e., it is not a method)
// - The function does not have any generic type parameters
// - The function does not return any values
func IsValidTestCase(funcDecl *ast.FuncDecl) (valid bool, badFormat bool) {
	if funcDecl == nil || funcDecl.Name == nil {
		return false, false
	}
	name := funcDecl.Name.Name

	// make sure the function name starts with "Test"
	// TODO allow this to accept "Fuzz" or "Benchmark" and indicate a different category somehow (maybe using enum `type` in TestCase)
	if !strings.HasPrefix(name, "Test") {
		// slog.Debug("\tfunction name does not start with 'Test'", "name", name)
		return false, false
	}

	// the function's 5th letter *should* be capitalized, but it's not strictly required
	// TODO update indices based on comment above
	if len(name) < 5 || (name[4] < 'A' || name[4] > 'Z') {
		// slog.Debug("\tfunction has bad format", "name", name)
		badFormat = true
	}

	funcType := funcDecl.Type

	// make sure the function has no receiver, type parameters, or return value
	if funcDecl.Recv != nil || funcType.TypeParams != nil || funcType.Results != nil {
		// todo maaaaaaaaybe allow this case with badFormat? print out how many times this case occurs and see if it's worth supporting
		// slog.Debug("\tfunction has bad signature", "name", name)
		return false, false
	}

	// make sure the function has exactly one parameter
	if len(funcType.Params.List) != 1 {
		// slog.Debug("\tfunction has wrong param count", "name", name)
		return false, false
	}
	paramType := funcType.Params.List[0].Type

	// safely extract all components of the parameter type, expecting something like `*testing.T`
	starExpr, ok := paramType.(*ast.StarExpr)
	if !ok {
		// slog.Debug("\tfunction has non-pointer param type", "name", name, "paramType", reflect.TypeOf(paramType))
		return false, false
	}
	selectorExpr, ok := starExpr.X.(*ast.SelectorExpr)
	if !ok {
		// slog.Debug("\tfunction has non-selector param type", "name", name, "paramType", reflect.TypeOf(starExpr.X))
		return false, false
	}
	paramPackageIdent, ok := selectorExpr.X.(*ast.Ident)
	if !ok {
		// slog.Debug("\tfunction has non-ident param package", "name", name, "paramType", reflect.TypeOf(selectorExpr.X))
		return false, false
	}

	// check that the parameter type is exactly `*testing.T`
	// TODO allow this to accept other param types for Fuzz/Benchmark tests (and maybe testing.TB)
	// TODO maybe allow this case with `badFormat`?
	if paramPackageIdent.Name != "testing" || selectorExpr.Sel.Name != "T" {
		// slog.Debug("\tfunction has invalid param type", "name", name, "paramType", reflect.TypeOf(paramType))
		return false, false
	}

	slog.Info("Found valid test case:", "name", name)

	return true, badFormat
}

// Return the list of statements in this test case
func (t *TestCase) GetStatements() []ast.Stmt {
	return t.FuncDecl.Body.List
}

// Return the number of statements in the test case
func (t TestCase) NumStatements() int {
	return len(t.GetStatements())
}

// Return the number of individual lines (not statements) that the test case spans
func (t TestCase) NumLines() int {
	start := t.fset.Position(t.FuncDecl.Pos())
	end := t.fset.Position(t.FuncDecl.End())
	return end.Line - start.Line + 1
}

func (t TestCase) String() string {
	return fmt.Sprintf("TestCase{Name: %s, Package: %s, FileName: %s, NumLines: %d}", t.Name, t.Package, t.FileName, t.NumLines())
}
