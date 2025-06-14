package commands

import (
	"fmt"
	"go/ast"
	"go/token"

	"github.com/maxgreen01/golang-test-parser/internal/config"
	"github.com/maxgreen01/golang-test-parser/internal/filewriter"
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

	// Output file writer
	output *filewriter.FileWriter

	// Data fields
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

// Create a new instance of the AnalyzeCommand with the same initial state and flags, COPYING `globals`.
// Note that `output` is shared by reference, so the same `FileWriter` instance is shared by all cloned instances.
func (cmd *AnalyzeCommand) Clone() parser.Task {
	globals := *cmd.globals
	return &AnalyzeCommand{
		globals:        &globals,
		analyzeOptions: cmd.analyzeOptions,
		output:         cmd.output,
	}
}

// Set the project directory for this task.
func (cmd *AnalyzeCommand) SetProjectDir(dir string) {
	cmd.globals.ProjectDir = dir
}

// Validate the values of this Command's flags, then run the task itself
func (cmd *AnalyzeCommand) Execute(args []string) error {
	if cmd.globals.OutputPath == "" {
		cmd.globals.OutputPath = "analyze_report.csv"
	}
	// Initialize the output writer with the specified output path
	cmd.output = filewriter.NewFileWriter(cmd.globals.OutputPath, cmd.globals.AppendOutput)
	if cmd.output == nil {
		return fmt.Errorf("failed to create output writer for path %q", cmd.globals.OutputPath)
	}

	// Actually run the task by starting the parser
	return parser.Parse(cmd, cmd.globals.ProjectDir, cmd.globals.SplitByDir)
}

func (a *AnalyzeCommand) Visit(fset *token.FileSet, file *ast.File) {
	// todo implement
}

func (a *AnalyzeCommand) ReportResults() error {
	// todo implement
	return nil
}

func (a *AnalyzeCommand) Close() {
	if a.output != nil {
		a.output.Close()
	}
}
