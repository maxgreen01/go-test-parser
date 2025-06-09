package commands

import (
	"go/ast"
	"go/token"

	"github.com/maxgreen01/golang-test-parser/internal/config"
	"github.com/maxgreen01/golang-test-parser/pkg/parser"
	"github.com/maxgreen01/golang-test-parser/pkg/testcase"

	"github.com/jessevdk/go-flags"
)

// Implementation of both the Parser Task interface and the Flags package's Commander interface.
// Stores input flags for the task, as well as fields representing the data to be collected.
type AnalyzeCommand struct {
	// Input flags
	globals *config.GlobalOptions // Avoid embedding because it flag parser treats this as duplicating the global options
	analyzeOptions

	// Output fields
	testCases []testcase.TestCase // list of actual test functions and related metadata
}

// Command-line flags for the Analyze command specifically
type analyzeOptions struct {
}

// Compile-time interface implementation checks
var _ parser.Task = (*AnalyzeCommand)(nil)
var _ flags.Commander = (*AnalyzeCommand)(nil)

func (a *AnalyzeCommand) Name() string {
	return "analyze"
}

// Create a new instance of the AnalyzeCommand using a reference to the global options.
func NewAnalyzeCommand(globals *config.GlobalOptions) *AnalyzeCommand {
	return &AnalyzeCommand{globals: globals}
}

// Create a new instance of the AnalyzeCommand with the same initial state (except the specified project directory).
func (cmd *AnalyzeCommand) Clone(dir string) parser.Task {
	globals := *cmd.globals
	globals.ProjectDir = dir
	return &AnalyzeCommand{
		globals:        &globals,
		analyzeOptions: cmd.analyzeOptions,
	}
}

// Validate the values of this Command's flags, then run the task itself
func (cmd *AnalyzeCommand) Execute(args []string) error {
	if cmd.globals.OutputPath == "" {
		cmd.globals.OutputPath = "analyze_report.csv"
	}

	return parser.Parse(cmd, cmd.globals.ProjectDir, cmd.globals.SplitByDir)
}

func (a *AnalyzeCommand) Visit(fset *token.FileSet, file *ast.File) {
	// todo implement
}

func (a *AnalyzeCommand) ReportResults() error {
	// todo implement
	return nil
}
