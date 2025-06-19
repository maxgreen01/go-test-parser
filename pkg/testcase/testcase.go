// Provides functionality for analyzing and storing test cases extracted from Go source files.
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
	"strings"

	"github.com/maxgreen01/golang-test-parser/internal/filewriter"
)

// Represents an individual test case defined at the top level of a Go source file
type TestCase struct {
	// High-level identifiers for the test case
	Name     string // the name of the test case
	Package  string // the fully qualified package name where the test case is defined
	FileName string // the name of the file where the test case is defined
	Project  string // the overarching project directory that the test case's package is part of

	// Analysis results
	tableDrivenType  string   // the type of table-driven test, if applicable (e.g., "struct, with loop", "map, no loop", "none, no loop") //todo convert this into a struct that stringifies later
	parsedStatements []string // the parsed statements in the test case, as strings
	// subtests         []subTest // the list of subtests defined in this test case, if any
	importedPackages []string // the list of imported packages in the test case's file

	// Actual syntax data
	FuncDecl *ast.FuncDecl  // the actual declaration of the test case
	File     *ast.File      // the actual file where the test case is defined (for context, e.g. imports)
	fset     *token.FileSet // the file set used when parsing this project - needed for position information
}

// Represents a subtest defined within a test case, for use in table-driven tests
type subTest struct {
	Name       string            // the name of the subtest, if any //todo how to detect?
	Definition *ast.CompositeLit // the actual literal that defines the subtest structure and values (expected to be either a struct or map)
	Runner     *ast.BlockStmt    // the actual code that runs the subtest (expected to be the body of a loop)
}

// Define a printer config for converting AST nodes to string representations
var printerCfg = &printer.Config{
	Mode:     printer.UseSpaces | printer.TabIndent,
	Tabwidth: 4,
}

// Create a new TestCase struct for storage
func CreateTestCase(funcDecl *ast.FuncDecl, file *ast.File, fset *token.FileSet, project string) TestCase {
	if funcDecl == nil || file == nil || fset == nil {
		slog.Error("Cannot create TestCase with nil syntax data", "funcDecl", funcDecl, "file", file, "fset", fset)
		return TestCase{}
	}

	return TestCase{
		Name:     funcDecl.Name.Name,
		Package:  file.Name.Name,
		FileName: fset.Position(funcDecl.Pos()).Filename,
		Project:  project,

		FuncDecl: funcDecl,
		File:     file,
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

	slog.Debug("Found valid test case:", "name", name)

	return true, badFormat
}

// Return the list of statements in this test case
func (t *TestCase) GetStatements() []ast.Stmt {
	if t.FuncDecl == nil || t.FuncDecl.Body == nil {
		slog.Warn("Cannot get statements from test case because FuncDecl or its body is nil", "testCase", t.Name)
		return nil
	}
	return t.FuncDecl.Body.List
}

// Return the number of statements in the test case
func (t TestCase) NumStatements() int {
	return len(t.GetStatements())
}

// Return the number of individual lines (not statements) that the test case spans
func (t TestCase) NumLines() int {
	if t.FuncDecl == nil || t.fset == nil {
		slog.Warn("Cannot determine number of lines in test case because FuncDecl or fset is nil", "testCase", t.Name)
		return -1
	}
	start := t.fset.Position(t.FuncDecl.Pos())
	end := t.fset.Position(t.FuncDecl.End())
	return end.Line - start.Line + 1
}

// Extract relevant information about this TestCase and save the results into its own corresponding fields
func (t *TestCase) Analyze() {
	// Analyze the individual statements in the test case
	stmts := t.GetStatements()

	t.parsedStatements = make([]string, len(stmts))
	t.tableDrivenType = "none" // Default value if no table-driven test information is detected
	var foundTableType, foundLoop bool
	for i, stmt := range stmts {
		// Stringify the entire statement
		t.parsedStatements[i] = nodeToString(stmt, t.fset)

		// Helper function for saving data if a statement is a struct or map, returning whether a struct or map was found
		// todo do more processing to extract specific data based on the type of literal
		detectTableDrivenType := func(expr ast.Expr) bool {
			if _, ok := expr.(*ast.StructType); ok {
				slog.Debug("Found struct definition in test case", "test", t.Name)
				t.tableDrivenType = "struct"
				foundTableType = true

			} else if _, ok := expr.(*ast.MapType); ok {
				slog.Debug("Found map definition in test case", "test", t.Name)
				t.tableDrivenType = "map"
				foundTableType = true
			}
			return foundTableType
		}

		// Extract struct or map declarations or assignments anywhere inside the function.
		// These are typically either inside a DeclStmt, AssignStmt, or RangeStmt as setup for table-driven tests
		if !foundTableType {
			ast.Inspect(stmt, func(n ast.Node) bool {
				// Check if this is a struct or map definition
				if genDecl, ok := n.(*ast.GenDecl); ok {
					for _, spec := range genDecl.Specs {
						if typeSpec, ok := spec.(*ast.TypeSpec); ok {
							if found := detectTableDrivenType(typeSpec.Type); found {
								return false
							}
						}
					}
				}

				// Check if this is a struct or map literal
				if compositeLit, ok := n.(*ast.CompositeLit); ok {
					// Struct literal is expected to be a slice
					if arrStmt, ok := compositeLit.Type.(*ast.ArrayType); ok {
						if found := detectTableDrivenType(arrStmt.Elt); found {
							return false
						}

						// Map literal is expected to be standalone
					} else if found := detectTableDrivenType(compositeLit.Type); found {
						return false
					}
				}

				return true // Keep checking children nodes
			})
		}

		// Extract the loop that runs the subtests
		if !foundLoop {
			// Helper func for detecting t.Run() calls inside a loop body
			detectTRun := func(block *ast.BlockStmt) {
				for _, stmt := range block.List {
					if exprStmt, ok := stmt.(*ast.ExprStmt); ok {
						if callExpr, ok := exprStmt.X.(*ast.CallExpr); ok {
							if selExpr, ok := callExpr.Fun.(*ast.SelectorExpr); ok {
								if ident, ok := selExpr.X.(*ast.Ident); ok && ident.Name == "t" && selExpr.Sel.Name == "Run" {
									t.tableDrivenType += ", using t.Run()"
								}
							}
						}
					}
				}
			}

			if rangeStmt, ok := stmt.(*ast.RangeStmt); ok {
				slog.Debug("Found range statement in test case", "test", t.Name)
				t.tableDrivenType += ", with range loop"
				foundLoop = true
				detectTRun(rangeStmt.Body)

			} else if forStmt, ok := stmt.(*ast.ForStmt); ok {
				slog.Debug("Found loop statement in test case", "test", t.Name)
				t.tableDrivenType += ", with for loop"
				foundLoop = true
				detectTRun(forStmt.Body)
			}
		}

		// todo do more work here to classify statements
	}

	// Assign default values if table-driven test information was not detected
	if !foundLoop {
		t.tableDrivenType += ", no loop"
	}

	// Extract imported packages from the file's AST

	var imports []*ast.ImportSpec
	if t.File != nil {
		imports = t.File.Imports
	} else {
		slog.Warn("Cannot extract imported packages in test case because File is nil", "testCase", t.Name)
	}
	for _, imp := range imports {
		t.importedPackages = append(t.importedPackages, strings.Trim(imp.Path.Value, "\""))
	}
}

// Return a string representation of the TestCase for logging and debugging purposes
func (t TestCase) String() string {
	return fmt.Sprintf("TestCase{Name: %s, Package: %s, FileName: %s, Project: %s, NumStatements: %d}", t.Name, t.Package, t.FileName, t.Project, t.NumStatements())
}

// Save the TestCase as JSON to a file named like `<project>/<project>_<package>_<testcase>.json` in the output directory
func (t TestCase) SaveAsJSON() error {
	slog.Info("Saving test case as JSON", "testCase", t)

	path := fmt.Sprintf("%s/%s_%s_%s.json", t.Project, t.Project, t.Package, t.Name) // save data as JSON
	err := filewriter.WriteToFile(path, t)
	if err != nil {
		return fmt.Errorf("saving test case %q as JSON: %w", t.Name, err)
	}

	slog.Info("Saved test case as JSON", "filePath", path)
	return nil
}

// Helper struct for Marshaling and Unmarshaling JSON.
// Transforms all `ast` nodes to their string representations.
type testCaseJSON struct {
	Name     string `json:"name"`
	Package  string `json:"package"`
	FileName string `json:"filename"`
	Project  string `json:"project"`

	TableDrivenType  string   `json:"tableDrivenType"`
	ParsedStatements []string `json:"parsedStatements"`
	ImportedPackages []string `json:"importedPackages"`

	FuncDecl string `json:"funcDecl"`
	// File and fset are not saved
}

// Marshal the TestCase for JSON output
func (t TestCase) MarshalJSON() ([]byte, error) {
	out := testCaseJSON{
		Name:     t.Name,
		Package:  t.Package,
		FileName: t.FileName,
		Project:  t.Project,

		TableDrivenType:  t.tableDrivenType,
		ParsedStatements: t.parsedStatements,
		ImportedPackages: t.importedPackages,

		FuncDecl: nodeToString(t.FuncDecl, t.fset),
	}
	return json.Marshal(out)
}

// Unmarshal the TestCase from JSON
func (t *TestCase) UnmarshalJSON(data []byte) error {
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
	*t = TestCase{
		Name:     aux.Name,
		Package:  aux.Package,
		FileName: aux.FileName,
		Project:  aux.Project,

		tableDrivenType:  aux.TableDrivenType,
		parsedStatements: aux.ParsedStatements,
		importedPackages: aux.ImportedPackages,

		FuncDecl: funcDecl,
		// File and fset cannot be restored from JSON because they aren't saved
	}
	return nil
}

// todo maybe move these to an internal utils package
// Convert an AST node to a string representation using `go/printer`, or return "ERROR" if formatting fails
func nodeToString(node ast.Node, fset *token.FileSet) string {
	var buf bytes.Buffer
	err := printerCfg.Fprint(&buf, fset, node)
	if err != nil {
		slog.Error("Failed to format AST node", "error", err, "nodeType", fmt.Sprintf("%T", node))
		return "ERROR" //todo maybe make this an actual error
	}

	return buf.String()
}

// Parse a string (usually from JSON) into the corresponding AST expression.
// This function tries to parse the string as a declaration, statement, or expression in that order.
func stringToNode(str string) (ast.Node, error) {
	// First try parsing the string as a declaration by treating the string as a Go source file
	fset := token.NewFileSet()
	fileStr := "package dummy\n" + str
	file, err := parser.ParseFile(fset, "", fileStr, parser.ParseComments)
	if err == nil {
		// Extract and return the first declaration in the file
		if len(file.Decls) > 0 {
			return file.Decls[0], nil
		}
		slog.Debug("Parsed dummy file has no declarations; now trying to parse as statement or expression", "input", str)
	}

	// Try parsing the string as a statement by wrapping the string in a function
	funcStr := "package dummy\nfunc dummyFunc() {\n" + str + "\n}"
	file, err = parser.ParseFile(fset, "", funcStr, parser.ParseComments)
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
