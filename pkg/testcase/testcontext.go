package testcase

import (
	"encoding/json"
	"fmt"
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

//
// ========== Field Getters ==========
//

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

// Get the entire import path of the test case's package
func (ctx *testContext) ImportPath() string {
	if ctx.pkgInfo == nil {
		return ""
	}
	return ctx.pkgInfo.PkgPath
}

// Get the "repository root path" part of the test case's package import path.
// This is the part of the import path before the third slash, e.g. "github.com/user/repo"
func (ctx *testContext) ImportPathRoot() string {
	if ctx.pkgInfo == nil {
		return ""
	}
	importPath := ctx.pkgInfo.PkgPath
	// Find the position of the third slash, and return everything before it
	slashCount := 0
	for i, c := range importPath {
		if c == '/' {
			slashCount++
			if slashCount == 3 {
				return importPath[:i]
			}
		}
	}
	// If there are fewer than 3 slashes, return the whole import path
	return importPath
}

// Get the container for all raw information about the test case's package
func (ctx *testContext) PackageInfo() *packages.Package { return ctx.pkgInfo }

// Get the AST file where the test case is defined
func (ctx *testContext) ASTFile() *ast.File { return ctx.file }

func (ctx *testContext) String() string {
	return fmt.Sprintf("testContext{Name: %s, Package: %s, FilePath: %s, Project: %s}", ctx.testName, ctx.packageName, ctx.filePath, ctx.projectName)
}

//
// ========== Action Methods ==========
//

// Convenience method for getting the type of an expression (including identifiers) within the current TestCase's project.
// Returns `nil` if the type information for the project is not available, or if the expression is not found.
func (ctx *testContext) TypeOf(expr ast.Expr) types.Type {
	typeInfo := ctx.TypeInfo()
	if typeInfo == nil || expr == nil {
		return nil
	}
	return typeInfo.TypeOf(expr)
}

// Convenience method for getting the Object corresponding to an identifier within the current TestCase's project.
// Returns `nil` if the type information for the project is not available, or if the identifier is not found.
func (ctx *testContext) ObjectOf(ident *ast.Ident) types.Object {
	typeInfo := ctx.TypeInfo()
	if typeInfo == nil || ident == nil {
		return nil
	}
	return typeInfo.ObjectOf(ident)
}

//
// ========== Output Methods ==========
//

// Helper struct for Marshaling JSON
type testContextJSON struct {
	ProjectName string `json:"project"`
	PackageName string `json:"package"`
	FilePath    string `json:"filePath"`
	Name        string `json:"name"`

	// Syntax data is not marshaled
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
