package tasks

import (
	"fmt"
	"go/ast"
	"go/token"
	"log/slog"
)

type AnalyzeTask struct {
}

func (a *AnalyzeTask) Name() string {
	return "analyze"
}

func (a *AnalyzeTask) Visit(fset *token.FileSet, file *ast.File) {
	ast.Inspect(file, func(n ast.Node) bool {
		if fn, ok := n.(*ast.FuncDecl); ok && fn.Name.Name == "TestMain" {
			slog.Info("Found TestMain", "pos", fset.Position(fn.Pos()))
			// todo is there a maybe way to skip execution for the rest of the file? (return from the outer function)
		}
		return true
	})
}

func (s *AnalyzeTask) ReportResults() error {
	fmt.Println("\nAnalysis complete")
	return nil
}
