package testcase

import (
	"go/ast"
	"go/token"
	"go/types"
	"log/slog"
	"slices"
	"strings"
)

// Populates the TestCase's ScenarioSet field using the information extracted from from the test's statements
func (tc *TestCase) IdentifyScenarioSet(stmts []ast.Stmt) {
	if len(stmts) == 0 {
		slog.Warn("Cannot identify ScenarioSet in TestCase because there are no statements", "testCase", tc.Name)
		return
	}

	// Reset the TestCase's ScenarioSet (which should be empty anyway), which will be populated over time with the relevant data
	tc.ScenarioSet = &ScenarioSet{}
	ss := tc.ScenarioSet

	fset, _ := tc.Fset() //todo find a better way
	ss.fset = &fset

	// Iterate in reverse to find the runner loop before trying to find the scenarios
	stmtsReversed := slices.Clone(stmts)
	slices.Reverse(stmtsReversed)
stmtLoop:
	for _, stmt := range stmtsReversed {
		// Extract the loop that runs the subtests
		if ss.Runner == nil {
			// Detect the loop itself
			if rangeStmt, ok := stmt.(*ast.RangeStmt); ok {
				slog.Debug("Found range statement in test case", "test", tc.Name)

				// Make sure the loop ranges over a valid data structure, and save it if so
				ss.detectScenarioDataStructure(tc.TypeOf(rangeStmt.X))

				if ss.DataStructure == ScenarioNoDS {
					// Can't do anything if the loop data structure is unknown
					slog.Warn("Detected a range loop in test case, but the data structure is unknown", "test", tc.Name, "path", tc.FilePath)
					continue stmtLoop // Try checking for additional loops
				}

				// Check if the scenario data structure is defined directly in the range statement
				if _, ok := rangeStmt.X.(*ast.CompositeLit); ok {
					scenariosDefinedInLoop := ss.SaveScenariosIfMatching(rangeStmt.X, tc)
					if scenariosDefinedInLoop {
						slog.Debug("Found scenario definition directly in the range statement", "test", tc.Name, "count", len(ss.Scenarios), "path", tc.FilePath)
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
						slog.Debug("Found scenario definition in function body", "test", tc.Name, "count", len(ss.Scenarios), "path", tc.FilePath)
						continue stmtLoop // Move to the next statement
					}
				}
			}
		}
	} // end of loop over function statements

	// If the loop was found but the Scenario definitions were not, check the file declarations in case they were defined outside the function
	if ss.Scenarios == nil && ss.ScenarioTemplate != nil {
		slog.Warn("No scenarios found in the test case, checking file declarations", "test", tc.Name, "path", tc.FilePath)
	declLoop:
		for _, decl := range tc.File.Decls {
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
								slog.Debug("Found scenario definition in file declarations", "test", tc.Name, "count", len(ss.Scenarios), "path", tc.FilePath)
								break declLoop // Stop checking file declarations
							}
						}
					}
				}
			}
		} // end of loop over file declarations
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

// Extract relevant information about this TestCase and save the results into its own corresponding fields
func (tc *TestCase) Analyze() {
	slog.Debug("Analyzing test case", "testCase", tc.Name, "filePath", tc.FilePath)
	if tc.FuncDecl == nil || tc.File == nil {
		slog.Warn("Cannot analyze TestCase because FuncDecl or File is nil", "testCase", tc.Name)
		return
	}

	// Analyze the individual statements in the test case
	stmts := tc.GetStatements()

	tc.ParsedStatements = make([]string, len(stmts))
	if fset, ok := tc.Fset(); ok {
		for i, stmt := range stmts {
			// Stringify the entire statement
			tc.ParsedStatements[i] = nodeToString(stmt, fset)
			// todo do more work here to classify statements?
		}
	}

	// Populate table-driven test data
	tc.IdentifyScenarioSet(stmts)

	// Extract imported packages from the file's AST
	var imports []*ast.ImportSpec
	if tc.File != nil {
		imports = tc.File.Imports
	} else {
		slog.Warn("Cannot extract imported packages in test case because File is nil", "testCase", tc.Name)
	}
	for _, imp := range imports {
		tc.ImportedPackages = append(tc.ImportedPackages, strings.Trim(imp.Path.Value, "\""))
	}
}
