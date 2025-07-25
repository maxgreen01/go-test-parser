package testcase

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/token"
	"log/slog"
	"slices"
	"strings"

	"golang.org/x/tools/go/ast/astutil"
)

// Represents the expanded form of a function call statement as a G-tree.
// If the statement is a function call, its inner statements are expanded recursively and stored in `Children`.
// If the statement involves function calls somehow (e.g. as part of an assignment or conditional statement), those calls
// are also considered as children.
// If the statement is not a function call and does not involve any function calls, its `Children` field is nil.
type ExpandedStatement struct {
	// The original statement
	Stmt ast.Stmt

	// The expanded form of the called function's inner statements, or nil if the statement is not a function call
	Children []*ExpandedStatement

	// The FileSet used when creating this ExpandedStatement, used for stringifying the original statement
	fset *token.FileSet
}

// Recursively create the fully expanded form of a function call statement, expanding depth first.
// If `testOnly` is true, only expand statements that are defined in a file with a `_test.go` suffix.
// Note that functions are only expanded when they're called, so function literals (e.g. inside `t.Run()`) are not expanded.
func ExpandStatement(stmt ast.Stmt, context *testContext, testOnly bool) *ExpandedStatement {
	return expandStatementWithStack(stmt, context, testOnly, nil)
}

// Helper for ExpandStatement that tracks the function call stack to avoid expanding recursive calls.
// Note that the order of processing a statement's "children" is partially determined by the implementation of `ast.Inspect()`.
func expandStatementWithStack(stmt ast.Stmt, context *testContext, testOnly bool, callStack []string) *ExpandedStatement {
	if stmt == nil {
		return nil
	}
	fset := context.FileSet()
	if fset == nil {
		slog.Error("Cannot expand statement because context FileSet is nil", "statement", stmt, "context", context)
		return nil
	}

	// Create the "root" ExpandedStatement for the original statement
	root := &ExpandedStatement{
		Stmt: stmt,
		fset: fset,
	}

	// Use ast.Inspect to walk all nodes in the statement, looking for function calls to expand.
	// Any time a function that can be expanded is found, it's treated as a new child of its parent statement.
	// This means functions called in the parameters or body of a node will be treated as separate children, which
	// will all be expanded as well. These non-body function calls are parsed and saved before a function's body statements.
	ast.Inspect(stmt, func(n ast.Node) bool {
		if n == nil {
			return false
		}
		// Look for function calls
		callExpr, ok := n.(*ast.CallExpr)
		if !ok {
			// The only time we want to continue checking the node's children via `Inspect()` is if the node is NOT a function call.
			// If the node is a function call, we instead want to manually visit the arguments and function definition to expand them properly.
			return true
		}

		// Make sure the function call itself has its own ExpandedStatement (regardless of how it's used in the parent),
		// which will then be used to expand its children before being saved back to the root statement.
		// Don't create a new parent if the root is an expression statement, though, because the structs would be identical.
		parent := root
		if _, ok := parent.Stmt.(*ast.ExprStmt); !ok {
			parent = &ExpandedStatement{
				Stmt: &ast.ExprStmt{X: callExpr},
				fset: fset,
			}
			// Save a reference to the parent in the root statement, so all changes to the parent are also saved to the root
			root.Children = append(root.Children, parent)
		}

		// Before expanding the function definition, expand the arguments of the function call
		for _, arg := range callExpr.Args {
			// If the argument expression is a function call, treat it as a standalone statement and expand it.
			// The callstack doesn't have to be adjusted here because the arg function is run in the same scope as the original statement.
			if _, ok := arg.(*ast.CallExpr); ok {
				argStmt := &ast.ExprStmt{X: arg}
				parent.Children = append(parent.Children, expandStatementWithStack(argStmt, context, testOnly, callStack))
			}
		}

		// Find the definition of the function being called
		definition, err := FindDefinition(callExpr.Fun, context, testOnly)
		if err != nil {
			slog.Error("Error finding definition for function call", "position", fset.Position(callExpr.Pos()), "err", err)
			return false
		}
		if definition == nil {
			// Don't expand this statement for some non-error reason
			return false
		}

		// Detect the function's name and inner statements
		var funcName string
		var innerStmts []ast.Stmt
		switch definition := definition.(type) {
		case *ast.FuncDecl:
			funcName = definition.Name.Name
			innerStmts = definition.Body.List
		case *ast.FuncLit:
			funcName = fmt.Sprintf("funcLit@%s", fset.Position(definition.Pos())) // Use the position as a unique identifier
			innerStmts = definition.Body.List
		}

		// Avoid expanding recursive functions by checking the callstack
		if slices.Contains(callStack, funcName) {
			slog.Debug("Skipping expansion of recursive function call", "function", funcName)
			return false
		}
		// Add the current function name to the callstack to indicate that we'll be working "inside" it
		callStack = append(callStack, funcName)

		// Recursively expand the function's inner statements using the updated callstack
		for _, inner := range innerStmts {
			parent.Children = append(parent.Children, expandStatementWithStack(inner, context, testOnly, callStack))
		}

		return false
	}) // end of `ast.Inspect()`

	return root
}

// Memoization cache for FindDefinition to avoid redundant lookups.
// Keys are strings formatted as "<position>-<project>-<package>-<testOnly>".
var findDefinitionMemo = make(map[string]ast.Node)

// Return the AST definition of the expression within the specified context package, if it exists.
// If the expression is not an identifier or selector expression, returns the original expression.
// Returns nil for both return values (indicating that the definition was deliberately excluded) in the following cases:
//   - The expression is not defined in the specified context package
//   - If `testOnly` is true and the expression is not defined in a file with a `_test.go` suffix
func FindDefinition(expr ast.Expr, context *testContext, testOnly bool) (ast.Node, error) {
	var ident *ast.Ident
	switch x := expr.(type) {
	case *ast.Ident:
		ident = x
	case *ast.SelectorExpr:
		ident = x.Sel
	default:
		return expr, nil // not an identifier or selector expression
	}

	// Get the type object corresponding to the identifier (i.e. its definition)
	obj := context.ObjectOf(ident)
	if obj == nil {
		return nil, fmt.Errorf("could not resolve identifier %q in the context %v", ident.Name, context)
	}
	pos := obj.Pos()
	pkg := obj.Pkg()

	// Don't attempt to expand functions that aren't defined within the same package path as the current project.
	// This helps avoid expanding functions defined in external or built-in libraries, and universe-scope functions.
	if pkg == nil || pos == token.NoPos {
		// Universe-scope function
		slog.Debug("Ignoring universe-scope function", "identifier", ident.Name)
		return nil, nil
	} else if pkg.Path() != context.ImportPath() {
		// Function defined outside the current package
		slog.Debug("Ignoring function defined outside the current package", "identifier", ident.Name, "package", pkg.Path())
		return nil, nil
	}

	// Check the memoization cache to see if the definition has already been found
	cacheKey := fmt.Sprintf("%d-%s-%s-%v", pos, context.packageName, context.projectName, testOnly)
	if node, ok := findDefinitionMemo[cacheKey]; ok {
		// Definition already found, so return it
		return node, nil
	}

	// Find the AST file containing the object
	var definitionFile *ast.File
	for _, file := range context.PackageFiles() {
		if file.FileStart <= pos && pos <= file.FileEnd {
			definitionFile = file
			break
		}
	}
	if definitionFile == nil {
		return nil, fmt.Errorf("could not find definition file for identifier %q in context %v", ident.Name, context)
	}

	if testOnly {
		// Only expand definitions inside test files
		fset := context.FileSet()
		if fset == nil {
			return nil, fmt.Errorf("could not check definition file path for identifier %q because FileSet is nil in context %v", ident.Name, context)
		}
		if !strings.HasSuffix(fset.Position(definitionFile.FileStart).Filename, "_test.go") {
			// Definition not in a test file
			slog.Debug("Ignoring identifier definition found outside a test file", "identifier", ident.Name, "context", context)
			return nil, nil
		}
	}

	// Get the AST node corresponding to the object, plus its ancestors
	path, _ := astutil.PathEnclosingInterval(definitionFile, pos, pos)

	// Resulting path should never be empty, so check the first element
	node := path[0]
	if _, ok := node.(*ast.File); ok {
		// Definition not found
		return nil, fmt.Errorf("could not find definition for identifier %q in context %v", ident.Name, context)
	}

	// The first node is expected to be the original identifier itself, so the second node should be the actual target definition
	if _, ok := node.(*ast.Ident); ok && len(path) > 1 && path[1] != nil {
		def := path[1]
		slog.Debug("Found definition for identifier", "identifier", ident.Name, "position", def.Pos(), "context", context)
		findDefinitionMemo[cacheKey] = def // Store the definition in the memoization cache
		return def, nil
	}

	return nil, fmt.Errorf("found definition for identifier %q, but found unexpected results (%v) in context %v", ident.Name, path, context)
}

//
// =============== Output Methods ===============
//

// Return a string representation of an expanded statement, including the stringified versions of its children.
func (es *ExpandedStatement) String() string {
	if es == nil {
		return "ExpandedStatement{Stmt: nil}"
	}
	if es.fset == nil {
		slog.Error("Cannot stringify ExpandedStatement because FileSet is nil", "expandedStatement", es)
		return "ExpandedStatement{Stmt: nil}"
	}
	if es.Children == nil {
		// If there are no children, just return the stringified statement
		return fmt.Sprintf("ExpandedStatement{Stmt: %v}", nodeToString(es.Stmt, es.fset))
	}

	children := make([]string, len(es.Children))
	for i, child := range es.Children {
		children[i] = child.String()
	}
	return fmt.Sprintf("ExpandedStatement{Stmt: %v, Children: [%v]}", nodeToString(es.Stmt, es.fset), strings.Join(children, ", "))
}

// Helper struct for Marshaling and Unmarshaling JSON
type expandedStatementJSON struct {
	Stmt     string               `json:"stmt"`
	Children []*ExpandedStatement `json:"children,omitempty"`
	// fset is not marshaled
}

// Marshal a TestCase for JSON output
func (es *ExpandedStatement) MarshalJSON() ([]byte, error) {
	return json.Marshal(expandedStatementJSON{
		Stmt:     nodeToString(es.Stmt, es.fset),
		Children: es.Children,
	})
}

// Unmarshal a TestCase from JSON
func (es *ExpandedStatement) UnmarshalJSON(data []byte) error {
	var jsonData expandedStatementJSON
	if err := json.Unmarshal(data, &jsonData); err != nil {
		return err
	}

	var recoveredStmt ast.Stmt
	expr, err := stringToNode(jsonData.Stmt)
	if err != nil {
		slog.Error("Failed to parse ExpandedStatement from JSON", "error", err)
	} else {
		// Only check the type if the string was parsed successfully
		if stmt, ok := expr.(ast.Stmt); ok {
			recoveredStmt = stmt
		} else {
			slog.Error("Failed to parse ExpandedStatement from JSON because it is not a valid statement", "string", jsonData.Stmt)
		}
	}

	*es = ExpandedStatement{
		Stmt:     recoveredStmt,
		Children: jsonData.Children,
	}
	return nil
}
