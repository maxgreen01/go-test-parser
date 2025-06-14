// Main application entry point
package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/maxgreen01/golang-test-parser/internal/commands"
	"github.com/maxgreen01/golang-test-parser/internal/config"
	"github.com/maxgreen01/golang-test-parser/pkg/parser"

	"github.com/jessevdk/go-flags"
	"github.com/lmittmann/tint"
	"github.com/mattn/go-colorable"
)

// =========== Global command-line flag definitions ===========
type GlobalOptions = config.GlobalOptions

// =========== Parse command-line flags and initialize the application ===========
func main() {
	// Create the flag parser itself
	var opts GlobalOptions
	flagParser := flags.NewParser(&opts, flags.Default|flags.AllowBoolValues)

	// Add commands for each Task
	flagParser.AddCommand("statistics", "Collect statistics about a Go project's tests", "", commands.NewStatisticsCommand(&opts))
	flagParser.AddCommand("analyze", "Analyze a Go projects' tests", "", commands.NewAnalyzeCommand(&opts))

	// Set up a hook to validate and apply global flags before executing any command.
	// Also handles logic for after the command finishes executing using `defer`.
	flagParser.CommandHandler = func(command flags.Commander, args []string) error {
		if command == nil {
			return nil
		}

		// Validate and apply global flags
		applyGlobals(&opts)

		task, ok := command.(parser.Task)
		if !ok {
			slog.Error("Command does not implement the Task interface")
			os.Exit(1)
		}

		// Set up timer hook
		if opts.Timer {
			startTime := time.Now()
			defer func() {
				// Runs after the command finishes executing
				slog.Info("Total execution time:", "duration", time.Since(startTime))
			}()
		}

		// Actually execute the command (which starts the parser)
		if err := command.Execute(args); err != nil {
			slog.Error("Error parsing project", "err", err, "task", task.Name(), "project", opts.ProjectDir)
			os.Exit(1)
		}

		return nil
	}

	// Actually run the flag parser and start the application
	_, err := flagParser.Parse()
	if err != nil {
		// Exit successfully when printing the help menu, but with a failure code otherwise
		if flags.WroteHelp(err) {
			os.Exit(0)
		}
		os.Exit(1)
	}
}

// Validate (in-place) and apply global flags such as logging level and color output
func applyGlobals(opts *GlobalOptions) {
	//
	// =========== Validate flag values ===========
	//

	// Validate the project directory, resolve it to an absolute path, and check that it exists and is a directory
	opts.ProjectDir = strings.Trim(opts.ProjectDir, "\t\n\v\f\r \"") // Trim whitespace and quotes
	if opts.ProjectDir == "" {
		fmt.Printf("You must provide a path to a Go project (e.g., ./myproject)!\n")
		os.Exit(1)
	}
	absPath, err := filepath.Abs(opts.ProjectDir)
	if err != nil {
		fmt.Printf("Error resolving absolute path to Go project %q: %v\n", opts.ProjectDir, err)
		os.Exit(1)
	}
	info, err := os.Stat(absPath)
	if err != nil {
		fmt.Printf("Error accessing project path %q: %v\n", absPath, err)
		os.Exit(1)
	}
	if !info.IsDir() {
		fmt.Printf("Provided project path %q is not a directory!\n", absPath)
		os.Exit(1)
	}
	opts.ProjectDir = absPath

	// Validate log level. Allowed options are handled by the `choice` tag in the struct definition.
	opts.LogLevel = strings.ToLower(strings.TrimSpace(opts.LogLevel))

	// Validate and resolve the output path, if specified. Additional validation and processing is done by FileWriter.
	opts.OutputPath = strings.Trim(opts.OutputPath, "\t\n\v\f\r \"") // Trim whitespace and quotes
	if opts.OutputPath != "" {
		absPath, err := filepath.Abs(opts.OutputPath)
		if err != nil {
			fmt.Printf("Error resolving absolute path for output file %q: %v\n", opts.OutputPath, err)
			os.Exit(1)
		}
		opts.OutputPath = absPath
	}

	// Validate the number of threads used if splitting by directory
	if opts.Threads < 1 {
		fmt.Printf("Invalid number of threads %d specified, must be at least 1\n", opts.Threads)
		os.Exit(1)
	}

	// Map log level string value to a `slog.Level`
	var level slog.Level
	switch opts.LogLevel {
	case "info":
		level = slog.LevelInfo
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		// Should never happen because `LogLevel` options should be validated already
		fmt.Printf("Invalid logLevel %q", opts.LogLevel)
		os.Exit(1)
	}

	//
	// =========== Set up the logger (with tint for colored output) ===========
	//
	slog.SetDefault(slog.New(
		tint.NewHandler(colorable.NewColorableStderr(), &tint.Options{
			Level:      level,
			TimeFormat: time.DateTime,
			ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
				// Write `error` values in red
				if a.Value.Kind() == slog.KindAny {
					if _, ok := a.Value.Any().(error); ok {
						return tint.Attr(9, a)
					}
				}
				return a
			},
		}),
	))
}
