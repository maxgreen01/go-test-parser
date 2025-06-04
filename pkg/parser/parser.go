// General-purpose parser for Go source files, using the Task interface to specify behavior.
package parser

import (
	"errors"
	"go/ast"
	"go/token"
	"log/slog"
	"strings"

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
	if rootDir == "" {
		return errors.New("empty root directory")
	}
	if task == nil {
		return errors.New("invalid task")
	}

	slog.Info("Running " + task.Name() + " task")

	fset := token.NewFileSet()
	cfg := &packages.Config{
		Mode:  packages.LoadAllSyntax | packages.NeedForTest,
		Dir:   rootDir,
		Fset:  fset,
		Tests: true, // Load test files as well
	}

	// Construct a pattern to load all packages in the specified directory and its subdirectories,
	// first removing all trailing forward slashes or backslashes to ensure a valid pattern
	pattern := strings.TrimRight(rootDir, "/\\") + "/..."
	pkgs, err := packages.Load(cfg, pattern)
	if err != nil {
		slog.Error("Failed to load packages: ", "err", err)
	}
	// todo print errors/warnings about bad Go files

	// todo note: don't forget to walk the import graph to analyze imported functions -- maybe cache these to avoid re-analyzing them?
	// could probably use the `packages.Visit` function's pre- and post-visit hooks to modify a map
	// maybe should do the entire iterating like this, where all results of flattening non-test functions are stored in a map?

	for _, pkg := range pkgs { // iterate over all top-level packages
		for _, file := range pkg.Syntax { // iterate over all files in the package
			// per-file logic
			slog.Debug("Processing file", "package", pkg.Name, "file", fset.Position(file.Pos()).Filename)

			task.Visit(fset, file)
		}

		// print any errors encountered while parsing the package
		packages.PrintErrors(pkgs)
	}

	// finished iterating without problem
	slog.Info("Finished parsing all source files")
	if err := task.ReportResults(); err != nil {
		slog.Error("Failed to report results", "err", err)
		return err
	}
	return nil
}
