// Provides functionality for storing and analyzing test cases extracted from Go source files.
package testcase

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"go/types"
	"log/slog"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/aaronriekenberg/gsm"
	"github.com/maxgreen01/go-test-parser/internal/filewriter"
)

// Represents an individual test case defined at the top level of a Go source file.
// The fields of this struct should not be modified directly.
type TestCase struct {
	// High-level identifiers for the test case
	Name     string // the name of the test case
	Package  string // the fully qualified package name where the test case is defined
	FilePath string // the path to the file where the test case is defined
	Project  string // the overarching project directory that the test case's package is part of

	// Analysis results - only available after running `Analyze(testCase)`
	// todo maybe make this all into its own struct like AnalysisResults that references this struct? alternatively, store the struct in this object and call `Analyze()` by default unless a bool is passed to `CreateTestCase`
	ScenarioSet      *ScenarioSet // the set of scenarios defined in this test case, if it is table-driven
	ParsedStatements []string     // the parsed statements in the test case, as strings
	ImportedPackages []string     // the list of imported packages in the test case's file

	// Actual syntax data
	FuncDecl *ast.FuncDecl // the actual declaration of the test case
	File     *ast.File     // the actual file where the test case is defined (for context, e.g. imports)
}

// Store `fset` (as `*token.FileSet`) and `typeInfo` (as `*types.Info`) data in thread-safe maps so they don't have to be stored in the TestCase struct.
// `fset` is the fileset used when parsing a project, and is needed for position information.
// `typeInfo` is the type information for a particular package, and is used for type checking during analysis.
// `fset` changes based on which overarching project is being parsed, and `typeInfo` is specific to each individual package.
var (
	fsetStore     gsm.GenericSyncMap[string, *token.FileSet]  // keys are PROJECT names
	typeInfoStore gsm.GenericSyncMap[*ast.Ident, *types.Info] // keys are PACKAGE identifiers
)

// Convenience method for getting the type of an expression (including identifiers) within the current TestCase's project.
// Returns `nil` if the type information for the project is not available, or if the expression is not found.
func (tc *TestCase) TypeOf(expr ast.Expr) types.Type {
	if tc.File == nil {
		return nil
	}
	typeInfo, ok := typeInfoStore.Load(tc.File.Name)
	if !ok {
		return nil
	}
	// Attempt to resolve the type
	return typeInfo.TypeOf(expr)
}

// Convenience method for getting the FileSet corresponding to this TestCase's project.
// The boolean result indicates whether the FileSet was successfully retrieved.
func (tc *TestCase) Fset() (*token.FileSet, bool) {
	return fsetStore.Load(tc.Project)
}

// Create a new TestCase struct for storage and analysis
func CreateTestCase(funcDecl *ast.FuncDecl, file *ast.File, project string, fset *token.FileSet, typeInfo *types.Info) TestCase {
	if funcDecl == nil || file == nil || fset == nil || typeInfo == nil {
		slog.Error("Cannot create TestCase with nil syntax or type data", "funcDecl", funcDecl, "file", file, "project", project, "fset", fset, "typeInfo", typeInfo)
		return TestCase{}
	}

	// Save the `fset` and `typeInfo` in the thread-safe maps for later retrieval (if they don't already exist)
	fsetStore.LoadOrStore(project, fset)
	typeInfoStore.LoadOrStore(file.Name, typeInfo)

	// Create the TestCase itself
	return TestCase{
		Name:     funcDecl.Name.Name,
		Package:  file.Name.Name,
		FilePath: fset.Position(funcDecl.Pos()).Filename,
		Project:  project,

		// Analysis results are empty by default

		FuncDecl: funcDecl,
		File:     file,
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

	slog.Debug("Found valid test case:", "name", name)

	return true, badFormat
}

// Return the list of statements in this test case
func (tc *TestCase) GetStatements() []ast.Stmt {
	if tc.FuncDecl == nil || tc.FuncDecl.Body == nil {
		slog.Warn("Cannot get statements from test case because FuncDecl or its body is nil", "testCase", tc.Name)
		return nil
	}
	return tc.FuncDecl.Body.List
}

// Return the number of statements in the test case
func (tc *TestCase) NumStatements() int {
	return len(tc.GetStatements())
}

// Return the number of individual lines (not statements) that the test case spans
func (tc *TestCase) NumLines() int {
	fset, fsetOk := tc.Fset()
	if tc.FuncDecl == nil || !fsetOk {
		slog.Warn("Cannot determine number of lines in test case because FuncDecl or fset is nil", "testCase", tc.Name)
		return -1
	}
	start := fset.Position(tc.FuncDecl.Pos())
	end := fset.Position(tc.FuncDecl.End())
	return end.Line - start.Line + 1
}

//
// =============== Output Methods ===============
//

// Return a string representation of the TestCase for logging and debugging purposes
func (tc *TestCase) String() string {
	return fmt.Sprintf("TestCase{Name: %s, Package: %s, FileName: %s, Project: %s, NumStatements: %d}", tc.Name, tc.Package, tc.FilePath, tc.Project, tc.NumStatements())
}

// Return the headers for the CSV representation of the TestCase
// Complex or large fields are excluded for the sake of brevity.
func (tc *TestCase) GetCSVHeaders() []string {
	return []string{
		"project",
		"filename",
		"package",
		"name",
		"tableDrivenDataStructure",
		"importedPackages",
	}
}

// Encode TestCase as a CSV row, returning the encoded data corresponding to the headers in `GetCSVHeaders`.
func (tc *TestCase) EncodeAsCSV() []string {
	return []string{
		tc.Project,
		tc.FilePath,
		tc.Package,
		tc.Name,
		tc.ScenarioSet.DataStructure.String(),
		strings.Join(tc.ImportedPackages, ", "),
	}
}

// Save the TestCase as JSON to a file named like `<project>/<project>_<package>_<testcase>.json` in the specified directory (or the output directory if not specified).
func (tc *TestCase) SaveAsJSON(dir string) error {
	slog.Info("Saving test case as JSON", "testCase", tc)

	// Construct the filepath using information from the test case, inside the provided directory.
	// If the directory is empty, the `filewriter` will automatically prepend the output directory instead.
	path := filepath.Join(dir, tc.Project, fmt.Sprintf("%s_%s_%s.json", tc.Project, tc.Package, tc.Name))

	// Create and write the file
	err := filewriter.WriteToFile(path, tc)
	if err != nil {
		return fmt.Errorf("saving test case %q as JSON: %w", tc.Name, err)
	}

	slog.Info("Saved test case as JSON", "filePath", path)
	return nil
}

// Helper struct for Marshaling and Unmarshaling JSON.
// Transforms all `ast` nodes to their string representations.
type testCaseJSON struct {
	Name     string `json:"name"`
	Package  string `json:"package"`
	FilePath string `json:"filePath"`
	Project  string `json:"project"`

	ScenarioSet      *ScenarioSet `json:"scenarioSet"`
	ParsedStatements []string     `json:"parsedStatements"`
	ImportedPackages []string     `json:"importedPackages"`

	FuncDecl string `json:"funcDecl"`
	// File is not saved
}

// Marshal the TestCase for JSON output
func (tc *TestCase) MarshalJSON() ([]byte, error) {
	fset, ok := tc.Fset()
	if !ok {
		slog.Error("Failed to find FileSet for project", "project", tc.Project)
		return nil, fmt.Errorf("could not find FileSet for project %q", tc.Project)
	}
	return json.Marshal(testCaseJSON{
		Name:     tc.Name,
		Package:  tc.Package,
		FilePath: tc.FilePath,
		Project:  tc.Project,

		ScenarioSet:      tc.ScenarioSet,
		ParsedStatements: tc.ParsedStatements,
		ImportedPackages: tc.ImportedPackages,

		FuncDecl: nodeToString(tc.FuncDecl, fset),
	})
}

// Unmarshal the TestCase from JSON
// todo maybe remove this because it probably doesn't decode properly
func (tc *TestCase) UnmarshalJSON(data []byte) error {
	var aux testCaseJSON
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	// Try to decode complex fields

	var funcDecl *ast.FuncDecl
	expr, err := stringToNode(aux.FuncDecl)
	if err != nil {
		slog.Error("Failed to parse TestCase FuncDecl from JSON", "error", err)
	} else {
		// Only check the type if the string was parsed successfully
		if decl, ok := expr.(*ast.FuncDecl); ok {
			funcDecl = decl
		} else {
			slog.Error("Failed to parse TestCase FuncDecl from JSON because it is not a valid function declaration", "string", aux.FuncDecl)
		}
	}

	// Save data into the main struct
	*tc = TestCase{
		Name:     aux.Name,
		Package:  aux.Package,
		FilePath: aux.FilePath,
		Project:  aux.Project,

		ScenarioSet:      aux.ScenarioSet,
		ParsedStatements: aux.ParsedStatements,
		ImportedPackages: aux.ImportedPackages,

		FuncDecl: funcDecl,
		// File cannot be restored from JSON because it isn't saved
	}
	return nil
}

// todo maybe move these functions to an internal AST utils package
// Define a printer config for converting AST nodes to string representations
var printerCfg = &printer.Config{
	Mode:     printer.UseSpaces | printer.TabIndent,
	Tabwidth: 4,
}

// Convert an AST node to a string representation using `go/printer`, or return an error string if formatting fails
// todo maybe return actual errors
func nodeToString(node ast.Node, fset *token.FileSet) string {
	if node == nil || reflect.ValueOf(node).IsNil() {
		return ""
	}

	var buf bytes.Buffer
	err := printerCfg.Fprint(&buf, fset, node)
	if err != nil {
		slog.Error("Failed to format AST node", "error", err, "nodeType", fmt.Sprintf("%T", node))
		return "Failed to format AST node: " + err.Error()
	}

	return buf.String()
}

// Parse a string (usually from JSON) into the corresponding AST expression.
// This function tries to parse the string as a declaration, statement, or expression in that order.
func stringToNode(str string) (ast.Node, error) {
	// First try parsing the string as a declaration by treating the string as a Go source file
	dummyFset := token.NewFileSet()
	fileStr := "package dummy\n" + str
	file, err := parser.ParseFile(dummyFset, "", fileStr, parser.ParseComments)
	if err == nil {
		// Extract and return the first declaration in the file
		if len(file.Decls) > 0 {
			return file.Decls[0], nil
		}
		slog.Debug("Parsed dummy file has no declarations; now trying to parse as statement or expression", "input", str)
	}

	// Try parsing the string as a statement by wrapping the string in a function
	funcStr := "package dummy\nfunc dummyFunc() {\n" + str + "\n}"
	file, err = parser.ParseFile(dummyFset, "", funcStr, parser.ParseComments)
	if err == nil {
		// Extract and return the first statement in the function body
		if len(file.Decls) > 0 {
			if funcDecl, ok := file.Decls[0].(*ast.FuncDecl); ok && len(funcDecl.Body.List) > 0 {
				return funcDecl.Body.List[0], nil
			}
			slog.Debug("Parsed dummy function has no statements; now trying to parse as expression", "file", funcStr)
		}
		// This case should never happen
		slog.Debug("Parsed dummy file (with dummy function) has no declarations; now trying to parse as expression", "input", str)
	}

	// Try parsing the original string as an expression
	expr, err := parser.ParseExpr(str)
	if err == nil {
		// If the string is a valid expression, return it
		return expr, nil
	}

	return nil, fmt.Errorf("failed to parse node string %q: %w", str, err)
}

// Returns a boolean indicating whether a statement is a function call expression of the form `owner.name(...)`.
func IsSelectorFuncCall(stmt ast.Stmt, owner, name string) bool {
	if exprStmt, ok := stmt.(*ast.ExprStmt); ok {
		if callExpr, ok := exprStmt.X.(*ast.CallExpr); ok {
			if selExpr, ok := callExpr.Fun.(*ast.SelectorExpr); ok {
				if ident, ok := selExpr.X.(*ast.Ident); ok && ident.Name == owner && selExpr.Sel.Name == name {
					return true
				}
			}
		}
	}
	return false
}
