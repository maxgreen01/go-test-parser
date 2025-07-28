package testcase

import (
	"encoding/json"
	"go/ast"
	"go/token"
	"log/slog"
	"strings"

	"github.com/maxgreen01/go-test-parser/pkg/asttools"
)

// Represents the result of a refactoring attempt on a test case.
type RefactorResult struct {
	// The refactoring strategy that was applied, if any
	Strategy RefactorStrategy

	// The status of the refactoring attempt
	Status RefactorStatus

	// The contents of the refactored test case, if the refactoring was successful
	Result *ast.FuncDecl
}

//
// =============== Supporting Type Definitions ===============
//

// Represents a refactoring strategy that can be applied to a test case.
// Each value corresponds to a refactoring method with a similar name.
type RefactorStrategy int

const (
	RefactorStrategyNone    RefactorStrategy = iota // No refactoring method specified
	RefactorStrategySubtest                         // Wrap the entire contents of the execution loop in a call to `t.Run()`
)

// Return the RefactorStrategy corresponding to the given string.
func RefactorStrategyFromString(method string) RefactorStrategy {
	switch strings.ToLower(method) {
	case "subtest":
		return RefactorStrategySubtest
	case "none":
		return RefactorStrategyNone
	default:
		slog.Warn("Unknown refactoring strategy", "strategy", method)
		return RefactorStrategyNone
	}
}

func (rm RefactorStrategy) String() string {
	switch rm {
	case RefactorStrategySubtest:
		return "subtest"
	default:
		return "none"
	}
}

func (rm RefactorStrategy) MarshalJSON() ([]byte, error) {
	return json.Marshal(rm.String())
}

func (rm *RefactorStrategy) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	*rm = RefactorStrategyFromString(str)
	return nil
}

// Represents the status of a refactoring attempt on a test case.
type RefactorStatus int

const (
	RefactorStatusNone    RefactorStatus = iota // No refactoring was attempted
	RefactorStatusFail                          // Refactoring was attempted but failed
	RefactorStatusSuccess                       // Refactoring was successful
)

func (rs RefactorStatus) String() string {
	switch rs {
	case RefactorStatusFail:
		return "fail"
	case RefactorStatusSuccess:
		return "success"
	default:
		return "none"
	}
}

func (rs RefactorStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(rs.String())
}

func (rs *RefactorStatus) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	switch strings.ToLower(str) {
	case "fail":
		*rs = RefactorStatusFail
	case "success":
		*rs = RefactorStatusSuccess
	case "none":
		*rs = RefactorStatusNone
	default:
		slog.Warn("Unknown refactoring strategy", "strategy", str)
		*rs = RefactorStatusNone
	}
	return nil
}

//
// ========== Output Methods ==========
//

// Helper struct for Marshaling and Unmarshaling JSON.
type refactorResultJSON struct {
	Strategy RefactorStrategy `json:"strategy"`
	Status   RefactorStatus   `json:"status"`
	Result   string           `json:"result"`
}

// Convert the RefactorResult to a JSON representation.
func (rr RefactorResult) ToJSON(fset *token.FileSet) refactorResultJSON {
	return refactorResultJSON{
		Strategy: rr.Strategy,
		Status:   rr.Status,
		Result:   asttools.NodeToString(rr.Result, fset),
	}
}

// Convert the JSON representation of a RefactorResult back to its original form.
// FIXME FIGURE OUT HOW TO DECODE RefactorResult!
// func (rr *refactorResultJSON) FromJSON(data []byte, fset *token.FileSet) error {
// 	var jsonData refactorResultJSON
// 	if err := json.Unmarshal(data, &jsonData); err != nil {
// 		return err
// 	}

// 	// Try to decode AST fields
// 	var funcDecl *ast.FuncDecl
// 	expr, err := asttools.StringToNode(jsonData.Result)
// 	if err != nil {
// 		return fmt.Errorf("parsing RefactorResult result function from JSON: %w", err)
// 	} else {
// 		// Only check the type if the string was parsed successfully
// 		if decl, ok := expr.(*ast.FuncDecl); ok {
// 			funcDecl = decl
// 		} else {
// 			return fmt.Errorf("RefactorResult result function is not a valid function declaration: %w", jsonData.Result)
// 		}
// 	}

// 	// Save data into the main struct
// 	*rr = RefactorResult{
// 		Strategy: jsonData.Strategy,
// 		Status:   jsonData.Status,
// 		Result:   funcDecl,
// 	}
// 	return nil
// }
