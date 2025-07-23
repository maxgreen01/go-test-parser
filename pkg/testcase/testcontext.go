package testcase

import (
	"encoding/json"
	"go/ast"
	"go/token"
	"go/types"
	"log/slog"

	"golang.org/x/tools/go/packages"
)

// Represents the static context in which a test case exists.
type testContext struct {
	// High-level identifiers
	testName    string // the name of the test case itself
	packageName string // the name of the package where the test case is defined, as it appears in the source code
	filePath    string // the path to the file where the test case is defined
	projectName string // the name of the overarching project that the test case is part of

	// Raw syntax data
	pkgInfo *packages.Package // the actual AST information about the test's package, including AST data, types, etc.
	file    *ast.File         // the AST file where the test case is defined
}

// Create a new testContext
func NewTestContext(testName string, file *ast.File, pkg *packages.Package, project string) *testContext {
	if file == nil || pkg == nil {
		slog.Error("Cannot create testContext with nil syntax data", "file", file, "pkg", pkg, "project", project)
		return nil
	}

	return &testContext{
		testName:    testName,
		packageName: file.Name.Name, // todo CLEANUP this should probably be pkg.PkgPath for extra precision
		filePath:    pkg.Fset.Position(file.Pos()).Filename,
		projectName: project,

		pkgInfo: pkg,
		file:    file,
	}
}

// Get the name of the test case
func (ctx *testContext) TestName() string { return ctx.testName }

// Get the name of the package where the test case is defined
func (ctx *testContext) PackageName() string { return ctx.packageName }

// Get the path to the file where the test case is defined
func (ctx *testContext) FilePath() string { return ctx.filePath }

// Get the name of the project that the test case is part of
func (ctx *testContext) ProjectName() string { return ctx.projectName }

// Get the the FileSet used for parsing the test's entire project
func (ctx *testContext) FileSet() *token.FileSet {
	if ctx.pkgInfo == nil {
		return nil
	}
	return ctx.pkgInfo.Fset
}

// Get the type information for the test case's package
func (ctx *testContext) TypeInfo() *types.Info {
	if ctx.pkgInfo == nil {
		return nil
	}
	return ctx.pkgInfo.TypesInfo
}

// Get all the AST files involved in the test case's package
func (ctx *testContext) PackageFiles() []*ast.File {
	if ctx.pkgInfo == nil {
		return nil
	}
	return ctx.pkgInfo.Syntax
}

// Get the AST file where the test case is defined
func (ctx *testContext) ASTFile() *ast.File { return ctx.file }

// Helper struct for Marshaling JSON
type testContextJSON struct {
	ProjectName string `json:"project"`
	PackageName string `json:"package"`
	FilePath    string `json:"filePath"`
	Name        string `json:"name"`
}

// Marshal a testContext for JSON output, excluding raw syntax data
func (ctx *testContext) MarshalJSON() ([]byte, error) {
	return json.Marshal(testContextJSON{
		Name:        ctx.testName,
		PackageName: ctx.packageName,
		FilePath:    ctx.filePath,
		ProjectName: ctx.projectName,

		// Syntax data is not marshaled
	})
}

// Unmarshal a testContext from JSON
func (ctx *testContext) UnmarshalJSON(data []byte) error {
	var aux testContextJSON
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	*ctx = testContext{
		testName:    aux.Name,
		packageName: aux.PackageName,
		filePath:    aux.FilePath,
		projectName: aux.ProjectName,

		// Syntax data cannot be recovered
	}
	return nil
}
