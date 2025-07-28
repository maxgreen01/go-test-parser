package testcase

// Implementations of various test case refactoring strategies based on their analysis results.

import (
	"fmt"
	"go/ast"
	"log/slog"
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
		if ar.IsTableDriven() && !ar.ScenarioSet.UsesSubtest {
			refactored, err := ar.refactorToSubtests()
			if err != nil {
				slog.Error("Error while refactoring test case to use subtests", "test", tc, "err", err)
				ar.RefactorResult = RefactorResult{Strategy: strategy, Status: RefactorStatusFail}
				return ar.RefactorResult
			}
			slog.Debug("Successfully refactored test case to use subtests", "testName", tc.TestName)
			ar.RefactorResult = RefactorResult{Strategy: strategy, Status: RefactorStatusSuccess, Result: refactored}
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
// These may assume that the AnalysisResult has already been populated with the necessary data.
//

// Refactors the test case to use subtests, wrapping the execution loop body in a call to `t.Run()`.
func (ar *AnalysisResult) refactorToSubtests() (*ast.FuncDecl, error) {
	ss := ar.ScenarioSet
	if ss == nil {
		return nil, fmt.Errorf("cannot refactor a test case that is not table-driven")
	}

	return nil, nil // FIXME work in progress
}
