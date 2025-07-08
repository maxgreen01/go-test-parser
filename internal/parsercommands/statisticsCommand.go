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

	"github.com/jessevdk/go-flags"
)

// Implementation of both the Parser Task interface and the Flags package's Commander interface.
// Stores input flags for the task, as well as fields representing the data to be collected.
type StatisticsCommand struct {
	// Input flags
	globals *config.GlobalOptions // Avoid embedding because it flag parser treats this as duplicating the global options
	statisticsOptions

	// Output file writer
	output *filewriter.FileWriter

	// Data fields
	testCases []testcase.TestCase // list of actual test functions and related metadata

	testFileCount  int // total number of files ending in "_test.go"
	totalFileCount int // total number of Go files
	totalTestLines int // total number of lines in all test functions
	totalLines     int // total number of lines across the entire project
}

// Command-line flags for the Statistics command specifically
type statisticsOptions struct {
}

// Compile-time interface implementation check
var _ ParserCommand = (*StatisticsCommand)(nil)

// Register the command with the global flag parser
func init() {
	RegisterCommand(func(flagParser *flags.Parser, opts *config.GlobalOptions) {
		flagParser.AddCommand("analyze", "Analyze a Go projects' tests", "", NewAnalyzeCommand(opts))
	})
}

// Create a new instance of the StatisticsCommand using a reference to the global options.
func NewStatisticsCommand(globals *config.GlobalOptions) *StatisticsCommand {
	return &StatisticsCommand{globals: globals}
}

func (cmd *StatisticsCommand) Name() string {
	return "statistics"
}

// Create a new instance of the StatisticsCommand with the same initial state and flags, COPYING `globals`.
// Note that `output` is shared by reference, so the same `FileWriter` instance is shared by all cloned instances.
func (cmd *StatisticsCommand) Clone() parser.Task {
	globals := *cmd.globals
	return &StatisticsCommand{
		globals:           &globals,
		statisticsOptions: cmd.statisticsOptions,
		output:            cmd.output,
	}
}

// Set the project directory for this task.
func (cmd *StatisticsCommand) SetProjectDir(dir string) {
	cmd.globals.ProjectDir = dir
}

// Validate the values of this Command's flags, then run the task itself.
// THIS SHOULD ONLY BE CALLED ONCE PER PROGRAM EXECUTION.
func (cmd *StatisticsCommand) Execute(args []string) error {
	if cmd.globals.OutputPath == "" {
		cmd.globals.OutputPath = "statistics_report.csv"
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

func (cmd *StatisticsCommand) Visit(fset *token.FileSet, file *ast.File) {
	projectName := filepath.Base(cmd.globals.ProjectDir)
	packageName := file.Name.Name
	fileName := fset.Position(file.Pos()).Filename

	// increment project-scale statistics
	cmd.totalFileCount++
	if strings.HasSuffix(fileName, "_test.go") {
		cmd.testFileCount++
	}
	cmd.totalLines += fset.Position(file.End()).Line - fset.Position(file.Pos()).Line + 1

	// Only iterate top level declarations
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}

		slog.Debug("Checking function...", "name", fn.Name.Name, "package", packageName, "file", fileName)

		// Save the function as a valid test case if it meets all the criteria
		valid, _ := testcase.IsValidTestCase(fn)
		if !valid {
			continue
		}

		tc := testcase.CreateTestCase(fn, file, fset, projectName)
		cmd.testCases = append(cmd.testCases, tc)

		lines := tc.NumLines()
		cmd.totalTestLines += lines
	}
}

func (cmd *StatisticsCommand) ReportResults() error {
	// Format output for printing the report to the terminal (and potentially writing to a text file)

	reportLines := []string{
		fmt.Sprintf("\n=============  Statistics Report for %q:  =============\n\n", cmd.globals.ProjectDir),
	}

	// Define additional result statistics
	numTests := len(cmd.testCases)
	avgTestLines := 0.0
	percentTestLines := 0.0

	if numTests == 0 {
		reportLines = append(reportLines, "No test cases found in the specified project.\n\n")
	} else {
		// Calculate additional result statistics
		avgTestLines = float64(cmd.totalTestLines) / float64(numTests)
		percentTestLines = float64(cmd.totalTestLines) / float64(cmd.totalLines) * 100

		reportLines = append(reportLines,
			fmt.Sprintf("Total number of test cases: %d\n", numTests),
			"\n",
			fmt.Sprintf("Number of '_test.go' files: %d\n", cmd.testFileCount),
			fmt.Sprintf("Total number of Go files: %d\n", cmd.totalFileCount),
			"\n",
			fmt.Sprintf("Total lines of test code: %d\n", cmd.totalTestLines),
			fmt.Sprintf("Average lines per test case: %.1f\n", avgTestLines),
			fmt.Sprintf("Percentage of total lines for test cases: %.1f%%\n", percentTestLines),
			"\n",
		)
	}

	// Print the report to the terminal
	slog.Info("Finished running statistics task on project \"" + cmd.globals.ProjectDir + "\"")
	fmt.Print(strings.Join(reportLines, "") + "\n")

	// Append results to output file (text or CSV)
	switch cmd.output.DetectFormat() {

	case filewriter.FormatTxt:
		return cmd.output.Write(reportLines)

	case filewriter.FormatCSV:
		csvHeaders := []string{
			"projectDir",
			"testCases",
			"testFiles",
			"totalFiles",
			"testLines",
			"avgLinesPerTest",
			"percentTestLines",
		}

		row := []string{
			cmd.globals.ProjectDir,
			fmt.Sprintf("%d", numTests),
			fmt.Sprintf("%d", cmd.testFileCount),
			fmt.Sprintf("%d", cmd.totalFileCount),
			fmt.Sprintf("%d", cmd.totalTestLines),
			fmt.Sprintf("%.1f", avgTestLines),
			fmt.Sprintf("%.1f", percentTestLines),
		}

		return cmd.output.Write(row, csvHeaders)

	default:
		return fmt.Errorf("unsupported output format (file %q)", cmd.output.GetPath())
	}
}

func (cmd *StatisticsCommand) Close() {
	if cmd.output != nil {
		cmd.output.Close()
	}
}
