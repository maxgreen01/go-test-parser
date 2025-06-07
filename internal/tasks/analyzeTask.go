package tasks

import (
	"fmt"
	"go/ast"
	"go/token"
)

type AnalyzeTask struct {
}

func (a *AnalyzeTask) Name() string {
	return "analyze"
}

func (a *AnalyzeTask) Visit(fset *token.FileSet, file *ast.File) {
	// todo implement
}

func (s *AnalyzeTask) ReportResults() error {
	fmt.Println("\nAnalysis complete")
	return nil
}
