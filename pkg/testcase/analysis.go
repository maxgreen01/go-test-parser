package testcase

import (
	"go/ast"
	"log/slog"
	"strings"
)

// Extract relevant information about this TestCase and save the results into its own corresponding fields
func Analyze(t *TestCase) {
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
