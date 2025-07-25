package testcase

import (
	"go/ast"
	"go/token"
	"go/types"
	"log/slog"
	"slices"
	"strings"
)

// Extracts relevant information about this TestCase and save the results into its own corresponding fields
func (tc *TestCase) Analyze() {
	slog.Debug("Analyzing test case", "testCase", tc.testName, "filePath", tc.filePath)
	if tc.FuncDecl == nil || tc.testContext == nil || tc.file == nil {
		slog.Error("Cannot analyze TestCase because it has nil syntax data", "testCase", tc.testName, "package", tc.packageName)
		return
	}

	fset := tc.FileSet()
	if fset == nil {
		slog.Error("Cannot analyze TestCase because FileSet is nil", "testCase", tc.testName, "package", tc.packageName)
		return
	}

	// Analyze the individual statements in the test case
	stmts := tc.GetStatements()

	tc.ParsedStatements = make([]*ExpandedStatement, len(stmts))
	for i, stmt := range stmts {
		// Try to expand the statement if it's a call to a testing helper function
		tc.ParsedStatements[i] = ExpandStatement(stmt, tc.testContext, true)
	}

	// Populate table-driven test data
	tc.IdentifyScenarioSet(stmts)

	// Extract imported packages from the file's AST
	var imports []*ast.ImportSpec
	if tc.file != nil {
		imports = tc.file.Imports
		for _, imp := range imports {
			tc.ImportedPackages = append(tc.ImportedPackages, strings.Trim(imp.Path.Value, "\""))
		}
	} else {
		slog.Error("Cannot extract imported packages in test case because File is nil", "testCase", tc.testName, "package", tc.packageName)
	}
}

// Populates the TestCase's ScenarioSet field using the information extracted from from the test's statements
func (tc *TestCase) IdentifyScenarioSet(stmts []ast.Stmt) {
	if len(stmts) == 0 {
		slog.Warn("Cannot identify ScenarioSet in TestCase because there are no statements", "testCase", tc.testName, "package", tc.packageName, "path", tc.filePath)
		return
	}

	// Initialize the TestCase's ScenarioSet, whose fields will be populated throughout this method with relevant data
	tc.ScenarioSet = &ScenarioSet{testContext: tc.testContext}
	ss := tc.ScenarioSet

	// Iterate in reverse to find the runner loop before trying to find the scenarios
	stmtsReversed := slices.Clone(stmts)
	slices.Reverse(stmtsReversed)
stmtLoop:
	for _, stmt := range stmtsReversed {
		// Extract the loop that runs the subtests
		if ss.Runner == nil {
			// Detect the loop itself
			if rangeStmt, ok := stmt.(*ast.RangeStmt); ok {
				slog.Debug("Found range statement in test case", "testCase", tc.testName)

				// Make sure the loop ranges over a valid data structure, and save it if so
				ss.detectScenarioDataStructure(tc.TypeOf(rangeStmt.X))

				if ss.DataStructure == ScenarioNoDS {
					// Can't do anything if the loop data structure is unknown
					slog.Debug("Detected a range loop in test case, but the data structure is unknown", "testCase", tc.testName, "package", tc.packageName, "path", tc.filePath)
					continue stmtLoop // Try checking for additional loops
				}

				// Check if the scenario data structure is defined directly in the range statement
				if _, ok := rangeStmt.X.(*ast.CompositeLit); ok {
					scenariosDefinedInLoop := ss.SaveScenariosIfMatching(rangeStmt.X, tc)
					if scenariosDefinedInLoop {
						slog.Debug("Found scenario definition directly in the range statement", "testCase", tc.testName, "count", len(ss.Scenarios), "package", tc.packageName, "path", tc.filePath)
					}
				}

				ss.Runner = rangeStmt.Body

				continue stmtLoop // Move to the next statement
			}

			// todo LATER add support for `for-i` loops
			//  else if forStmt, ok := stmt.(*ast.ForStmt); ok {
			// 	slog.Debug("Found loop statement in test case", "test", t.Name)
			// 	t.TableDrivenType += ", with for loop"
			// 	detectTRun(forStmt.Body)
			// }
		}

		// Search for variable assignments matching the detected scenario data structure, with the goal of finding the scenario definitions
		if ss.Scenarios == nil && ss.ScenarioTemplate != nil {
			if assignStmt, ok := stmt.(*ast.AssignStmt); ok {
				for _, expr := range assignStmt.Rhs {
					found := ss.SaveScenariosIfMatching(expr, tc)
					if found {
						slog.Debug("Found scenario definition in function body", "testCase", tc.testName, "count", len(ss.Scenarios), "path", tc.filePath)
						continue stmtLoop // Move to the next statement
					}
				}
			}
		}
	} // end of loop over function statements

	// If the loop was found but the Scenario definitions were not, check the file declarations in case they were defined outside the function
	if ss.Scenarios == nil && ss.ScenarioTemplate != nil {
		slog.Debug("No scenarios found in the test case, checking file declarations", "testCase", tc.testName, "path", tc.filePath)

		if tc.file == nil {
			slog.Error("Cannot check file declarations because File is nil", "testCase", tc.testName, "package", tc.packageName)
		} else {
		declLoop:
			for _, decl := range tc.file.Decls {
				if genDecl, ok := decl.(*ast.GenDecl); ok {
					if genDecl.Tok != token.VAR {
						continue declLoop // Only check variable declarations
					}
					// Loop over the right-hand side expressions of each variable declaration
					for _, spec := range genDecl.Specs {
						if valueSpec, ok := spec.(*ast.ValueSpec); ok {
							for _, expr := range valueSpec.Values {
								found := ss.SaveScenariosIfMatching(expr, tc)
								if found {
									slog.Debug("Found scenario definition in file declarations", "testCase", tc.testName, "count", len(ss.Scenarios), "path", tc.filePath)
									break declLoop // Stop checking file declarations
								}
							}
						}
					}
				}
			} // end of loop over file declarations
		}
	}

	// Attempt to perform additional analysis on the ScenarioSet
	ss.Analyze()
}

// Checks whether an expression has the same underlying type as the ScenarioTemplate, and if so, saves the scenarios from the expression.
// Returns whether the scenarios were saved successfully. Always returns `false` if the `ScenarioSet.DataStructure` is unknown.
// See https://go.dev/ref/spec#Type_identity for details of the `types.Identical` comparison method.
func (ss *ScenarioSet) SaveScenariosIfMatching(expr ast.Expr, tc *TestCase) bool {
	// Both []struct and map are defined using a CompositeLit, so make sure this matches
	if compositeLit, ok := expr.(*ast.CompositeLit); ok {
		if len(compositeLit.Elts) == 0 {
			return false
		}

		// Depending on the scenario data structure, extract and save the scenarios themselves
		// todo LATER construct Scenario structs inside the cases.    also might have to make changes here to handle non-struct fields
		switch ss.DataStructure {

		case ScenarioStructListDS:
			// Scenarios are directly stored as the elements of the slice
			typ := tc.TypeOf(compositeLit.Elts[0])
			if typ != nil && types.Identical(typ.Underlying(), ss.ScenarioTemplate) {
				ss.Scenarios = compositeLit.Elts
				return true
			}

		case ScenarioMapDS:
			// Scenarios are stored as the values of the `KeyValueExpr` elements
			kvExpr, ok := compositeLit.Elts[0].(*ast.KeyValueExpr)
			if ok && types.Identical(tc.TypeOf(kvExpr.Value).Underlying(), ss.ScenarioTemplate) {
				for _, elt := range compositeLit.Elts {
					if kvExpr, ok := elt.(*ast.KeyValueExpr); ok {
						ss.Scenarios = append(ss.Scenarios, kvExpr)
					}
				}
				return true
			}
		}
	}
	return false
}

// Detects the type of data structure used to store scenarios in a table-driven test as well as
// the underlying struct type used to define scenarios, then saves both to the `ScenarioSet`.
// Also checks if the key of a map structure is used to define scenario names.
//
// Returns the ScenarioDataStructure type and the underlying struct type used to define scenarios,
// which are already saved to the `ScenarioSet`.
func (ss *ScenarioSet) detectScenarioDataStructure(typ types.Type) (sds ScenarioDataStructure, scenarioType *types.Struct) {
	// Whenever the function returns, save the detected data to the ScenarioSet
	defer func() {
		ss.DataStructure = sds
		ss.ScenarioTemplate = scenarioType
	}()

	if typ == nil {
		sds, scenarioType = ScenarioNoDS, nil
		return
	}

	// Check the underlying type
	// todo CLEANUP maybe refactor for less duplication
	switch x := typ.Underlying().(type) {

	case *types.Slice:
		// Check for []struct
		if structType, ok := x.Elem().Underlying().(*types.Struct); ok {
			sds, scenarioType = ScenarioStructListDS, structType
			return
		}
	case *types.Array:
		// Check for [N]struct
		if structType, ok := x.Elem().Underlying().(*types.Struct); ok {
			sds, scenarioType = ScenarioStructListDS, structType
			return
		}

	case *types.Map:
		// Check for map[any]struct
		sds = ScenarioMapDS
		if structType, ok := x.Elem().Underlying().(*types.Struct); ok {
			scenarioType = structType
		}

		// todo LATER this would be the place to handle maps with non-struct values, like map[string]bool

		// If the map key is a string, assume it's the scenario name
		if keyType, ok := x.Key().Underlying().(*types.Basic); ok {
			if keyType.Kind() == types.String {
				ss.NameField = "map key"
			}
		}

		return
	}

	// Default or unknown case if other logic doesn't match
	sds, scenarioType = ScenarioNoDS, nil
	return
}
