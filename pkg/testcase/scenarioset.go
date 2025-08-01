package testcase

import (
	"encoding/json"
	"go/ast"
	"go/types"
	"iter"
	"strings"

	"github.com/maxgreen01/go-test-parser/pkg/asttools"
)

// Represents the properties of a table-driven test by storing information about the scenarios and their structure,
// as well as various analysis results derived from this information.
type ScenarioSet struct {
	// Reference to the TestCase this ScenarioSet belongs to
	TestCase *TestCase

	// Core data fields
	// todo LATER expand to support scenario definitions like `map[string]bool` without a struct template (probably by making changes to `DetectScenarioDataStructure`)
	ScenarioTemplate *types.Struct // the definition of the `struct` type that individual scenarios are based on

	DataStructure ScenarioDataStructure // describes the type of data structure used to store scenarios
	Scenarios     []ast.Expr            // the individual scenarios themselves //todo LATER convert to type `[]Scenario`

	Runner ast.Stmt // the actual code that runs the subtest (which is expected to be either a `ForStmt` or a `RangeStmt`)

	// Derived analysis results
	NameField         string   // the name of the field representing each scenario's name, or "map key" if the map key is used as the name
	ExpectedFields    []string // the names of fields representing the expected results of each scenario
	HasFunctionFields bool     // whether the scenario type has any fields whose type is a function
	UsesSubtest       bool     // whether the test calls `t.Run()` inside the loop body
}

//
// =============== Supporting Type Definitions ===============
//

// Represents the type of data structure used to store scenarios
type ScenarioDataStructure int

const (
	ScenarioNoDS         ScenarioDataStructure = iota // no table-driven test structure detected
	ScenarioStructListDS                              // table-driven test using a slice or array of structs
	ScenarioMapDS                                     // table-driven test using a map
)

func (sds ScenarioDataStructure) String() string {
	switch sds {
	case ScenarioStructListDS:
		return "structList"
	case ScenarioMapDS:
		return "map"
	default:
		return "none"
	}
}

func (sds ScenarioDataStructure) MarshalJSON() ([]byte, error) {
	return json.Marshal(sds.String())
}

func (sds *ScenarioDataStructure) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	switch str {
	case "structList":
		*sds = ScenarioStructListDS
	case "map":
		*sds = ScenarioMapDS
	default:
		*sds = ScenarioNoDS
	}
	return nil
}

//
// =============== Analysis Methods ===============
//

// Perform additional analysis based on the core data fields, populating the corresponding fields
func (ss *ScenarioSet) Analyze() {
	if ss.ScenarioTemplate == nil {
		return // Nothing to analyze
	}

	ss.NameField = ss.detectNameField()
	ss.ExpectedFields = ss.detectExpectedFields()
	ss.HasFunctionFields = ss.detectFunctionFields()
	ss.UsesSubtest, _ = ss.detectSubtest()

	// todo LATER consider expanding the statements inside the runner loop, just like with TestCase statements
	//     since TestCase already expands all statements, we can probably store a copy of the corresponding statement without recomputing
}

// Returns the name of the field representing the name of each scenario
func (ss *ScenarioSet) detectNameField() string {
	if ss.ScenarioTemplate == nil {
		return "" // Nothing to analyze
	}

	// In the special case for map data structures where the key represents the scenario name,
	// the name field would already be set by `DetectScenarioDataStructure()`
	if ss.DataStructure == ScenarioMapDS && ss.NameField != "" {
		return ss.NameField
	}

	// If the scenario uses subtests, check if the first arg of `t.Run()` is a field of the scenario struct
	if ok, callExpr := ss.detectSubtest(); ok {
		// Get the first argument of the `t.Run()` call
		if len(callExpr.Args) > 0 {
			if selExpr, ok := callExpr.Args[0].(*ast.SelectorExpr); ok {
				// todo CLEANUP replace this with using the type system to check if the owner is the scenario struct, and the name is a field of it

				// Check if the identifier is a field of the scenario struct
				name := selExpr.Sel.Name
				for field := range ss.GetFields() {
					if field.Name() == name {
						return name
					}
				}
			}
		}
		// If the test uses `t.Run()` but the first arg isn't a "valid" field, consider this to not have a name field
		return ""
	}

	// If all other cases fail, match field names by substring search (ensuring the field is a string)
	for field := range ss.GetFields() {
		if !asttools.IsBasicType(field.Type(), types.IsString) {
			// Skip non-string fields
			continue
		}
		lowercase := strings.ToLower(field.Name())
		if strings.Contains(lowercase, "name") || strings.Contains(lowercase, "desc") {
			return field.Name()
		}
	}
	return ""
}

// Returns the names of the fields representing the expected results of each scenario
// todo LATER try expanding this to detect fields that are used in assertions or comparisons
func (ss *ScenarioSet) detectExpectedFields() []string {
	if ss.ScenarioTemplate == nil {
		return nil // Nothing to analyze
	}

	// Save the names of fields containing the string "expect", "want", or "result"
	var expectedFields []string
	for field := range ss.GetFields() {
		lowercase := strings.ToLower(field.Name())
		if strings.Contains(lowercase, "expect") || strings.Contains(lowercase, "want") || strings.Contains(lowercase, "result") {
			expectedFields = append(expectedFields, field.Name())
		}
	}
	return expectedFields
}

// Returns a bool indicating whether the scenario type has any fields whose type is a function
func (ss *ScenarioSet) detectFunctionFields() bool {
	if ss.ScenarioTemplate == nil {
		return false // Nothing to analyze
	}

	for field := range ss.GetFields() {
		if _, ok := field.Type().Underlying().(*types.Signature); ok {
			return true
		}
	}
	return false
}

// Returns a bool indicating whether `t.Run()` is called inside the loop body, as well as a reference to the `t.Run()` statement
func (ss *ScenarioSet) detectSubtest() (bool, *ast.CallExpr) {
	statements := ss.GetRunnerStatements()
	for _, stmt := range statements {
		if ok, callExpr := asttools.IsSelectorFuncCall(stmt, "t", "Run"); ok {
			return true, callExpr
		}
	}
	return false, nil
}

// todo add more analysis methods, like whether the scenario type and/or scenarios themselves are defined outside the function by comparing their `Pos` against the overall test function's bounds

//
// =============== Result Getters ===============
//

// Returns the fields of the scenario struct definition
// todo note that defining fields like `a, b int` counts as one `Field` element with multiple Names -- need to account for this
func (ss *ScenarioSet) GetFields() iter.Seq[*types.Var] {
	if ss.ScenarioTemplate == nil {
		// Return empty iterator to avoid a panic when trying to range over nil
		return iter.Seq[*types.Var](func(yield func(*types.Var) bool) {})
	}
	return ss.ScenarioTemplate.Fields()
}

// Returns the statements that make up the loop body
func (ss *ScenarioSet) GetRunnerStatements() []ast.Stmt {
	if ss.Runner == nil {
		return nil
	}

	var body *ast.BlockStmt
	switch loop := ss.Runner.(type) {
	case *ast.RangeStmt:
		body = loop.Body
	case *ast.ForStmt:
		body = loop.Body
	}
	if body == nil {
		return nil
	}

	return body.List
}

// Returns whether the detected information in the ScenarioSet is indicative of a table-driven test
func (ss *ScenarioSet) IsTableDriven() bool {
	if ss == nil {
		return false
	}
	// FIXME NOTE: the two commented conditions counting subtests excludes structures like `map[string]bool`
	return ss.DataStructure != ScenarioNoDS /* && ss.ScenarioTemplate != nil && len(ss.Scenarios) > 0 */
}

//
// =============== Output Methods ===============
//

// Helper struct for Marshaling and Unmarshaling JSON.
// Transforms all `ast` nodes to their string representations.
type scenarioSetJSON struct {
	// Parent TestCase is deliberately not included

	ScenarioTemplate string `json:"scenarioTemplate"`

	DataStructure ScenarioDataStructure `json:"dataStructure"`
	Scenarios     []string              `json:"scenarios"`

	Runner string `json:"runner"`

	NameField         string   `json:"nameField"`
	ExpectedFields    []string `json:"expectedFields"`
	HasFunctionFields bool     `json:"hasFunctionFields"`
	UsesSubtest       bool     `json:"usesSubtest"`
}

// Marshal the ScenarioSet for JSON output
func (ss *ScenarioSet) MarshalJSON() ([]byte, error) {
	if ss == nil || ss.TestCase == nil {
		// Can't do anything with improperly initialized ScenarioSet, so return empty JSON data
		return json.Marshal(scenarioSetJSON{})
	}

	var scenarioTemplateStr string
	if ss.ScenarioTemplate != nil {
		scenarioTemplateStr = ss.ScenarioTemplate.String()
	}

	// Marshal individual Scenario data
	// todo LATER remove when implement Marshal in Scenario
	fset := ss.TestCase.FileSet()
	scenarioStrs := make([]string, len(ss.Scenarios))
	for i, node := range ss.Scenarios {
		scenarioStrs[i] = asttools.NodeToString(node, fset)
	}

	return json.Marshal(scenarioSetJSON{
		ScenarioTemplate: scenarioTemplateStr,

		DataStructure: ss.DataStructure,
		Scenarios:     scenarioStrs,

		Runner: asttools.NodeToString(ss.Runner, fset),

		NameField:         ss.NameField,
		ExpectedFields:    ss.ExpectedFields,
		HasFunctionFields: ss.HasFunctionFields,
		UsesSubtest:       ss.UsesSubtest,
	})
}

// todo CLEANUP add UnmarshalJSON method
