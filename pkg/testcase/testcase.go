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
	"log/slog"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"github.com/maxgreen01/go-test-parser/internal/filewriter"
	"golang.org/x/tools/go/packages"
)

// Represents an individual test case defined at the top level of a Go source file.
// The fields of this struct should never be modified directly.
type TestCase struct {
	// Contextual identifiers, package-level data, and AST syntax
	*testContext
	FuncDecl *ast.FuncDecl // the actual declaration of the test case

	// Analysis results - only available after running `Analyze()`
	// todo maybe make this all into its own struct like AnalysisResults that references this struct? alternatively, store the struct in this object and call `Analyze()` by default unless a bool is passed to `CreateTestCase`
	ScenarioSet      *ScenarioSet         // the set of scenarios defined in this test case, if it is table-driven
	ParsedStatements []*ExpandedStatement // the list of parsed and fully-expanded statements in the test case
	ImportedPackages []string             // the list of imported packages in the test case's file
}

// Create a new TestCase struct for storage and analysis
// todo return error value more clearly either by returning nil or an error type
func CreateTestCase(funcDecl *ast.FuncDecl, file *ast.File, pkg *packages.Package, project string) TestCase {
	if funcDecl == nil || file == nil || pkg == nil {
		slog.Error("Cannot create TestCase with nil syntax data", "funcDecl", funcDecl, "file", file, "pkg", pkg, "project", project)
		return TestCase{}
	}

	// Create the TestCase itself
	return TestCase{
		testContext: NewTestContext(funcDecl.Name.Name, file, pkg, project),
		FuncDecl:    funcDecl,

		// Analysis results are empty until `Analyze()` is called
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
	// todo MAYBE allow this (and condition below) to accept "Fuzz" or "Benchmark" and indicate a different category somehow (maybe using enum `type` in TestCase)
	if !strings.HasPrefix(name, "Test") {
		// slog.Debug("\tfunction name does not start with 'Test'", "name", name)
		return false, false
	}

	// the function's 5th letter *should* be capitalized, but it's not strictly required
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
		slog.Error("Cannot get statements from test case because FuncDecl or its body is nil", "testCase", tc.testName, "package", tc.packageName)
		return nil
	}
	return tc.FuncDecl.Body.List
}

// Return the number of statements in the test case
func (tc *TestCase) NumStatements() int {
	return len(tc.GetStatements())
}

// Return the number of individual lines (not statements) that the test case spans,
// or 0 if the number of lines cannot be determined.
func (tc *TestCase) NumLines() int {
	fset := tc.FileSet()
	if tc.FuncDecl == nil || fset == nil {
		slog.Error("Cannot determine number of lines in test case because FuncDecl or FileSet is nil", "testCase", tc.testName, "package", tc.packageName)
		return 0
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
	return fmt.Sprintf("TestCase{%v, NumStatements: %d, NumLines: %d}", tc.testContext, tc.NumStatements(), tc.NumLines())
}

// Return the headers for the CSV representation of the TestCase
// Complex or large fields are excluded for the sake of brevity.
func (tc *TestCase) GetCSVHeaders() []string {
	return []string{
		"project",
		"filename",
		"package",
		"name",
		"scenarioDataStructure",
		"scenarioNameField",
		"scenarioExpectedFields",
		"scenarioHasFunctionFields",
		"scenarioUsesSubtest",
		"importedPackages",
	}
}

// Encode TestCase as a CSV row, returning the encoded data corresponding to the headers in `GetCSVHeaders`.
func (tc *TestCase) EncodeAsCSV() []string {
	ss := tc.ScenarioSet
	if ss == nil {
		ss = &ScenarioSet{} // Use an empty ScenarioSet to avoid nil pointer dereference
	}

	return []string{
		tc.projectName,
		tc.filePath,
		tc.packageName,
		tc.testName,
		ss.DataStructure.String(),
		ss.NameField,
		strings.Join(ss.ExpectedFields, ", "),
		strconv.FormatBool(ss.HasFunctionFields),
		strconv.FormatBool(ss.UsesSubtest),
		strings.Join(tc.ImportedPackages, ", "),
	}
}

// Save the TestCase as JSON to a file named like `<project>/<project>_<package>_<testcase>.json` in the specified directory (or the output directory if not specified).
func (tc *TestCase) SaveAsJSON(dir string) error {
	slog.Info("Saving test case as JSON", "testCase", tc)

	// Construct the filepath using information from the test case, inside the provided directory.
	// If the directory is empty, the `filewriter` will automatically prepend the output directory instead.
	path := filepath.Join(dir, tc.projectName, fmt.Sprintf("%s_%s_%s.json", tc.projectName, tc.packageName, tc.testName))

	// Create and write the file
	err := filewriter.WriteToFile(path, tc)
	if err != nil {
		return fmt.Errorf("saving test case %q as JSON: %w", tc.testName, err)
	}

	slog.Info("Saved test case as JSON", "filePath", path)
	return nil
}

// Helper struct for Marshaling and Unmarshaling JSON
type testCaseJSON struct {
	TestContext *testContext `json:"testContext"`
	FuncDecl    string       `json:"funcDecl"`

	ScenarioSet      *ScenarioSet         `json:"scenarioSet"`
	ParsedStatements []*ExpandedStatement `json:"parsedStatements"`
	ImportedPackages []string             `json:"importedPackages"`
}

// Marshal a TestCase for JSON output
func (tc *TestCase) MarshalJSON() ([]byte, error) {
	return json.Marshal(testCaseJSON{
		TestContext: tc.testContext,

		ScenarioSet:      tc.ScenarioSet,
		ParsedStatements: tc.ParsedStatements,
		ImportedPackages: tc.ImportedPackages,

		FuncDecl: nodeToString(tc.FuncDecl, tc.FileSet()),
	})
}

// Unmarshal a TestCase from JSON
// todo maybe remove this because it probably doesn't decode properly
func (tc *TestCase) UnmarshalJSON(data []byte) error {
	var jsonData testCaseJSON
	if err := json.Unmarshal(data, &jsonData); err != nil {
		return err
	}

	// Try to decode complex fields

	var funcDecl *ast.FuncDecl
	expr, err := stringToNode(jsonData.FuncDecl)
	if err != nil {
		slog.Error("Failed to parse TestCase FuncDecl from JSON", "error", err)
	} else {
		// Only check the type if the string was parsed successfully
		if decl, ok := expr.(*ast.FuncDecl); ok {
			funcDecl = decl
		} else {
			slog.Error("Failed to parse TestCase FuncDecl from JSON because it is not a valid function declaration", "string", jsonData.FuncDecl)
		}
	}

	// Save data into the main struct
	*tc = TestCase{
		testContext: jsonData.TestContext,
		FuncDecl:    funcDecl,

		ScenarioSet:      jsonData.ScenarioSet,
		ParsedStatements: jsonData.ParsedStatements,
		ImportedPackages: jsonData.ImportedPackages,
	}
	return nil
}

// TODO IMPROVE maybe move these functions to an internal AST utils package

// Define a printer config for converting AST nodes to string representations
var printerCfg = &printer.Config{
	Mode:     printer.UseSpaces | printer.TabIndent,
	Tabwidth: 4,
}

// Convert an AST node to a string representation using `go/printer`, or return an error string if formatting fails
// todo CLEANUP should return actual errors
func nodeToString(node ast.Node, fset *token.FileSet) string {
	if node == nil || reflect.ValueOf(node).IsNil() {
		return ""
	}
	if fset == nil {
		slog.Error("Failed to format AST node because FileSet is nil", "nodeType", fmt.Sprintf("%T", node))
		return ""
	}

	var buf bytes.Buffer
	err := printerCfg.Fprint(&buf, fset, node)
	if err != nil {
		slog.Error("Failed to format AST node", "error", err, "nodeType", fmt.Sprintf("%T", node))
		return ""
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
		} else {
			// This should never happen
			slog.Debug("Parsed dummy file (with dummy function) has no declarations; now trying to parse as expression", "input", str)
		}
	}

	// Try parsing the original string as an expression
	expr, err := parser.ParseExpr(str)
	if err != nil {
		return nil, fmt.Errorf("parsing node string %q: %w", str, err)
	}

	// The string is a valid expression
	return expr, nil
}

// Returns a boolean indicating whether a statement is a function call expression of the form `owner.name(...)`,
// as well as a reference to the `ast.CallExpr` if the statement matches.
func IsSelectorFuncCall(stmt ast.Stmt, owner, name string) (bool, *ast.CallExpr) {
	if exprStmt, ok := stmt.(*ast.ExprStmt); ok {
		if callExpr, ok := exprStmt.X.(*ast.CallExpr); ok {
			if MatchSelectorExpr(callExpr.Fun, owner, name) {
				return true, callExpr
			}
		}
	}
	return false, nil
}

// Returns a boolean indicating whether a selector expression has the form `owner.name`.
func MatchSelectorExpr(expr ast.Expr, owner, name string) bool {
	if selExpr, ok := expr.(*ast.SelectorExpr); ok {
		if ident, ok := selExpr.X.(*ast.Ident); ok && ident.Name == owner && selExpr.Sel.Name == name {
			return true
		}
	}
	return false
}
