package testcase

import (
	"encoding/json"
	"go/ast"
	"go/token"
	"go/types"
	"iter"
)

// Represents the set of scenarios defined by a table-driven test
type ScenarioSet struct {
	// todo LATER expand to support scenario definitions like `map[string]bool` without a struct template (probably by making changes to `DetectScenarioDataStructure`)
	ScenarioTemplate *types.Struct // the definition of the `struct` type that individual scenarios are based on

	DataStructure ScenarioDataStructure // describes the type of data structure used to store scenarios
	Scenarios     []ast.Expr            // the individual scenarios themselves //todo LATER convert to type `[]Scenario`

	Runner *ast.BlockStmt // the actual code that runs the subtest (which is the body of a loop)

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
// =============== Regular-Use Methods ===============
//

// Returns the fields of the scenario struct definition
// todo note that defining fields like `a, b int` counts as one `Field` element with multiple Names -- need to account for this
func (ss *ScenarioSet) GetFields() iter.Seq[*types.Var] {
	if ss.ScenarioTemplate == nil {
		return nil
	}
	return ss.ScenarioTemplate.Fields()
}

// Returns a bool indicating whether t.Run() is called inside the loop body
func (ss *ScenarioSet) UsesSubtest() bool {
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

	Runner      string `json:"runner"`
	UsesSubtest bool   `json:"usesSubtest"`
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

		Runner:      nodeToString(ss.Runner, *ss.fset),
		UsesSubtest: ss.UsesSubtest(),
	})
}
