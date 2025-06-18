package commands

import (
	"fmt"
	"go/ast"
	"go/token"
	"log/slog"
	"path/filepath"

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
// THIS SHOULD ONLY BE CALLED ONCE PER PROGRAM EXECUTION.
func (cmd *AnalyzeCommand) Execute(args []string) error {
	if cmd.globals.OutputPath == "" {
		cmd.globals.OutputPath = "analyze_report.csv"
	}
	// Initialize the output writer with the specified output path
	writer, err := filewriter.NewFileWriter(cmd.globals.OutputPath, cmd.globals.AppendOutput)
	if err != nil {
		return fmt.Errorf("failed to create output writer for path %q", cmd.globals.OutputPath)
	}
	cmd.output = writer

	// Actually run the task by starting the parser
	return parser.Parse(cmd, cmd.globals.ProjectDir, cmd.globals.SplitByDir, cmd.globals.Threads)
}

func (a *AnalyzeCommand) Visit(fset *token.FileSet, file *ast.File) {
	projectName := filepath.Base(a.globals.ProjectDir)
	// packageName := file.Name.Name
	// fileName := fset.Position(file.Pos()).Filename

	// Only iterate top level declarations
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}

		// slog.Debug("Checking function...", "name", fn.Name.Name, "package", packageName, "file", fileName)

		// Save the function as a valid test case if it meets all the criteria
		valid, _ := testcase.IsValidTestCase(fn)
		// todo do something with the `badFormat` return value
		if !valid {
			continue
		}
		tc := testcase.CreateTestCase(fn, file, fset, projectName)
		tc.Analyze()

		a.testCases = append(a.testCases, tc)
	}
}

func (a *AnalyzeCommand) ReportResults() error {
	slog.Info("Analysis complete", "testCases", len(a.testCases))

	for _, tc := range a.testCases {
		err := tc.SaveAsJSON()
		if err != nil {
			return fmt.Errorf("saving test case %q as JSON: %w", tc.Name, err)
		}
	}

	return nil
}

func (a *AnalyzeCommand) Close() {
	if a.output != nil {
		a.output.Close()
	}
}
