package tasks

import (
	"fmt"
	"go/ast"
	"go/token"
)

type StatisticsTask struct {
	FuncCount int
}

func (s *StatisticsTask) Name() string {
	return "statistics"
}

func (s *StatisticsTask) Visit(fset *token.FileSet, file *ast.File) {
	ast.Inspect(file, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncDecl); ok {
			s.FuncCount++
		}
		return true
	})

	// filename := fset.Position(file.Pos()).Filename
	// if !strings.HasSuffix(filename, "_test.go") {
	// 	return; // Skip non-test files
	// }
	// ast.Inspect(file, func(n ast.Node) bool {
	// 	if fn, ok := n.(*ast.FuncDecl); ok && strings.HasPrefix(fn.Name.Name, "Test") {
	// 		fmt.Printf("Found test: %s in %s\n", fn.Name.Name, filename)
	// 	}
	// 	return true
	// })
}

func (s *StatisticsTask) ReportResults() error {
	fmt.Println("\nStatistics Report:")
	if s.FuncCount == 0 {
		fmt.Println("No functions found in the provided Go files.")
	} else {
		fmt.Println("Total functions found:", s.FuncCount)
	}
	return nil
}
