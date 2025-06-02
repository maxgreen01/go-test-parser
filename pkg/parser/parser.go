// General-purpose parser for Go source files, using the Task interface to specify behavior.
package parser

import (
	"go/ast"
	"go/token"
	"log/slog"

	"golang.org/x/tools/go/packages"
)

// The Task interface defines a task that can be performed on all the Go source files in a project.
// This includes a method to visit each source file, and another to report results after all files have been processed.
// Implementations should include fields (either public or private) to track progress, results, etc. across the entire project.
type Task interface {
	Name() string
	Visit(fset *token.FileSet, file *ast.File)
	ReportResults() error
}

// Iterates over all Go source files in the specified directory and runs the provided task on each file.
// After processing all files, calls the task's ReportResults method to output any accumulated results.
func Parse(rootDir string, task Task) error {
	slog.Info("Running " + task.Name() + " task")

	fset := token.NewFileSet()
	cfg := &packages.Config{
		Mode: packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedName | packages.NeedFiles,
		Dir:  rootDir,
		Fset: fset,
	}

	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		slog.Error("Failed to load packages: ", "err", err)
	}

	for _, pkg := range pkgs {
		for _, file := range pkg.Syntax {
			// per-file logic
			slog.Debug("Processing file", "file", file.Name.Name, "package", pkg.Name)

			task.Visit(fset, file)
		}
	}

	// finished iterating without problem
	slog.Info("Finished parsing all source files")
	if err := task.ReportResults(); err != nil {
		slog.Error("Failed to report results", "err", err)
		return err
	}
	return nil
}
