package testcase

import (
	"encoding/json"
	"go/ast"
	"go/token"
	"go/types"
	"iter"
	"strings"
)

// Represents the set of scenarios defined by a table-driven test
type ScenarioSet struct {
	// Core data fields
	// todo LATER expand to support scenario definitions like `map[string]bool` without a struct template (probably by making changes to `DetectScenarioDataStructure`)
	ScenarioTemplate *types.Struct // the definition of the `struct` type that individual scenarios are based on

	DataStructure ScenarioDataStructure // describes the type of data structure used to store scenarios
	Scenarios     []ast.Expr            // the individual scenarios themselves //todo LATER convert to type `[]Scenario`

	Runner *ast.BlockStmt // the actual code that runs the subtest (which is the body of a loop)

	// Derived analysis results
	NameField         string   // the name of the field representing each scenario's name, or "map key" if the map key is used as the name
	ExpectedFields    []string // the names of fields representing the expected results of each scenario
	HasFunctionFields bool     // whether the scenario type has any fields whose type is a function
	UsesSubtest       bool     // whether the test calls `t.Run()` inside the loop body

	// Miscellaneous fields
	fset **token.FileSet // pointer to the FileSet associated with the parent TestCase, used for printing AST nodes // todo find a better way and remove
}

//
// =============== Field Definitions ===============
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

// Returns the fields of the scenario struct definition
// todo note that defining fields like `a, b int` counts as one `Field` element with multiple Names -- need to account for this
func (ss *ScenarioSet) GetFields() iter.Seq[*types.Var] {
	if ss.ScenarioTemplate == nil {
		return nil
	}
	return ss.ScenarioTemplate.Fields()
}

// Perform additional analysis based on the core data fields, populating the corresponding fields
func (ss *ScenarioSet) Analyze() {
	if ss.ScenarioTemplate == nil {
		return // Nothing to analyze
	}

	ss.NameField = ss.detectNameField()
	ss.ExpectedFields = ss.detectExpectedFields()
	ss.HasFunctionFields = ss.detectFunctionFields()
	ss.UsesSubtest = ss.detectSubtest()
}

// Returns the name of the first field containing "name" or "desc", representing the name of each scenario
func (ss *ScenarioSet) detectNameField() string {
	if ss.ScenarioTemplate == nil {
		return "" // Nothing to analyze
	}

	// Special case for map data structures where the key is the scenario name,
	// and this field would already be set by `DetectScenarioDataStructure()`
	if ss.DataStructure == ScenarioMapDS && ss.NameField != "" {
		return ss.NameField
	}

	for field := range ss.GetFields() {
		lowercase := strings.ToLower(field.Name())
		if strings.Contains(lowercase, "name") || strings.Contains(lowercase, "desc") {
			return field.Name()
		}
	}
	return ""
}

// Returns the names of the fields containing "expect", "want", or "result", representing the expected results of each scenario
// todo LATER try expanding this to detect fields that are used in assertions or comparisons
func (ss *ScenarioSet) detectExpectedFields() []string {
	if ss.ScenarioTemplate == nil {
		return nil // Nothing to analyze
	}

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

// Returns a bool indicating whether `t.Run()` is called inside the loop body
func (ss *ScenarioSet) detectSubtest() bool {
	if ss.Runner == nil {
		return false
	}
	for _, stmt := range ss.Runner.List {
		if IsSelectorFuncCall(stmt, "t", "Run") {
			return true
		}
	}
	return false
}

// todo add similar methods like whether the type and/or scenarios are defined outside the function by comparing their `Pos` against the overall test's bounds

//
// =============== Output Methods ===============
//

// Helper struct for Marshaling and Unmarshaling JSON.
// Transforms all `ast` nodes to their string representations.
type scenarioSetJSON struct {
	ScenarioTemplate string `json:"scenarioTemplate"`

	DataStructure ScenarioDataStructure `json:"dataStructure"`
	Scenarios     []string              `json:"scenarios"`

	Runner string `json:"runner"`

	NameField         string   `json:"nameField"`
	ExpectedFields    []string `json:"expectedFields"`
	HasFunctionFields bool     `json:"hasFunctionFields"`
	UsesSubtest       bool     `json:"usesSubtest"`

	// fset is not saved
}

// Marshal the ScenarioSet for JSON output
func (ss *ScenarioSet) MarshalJSON() ([]byte, error) {
	var scenarioTemplateStr string
	if ss.ScenarioTemplate != nil {
		scenarioTemplateStr = ss.ScenarioTemplate.String()
	}

	// Marshal Scenario data
	// todo LATER remove when implement Marshal in Scenario
	scenarioStrs := make([]string, len(ss.Scenarios))
	for i, node := range ss.Scenarios {
		scenarioStrs[i] = nodeToString(node, *ss.fset)
	}

	return json.Marshal(scenarioSetJSON{
		ScenarioTemplate: scenarioTemplateStr,

		DataStructure: ss.DataStructure,
		Scenarios:     scenarioStrs,

		Runner: nodeToString(ss.Runner, *ss.fset),

		NameField:         ss.NameField,
		ExpectedFields:    ss.ExpectedFields,
		HasFunctionFields: ss.HasFunctionFields,
		UsesSubtest:       ss.UsesSubtest,
	})
}
