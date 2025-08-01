// General-purpose parser for Go source files, using the Task interface to specify behavior.
package parser

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sync/errgroup"
	"golang.org/x/tools/go/packages"
)

// The Task interface defines a task that can be performed on all the Go source files in a project.
// This includes a method to visit each source file, and another to report results after all files have been processed.
// Implementations should include fields (either public or private) to track progress, results, etc. across the entire project.
type Task interface {
	// Return the lowercase name of the task
	Name() string

	// Function called on every Go source file in the project, which may modify local state to save results
	Visit(file *ast.File, fset *token.FileSet, pkg *packages.Package)

	// Create a new instance of the task with the same initial state and flags.
	// Used to ensure that each parsed directory can have an independent output if `splitByDir` is true.
	Clone() Task

	// Set the project directory for this task. Often used after Clone to set the directory for the new instance.
	SetProjectDir(dir string)

	// Function called after all files in the specified project directory have been processed
	ReportResults() error

	// Close any resources used by this task and its clones, like file handles.
	// Should only be called once after all instances of the task have finished, i.e. the parser is completely finished.
	Close()
}

// Runs the specified task on all Go source files in the given directory.
// If `splitByDir` is true, parses each top-level directory in the specified directory separately (ignoring top-level Go files).
// todo maybe update the Task interface to include a method for getting flags (to avoid passing so many boilerplate params)
func Parse(t Task, rootDir string, splitByDir bool, threads int) error {
	if rootDir == "" {
		return errors.New("empty root directory provided")
	}
	if t == nil {
		return errors.New("nil task provided")
	}

	fmt.Println()
	slog.Info("============ Running " + t.Name() + " task on project \"" + rootDir + "\" ============")
	fmt.Println()

	// Run the parser either on the entire directory at once, or on each top-level sub-directory separately
	if splitByDir {
		// Parse each top-level directory separately
		slog.Info("Parsing each top-level directory separately")

		entries, err := os.ReadDir(rootDir)
		if err != nil {
			return err
		}

		var foundDir bool
		// Define concurrency control variables
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		g, gctx := errgroup.WithContext(ctx)
		g.SetLimit(threads) // Limit the number of concurrent goroutines to avoid overwhelming the system
		slog.Info("Using " + fmt.Sprint(threads) + " threads for parsing")

		for _, entry := range entries {
			if entry.IsDir() {
				foundDir = true

				// Start a new goroutine for each subdirectory
				g.Go(func() error {
					// Clone the Task instance so each parsing run has a distinct output but uses the same underlying resources
					newTask := t.Clone()

					// Check for cancellation before doing any work
					select {
					case <-gctx.Done():
						return gctx.Err()
					default:
					}

					// Parse the subdirectory
					subDir := filepath.Join(rootDir, entry.Name())
					if err := parseDir(gctx, newTask, subDir); err != nil {
						return fmt.Errorf("parsing subdirectory %q: %w", subDir, err)
					}
					return nil
				})
			}
		}
		if !foundDir {
			slog.Warn("No subdirectories found in project directory " + rootDir)
			return nil // No files to process, so just return
		}

		// Wait for all the goroutines to finish
		if err := g.Wait(); err != nil {
			return err
		}
	} else {
		// Parse the entire directory as a single unit
		if err := parseDir(context.Background(), t, rootDir); err != nil {
			return err
		}
	}

	// Successfully parsed all directories and files
	fmt.Println()
	slog.Info("Finished running the parser!", "task", t.Name(), "project", rootDir)
	fmt.Println()

	// Clean up resources used by the task
	slog.Debug("Closing task resources")
	t.Close()

	return nil
}

// Iterates over all Go source files in the specified directory and runs the provided task on each file.
// After processing all files, calls the task's ReportResults method to output any accumulated results.
// todo make this multithreaded even without `splitByDir` somehow?
func parseDir(ctx context.Context, task Task, dir string) error {
	// Check for cancellation before starting
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	task.SetProjectDir(dir)

	fmt.Println()
	fmt.Println()
	slog.Info("~~~~~ Parsing directory \"" + dir + "\" ~~~~~")

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
		return fmt.Errorf("loading packages in directory %q: %w", dir, err)
	}
	if len(pkgs) == 0 {
		// todo maybe this should be an error?
		slog.Warn("No packages found in directory " + dir)
		return nil // No packages to process, so just return
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
			// Print every error in the package
			slog.Error("Error in package:", "error", e.Msg, "package", pkg.Name, "position", e.Pos)

			colonIdx := strings.Index(e.Pos, ":")
			if colonIdx > 0 {
				file := e.Pos[:colonIdx]
				errFiles[file] = struct{}{}
			}
		}

		// ========== Iterate over all files in the package ==========
		for _, file := range pkg.Syntax {
			// Check for cancellation before processing each file
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			filePath := fset.Position(file.FileStart).Filename

			// Skip files in `vendor/` directory
			if strings.Contains(filePath, filepath.Join("vendor", "")) {
				slog.Debug("Skipping vendored file", "file", filePath)
				continue
			}

			// Skip files that have errors
			if _, found := errFiles[filePath]; found {
				slog.Info("Skipping file with errors", "file", filePath)
				continue
			}

			// Actually process the file
			// slog.Debug("Processing file", "package", pkg.Name, "file", filePath)
			task.Visit(file, fset, pkg)
		}
	}

	// finished iterating without problem
	slog.Info("Finished parsing all source files in directory", "dir", dir)
	if err := task.ReportResults(); err != nil {
		slog.Error("Error reporting task results", "err", err)
	}
	return nil
}
