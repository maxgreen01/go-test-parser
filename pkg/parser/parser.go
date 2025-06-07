// General-purpose parser for Go source files, using the Task interface to specify behavior.
package parser

import (
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/maxgreen01/golang-test-parser/internal/tasks"

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

// TaskFactory returns a new Task based on the provided name, or an error if the Task does not exist.
func TaskFactory(taskName string) (Task, error) {
	switch taskName {
	case "statistics":
		return &tasks.StatisticsTask{}, nil
	case "analyze":
		return &tasks.AnalyzeTask{}, nil
	default:
		return nil, errors.New("invalid task name")
	}
}

// Runs the specified task on all Go source files in the given directory.
// If `splitByDir` is true, parses each top-level directory in the specified directory separately (ignoring top-level Go files).
func Parse(rootDir string, taskName string, splitByDir bool) error {
	if rootDir == "" {
		return errors.New("empty root directory")
	}
	// ensure the task is valid
	if _, err := TaskFactory(taskName); err != nil {
		return err
	}

	fmt.Printf("\n============ Running %q task on directory %q ============\n", taskName, rootDir)

	// Run the parser either on the entire directory at once, or on each top-level sub-directory separately
	if splitByDir {
		// Parse each top-level directory separately
		fmt.Printf("Parsing each top-level directory separately\n")

		entries, err := os.ReadDir(rootDir)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.IsDir() {
				subDir := filepath.Join(rootDir, entry.Name())
				if err := parseDir(subDir, taskName); err != nil {
					return errors.New("Parsing subdirectory " + subDir + ": " + err.Error())
				}
			}
		}
	} else {
		// Parse the entire directory as a single unit
		if err := parseDir(rootDir, taskName); err != nil {
			return err
		}
	}

	// Successfully parsed all files
	return nil
}

// Iterates over all Go source files in the specified directory and runs the provided task on each file.
// After processing all files, calls the task's ReportResults method to output any accumulated results.
func parseDir(dir string, taskName string) error {
	// Create a new Task instance
	task, err := TaskFactory(taskName)
	if err != nil {
		return err
	}

	fmt.Printf("\n\n~~~~~ Parsing directory %q ~~~~~\n", dir)

	fset := token.NewFileSet()
	cfg := &packages.Config{
		Mode:  packages.LoadAllSyntax | packages.NeedForTest,
		Dir:   dir,
		Fset:  fset,
		Tests: true, // Load test files as well
	}

	// Construct a pattern to load all packages in the specified directory and its subdirectories,
	// first removing all trailing forward slashes or backslashes to ensure a valid pattern
	pattern := strings.TrimRight(dir, "/\\") + "/..."
	pkgs, err := packages.Load(cfg, pattern)
	if err != nil {
		slog.Error("Failed to load packages: ", "err", err)
	}

	// todo note: don't forget to walk the import graph to analyze imported functions -- maybe cache these to avoid re-analyzing them?
	// could probably use the `packages.Visit` function's pre- and post-visit hooks to modify a map
	// maybe should do the entire iterating like this, where all results of flattening non-test functions are stored in a map?

	// ========== Iterate over all top-level packages ==========
	for _, pkg := range pkgs {
		pkgErrs := pkg.Errors

		// Build a "set" of filepaths that have errors in this package before iterating files
		errFiles := make(map[string]struct{}, len(pkgErrs))
		for _, e := range pkgErrs {
			colonIdx := strings.Index(e.Pos, ":")
			if colonIdx > 0 {
				file := e.Pos[:colonIdx]
				errFiles[file] = struct{}{}
			}
		}

		// ========== Iterate over all files in the package ==========
		for _, file := range pkg.Syntax {
			filePath := fset.Position(file.Pos()).Filename

			// Skip files in `vendor/` directory
			if strings.Contains(filePath, filepath.Join("vendor", "")) {
				slog.Info("Skipping vendored file", "file", filePath)
				continue
			}

			// Skip files that have errors
			if _, found := errFiles[filePath]; found {
				slog.Info("Skipping file with errors", "file", filePath)
				continue
			}

			// Actually process the file
			slog.Debug("Processing file", "package", pkg.Name, "file", filePath)
			task.Visit(fset, file)
		}
	}

	// finished iterating without problem
	slog.Info("Finished parsing all source files in directory", "dir", dir)
	if err := task.ReportResults(); err != nil {
		slog.Error("Failed to report results", "err", err)
		return err
	}
	return nil
}
