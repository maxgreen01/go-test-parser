package asttools

// A collection of general-purpose AST-related utility functions

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"go/types"
	"log/slog"
	"reflect"
)

//
// ========== Conversion Functions ==========
//

// Define a printer config for converting AST nodes to string representations
var printerCfg = &printer.Config{
	Mode:     printer.UseSpaces | printer.TabIndent,
	Tabwidth: 4,
}

// Convert an AST node to a string representation using `go/printer`, or return an error string if formatting fails
// todo CLEANUP should return actual errors
func NodeToString(node ast.Node, fset *token.FileSet) string {
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

// Fake package and function declarations are used when parsing strings into AST nodes
const (
	_fakePackage = "package _"
	_fakeFunc    = "func _() "
)

// Parse a string (usually from JSON) into the corresponding AST expression.
// This function tries to parse the string as a declaration, statement, or expression in that order.
func StringToNode(str string) (ast.Node, error) {
	// First try parsing the string as a declaration by treating the string as a Go source file
	fakeFset := token.NewFileSet()
	fileStr := _fakePackage + "\n" + str
	file, err := parser.ParseFile(fakeFset, "", fileStr, parser.ParseComments)
	if err == nil {
		// Extract and return the first declaration in the file
		if len(file.Decls) > 0 {
			return file.Decls[0], nil
		}
		slog.Debug("Parsed fake file has no declarations; now trying to parse as statement or expression", "input", str)
	}

	// Try parsing the string as a statement by wrapping the string in a function
	funcStr := _fakeFunc + "\n" + _fakeFunc + "{\n" + str + "\n}"
	file, err = parser.ParseFile(fakeFset, "", funcStr, parser.ParseComments)
	if err == nil {
		// Extract and return the first statement in the function body
		if len(file.Decls) > 0 {
			if funcDecl, ok := file.Decls[0].(*ast.FuncDecl); ok && len(funcDecl.Body.List) > 0 {
				return funcDecl.Body.List[0], nil
			}
			slog.Debug("Parsed fake function has no statements; now trying to parse as expression", "file", funcStr)
		} else {
			// This should never happen
			slog.Debug("Parsed fake file (with fake function) has no declarations; now trying to parse as expression", "input", str)
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

//
// ========== Node Detection Functions ==========
//

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

//
// ========== Node Creation Functions ==========
//

// Creates a selector expression of the form `owner.name`.
func NewSelectorExpr(owner, name string) ast.Expr {
	return &ast.SelectorExpr{
		X:   ast.NewIdent(owner),
		Sel: ast.NewIdent(name),
	}
}

//
// ========== Type System Functions ==========
//

// Returns whether a Type is Basic and has the specified info.
// See `go/types.Basic` for more details.
func IsBasicType(typ types.Type, info types.BasicInfo) bool {
	if basic, ok := typ.Underlying().(*types.Basic); ok {
		return basic.Info() == info
	}
	return false
}

// Returns T given *T or an alias thereof.
// For all other types it is the identity function.
// [copied from `go/typesinternal` package]
func Unpointer(t types.Type) types.Type {
	if ptr, ok := t.Underlying().(*types.Pointer); ok {
		return ptr.Elem()
	}
	return t
}
