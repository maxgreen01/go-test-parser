package tasks

import (
	"fmt"
	"go/ast"
	"go/token"
	"log/slog"
	"strings"

	"github.com/maxgreen01/golang-test-parser/internal/testcase"
)

type StatisticsTask struct {
	TestCases []testcase.TestCase // list of actual test functions and related metadata

	TestFileCount  int // total number of files ending in "_test.go"
	TotalFileCount int // total number of Go files
	TotalTestLines int // total number of lines in all test functions
	TotalLines     int // total number of lines across the entire project
}

func (s *StatisticsTask) Name() string {
	return "statistics"
}

func (s *StatisticsTask) Visit(fset *token.FileSet, file *ast.File) {
	packageName := file.Name.Name
	fileName := fset.Position(file.Pos()).Filename

	// increment project-scale statistics
	s.TotalFileCount++
	if strings.HasSuffix(fileName, "_test.go") {
		s.TestFileCount++
	}
	s.TotalLines += fset.Position(file.End()).Line - fset.Position(file.Pos()).Line + 1

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
			s.TestCases = append(s.TestCases, tc)

			lines := tc.NumLines()
			s.TotalTestLines += lines
		}
	}
}

func (s *StatisticsTask) ReportResults() error {
	fmt.Println("\n=================  Statistics Report:  =================\n")

	numTests := len(s.TestCases)
	if numTests == 0 {
		fmt.Println("No test cases found in the provided Go files.")
	} else {
		fmt.Printf("Total number of test cases: %d\n", numTests)
		fmt.Printf("\n")
		fmt.Printf("Number of '_test.go' files: %d\n", s.TestFileCount)
		fmt.Printf("Total number of Go files: %d\n", s.TotalFileCount)
		fmt.Printf("\n")
		fmt.Printf("Total lines of test code: %d\n", s.TotalTestLines)
		fmt.Printf("Average lines per test case: %.1f\n", float64(s.TotalTestLines)/float64(numTests))
		fmt.Printf("Percentage of total lines for test cases: %.1f%%\n", float64(s.TotalTestLines)/float64(s.TotalLines)*100)
	}
	return nil
}
