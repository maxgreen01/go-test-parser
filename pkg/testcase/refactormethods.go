package testcase

// Implementations of various test case refactoring strategies based on their analysis results.

import (
	"fmt"
	"go/ast"
	"go/types"
	"log/slog"

	"github.com/maxgreen01/go-test-parser/pkg/asttools"
)

// Attempts to refactor a test case using the specified strategy.
// Saves the result of the refactoring attempt to the AnalysisResult, and also returns a copy of the result.
func (ar *AnalysisResult) AttemptRefactoring(strategy RefactorStrategy) RefactorResult {
	if ar == nil {
		slog.Error("Attempted to refactor a nil AnalysisResult", "strategy", strategy)
		return RefactorResult{Strategy: strategy, Status: RefactorStatusFail}
	}

	tc := ar.TestCase
	if tc == nil {
		slog.Error("Attempted to refactor a nil TestCase", "strategy", strategy)
		ar.RefactorResult = RefactorResult{Strategy: strategy, Status: RefactorStatusFail}
		return ar.RefactorResult
	}

	// Determine which refactoring method to apply
	switch strategy {
	case RefactorStrategyNone:
		// Nothing to do
		ar.RefactorResult = RefactorResult{}
		return ar.RefactorResult

	case RefactorStrategySubtest:
		if ar.ScenarioSet == nil {
			// Not a candidate for refactoring
			ar.RefactorResult = RefactorResult{Strategy: strategy, Status: RefactorStatusNone}
			return ar.RefactorResult
		}
		// Only refactor if the test case is table-driven and does not already use subtests
		if ar.IsTableDriven() && !ar.ScenarioSet.UsesSubtest {
			refactored, status, err := ar.refactorToSubtests()
			if err != nil {
				slog.Error("Error while refactoring test case to use subtests", "err", err, "test", tc)
				ar.RefactorResult = RefactorResult{Strategy: strategy, Status: RefactorStatusFail}
				return ar.RefactorResult
			}
			slog.Debug("Successfully refactored test case to use subtests", "test", tc)
			ar.RefactorResult = RefactorResult{Strategy: strategy, Status: status, Result: refactored}
			return ar.RefactorResult
		}

	default:
		slog.Warn("Unknown refactoring strategy", "strategy", strategy)
	}
	// Didn't refactor for some non-error reason
	ar.RefactorResult = RefactorResult{Strategy: strategy, Status: RefactorStatusNone}
	return ar.RefactorResult
}

//
// ========== Refactoring Methods ==========
// These may assume that the AnalysisResult has already been populated with the necessary data via `Analyze()`.
//

// Refactors the test case to use subtests by wrapping the execution loop body in a call to `t.Run()`.
// Returns the updated test function declaration, the status of the refactoring attempt, and any error that occurred.
func (ar *AnalysisResult) refactorToSubtests() (*ast.FuncDecl, RefactorStatus, error) {
	tc := ar.TestCase
	if tc == nil || tc.funcDecl == nil {
		return nil, RefactorStatusError, fmt.Errorf("cannot refactor a test case that has no function declaration")
	}
	ss := ar.ScenarioSet
	if ss == nil {
		return nil, RefactorStatusError, fmt.Errorf("cannot refactor a test case that is not table-driven")
	}

	// Detect the variable names used by the loop (e.g. the name for scenarios within the loop)
	var loopKeyName string
	var loopValueName string
	switch loop := ss.Runner.(type) {
	case *ast.RangeStmt:
		if loop.Key == nil || loop.Value == nil {
			slog.Warn("Cannot refactor test case with range loop that has a nil key or value variable", "key", loop.Key, "value", loop.Value, "testName", tc.TestName)
			return nil, RefactorStatusFail, nil
		}
		loopKeyName = loop.Key.(*ast.Ident).Name
		loopValueName = loop.Value.(*ast.Ident).Name

	// todo LATER add support for `for-i` loops	(and modify assignment at end of func)
	default:
		slog.Warn("Cannot refactor test case with unsupported loop type", "type", fmt.Sprintf("%T", ss.Runner), "testName", tc.TestName)
		return nil, RefactorStatusFail, nil
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
		slog.Warn("Cannot refactor test case because no valid scenario name field was detected", "testName", tc.TestName)
		return nil, RefactorStatusBadFields, nil
	}

	// Detect the name of the variable representing each scenario in the loop
	scenarioVarName := loopValueName // e.g. `tt` in `for _, tt := range scenarios`

	// Special case where map key is used -- range over the actual key
	if nameField == "map key" {
		// If the key is ignored, use a default name
		if loopKeyName == "_" {
			loopKeyName = "testName"
		}
		scenarioVarName = loopKeyName
	}

	// Construct the actual `t.Run()` call statement
	// todo LATER maybe detect the name of the original `testing.T` param and use it instead of hardcoding "t"
	tRunCall := &ast.ExprStmt{
		X: &ast.CallExpr{
			Fun: asttools.NewSelectorExpr("t", "Run"),
			Args: []ast.Expr{
				// Scenario name, like `tt.Name`
				asttools.NewSelectorExpr(scenarioVarName, nameField),

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

	// Update the runner loop to use the refactored version
	switch loop := ss.Runner.(type) {
	case *ast.RangeStmt:
		loop.Body.List = []ast.Stmt{tRunCall}
		// If the range key identifier changed, update that too
		if loopKeyName != loop.Key.(*ast.Ident).Name {
			loop.Key = ast.NewIdent(loopKeyName)
		}

		// unsupported loop types are handled above
	}

	return tc.funcDecl, RefactorStatusSuccess, nil
}
