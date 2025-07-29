package testcase

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"log/slog"
	"strconv"
	"strings"

	"github.com/maxgreen01/go-test-parser/internal/filewriter"
)

// Represents the result of analyzing a TestCase, including information about its table-driven structure.
type AnalysisResult struct {
	// Reference to the original test case being analyzed
	TestCase *TestCase

	// Analysis data
	ScenarioSet      *ScenarioSet         // the set of scenarios defined in this test case, if it is table-driven
	ParsedStatements []*ExpandedStatement // the list of parsed and fully-expanded statements in the test case
	ImportedPackages []string             // the list of imported packages in the test case's file

	// Refactoring result - only available after running `AttemptRefactoring()`
	RefactorResult RefactorResult // the result of refactoring the test case
}

// Extracts relevant information about a TestCase and saves the results to a new AnalysisResult instance
func Analyze(tc *TestCase) *AnalysisResult {
	slog.Debug("Analyzing TestCase", "testCase", tc)

	// Initialize the AnalysisResult
	result := &AnalysisResult{
		TestCase: tc,
	}

	if tc == nil || tc.GetFuncDecl() == nil || tc.GetFile() == nil {
		slog.Error("Cannot analyze TestCase because it has nil syntax data", "testCase", tc)
		return nil
	}
	fset := tc.FileSet()
	if fset == nil {
		slog.Error("Cannot analyze TestCase because FileSet is nil", "testCase", tc)
		return nil
	}

	// Expand all the individual statements in the test case's body
	stmts := tc.GetStatements()
	result.ParsedStatements = make([]*ExpandedStatement, len(stmts))
	for i, stmt := range stmts {
		// Try to expand the statement if it's a call to a testing helper function
		result.ParsedStatements[i] = ExpandStatement(stmt, tc, true)
	}

	// Populate table-driven test data
	result.ScenarioSet = IdentifyScenarioSet(tc, result.ParsedStatements)

	// Extract imported packages from the file's AST
	var imports []*ast.ImportSpec
	if tc.GetFile() != nil {
		imports = tc.GetFile().Imports
		for _, imp := range imports {
			result.ImportedPackages = append(result.ImportedPackages, strings.Trim(imp.Path.Value, "\""))
		}
	} else {
		slog.Error("Cannot extract imported packages in TestCase because File is nil", "testCase", tc)
	}

	return result
}

// Return whether the test case is table-driven, based on the detected ScenarioSet data
func (ar *AnalysisResult) IsTableDriven() bool {
	if ar.ScenarioSet == nil {
		return false
	}
	return ar.ScenarioSet.IsTableDriven()
}

//
// ========== Output Methods ==========
//

// Return the headers for the CSV representation of the AnalysisResult.
// Complex or large fields are excluded for the sake of brevity.
func (ar *AnalysisResult) GetCSVHeaders() []string {
	return []string{
		"project",
		"filePath",
		"package",
		"name",
		"scenarioDataStructure",
		"scenarioCount",
		"scenarioNameField",
		"scenarioExpectedFields",
		"scenarioHasFunctionFields",
		"scenarioUsesSubtest",
		"refactorStrategy",
		"refactorStatus",
		"importedPackages",
	}
}

// Encode the AnalysisResult as a CSV row, returning the encoded data corresponding to the headers in `GetCSVHeaders()`.
func (ar *AnalysisResult) EncodeAsCSV() []string {
	// Replace nil fields with empty data to avoid nil pointer dereferences
	tc := ar.TestCase
	if tc == nil {
		tc = &TestCase{}
	}
	ss := ar.ScenarioSet
	if ss == nil {
		ss = &ScenarioSet{}
	}
	rr := ar.RefactorResult

	return []string{
		tc.ProjectName,
		tc.FilePath,
		tc.PackageName,
		tc.TestName,
		ss.DataStructure.String(),
		strconv.Itoa(len(ss.Scenarios)),
		ss.NameField,
		strings.Join(ss.ExpectedFields, ", "),
		strconv.FormatBool(ss.HasFunctionFields),
		strconv.FormatBool(ss.UsesSubtest),
		rr.Strategy.String(),
		rr.Status.String(),
		strings.Join(ar.ImportedPackages, ", "),
	}
}

// Save the AnalysisResult as JSON to a file named like `<project>/<project>_<package>_<testName>.json` in the specified directory (or the output directory if not specified).
func (ar *AnalysisResult) SaveAsJSON(dir string) error {
	tc := ar.TestCase
	slog.Info("Saving test case analysis results as JSON", "testCase", tc)

	// Construct the filepath using information from the test case, inside the provided directory.
	// If the directory is empty, the `filewriter` will automatically prepend the output directory instead.
	path := tc.GetJSONFilePath(dir)

	// Create and write the file
	err := filewriter.WriteToFile(path, ar)
	if err != nil {
		return fmt.Errorf("saving analysis results for test case %q as JSON: %w", tc.TestName, err)
	}

	slog.Info("Saved test case analysis results as JSON", "filePath", path)
	return nil
}

// Helper struct for Marshaling and Unmarshaling JSON
type analysisResultJSON struct {
	TestCase *TestCase `json:"testCase"`

	ScenarioSet      *ScenarioSet         `json:"scenarioSet"`
	ParsedStatements []*ExpandedStatement `json:"parsedStatements"`
	ImportedPackages []string             `json:"importedPackages"`

	RefactorResult refactorResultJSON `json:"refactorResult"`
}

// Marshal a TestCase for JSON output
func (ar *AnalysisResult) MarshalJSON() ([]byte, error) {
	if ar == nil || ar.TestCase == nil {
		// Can't do anything with improperly initialized AnalysisResult, so return empty JSON data
		return json.Marshal(analysisResultJSON{})
	}

	return json.Marshal(analysisResultJSON{
		TestCase: ar.TestCase,

		ScenarioSet:      ar.ScenarioSet,
		ParsedStatements: ar.ParsedStatements,
		ImportedPackages: ar.ImportedPackages,

		RefactorResult: ar.RefactorResult.ToJSON(ar.TestCase.FileSet()),
	})
}

// Unmarshal a TestCase from JSON
// FIXME FIGURE OUT HOW TO DECODE RefactorResult!
// func (result *AnalysisResult) UnmarshalJSON(data []byte) error {
// 	var jsonData analysisResultJSON
// 	if err := json.Unmarshal(data, &jsonData); err != nil {
// 		return err
// 	}

// 	// Unmarshal the RefactorResult
// 	if err := result.RefactorResult.FromJSON(jsonData.RefactorResult, result.TestCase.FileSet()); err != nil {
// 		return fmt.Errorf("unmarshaling RefactorResult: %w", err)
// 	}

// 	// Save data into the main struct
// 	*result = AnalysisResult{
// 		TestCase: jsonData.TestCase,

// 		ScenarioSet:      jsonData.ScenarioSet,
// 		ParsedStatements: jsonData.ParsedStatements,
// 		ImportedPackages: jsonData.ImportedPackages,

// 		RefactorResult: result.RefactorResult,
// 	}
// 	return nil
// }
