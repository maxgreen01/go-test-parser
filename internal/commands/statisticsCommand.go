package commands

import (
	"fmt"
	"go/ast"
	"go/token"
	"log/slog"
	"strings"

	"github.com/maxgreen01/golang-test-parser/internal/config"
	"github.com/maxgreen01/golang-test-parser/pkg/parser"
	"github.com/maxgreen01/golang-test-parser/pkg/testcase"

	"github.com/jessevdk/go-flags"
)

// Implementation of both the Parser Task interface and the Flags package's Commander interface.
// Stores input flags for the task, as well as fields representing the data to be collected.
type StatisticsCommand struct {
	// Input flags
	*config.GlobalOptions
	statisticsOptions

	// Output fields
	testCases []testcase.TestCase // list of actual test functions and related metadata

	testFileCount  int // total number of files ending in "_test.go"
	totalFileCount int // total number of Go files
	totalTestLines int // total number of lines in all test functions
	totalLines     int // total number of lines across the entire project
}

// Command-line flags for the Statistics command specifically
type statisticsOptions struct {
}

// Compile-time interface implementation checks
var _ parser.Task = (*StatisticsCommand)(nil)
var _ flags.Commander = (*StatisticsCommand)(nil)

func (s *StatisticsCommand) Name() string {
	return "statistics"
}

// Create a new instance of the StatisticsCommand using a reference to the global options.
func NewStatisticsCommand(globals *config.GlobalOptions) *StatisticsCommand {
	if globals.OutputPath == "" {
		globals.OutputPath = "statistics_report.csv"
	}

	return &StatisticsCommand{GlobalOptions: globals}
}

// Create a new instance of the StatisticsCommand with the same initial state.
func (cmd *StatisticsCommand) Clone() parser.Task {
	return &StatisticsCommand{
		GlobalOptions:     cmd.GlobalOptions,
		statisticsOptions: cmd.statisticsOptions,
	}
}

// Validate the values of this Command's flags, then run the task itself
func (cmd *StatisticsCommand) Execute(args []string) error {
	return parser.Parse(cmd, cmd.ProjectDir, cmd.SplitByDir)
}

func (s *StatisticsCommand) Visit(fset *token.FileSet, file *ast.File) {
	packageName := file.Name.Name
	fileName := fset.Position(file.Pos()).Filename

	// increment project-scale statistics
	s.totalFileCount++
	if strings.HasSuffix(fileName, "_test.go") {
		s.testFileCount++
	}
	s.totalLines += fset.Position(file.End()).Line - fset.Position(file.Pos()).Line + 1

	// Only iterate top level declarations
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}

		slog.Debug("Checking function...", "name", fn.Name.Name, "package", packageName, "file", fileName)

		// Save the function as a valid test case if it meets all the criteria
		if valid, _ := testcase.IsValidTestCase(fn); valid {
			tc := testcase.CreateTestCase(fn, fset, packageName)
			s.testCases = append(s.testCases, tc)

			lines := tc.NumLines()
			s.totalTestLines += lines
		}
	}
}

func (s *StatisticsCommand) ReportResults() error {
	fmt.Println("\n=================  Statistics Report:  =================\n")

	numTests := len(s.testCases)
	if numTests == 0 {
		fmt.Println("No test cases found in the provided Go files.")
	} else {
		fmt.Printf("Total number of test cases: %d\n", numTests)
		fmt.Printf("\n")
		fmt.Printf("Number of '_test.go' files: %d\n", s.testFileCount)
		fmt.Printf("Total number of Go files: %d\n", s.totalFileCount)
		fmt.Printf("\n")
		fmt.Printf("Total lines of test code: %d\n", s.totalTestLines)
		fmt.Printf("Average lines per test case: %.1f\n", float64(s.totalTestLines)/float64(numTests))
		fmt.Printf("Percentage of total lines for test cases: %.1f%%\n", float64(s.totalTestLines)/float64(s.totalLines)*100)
	}

	// todo append results to output file

	return nil
}
