package parsercommands

import (
	"fmt"
	"go/ast"
	"go/token"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/maxgreen01/go-test-parser/internal/config"
	"github.com/maxgreen01/go-test-parser/internal/filewriter"
	"github.com/maxgreen01/go-test-parser/pkg/parser"
	"github.com/maxgreen01/go-test-parser/pkg/testcase"
	"golang.org/x/tools/go/packages"

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
	testCases []*testcase.TestCase // list of actual test functions and related metadata
}

// Command-line flags for the Analyze command specifically
type analyzeOptions struct {
}

// Compile-time interface implementation check
var _ ParserCommand = (*AnalyzeCommand)(nil)

// Register the command with the global flag parser
func init() {
	RegisterCommand(func(flagParser *flags.Parser, opts *config.GlobalOptions) {
		flagParser.AddCommand("statistics", "Collect statistics about a Go project's tests", "", NewStatisticsCommand(opts))
	})
}

// Create a new instance of the AnalyzeCommand using a reference to the global options.
func NewAnalyzeCommand(globals *config.GlobalOptions) *AnalyzeCommand {
	return &AnalyzeCommand{globals: globals}
}

func (cmd *AnalyzeCommand) Name() string {
	return "analyze"
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

func (cmd *AnalyzeCommand) Visit(file *ast.File, fset *token.FileSet, pkg *packages.Package) {
	projectName := filepath.Base(cmd.globals.ProjectDir)
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
		tc := testcase.CreateTestCase(fn, file, pkg, projectName)
		cmd.testCases = append(cmd.testCases, &tc)

		// Analyze the test case and save it immediately
		tc.Analyze()
		err := tc.SaveAsJSON(cmd.output.GetPathDir())
		if err != nil {
			slog.Error("saving test case as JSON", "name", tc.TestName(), "err", err)
		}
	}
}

func (cmd *AnalyzeCommand) ReportResults() error {
	// Format output for printing the report to the terminal (and potentially writing to a text file)

	reportLines := []string{
		fmt.Sprintf("\n=============  Analysis Report for %q:  =============\n\n", cmd.globals.ProjectDir),
	}

	numTests := len(cmd.testCases)

	if numTests == 0 {
		reportLines = append(reportLines, "No test cases found in the specified project.\n\n")
	} else {
		reportLines = append(reportLines,
			fmt.Sprintf("Number of test cases: %d\n", numTests),
			"\n",
			// todo maybe put more here
		)
	}

	// Print the report to the terminal
	slog.Info("Finished running analysis task on project \"" + cmd.globals.ProjectDir + "\"")
	fmt.Print(strings.Join(reportLines, "") + "\n")

	// Append results to output file (text or CSV)
	switch cmd.output.DetectFormat() {

	case filewriter.FormatTxt:
		return cmd.output.Write(reportLines)

	case filewriter.FormatCSV:
		if numTests == 0 {
			return nil
		}

		// Save a condensed version of each analyzed test case
		rows := make([][]string, 0, numTests)
		for _, tc := range cmd.testCases {
			rows = append(rows, tc.EncodeAsCSV())
		}
		return cmd.output.WriteMultiple(rows, cmd.testCases[0].GetCSVHeaders())

	default:
		return fmt.Errorf("unsupported output format (file %q)", cmd.output.GetPath())
	}
}

func (cmd *AnalyzeCommand) Close() {
	if cmd.output != nil {
		cmd.output.Close()
	}
}
