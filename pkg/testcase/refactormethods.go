package testcase

// Implementations of various test case refactoring strategies based on their analysis results.

import (
	"fmt"
	"go/ast"
	"go/types"
	"log/slog"
	"os"

	"github.com/go-toolsmith/astcopy"
	"github.com/maxgreen01/go-test-parser/pkg/asttools"
)

// Attempts to refactor a test case using the specified strategy.
// Saves the result of the refactoring attempt to the AnalysisResult, and also returns a copy of the result.
func (ar *AnalysisResult) AttemptRefactoring(strategy RefactorStrategy) RefactorResult {
	if ar == nil {
		slog.Error("Attempted to refactor a nil AnalysisResult", "strategy", strategy)
		return RefactorResult{Strategy: strategy, GenerationStatus: RefactorGenerationStatusFail}
	}

	// Create the RefactorResult return object and store it in the AnalysisResult
	ar.RefactorResult = RefactorResult{Strategy: strategy}
	rr := &ar.RefactorResult

	if strategy == RefactorStrategyNone {
		// Nothing to do
		return *rr
	}

	tc := ar.TestCase
	if tc == nil {
		slog.Error("Attempted to refactor a nil TestCase", "strategy", strategy)
		rr.GenerationStatus = RefactorGenerationStatusFail
		return *rr
	}
	fset := tc.FileSet()
	if fset == nil {
		slog.Error("Cannot refactor TestCase because FileSet is nil", "testCase", tc)
		rr.GenerationStatus = RefactorGenerationStatusFail
		return *rr
	}

	// Determine which refactoring strategy to apply
	switch strategy {
	case RefactorStrategySubtest:
		// Only refactor if the test case is table-driven and does not already use subtests
		if ar.ScenarioSet == nil || !ar.IsTableDriven() || ar.ScenarioSet.UsesSubtest {
			// Not a candidate for refactoring
			return *rr
		}

		// Perform the actual refactoring
		refactored, status, err := ar.refactorToSubtests()
		if err != nil {
			slog.Error("Error refactoring test case to use subtests", "err", err, "test", tc)
			rr.GenerationStatus = RefactorGenerationStatusFail
			return *rr
		}
		rr.GenerationStatus = status
		rr.Refactorings = refactored
		// Only move on to execute the test if the refactor generation step was actually successful
		if status != RefactorGenerationStatusSuccess {
			slog.Info("Issue performing subtest refactoring for test case", "status", status, "test", tc)
			return *rr
		}

	default:
		slog.Warn("Unknown refactoring strategy", "strategy", strategy)
		return *rr
	}

	//
	// If we've reached this point, the refactoring was successful and should be verified by executing the test
	//
	slog.Info("Successfully generated a refactoring for test case", "test", tc)

	// Execute the test case before saving the refactoring.
	// This is run only after refactoring succeeds to avoid running tests unnecessarily (which is quite slow).
	originalExecResult, err := tc.Execute()
	if err != nil {
		if originalExecResult == TestExecutionResultFail {
			slog.Info("Test case execution failed normally before refactoring", "err", err, "test", tc)
		} else {
			slog.Error("Error executing test case before refactoring", "err", err, "test", tc)
		}
	}
	rr.OriginalExecutionResult = originalExecResult

	// Save the original contents of every affected file for later restoration, then update
	// all the files on the disk using the new refactored AST data
	originalFileContents := make(map[string][]byte)
	for _, refactoring := range rr.Refactorings {
		filePath := refactoring.FilePath
		if _, ok := originalFileContents[filePath]; ok {
			// Already processed this file
			continue
		}

		// Read the entire original file contents so it can be restored after the refactoring is complete
		// todo CLEANUP - this reads the entire file into memory, which isn't ideal if multiple files need to be modified.
		//    This probably isn't a problem when files are a few MB at most, but a backup manager would be better.
		fileContents, err := os.ReadFile(filePath)
		if err != nil {
			slog.Error("Error reading original file contents", "err", err, "filePath", filePath, "test", tc)
			return *rr
		}
		originalFileContents[filePath] = fileContents

		// Update the file with the new AST data
		if err := asttools.SaveFileContents(filePath, refactoring.File, fset); err != nil {
			slog.Error("Error saving refactored file", "err", err, "filePath", filePath, "test", tc)
			return *rr
		}
	}

	// Run the test after refactoring
	refactoredExecResult, err := tc.Execute()
	if err != nil {
		if refactoredExecResult == TestExecutionResultFail {
			slog.Info("Test case execution failed normally after refactoring", "err", err, "test", tc)
		} else {
			slog.Error("Error executing test case after refactoring", "err", err, "test", tc)
		}
	}
	rr.RefactoredExecutionResult = refactoredExecResult

	// Restore the original file contents on the disk to ensure that refactorings don't interfere with each other
	for _, refactoring := range rr.Refactorings {
		// Write the original file contents back to the disk
		if err := os.WriteFile(refactoring.FilePath, originalFileContents[refactoring.FilePath], 0644); err != nil {
			slog.Error("Error restoring original test file contents after refactoring", "err", err, "test", tc)
			return *rr
		}

		// Restore the original AST File data (and any dependents) to ensure that refactorings don't interfere with each other
		refactoring.Cleanup()
	}

	return *rr
}

//
// ========== Refactoring Methods ==========
//
// These may assume that the AnalysisResult has already been populated with the necessary data via `Analyze()`.
// Refactorings of helper functions are performed on *copies* of the original AST nodes to ensure that other
// analysis results are not affected if the helper is used by any other tests. The cleanup of these copy changes
// is handled by AttemptRefactoring so that they can be saved  Note that type information from
// `go/types` is NOT available for these copies since the underlying pointer values are different than the originals.
// TODO LATER - this AST copying behavior is only present when expanding helper statements, not necessarily when finding definitions or using the type system
//

// Refactors the test case to use subtests by wrapping the execution loop body in a call to `t.Run()`.
// Returns a one-element list containing the updated function if successful, as well as the status of
// the refactor generation attempt and any error that may have occurred.
func (ar *AnalysisResult) refactorToSubtests() ([]RefactoredFunction, RefactorGenerationStatus, error) {
	tc := ar.TestCase
	if tc == nil || tc.funcDecl == nil {
		return nil, RefactorGenerationStatusError, fmt.Errorf("cannot refactor a test case that has no function declaration")
	}
	ss := ar.ScenarioSet
	if ss == nil {
		return nil, RefactorGenerationStatusError, fmt.Errorf("cannot refactor a test case that is not table-driven")
	}

	// If the modified nodes are in a helper function, perform the refactoring on a copy to avoid modifying the original AST.
	// This creates the RefactoredFunction that will eventually be returned if the statement is part of a helper, because
	// the AST data it contains will be modified in-place during refactoring.
	result := cloneHelperFunction(ss.Runner, ar)

	// Detect the key/value variable names used by the loop (used to work with scenarios within the loop)
	var loopKeyName string
	var loopValueName string
	switch loop := ss.Runner.(type) {
	case *ast.RangeStmt:
		if loop.Key == nil || loop.Value == nil {
			slog.Warn("Cannot refactor test case with range loop with nil key or value variable", "key", loop.Key, "value", loop.Value, "test", tc)
			return nil, RefactorGenerationStatusFail, nil
		}
		loopKeyName = loop.Key.(*ast.Ident).Name
		loopValueName = loop.Value.(*ast.Ident).Name

	// todo LATER add support for `for-i` loops	(and modify assignment at end of func)
	default:
		slog.Warn("Cannot refactor test case with unsupported loop type", "type", fmt.Sprintf("%T", ss.Runner), "test", tc)
		return nil, RefactorGenerationStatusFail, nil
	}

	// Use the detected scenario name field, or use the first string-typed struct field if one is not detected
	nameField := ss.NameField
	if nameField == "" {
		for field := range ss.GetFields() {
			if asttools.IsBasicType(field.Type(), types.IsString) {
				nameField = field.Name()
				break
			}
		}
	}
	if nameField == "" {
		slog.Debug("Cannot refactor test case because no valid scenario name field was detected", "test", tc)
		return nil, RefactorGenerationStatusBadFields, nil
	}

	// Create an expression representing the the scenario name, e.g. `tt.Name``
	var scenarioNameExpr ast.Expr
	if nameField == "map key" {
		// Special case where map key is used -- name is the loop key

		// If the key is ignored, use a default name instead of the detected loop key
		if loopKeyName == "_" {
			loopKeyName = "test"
		}

		scenarioNameExpr = ast.NewIdent(loopKeyName)
	} else {
		// Regular case -- name is a scenario field

		// Detect the name of the variable representing each scenario in the loop
		scenarioVarName := loopValueName // e.g. `tt` in `for _, tt := range scenarios`

		scenarioNameExpr = asttools.NewSelectorExpr(scenarioVarName, nameField)
	}

	// Construct the actual `t.Run()` call statement
	// todo LATER maybe detect the name of the original `testing.T` param and use it instead of hardcoding "t"
	tRunCall := &ast.ExprStmt{
		X: &ast.CallExpr{
			Fun: asttools.NewSelectorExpr("t", "Run"),
			Args: []ast.Expr{
				// Scenario name, like `tt.Name`
				scenarioNameExpr,

				// Function literal for the test body, of form `func(t *testing.T) { ... }`
				&ast.FuncLit{
					Type: &ast.FuncType{
						// The `*testing.T` parameter
						Params: &ast.FieldList{
							List: []*ast.Field{
								{
									Names: []*ast.Ident{
										ast.NewIdent("t"),
									},
									Type: &ast.StarExpr{
										X: asttools.NewSelectorExpr("testing", "T"),
									},
								},
							},
						},
					},
					// The function body, populated with the original loop body statements
					Body: &ast.BlockStmt{
						List: ss.GetRunnerStatements(),
					},
				},
			},
		},
	} // end of constructing `t.Run()` call

	// Apply the refactoring changes to the underlying AST now that the refactoring logic is complete
	switch loop := ss.Runner.(type) {
	case *ast.RangeStmt:
		loop.Body.List = []ast.Stmt{tRunCall}
		// If the range key identifier changed, update that too
		if loopKeyName != loop.Key.(*ast.Ident).Name {
			loop.Key.(*ast.Ident).Name = loopKeyName
		}

		// unsupported loop types are handled above
	}

	// If `result` is non-nil, the statement was part of a helper function and the refactored data should already be
	// contained within this struct. However, the string representation of the refactored function needs to be updated.
	if result != nil {
		result.UpdateStringRepresentation(tc.FileSet())
		return []RefactoredFunction{*result}, RefactorGenerationStatusSuccess, nil
	}

	// Either the statement is not part of a helper function (or an error occurred while checking for that),
	// so we assume that the refactoring happened inside the original test function and doesn't need any cleanup.
	return []RefactoredFunction{*NewRefactoredFunction(tc.funcDecl, tc.file, nil, tc.FileSet())}, RefactorGenerationStatusSuccess, nil
}

//
// ========== Helper Functions ==========
//

// If the provided statement is part of a helper function (i.e. not the test case function itself), this replaces
// the surrounding helper function with a deep copy of itself in the included TestCase's AST file. It also updates
// the AST references in the included ScenarioSet to match the new data. This returns a representation of the
// refactored function, where the Refactored field is the unmodified copy of the original function declaration.
//
// If the statement is not part of a helper function or is not part of the package, this does nothing.
func cloneHelperFunction(stmt ast.Stmt, ar *AnalysisResult) *RefactoredFunction {
	// Assumed to be non-nil by this point
	tc := ar.TestCase
	ss := ar.ScenarioSet

	originalFunc, enclosingFile := asttools.GetEnclosingFunction(stmt.Pos(), tc.GetPackageFiles())
	if originalFunc == nil || enclosingFile == nil {
		slog.Warn("Tried processing a statement that is not part of a function in the package", "statement", stmt, "test", tc)
		return nil
	}
	fset := tc.FileSet()
	if fset == nil {
		slog.Warn("Cannot determine path to file enclosing a helper function because FileSet is nil", "function", originalFunc.Name.Name, "test", tc)
		return nil
	}

	if originalFunc.Name.Name == tc.funcDecl.Name.Name && enclosingFile.Name.Name == tc.PackageName {
		// Statement is part of the test case function itself, so no need to clone it
		slog.Debug("Statement is part of the test case function itself", "statement", stmt, "function", originalFunc.Name.Name, "test", tc)
		return nil
	}
	slog.Debug("Statement is part of a helper function", "statement", stmt, "function", originalFunc.Name.Name, "test", tc)

	// Create a deep copy of the enclosing function to avoid modifying the original AST
	copiedFunc := astcopy.FuncDecl(originalFunc)

	// Replace the original function with the copy
	if err := asttools.ReplaceFuncDecl(originalFunc, copiedFunc, enclosingFile); err != nil {
		slog.Error("Failed to replace function declaration with its copy", "err", err, "test", tc)
		return nil
	}
	// Create a closure to restore the original function declaration within the file
	restoreFuncDecl := func() error {
		if err := asttools.ReplaceFuncDecl(copiedFunc, originalFunc, enclosingFile); err != nil {
			return fmt.Errorf("restoring original function declaration: %w", err)
		}
		return nil
	}

	// Now that the copied data is in place, update the AST references in the ScenarioSet too
	originalRunner := ss.Runner // Save a copy of the original reference so it can be restored later
	copiedRunner, err := asttools.GetStmtWithSameIndex(ss.Runner, originalFunc.Body.List, copiedFunc.Body.List)
	if err != nil {
		slog.Error("Failed to update ScenarioSet runner statement reference", "err", err, "function", originalFunc.Name.Name, "test", tc)
		// If something went wrong, restore the original function declaration within the file
		if err := restoreFuncDecl(); err != nil {
			slog.Error("Failed to restore original function declaration", "err", err)
		}
		return nil
	}
	ss.Runner = copiedRunner

	// Create a closure to restore the original function declaration and all AST ScenarioSet references once all refactoring is done
	cleanupFunc := func() error {
		if err := restoreFuncDecl(); err != nil {
			return err
		}
		ss.Runner = originalRunner
		return nil
	}

	return NewRefactoredFunction(copiedFunc, enclosingFile, cleanupFunc, fset)
}
